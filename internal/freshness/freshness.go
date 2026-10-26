// Package freshness builds registry-owner publications and retains their complete
// accepted history. It supplies no witness client, scheduler or durable store.
package freshness

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/relux-works/swarma-envelope/record"
	"github.com/relux-works/swarma-envelope/verifier"
	"github.com/relux-works/swarma-owner-registry/internal/journal"
	"github.com/relux-works/swarma-owner-registry/internal/plan"
	"github.com/relux-works/swarma-owner-registry/internal/qtime"
)

// Anchor is exactly the eight-field S4 FreshnessAnchor. Only explicitly
// authorized genesis has PublicationSeq zero and nil publication ID/time.
type Anchor struct {
	RegistryID              string                     `json:"registry_id"`
	Owner                   string                     `json:"owner"`
	PublicationSeq          int64                      `json:"publication_seq"`
	FreshnessID             *string                    `json:"freshness_id"`
	IssuedAt                *string                    `json:"issued_at"`
	Snapshot                string                     `json:"snapshot"`
	RegistrySeq             int64                      `json:"registry_seq"`
	CheckpointID            string                     `json:"checkpoint_id"`
	RegistryCurrencyVersion int                        `json:"registry_currency_version,omitempty"`
	Snapshots               []verifier.SnapshotBinding `json:"snapshots,omitempty"`
}

// MarshalJSON preserves every /1 member and its original ordering. Only /2
// selects the explicit local codec; the verifier Evidence remains its closed shape.
func (a Anchor) MarshalJSON() ([]byte, error) {
	type legacy Anchor
	if a.RegistryCurrencyVersion != 2 {
		return json.Marshal(legacy(a))
	}
	return json.Marshal(struct {
		RegistryID              string                     `json:"registry_id"`
		Owner                   string                     `json:"owner"`
		PublicationSeq          int64                      `json:"publication_seq"`
		FreshnessID             *string                    `json:"freshness_id"`
		IssuedAt                *string                    `json:"issued_at"`
		RegistrySeq             int64                      `json:"registry_seq"`
		CheckpointID            string                     `json:"checkpoint_id"`
		RegistryCurrencyVersion int                        `json:"registry_currency_version"`
		Snapshots               []verifier.SnapshotBinding `json:"snapshots"`
	}{a.RegistryID, a.Owner, a.PublicationSeq, a.FreshnessID, a.IssuedAt, a.RegistrySeq, a.CheckpointID, 2, a.Snapshots})
}

func (a Anchor) clone() Anchor {
	if a.Snapshots != nil {
		rows := make([]verifier.SnapshotBinding, len(a.Snapshots))
		copy(rows, a.Snapshots)
		a.Snapshots = rows
	}
	if a.FreshnessID != nil {
		x := *a.FreshnessID
		a.FreshnessID = &x
	}
	if a.IssuedAt != nil {
		x := *a.IssuedAt
		a.IssuedAt = &x
	}
	return a
}
func (a Anchor) fact() verifier.Evidence {
	var id, at any
	if a.FreshnessID != nil {
		id = *a.FreshnessID
	}
	if a.IssuedAt != nil {
		at = *a.IssuedAt
	}
	m := map[string]any{"registry_id": a.RegistryID, "owner": a.Owner, "publication_seq": a.PublicationSeq, "freshness_id": id, "issued_at": at, "registry_seq": a.RegistrySeq, "checkpoint_id": a.CheckpointID}
	if a.RegistryCurrencyVersion == 2 {
		rows := make([]any, 0, len(a.Snapshots))
		for _, r := range a.Snapshots {
			rows = append(rows, map[string]any{"root_role": r.RootRole, "root_key_id": r.RootKeyID, "snapshot_id": r.SnapshotID})
		}
		m["snapshots"] = rows
	} else {
		m["snapshot"] = a.Snapshot
	}
	e, _ := verifier.Present(m)
	return e
}

// Outcome retains the exact accepted code/plan, not an S5 consumer receipt.
// Receipt composition belongs to the separately qualified consumer adapter.
type Outcome struct {
	Code string        `json:"code"`
	Plan verifier.Plan `json:"plan"`
}

func (r Outcome) clone() Outcome { r.Plan = plan.Clone(r.Plan); return r }

// Publication archives exact accepted bytes and their original evidence/outcome.
// Archive membership records admission, never present or historical authority.
type Publication struct {
	Bytes      []byte           `json:"bytes"`
	Checkpoint []byte           `json:"checkpoint"`
	Snapshot   []byte           `json:"snapshot"`
	Witness    []byte           `json:"witness"`
	ReadAt     string           `json:"read_at"`
	Anchor     Anchor           `json:"anchor"`
	Outcome    Outcome          `json:"outcome"`
	Time       qtime.Interval   `json:"time"`
	Projection qtime.Projection `json:"projection"`
	Currency   *CurrencyProof   `json:"currency,omitempty"`
}

