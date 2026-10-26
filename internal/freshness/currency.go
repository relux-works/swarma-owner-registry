package freshness

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/relux-works/swarma-owner-registry/internal/journal"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

const MaxCurrencyStateBytes = 16777216

// CurrencyPersistence extends the same atomic CAS boundary with a pre-signing
// capacity check. The upper bound includes the existing archive, new proof and
// bounded publication/plan. Implementations must reserve smaller local quotas
// too; staging plus committed bytes may not exceed 33554432. Neither method
// qualifies storage or grants authority. Recovery must independently enforce
// version/pin floors and exclude old instances before constructing a Publisher.
type CurrencyPersistence interface {
	Persistence
	Preflight(before string, maximumAfterBytes int) error
}

// CurrencyProof archives the complete qualified prefix anchor, authoritative
// suffix, exact originals and whole snapshot premise. Anchor is bounded CCJ-1
// JSON, allowing explicit null genesis endpoints; it is never inferred from
// empty strings or a partial /1 index. Qualification belongs to the existing
// Recovery boundary. Each new suffix is authenticated by the pinned verifier.
type CurrencyProof struct {
	Anchor    json.RawMessage                  `json:"anchor"`
	Entries   [][]byte                         `json:"entries"`
	Originals map[string][]byte                `json:"originals"`
	Snapshots map[string]journal.SnapshotFacts `json:"snapshots"`
}

func (p *CurrencyProof) clone() *CurrencyProof {
	if p == nil {
		return nil
	}
	out := *p
	out.Anchor = bytes.Clone(p.Anchor)
	if p.Entries != nil {
		out.Entries = make([][]byte, len(p.Entries))
		for i, b := range p.Entries {
			out.Entries[i] = bytes.Clone(b)
		}
	}
	if p.Originals != nil {
		out.Originals = map[string][]byte{}
		for id, b := range p.Originals {
			out.Originals[id] = bytes.Clone(b)
		}
	}
	if p.Snapshots != nil {
		out.Snapshots = map[string]journal.SnapshotFacts{}
		for k, s := range p.Snapshots {
			s.Bytes = bytes.Clone(s.Bytes)
			out.Snapshots[k] = s
		}
	}
	return &out
}

// Preflight bounds raw supplied artifacts before copying or canonicalization.
func (p *CurrencyProof) preflight() error {
	if p == nil || len(p.Anchor) > 1<<20 || len(p.Entries) > 64 {
		return unavailable()
	}
	total := len(p.Anchor)
	charge := func(k string, b []byte) bool { total += len(k) + len(b); return len(b) <= 8192 && total <= 2097152 }
	for _, b := range p.Entries {
		if !charge("", b) {
			return unavailable()
		}
	}
	for id, b := range p.Originals {
		if !charge(id, b) {
			return unavailable()
		}
	}
	for k, s := range p.Snapshots {
		if !charge(k, s.Bytes) {
			return unavailable()
		}
	}
	return nil
}
func (p *CurrencyProof) evidence() (verifier.Evidence, verifier.Evidence, error) {
	if p.preflight() != nil {
		return verifier.Evidence{}, verifier.Evidence{}, unavailable()
	}
	anchor, e := record.Parse(p.Anchor, 1<<20, 16)
	if e != nil {
		return verifier.Evidence{}, verifier.Evidence{}, e
	}
	entries := make([]any, len(p.Entries))
	for i, b := range p.Entries {
		entries[i] = b
	}
	j, e := verifier.Present(map[string]any{"anchor": anchor, "entries": entries})
	if e != nil {
		return verifier.Evidence{}, verifier.Evidence{}, e
	}
	snapshots := map[string]any{}
	for k, s := range p.Snapshots {
		availability := "unavailable"
		if s.Available {
			availability = "available"
		}
		snapshots[k] = map[string]any{"snapshot": s.Bytes, "availability": availability, "rollback_safe": s.RollbackSafe, "max_age": s.MaxAge}
	}
	s, e := verifier.Present(snapshots)
	return j, s, e
}

