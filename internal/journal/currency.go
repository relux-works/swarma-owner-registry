package journal

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

// NewCurrencySigner extends the isolated owner signer with explicit protected
// role/key pins. It signs content only for the registry; external roots retain
// their own signing authority. No custody transport or deployment is supplied.
func NewCurrencySigner(registry, owner string, roots map[string]string, service SignatureService) (Signer, error) {
	if nilInterface(service) {
		return nil, fail(SignerUnavailable)
	}
	legacy, e := NewSigner(registry, owner, service)
	if e != nil {
		return nil, e
	}
	if len(roots) < 1 || len(roots) > 3 || roots["registry-owner"] != registry {
		return nil, fail(ProfileRefused)
	}
	seen := map[string]bool{}
	copyRoots := map[string]string{}
	for role, k := range roots {
		if (role != "registry-owner" && role != "operator" && role != "grant") || record.ValidateKey(k) != nil || seen[k] {
			return nil, fail(ProfileRefused)
		}
		seen[k] = true
		copyRoots[role] = k
	}
	s := legacy.(*ownerSigner)
	s.currencyRoots = copyRoots
	return s, nil
}
func currencyBody(b map[string]any, roots map[string]string) bool {
	kind := text(b["type"])
	if kind == "checkpoint/2" {
		if !shape(b, "type registry owner seq head at snapshots") || !common(b) || !hash(b["head"]) {
			return false
		}
	} else if kind == "freshness/2" {
		if !shape(b, "type registry owner publication_seq prev issued_at snapshots registry_seq checkpoint_id") || !sequence(b["publication_seq"]) || !sequence(b["registry_seq"]) || !timestamp(b["issued_at"]) || !hash(b["checkpoint_id"]) || (number(b["publication_seq"]) == 1 && b["prev"] != nil) || (b["prev"] != nil && !hash(b["prev"])) {
			return false
		}
	} else {
		return false
	}
	rows, ok := b["snapshots"].([]any)
	if !ok || len(rows) < 1 || len(rows) > 3 {
		return false
	}
	previous := ""
	seen := map[string]bool{}
	registry := false
	for _, v := range rows {
		r := object(v)
		role := text(r["root_role"])
		k := text(r["root_key_id"])
		order := role + "\x00" + k
		if !shape(r, "root_role root_key_id snapshot_id") || roots[role] != k || k == "" || seen[role] || order <= previous || !hash(r["snapshot_id"]) {
			return false
		}
		previous = order
		seen[role] = true
		registry = registry || role == "registry-owner"
	}
	return registry
}

// CurrencyPublication preserves the complete, original caller-qualified
// Context premises. SnapshotJournal supplies the qualified prefix; Entries are
// authoritative for this J call. IDJournal must retain exact originals and all
// predecessor checkpoints. The result remains a candidate, never a commit.
type CurrencyPublication struct {
	Entries                       [][]byte
	CheckpointBody, JournalAnchor []byte
	Originals                     map[string][]byte
	// Capacity checks the actual full archive and smaller carrier/store quotas
	// under the caller's existing publication exclusion, before custody is called.
	// The argument bounds builder-owned serialized bytes; also charge the whole
	// protected premise and caller metadata. It is not a storage acknowledgement.
	Capacity func(maximumNewBytes int) error
}