func (p Publication) clone() Publication {
	p.Bytes = bytes.Clone(p.Bytes)
	p.Checkpoint = bytes.Clone(p.Checkpoint)
	p.Snapshot = bytes.Clone(p.Snapshot)
	p.Witness = bytes.Clone(p.Witness)
	p.Currency = p.Currency.clone()
	p.Anchor = p.Anchor.clone()
	p.Outcome = p.Outcome.clone()
	return p
}

// State retains all publications, including prior heads, without truncation.
// Its authenticated genesis and watermark are independent of a local counter.
type State struct {
	Version       int            `json:"version,omitempty"`
	PinIdentity   string         `json:"pin_identity,omitempty"`
	Currency      *CurrencyProof `json:"currency,omitempty"`
	Genesis       Anchor         `json:"genesis"`
	Anchor        Anchor         `json:"anchor"`
	History       []Publication  `json:"history"`
	Watermark     string         `json:"watermark"`
	DecisionFloor string         `json:"decision_floor"`
	Boot          string         `json:"boot"`
}

func (s State) Clone() State {
	s.Currency = s.Currency.clone()
	s.Genesis = s.Genesis.clone()
	s.Anchor = s.Anchor.clone()
	if s.History != nil {
		history := make([]Publication, len(s.History))
		for i, p := range s.History {
			history[i] = p.clone()
		}
		s.History = history
	}
	return s
}

// StateID is a local inventory digest, not a witness signature or reconciliation
// proof. Recovery.StateID must come from independent qualified retained evidence.
func StateID(s State) (string, error) {
	raw, e := json.Marshal(s)
	if e != nil {
		return "", e
	}
	prefix := "freshness-state-v1\n"
	if s.Version == 2 {
		prefix = "freshness-state-v2\n"
	} else if s.Version != 0 {
		return "", unavailable()
	}
	return record.Digest(append([]byte(prefix), raw...)), nil
}

// Recovery is supplied only after an independently qualified complete-history
// comparison to the published floor, real-head reconciliation, time continuity
// and old-instance exclusion. Local restored bytes cannot supply this evidence.
// GenesisAuthorized is separate explicit authorization, not missing-state repair.
type Recovery struct {
	StateID                       string
	Reconciled, GenesisAuthorized bool
}

// Update binds a complete retained outcome/history to its exact predecessor.
type Update struct {
	Before string
	After  State
}

func (u Update) Clone() Update { u.After = u.After.Clone(); return u }

// Persistence must atomically compare Before, durably retain all exact history,
// publication/base/checkpoint/witness/read/outcome/watermark and candidate pointer,
// and acknowledge the exact StateID(After) after checked sync. This includes
// retention/budget preflight and the shared real-publication exclusion boundary.
// Errors or an uncertain/misbound acknowledgement fence; no implementation ships
// here. Implementing this interface alone does not qualify crash durability.
type Persistence interface{ Commit(Update) (string, error) }
type Trust struct {
	Registry, Owner string
	Signer          journal.Signer
	Retained        State
	Recovery        Recovery
	Persistence     Persistence
	// CurrencyRoots is independently protected configuration, not recovered policy.
	CurrencyRoots map[string]string
}

// WitnessRead contains already authenticated original evidence and the exact
// protected read timestamp/fact. The later witness client must bind all three,
// exclude unresolved intent and revalidate the resolved head under serialization.
// This package authenticates no witness transport or challenge.
type WitnessRead struct {
	Fact     verifier.Evidence
	ReadAt   string
	Original []byte
}

// Input supplies one caller-qualified interval and current resolved evidence.
// Context's point time and watermark are replaced by the interval projection
// and independently retained state, never by the provider's consumed floor.
type Input struct {
	Context    verifier.Context
	Time       qtime.Interval
	Checkpoint []byte
	Snapshot   journal.SnapshotFacts
	Witness    WitnessRead
	IDs, Keys  []string
	Currency   *CurrencyProof
}