func currencyPinIdentity(registry, owner string, roots map[string]string) (string, error) {
	if len(roots) < 1 || len(roots) > 3 || roots["registry-owner"] != registry {
		return "", unavailable()
	}
	m := map[string]any{}
	seen := map[string]bool{}
	for role, k := range roots {
		if (role != "grant" && role != "operator" && role != "registry-owner") || record.ValidateKey(k) != nil || seen[k] {
			return "", unavailable()
		}
		seen[k] = true
		m[role] = k
	}
	b, e := record.Canonical(map[string]any{"version": int64(2), "registry": registry, "owner": owner, "roots": m})
	if e != nil {
		return "", e
	}
	return record.Digest(b), nil
}

// CurrencyPinIdentity binds independently supplied policy into a versioned local
// inventory. It is not a restore-proof floor or a signature by an authority.
func CurrencyPinIdentity(registry, owner string, roots map[string]string) (string, error) {
	return currencyPinIdentity(registry, owner, roots)
}
func validSnapshotAnchor(a Anchor) bool {
	if a.RegistryCurrencyVersion == 0 {
		return record.IsContentID(a.Snapshot) && a.Snapshots == nil
	}
	if a.RegistryCurrencyVersion != 2 || a.Snapshot != "" || len(a.Snapshots) < 1 || len(a.Snapshots) > 3 {
		return false
	}
	previous := ""
	seen := map[string]bool{}
	registry := false
	for _, r := range a.Snapshots {
		order := r.RootRole + "\x00" + r.RootKeyID
		if (r.RootRole != "grant" && r.RootRole != "operator" && r.RootRole != "registry-owner") || record.ValidateKey(r.RootKeyID) != nil || !record.IsContentID(r.SnapshotID) || order <= previous || seen[r.RootRole] {
			return false
		}
		previous = order
		seen[r.RootRole] = true
		registry = registry || r.RootRole == "registry-owner" && r.RootKeyID == a.RegistryID
	}
	return registry
}