func BuildCurrencyPublication(c verifier.Context, s Signer, p CurrencyPublication) (Prepared, verifier.Result) {
	refuse := func(code string) (Prepared, verifier.Result) { return Prepared{}, result(code) }
	if len(p.Entries) > MaxEntries {
		return refuse(string(Malformed))
	}
	for _, b := range p.Entries {
		if len(b) > MaxRecordBytes {
			return refuse(string(Malformed))
		}
	}

	anchor, e := parse(p.JournalAnchor)
	if e != nil {
		return refuse(string(Malformed))
	}
	entries := make([]any, len(p.Entries))
	for i, b := range p.Entries {
		entries[i] = b
	}
	c.SnapshotJournal, e = verifier.Present(map[string]any{"anchor": anchor, "entries": entries})
	if e != nil {
		return refuse(string(Malformed))
	}
	originals := map[string]any{}
	owned := map[string][]byte{}
	for id, b := range p.Originals {
		if len(b) > MaxRecordBytes {
			return refuse(string(Malformed))
		}
		originals[id] = b
	}
	c.IDJournal, e = verifier.Present(originals)
	if e != nil {
		return refuse(string(Malformed))
	}
	c.IDJournalAvailable, _ = verifier.Present(true)
	b, e := parse(p.CheckpointBody)
	if e != nil || b["type"] != "checkpoint/2" {
		return refuse(string(Malformed))
	}
	id, e := record.ContentID("checkpoint/2", b)
	if e != nil {
		return refuse(string(Malformed))
	}
	// The deliberately unsigned envelope is only a bounded capacity probe: S8
	// charges raw arguments and all Evidence before its checkpoint signature gate.
	// It is never returned, committed, or treated as a successful proof.
	placeholder, e := encode(map[string]any{"version": "swarm-envelope/2", "body": b, "id": id, "signatures": []any{map[string]any{"role": "author", "key": b["registry"], "sig": base64.RawURLEncoding.EncodeToString(make([]byte, 64))}}})
	if e != nil {
		return refuse(string(Malformed))
	}
	preflight := Verify(c, p.Entries, placeholder, nil)
	if preflight.Code == string(Malformed) {
		return Prepared{}, preflight
	}
	if c.RegistryCurrencyVersion != 2 {
		return refuse("registry_currency_version_mismatch")
	}
	// A real registry change must select a newly added registry-owned snapshot;
	// re-adding the old cumulative snapshot cannot satisfy the reissue rule.
	var registrySnapshot string
	for _, v := range arrayValues(b["snapshots"]) {
		r := object(v)
		if r["root_role"] == "registry-owner" && r["root_key_id"] == b["registry"] {
			registrySnapshot = text(r["snapshot_id"])
		}
	}
	var previous string
	for _, v := range arrayValues(anchor["latest"]) {
		r := object(v)
		if r["root_role"] == "registry-owner" {
			previous = text(r["snapshot_id"])
		}
	}
	if registrySnapshot == "" || registrySnapshot == previous || len(p.Entries) == 0 {
		return refuse("checkpoint_snapshot_invalid")
	}
	last, e := parse(p.Entries[len(p.Entries)-1])
	if e != nil {
		return refuse(string(Malformed))
	}
	lb := object(last["body"])
	if lb["op"] != "add" || lb["record_type"] != "revocation/1" || lb["record"] != registrySnapshot || number(lb["seq"]) <= number(anchor["registry_seq"]) {
		return refuse("checkpoint_snapshot_invalid")
	}

	artifacts, e := json.Marshal(struct {
		Anchor, Body []byte
		Entries      [][]byte
		Originals    map[string][]byte
	}{p.JournalAnchor, p.CheckpointBody, p.Entries, p.Originals})
	if e != nil {
		return refuse(string(Malformed))
	}
	maximum := len(artifacts) + 131072
	if p.Capacity == nil || maximum > 16777216 || p.Capacity(maximum) != nil {
		return refuse("journal_capacity_unavailable")
	}
	if nilInterface(s) {
		return refuse(string(SignerUnavailable))
	}
	raw, e := s.Sign(bytes.Clone(p.CheckpointBody))
	if e != nil {
		var t *Error
		if errors.As(e, &t) && t != nil {
			switch t.Code {
			case Malformed, ProfileRefused, SignerUnavailable, SignerInvalid, Rollback:
				return refuse(string(t.Code))
			}
		}
		return refuse(string(SignerUnavailable))
	}
	env, e := parse(raw)
	if e != nil {
		return refuse("checkpoint_invalid")
	}
	signed, e := record.Canonical(env["body"])
	expected, _ := record.Canonical(b)
	if e != nil || !bytes.Equal(signed, expected) {
		return refuse("checkpoint_invalid")
	}
	r := Verify(c, p.Entries, raw, nil)
	if r.Code != "accept" {
		return Prepared{}, r
	}
	for id, b := range p.Originals {
		owned[id] = bytes.Clone(b)
	}
	return Prepared{Entries: copyEntries(p.Entries), Candidate: bytes.Clone(raw), Snapshot: bytes.Clone(p.Originals[registrySnapshot]), IDJournal: owned, IDJournalAvailable: true, Context: c}, r
}

func arrayValues(v any) []any { rows, _ := v.([]any); return rows }

func nilInterface(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return r.IsNil()
	}
	return false
}