// Publisher serializes local calls. External serialization/exclusion remains a
// Persistence/witness integration obligation. A durability failure fences all
// later calls; refusal plans and uncommitted candidate bytes never escape.
type Publisher struct {
	mu              sync.Mutex
	registry, owner string
	signer          journal.Signer
	store           Persistence
	state           State
	stateID         string
	fenced          bool
	currencyRoots   map[string]string
}
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }
func unavailable() error       { return &Error{Code: "freshness_state_unavailable"} }
func absent(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}
func timestamp(s string) bool {
	v, e := time.Parse("2006-01-02T15:04:05Z", s)
	return e == nil && v.Year() >= 1970 && v.Year() <= 9999 && v.Format("2006-01-02T15:04:05Z") == s
}
func validAnchor(a Anchor) bool {
	if a.RegistryID == "" || a.Owner == "" || len(a.Owner) > 255 || !utf8.ValidString(a.Owner) || a.PublicationSeq < 0 || a.PublicationSeq > journal.MaxSequence || a.RegistrySeq < 1 || a.RegistrySeq > journal.MaxSequence || !validSnapshotAnchor(a) || !record.IsContentID(a.CheckpointID) {
		return false
	}
	if a.PublicationSeq == 0 {
		return a.FreshnessID == nil && a.IssuedAt == nil
	}
	return a.FreshnessID != nil && record.IsContentID(*a.FreshnessID) && a.IssuedAt != nil && timestamp(*a.IssuedAt)
}