// publishCurrency runs under Publisher.mu, preserving the legacy serialization
// and fencing boundary. It repeats the checkpoint's exact coverage on idle
// publications; journal positions are supplied, never synthesized or skipped.
func (p *Publisher) publishCurrency(in Input, retryOnly bool) ([]byte, Outcome) {
	if len(in.Checkpoint) > journal.MaxRecordBytes || len(in.Witness.Original) > journal.MaxRecordBytes {
		return no("freshness_malformed")
	}
	if in.Context.RegistryCurrencyVersion != 2 {
		return no("registry_currency_version_mismatch")
	}
	if in.Currency == nil || len(in.Witness.Original) == 0 {
		return no("freshness_unavailable")
	}
	if e := in.Currency.preflight(); e != nil {
		return no("freshness_malformed")
	}
	cp, e := parse(in.Checkpoint)
	if e != nil {
		return no("freshness_malformed")
	}
	cb := obj(cp["body"])
	if cb["type"] != "checkpoint/2" {
		return no("registry_currency_version_mismatch")
	}
	proof := in.Currency.clone()
	if proof.Originals == nil {
		proof.Originals = map[string][]byte{}
	}
	checkpointID := text(cp["id"])
	if old, exists := proof.Originals[checkpointID]; exists && !bytes.Equal(old, in.Checkpoint) {
		return no("freshness_state_invalid")
	}
	proof.Originals[checkpointID] = bytes.Clone(in.Checkpoint)
	j, s, e := proof.evidence()
	if e != nil {
		return no("freshness_malformed")
	}
	originals := map[string]any{}
	for id, b := range proof.Originals {
		originals[id] = b
	}
	idJournal, e := verifier.Present(originals)
	if e != nil {
		return no("freshness_malformed")
	}
	identical := false
	if len(p.state.History) > 0 {
		last := p.state.History[len(p.state.History)-1]
		identical = in.Time == last.Time && bytes.Equal(in.Checkpoint, last.Checkpoint) && reflect.DeepEqual(proof, last.Currency)
	}
	floor := p.state.DecisionFloor
	if identical {
		floor = in.Time.JournalFloor
	}
	clock, e := in.Time.Project(p.state.Watermark, floor)
	if e != nil {
		return no("time_unavailable")
	}

	c := in.Context
	c.GrantRoot = verifier.Evidence{}
	c.OperatorRoot = verifier.Evidence{}
	if k := p.currencyRoots["grant"]; k != "" {
		c.GrantRoot, _ = verifier.Present(k)
	}
	if k := p.currencyRoots["operator"]; k != "" {
		c.OperatorRoot, _ = verifier.Present(k)
	}
	c.FreshnessAnchor = p.state.Anchor.fact()
	c.SnapshotJournal = j
	c.SignedSnapshots = s
	c.IDJournal = idJournal
	c.IDJournalAvailable, _ = verifier.Present(true)
	c.Now, _ = verifier.Present(clock.Now)
	c.Time, _ = verifier.Present(map[string]any{"trusted": true, "fresh": true, "authority_until": clock.AuthorityUntil})
	c.Watermark, _ = verifier.Present(clock.Watermark)
	c.WatermarkAvailable, _ = verifier.Present(true)
	c.JournalWitness = in.Witness.Fact
	c.WitnessReadAt, _ = verifier.Present(in.Witness.ReadAt)
	retry := retryOnly
	if len(p.state.History) > 0 {
		last := p.state.History[len(p.state.History)-1]
		retry = retry || (in.Time == last.Time && bytes.Equal(in.Checkpoint, last.Checkpoint) && reflect.DeepEqual(proof, last.Currency))
	}
	if retry {
		if len(p.state.History) == 0 {
			return no("freshness_unavailable")
		}
		last := p.state.History[len(p.state.History)-1]
		// A retry cannot replace the original byte/premise inventory or its outcome.
		if !bytes.Equal(in.Checkpoint, last.Checkpoint) || !reflect.DeepEqual(proof, last.Currency) {
			return no("freshness_binding_mismatch")
		}
		r := Verify(c, [][]byte{last.Bytes}, in.Checkpoint, in.IDs, in.Keys)
		if r.Code != "accept" {
			return nil, Outcome{Code: r.Code}
		}
		if identical {
			return bytes.Clone(last.Bytes), last.Outcome.clone()
		}
		next := p.state.Clone()
		next.Watermark, next.DecisionFloor, next.Boot = clock.Watermark, clock.DecisionFloor, clock.Boot
		serialized, _ := json.Marshal(next)
		if len(serialized) > MaxCurrencyStateBytes || p.store.(CurrencyPersistence).Preflight(p.stateID, len(serialized)) != nil {
			return no("freshness_capacity_unavailable")
		}
		if !p.commit(next) {
			return no("freshness_persistence_unavailable")
		}
		return bytes.Clone(last.Bytes), last.Outcome.clone()
	}
	if !advances(clock.IssuedAt, p.state.Anchor.IssuedAt) {
		return no("freshness_time_invalid")
	}
	if p.state.Anchor.PublicationSeq == journal.MaxSequence {
		return no("freshness_state_invalid")
	}
	var prev any
	if number(cb["seq"]) == p.state.Anchor.RegistrySeq && p.state.Anchor.FreshnessID != nil {
		prev = *p.state.Anchor.FreshnessID
	}
	body := map[string]any{"type": "freshness/2", "registry": p.registry, "owner": p.owner, "publication_seq": p.state.Anchor.PublicationSeq + 1, "prev": prev, "issued_at": clock.IssuedAt, "snapshots": cb["snapshots"], "registry_seq": cb["seq"], "checkpoint_id": cp["id"]}
	encoded, e := record.Canonical(body)
	if e != nil {
		return no("freshness_malformed")
	}
	id, e := record.ContentID("freshness/2", body)
	if e != nil {
		return no("freshness_malformed")
	}
	placeholder, e := record.Canonical(map[string]any{"version": "swarm-envelope/2", "body": body, "id": id, "signatures": []any{map[string]any{"role": "author", "key": p.registry, "sig": base64.RawURLEncoding.EncodeToString(make([]byte, 64))}}})
	if e != nil {
		return no("freshness_malformed")
	}
	// S8 charges the whole call before signature verification. This invalid
	// signature probe is capacity-only and can never supply an admitted plan.
	bound := Verify(c, [][]byte{placeholder}, in.Checkpoint, in.IDs, in.Keys)
	if bound.Code == "freshness_malformed" {
		return no(bound.Code)
	}
	before, _ := json.Marshal(p.state)
	metadata, _ := json.Marshal(struct {
		Time       any
		Projection any
	}{in.Time, clock})
	extra, _ := json.Marshal(proof)
	// <=64 publications and journal entries, three roots and deduplicated original
	// references bound the new plan below this reserve. Raw envelopes are <=8192;
	// their base64 JSON and metadata fit here too. Keep the full old archive.
	maximum := len(before) + 2*len(extra) + len(in.Checkpoint)*2 + len(in.Witness.Original)*2 + 2*len(metadata) + 131072
	if maximum > MaxCurrencyStateBytes {
		return no("freshness_capacity_unavailable")
	}
	if e = p.store.(CurrencyPersistence).Preflight(p.stateID, maximum); e != nil {
		return no("freshness_capacity_unavailable")
	}
	raw, e := p.signer.Sign(bytes.Clone(encoded))
	if e != nil {
		var typed *journal.Error
		if errors.As(e, &typed) && typed != nil {
			switch typed.Code {
			case journal.Malformed, journal.ProfileRefused, journal.SignerUnavailable, journal.SignerInvalid, journal.Rollback:
				return no(string(typed.Code))
			}
		}
		return no(string(journal.SignerUnavailable))
	}
	r := Verify(c, [][]byte{raw}, in.Checkpoint, in.IDs, in.Keys)
	if r.Code != "accept" {
		return nil, Outcome{Code: r.Code}
	}
	env, e := parse(raw)
	if e != nil {
		return no("freshness_malformed")
	}
	signed, e := record.Canonical(env["body"])
	if e != nil || !bytes.Equal(encoded, signed) {
		return no(string(journal.SignerInvalid))
	}
	var rows []verifier.SnapshotBinding
	rowJSON, _ := json.Marshal(body["snapshots"])
	if json.Unmarshal(rowJSON, &rows) != nil {
		return no("freshness_malformed")
	}
	issued := clock.IssuedAt
	a := Anchor{RegistryID: p.registry, Owner: p.owner, PublicationSeq: p.state.Anchor.PublicationSeq + 1, FreshnessID: &id, IssuedAt: &issued, RegistrySeq: number(cb["seq"]), CheckpointID: text(cp["id"]), RegistryCurrencyVersion: 2, Snapshots: rows}
	out := Outcome{Code: r.Code, Plan: r.Plan}.clone()
	next := p.state.Clone()
	next.Anchor = a
	next.Currency = proof.clone()
	next.Watermark, next.DecisionFloor, next.Boot = clock.Watermark, clock.DecisionFloor, clock.Boot
	next.History = append(next.History, Publication{Bytes: bytes.Clone(raw), Checkpoint: bytes.Clone(in.Checkpoint), Witness: bytes.Clone(in.Witness.Original), ReadAt: in.Witness.ReadAt, Anchor: a.clone(), Outcome: out.clone(), Time: in.Time, Projection: clock, Currency: proof.clone()})
	serialized, e := json.Marshal(next)
	if e != nil || len(serialized) > maximum {
		p.fenced = true
		return no("freshness_capacity_unavailable")
	}
	if !p.commit(next) {
		return no("freshness_persistence_unavailable")
	}
	return bytes.Clone(raw), out
}

// Index rows cover exactly the independently protected roles, including omitted
// snapshots. This shape check cannot authenticate or provision a prior prefix.
func currencyIndexPins(raw []byte, registry, owner string, roots map[string]string) bool {
	v, e := record.Parse(raw, 1<<20, 16)
	if e != nil {
		return false
	}
	m := obj(v)
	rows, ok := m["latest"].([]any)
	if !ok || m["version"] != int64(2) || m["registry_id"] != registry || m["owner"] != owner || len(rows) != len(roots) {
		return false
	}
	seen := map[string]bool{}
	for _, v := range rows {
		r := obj(v)
		role := text(r["root_role"])
		if roots[role] == "" || roots[role] != r["root_key_id"] || seen[role] {
			return false
		}
		seen[role] = true
	}
	return true
}