// New admits independently reconciled state, never an implicit bootstrap. It
// checks local archive completeness, not historical signature/currentness again;
// the externally qualified Recovery contract is mandatory even for intact bytes.
func New(t Trust) (*Publisher, error) {
	s := t.Retained
	if s.Version == 2 {
		id, e := currencyPinIdentity(t.Registry, t.Owner, t.CurrencyRoots)
		if e != nil || id != s.PinIdentity || s.Currency == nil {
			return nil, unavailable()
		}
		if _, ok := t.Persistence.(CurrencyPersistence); !ok {
			return nil, unavailable()
		}
		if _, _, e := s.Currency.evidence(); e != nil {
			return nil, unavailable()
		}
		current, e := parse(s.Currency.Originals[s.Anchor.CheckpointID])
		if e != nil || current["id"] != s.Anchor.CheckpointID {
			return nil, unavailable()
		}
		if !currencyIndexPins(s.Currency.Anchor, t.Registry, t.Owner, t.CurrencyRoots) {
			return nil, unavailable()
		}
		encoded, e := json.Marshal(s)
		if e != nil || len(encoded) > MaxCurrencyStateBytes {
			return nil, unavailable()
		}
	} else if s.Version != 0 || s.Currency != nil || s.PinIdentity != "" || t.CurrencyRoots != nil || s.Anchor.RegistryCurrencyVersion != 0 {
		return nil, unavailable()
	}
	if absent(t.Signer) || absent(t.Persistence) || record.ValidateKey(t.Registry) != nil || t.Owner == "" || !t.Recovery.Reconciled || !validAnchor(s.Genesis) || !validAnchor(s.Anchor) || s.Genesis.PublicationSeq != 0 || s.Anchor.RegistryID != t.Registry || s.Genesis.RegistryID != t.Registry || s.Anchor.Owner != t.Owner || s.Genesis.Owner != t.Owner || !timestamp(s.Watermark) || !qtime.ValidFloor(s.DecisionFloor) || s.Boot == "" || int64(len(s.History)) != s.Anchor.PublicationSeq {
		return nil, unavailable()
	}
	if s.Anchor.PublicationSeq == 0 && (!t.Recovery.GenesisAuthorized || !reflect.DeepEqual(s.Genesis, s.Anchor)) {
		return nil, unavailable()
	}
	for i, p := range s.History {
		clock, e := p.Time.Project(p.Projection.Watermark, p.Time.JournalFloor)
		if e != nil || clock != p.Projection || p.Outcome.Plan.Watermark != clock.Now || s.Watermark < clock.Watermark || !qtime.FloorAtLeast(s.DecisionFloor, clock.DecisionFloor) {
			return nil, unavailable()
		}
		if p.Anchor.PublicationSeq != int64(i)+1 || !validAnchor(p.Anchor) || p.Anchor.RegistryID != t.Registry || p.Anchor.Owner != t.Owner || len(p.Bytes) == 0 || len(p.Bytes) > journal.MaxRecordBytes || len(p.Checkpoint) == 0 || len(p.Checkpoint) > journal.MaxRecordBytes || (len(p.Snapshot) == 0 && p.Currency == nil) || len(p.Snapshot) > journal.MaxRecordBytes || len(p.Witness) == 0 || !timestamp(p.ReadAt) || p.Outcome.Code != "accept" || p.Outcome.Plan.Reanchor != nil {
			return nil, unavailable()
		}
	}
	if len(s.History) > 0 {
		last := s.History[len(s.History)-1]
		if !reflect.DeepEqual(last.Anchor, s.Anchor) {
			return nil, unavailable()
		}
	}
	id, e := StateID(s)
	if e != nil || id != t.Recovery.StateID {
		return nil, unavailable()
	}
	roots := map[string]string{}
	for role, k := range t.CurrencyRoots {
		roots[role] = k
	}
	return &Publisher{currencyRoots: roots, registry: t.Registry, owner: t.Owner, signer: t.Signer, store: t.Persistence, state: s.Clone(), stateID: id}, nil
}
func (p *Publisher) Fenced() bool {
	if p == nil {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fenced
}

// LastResolved is copied recovery evidence. After uncertain persistence it does
// not prove durable storage is unchanged, nor authorize serving the old position.
func (p *Publisher) LastResolved() State {
	if p == nil {
		return State{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state.Clone()
}

// Verify forwards only normal pinned freshness admission. It never falls back
// to re-anchor or changes a refusal code/zero plan.
func Verify(c verifier.Context, chain [][]byte, checkpoint []byte, ids, keys []string) verifier.Result {
	return verifier.New(c).VerifyFreshness(chain, checkpoint, ids, keys)
}
func no(code string) ([]byte, Outcome) { return nil, Outcome{Code: code} }
func obj(v any) map[string]any         { m, _ := v.(map[string]any); return m }
func text(v any) string                { s, _ := v.(string); return s }
func number(v any) int64               { n, _ := v.(int64); return n }
func parse(raw []byte) (map[string]any, error) {
	v, e := record.Parse(raw, journal.MaxRecordBytes, 16)
	if e != nil {
		return nil, e
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("not an object")
	}
	return m, nil
}

// Publish derives global seq/prev, signs via the existing closed-profile signer,
// and self-checks against the exact retained anchor. Accepted bytes/code/plan are
// exposed only after exact persistence acknowledgement. Exact retransmission is
// rechecked for currentness but returns its original outcome without renewal.
func (p *Publisher) Publish(in Input) ([]byte, Outcome) { return p.publish(in, false) }

// Retry rechecks the last exact publication under a new interval without issuing
// another publication. Success preserves its original outcome and durably retains
// the new decision floor; refusal never renews or exposes that outcome.
func (p *Publisher) Retry(in Input) ([]byte, Outcome) { return p.publish(in, true) }

func advances(at string, previous *string) bool { return previous == nil || at > *previous }

func (p *Publisher) publish(in Input, retryOnly bool) ([]byte, Outcome) {
	if p == nil {
		return no("freshness_state_unavailable")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fenced {
		return no("freshness_state_unavailable")
	}
	if p.state.Version == 2 {
		return p.publishCurrency(in, retryOnly)
	}
	if in.Currency != nil || in.Context.RegistryCurrencyVersion == 2 {
		return no("registry_currency_version_mismatch")
	}
	if len(in.Snapshot.Bytes) > journal.MaxRecordBytes || len(in.Witness.Original) > journal.MaxRecordBytes {
		return no("freshness_malformed")
	}
	if len(in.Witness.Original) == 0 {
		return no("freshness_unavailable")
	}
	retry := false
	identical := false
	floor := p.state.DecisionFloor
	if len(p.state.History) > 0 {
		last := p.state.History[len(p.state.History)-1]
		identical = in.Time == last.Time && bytes.Equal(in.Checkpoint, last.Checkpoint) && bytes.Equal(in.Snapshot.Bytes, last.Snapshot)
		retry = retryOnly || identical
		// Exact retransmission revalidates the original interval against its original
		// decision floor. It creates no new time decision or renewed outcome.
		if identical {
			floor = in.Time.JournalFloor
		}
	} else if retryOnly {
		return no("freshness_unavailable")
	}
	clock, e := in.Time.Project(p.state.Watermark, floor)
	if e != nil {
		return no("time_unavailable")
	}
	if !retry && !advances(clock.IssuedAt, p.state.Anchor.IssuedAt) {
		return no("freshness_time_invalid")
	}
	checkpoint, e := parse(in.Checkpoint)
	if e != nil {
		return no("freshness_malformed")
	}
	cb := obj(checkpoint["body"])
	c := in.Context
	c.FreshnessAnchor = p.state.Anchor.fact()
	c.Now, _ = verifier.Present(clock.Now)
	c.Time, _ = verifier.Present(map[string]any{"trusted": true, "fresh": true, "authority_until": clock.AuthorityUntil})
	c.Watermark, _ = verifier.Present(clock.Watermark)
	c.WatermarkAvailable, _ = verifier.Present(true)
	c.JournalWitness = in.Witness.Fact
	c.WitnessReadAt, _ = verifier.Present(in.Witness.ReadAt)
	availability := "unavailable"
	if in.Snapshot.Available {
		availability = "available"
	}
	c.SignedSnapshots, e = verifier.Present(map[string]any{p.registry: map[string]any{"snapshot": in.Snapshot.Bytes, "availability": availability, "max_age": in.Snapshot.MaxAge, "rollback_safe": in.Snapshot.RollbackSafe}})
	if e != nil {
		return no("freshness_malformed")
	}
	var raw, encoded []byte
	if retry {
		raw = bytes.Clone(p.state.History[len(p.state.History)-1].Bytes)
	}
	if !retry {
		if p.state.Anchor.PublicationSeq == journal.MaxSequence {
			return no("freshness_state_invalid")
		}
		seq := p.state.Anchor.PublicationSeq + 1
		var prev any
		if number(cb["seq"]) == p.state.Anchor.RegistrySeq && p.state.Anchor.FreshnessID != nil {
			prev = *p.state.Anchor.FreshnessID
		}
		body := map[string]any{"type": "freshness/1", "registry": p.registry, "owner": p.owner, "publication_seq": seq, "prev": prev, "issued_at": clock.IssuedAt, "snapshot": cb["snapshot"], "registry_seq": cb["seq"], "checkpoint_id": checkpoint["id"]}
		encoded, e = record.Canonical(body)
		if e != nil {
			return no("freshness_malformed")
		}
		raw, e = p.signer.Sign(encoded)
		if e != nil {
			var typed *journal.Error
			if errors.As(e, &typed) && typed != nil {
				// Rollback preserves BuildCheckpoint's retained-time failure in the shared vocabulary.
				switch typed.Code {
				case journal.Malformed, journal.ProfileRefused, journal.SignerUnavailable, journal.SignerInvalid, journal.Rollback:
					return no(string(typed.Code))
				}
			}
			return no(string(journal.SignerUnavailable))
		}
	}
	r := Verify(c, [][]byte{raw}, in.Checkpoint, in.IDs, in.Keys)
	if r.Code != "accept" {
		return nil, Outcome{Code: r.Code, Plan: r.Plan}
	}
	if retry {
		if !identical {
			next := p.state.Clone()
			next.Watermark, next.DecisionFloor, next.Boot = clock.Watermark, clock.DecisionFloor, clock.Boot
			if !p.commit(next) {
				return no("freshness_persistence_unavailable")
			}
		}
		return bytes.Clone(raw), p.state.History[len(p.state.History)-1].Outcome.clone()
	}
	env, e := parse(raw)
	if e != nil {
		return no("freshness_malformed")
	}
	// Preserve full verifier refusal order, then require the signer to have
	// returned exactly the requested body rather than another valid publication.
	signed, e := record.Canonical(env["body"])
	if e != nil || !bytes.Equal(encoded, signed) {
		return no(string(journal.SignerInvalid))
	}
	b := obj(env["body"])
	id, issued := text(env["id"]), text(b["issued_at"])
	a := Anchor{RegistryID: p.registry, Owner: p.owner, PublicationSeq: number(b["publication_seq"]), FreshnessID: &id, IssuedAt: &issued, Snapshot: text(b["snapshot"]), RegistrySeq: number(b["registry_seq"]), CheckpointID: text(b["checkpoint_id"])}
	outcome := Outcome{Code: r.Code, Plan: r.Plan}.clone()
	next := p.state.Clone()
	next.Anchor = a
	next.Watermark, next.DecisionFloor, next.Boot = clock.Watermark, clock.DecisionFloor, clock.Boot
	next.History = append(next.History, Publication{bytes.Clone(raw), bytes.Clone(in.Checkpoint), bytes.Clone(in.Snapshot.Bytes), bytes.Clone(in.Witness.Original), in.Witness.ReadAt, a.clone(), outcome.clone(), in.Time, clock, nil})
	if !p.commit(next) {
		return no("freshness_persistence_unavailable")
	}

	return bytes.Clone(raw), outcome
}

func (p *Publisher) commit(next State) bool {
	if next.Version == 2 {
		encoded, e := json.Marshal(next)
		if e != nil || len(encoded) > MaxCurrencyStateBytes {
			p.fenced = true
			return false
		}
	}
	nextID, e := StateID(next)
	if e != nil {
		p.fenced = true
		return false
	}
	ack, e := p.store.Commit(Update{p.stateID, next.Clone()})
	if e != nil || ack != nextID {
		p.fenced = true
		return false
	}
	p.state, p.stateID = next, nextID
	return true
}
