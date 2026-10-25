// Package snapshot constructs private registry-owner base snapshot candidates.
// It performs no persistence, witness operation, clock read or publication.
package snapshot

import (
	"bytes"
	"encoding/base64"
	"errors"
	"time"

	"github.com/relux-works/swarma-owner-registry/internal/journal"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

const MaxEntries = 64
const MaxBytes = 8192

type Code string

const (
	Malformed         Code = "snapshot_invalid"
	Gap               Code = "snapshot_gap"
	Fork              Code = "snapshot_fork"
	Rollback          Code = "snapshot_rollback"
	CumulativeRemoval Code = "snapshot_cumulative_removal"
	CapacityExhausted Code = "capacity_exhausted"
	TimeUnavailable   Code = "time_unavailable"
)

// Error contains only a stable code. Signal requires the caller to fence the
// affected domain/head and retain both originals for a fork, or the entire
// rejected addition and last durable complete set for capacity exhaustion.
// No refusal selects, updates or discards retained state.
type Error struct {
	Code   Code
	Signal bool
}

func (e *Error) Error() string { return string(e.Code) }
func fail(code Code) error {
	return &Error{code, code == Fork || code == CapacityExhausted || code == Code(journal.Fork)}
}
func upstream(e error) error {
	var j *journal.Error
	if errors.As(e, &j) {
		return fail(Code(j.Code))
	}
	return fail(Code("signer_unavailable"))
}

// Entry is a cumulative revocation, including future-effective revocations.
// Kind is exactly key or record; From is an exact UTC second. Duplicate
// (Kind,ID) pairs are refused rather than silently combined.
type Entry struct{ Kind, ID, From string }

// Retained is caller-protected, independently reconciled state, not received
// snapshot metadata. The exact immutable base and its checkpoint must already
// have qualified signatures, completeness, authority and cumulative history.
// Missing history is not genesis. Explicit bootstrap belongs to the embedding
// service. All older originals remain in its durable inventory; these APIs
// neither replace that inventory nor establish rollback safety from a boolean.
type Retained struct {
	Available, RollbackSafe bool
	Registry, Owner         string
	Snapshot, Checkpoint    []byte
}

// TimeSample is a fresh caller-qualified decision sample from the shared time
// provider. Confident means its conservative uncertainty and continuity passed
// the protected policy; AuthorityUntil is its exclusive authority endpoint.
// Now and Watermark are exact UTC seconds, with zero backward tolerance.
// A production adapter must acquire fresh confidence at each call and retain
// watermark/hold history independently. These fields do not implement timeconf
// or cross-process continuity and must never come from request data. No local
// clock, collector, environment source or cached Context time is consulted.
type TimeSample struct {
	Now, Watermark, AuthorityUntil string
	Confident, WatermarkAvailable  bool
}

type Issued struct {
	ID    string
	Bytes []byte
	// Advisory local thresholds: at least 56 entries or 7 KiB signed bytes.
	// Hard limits remain independent: 64 entries AND 8192 whole signed bytes.
	CapacityWarning bool
}

// Candidate contains one complete, owned journal transaction candidate. A
// later serialized adapter must persist the record, reissue, entries, checkpoint
// and exact prior bindings atomically through intent/commit/resolution, with
// fresh time and exclusion checks. Success here authorizes no public response.
type Candidate struct {
	Publication     journal.Prepared
	Result          verifier.Result
	CapacityWarning bool
}

func timestamp(s string) bool {
	if len(s) != 20 {
		return false
	}
	t, e := time.Parse("2006-01-02T15:04:05Z", s)
	return e == nil && t.Year() >= 1970 && t.Year() <= 9999 && t.Format("2006-01-02T15:04:05Z") == s
}
func timeContext(c verifier.Context, t TimeSample) (verifier.Context, error) {
	if !t.Confident || !t.WatermarkAvailable || !timestamp(t.Now) || !timestamp(t.Watermark) || !timestamp(t.AuthorityUntil) || t.Now < t.Watermark || t.Now >= t.AuthorityUntil {
		return verifier.Context{}, fail(TimeUnavailable)
	}
	c.Now, _ = verifier.Present(t.Now)
	c.Watermark, _ = verifier.Present(t.Watermark)
	c.WatermarkAvailable, _ = verifier.Present(true)
	c.Time, _ = verifier.Present(map[string]any{"trusted": true, "fresh": true, "authority_until": t.AuthorityUntil})
	return c, nil
}
func readObject(raw []byte) (map[string]any, error) {
	v, e := record.Parse(raw, MaxBytes, 16)
	if e != nil {
		return nil, fail(Malformed)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fail(Malformed)
	}
	return m, nil
}
func mapValue(v any) map[string]any { m, _ := v.(map[string]any); return m }
func stringValue(v any) string      { s, _ := v.(string); return s }
func exact(m map[string]any, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}
func entryValues(rows []Entry) []any {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{"kind": r.Kind, "id": r.ID, "from": r.From}
	}
	return out
}
func entryMap(rows []Entry) (map[string]string, error) {
	if len(rows) > MaxEntries {
		return nil, fail(CapacityExhausted)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if !timestamp(r.From) {
			return nil, fail(Malformed)
		}
		switch r.Kind {
		case "record":
			if !record.IsContentID(r.ID) {
				return nil, fail(Malformed)
			}
		case "key":
			if len(r.ID) > 128 || record.ValidateKey(r.ID) != nil {
				return nil, fail(Malformed)
			}
		default:
			return nil, fail(Malformed)
		}
		k := r.Kind + ":" + r.ID
		if _, ok := out[k]; ok {
			return nil, fail(Malformed)
		}
		out[k] = r.From
	}
	return out, nil
}
func snapshot(raw []byte, root string) (map[string]any, []Entry, error) {
	if len(raw) > MaxBytes {
		return nil, nil, fail(CapacityExhausted)
	}
	if e := checkEntryCount(raw); e != nil {
		return nil, nil, e
	}
	m, e := readObject(raw)
	if e != nil {
		return nil, nil, e
	}
	b := mapValue(m["body"])
	if !exact(m, "version", "body", "id", "signatures") || m["version"] != "swarm-envelope/2" || !exact(b, "type", "issuer", "issued_at", "complete", "entries") || b["type"] != "revocation/1" || b["issuer"] != root || b["complete"] != true || !timestamp(stringValue(b["issued_at"])) {
		return nil, nil, fail(Malformed)
	}
	id, e := record.ContentID("revocation/1", b)
	if e != nil || m["id"] != id {
		return nil, nil, fail(Malformed)
	}
	values, ok := b["entries"].([]any)
	if !ok {
		return nil, nil, fail(Malformed)
	}
	if len(values) > MaxEntries {
		return nil, nil, fail(CapacityExhausted)
	}
	rows := make([]Entry, len(values))
	for i, v := range values {
		r := mapValue(v)
		if !exact(r, "kind", "id", "from") {
			return nil, nil, fail(Malformed)
		}
		rows[i] = Entry{stringValue(r["kind"]), stringValue(r["id"]), stringValue(r["from"])}
	}
	if _, e = entryMap(rows); e != nil {
		return nil, nil, e
	}
	return b, rows, nil
}
func retained(s Retained) (map[string]any, []Entry, error) {
	if !s.Available || !s.RollbackSafe || len(s.Snapshot) == 0 || len(s.Checkpoint) == 0 {
		return nil, nil, fail(Gap)
	}
	if record.ValidateKey(s.Registry) != nil || s.Owner == "" {
		return nil, nil, fail(Malformed)
	}
	b, rows, e := snapshot(s.Snapshot, s.Registry)
	if e != nil {
		return nil, nil, e
	}
	cp, e := readObject(s.Checkpoint)
	if e != nil {
		return nil, nil, e
	}
	cb := mapValue(cp["body"])
	id, e := record.ContentID("checkpoint/1", cb)
	seq, ok := cb["seq"].(int64)
	snapID, _ := record.ContentID("revocation/1", b)
	if e != nil || cp["id"] != id || cb["registry"] != s.Registry || cb["owner"] != s.Owner || cb["snapshot"] != snapID || cb["type"] != "checkpoint/1" || !ok || seq < 1 || seq > journal.MaxSequence || !record.IsContentID(stringValue(cb["head"])) || !timestamp(stringValue(cb["at"])) || stringValue(b["issued_at"]) > stringValue(cb["at"]) {
		return nil, nil, fail(Gap)
	}
	return b, rows, nil
}
func cumulative(old, next []Entry) error {
	before, e := entryMap(old)
	if e != nil {
		return e
	}
	after, e := entryMap(next)
	if e != nil {
		return e
	}
	for k, from := range before {
		n, ok := after[k]
		if !ok || n > from {
			return fail(CumulativeRemoval)
		}
	}
	return nil
}
func transition(s Retained, old map[string]any, oldRows []Entry, raw []byte, t TimeSample) error {
	b, rows, e := snapshot(raw, s.Registry)
	if e != nil {
		return e
	}
	at := stringValue(b["issued_at"])
	prev := stringValue(old["issued_at"])
	if at == prev && !bytes.Equal(raw, s.Snapshot) {
		return fail(Fork)
	}
	if at <= prev {
		return fail(Rollback)
	}
	if at > t.Watermark {
		return fail(TimeUnavailable)
	}
	if e = cumulative(oldRows, rows); e != nil {
		return e
	}
	return nil
}
func warning(count, size int) bool { return count >= 56 || size >= 7168 }

// Issue signs a strictly newer complete private candidate, preserving every
// retained entry and never moving its effective time later. Entries is the
// caller's protected current cumulative set, not a received claim of completeness.
// Reordering changes content; no entries are sorted, deduplicated or dropped.
func Issue(signer journal.Signer, s Retained, t TimeSample, issuedAt string, rows []Entry) (*Issued, error) {
	if _, e := timeContext(verifier.Context{}, t); e != nil {
		return nil, e
	}
	old, oldRows, e := retained(s)
	if e != nil {
		return nil, e
	}
	if !timestamp(issuedAt) {
		return nil, fail(Malformed)
	}
	if _, e = entryMap(rows); e != nil {
		return nil, e
	}
	body := map[string]any{"type": "revocation/1", "issuer": s.Registry, "issued_at": issuedAt, "complete": true, "entries": entryValues(rows)}
	id, e := record.ContentID("revocation/1", body)
	if e != nil {
		return nil, fail(Malformed)
	}
	placeholder := map[string]any{"version": "swarm-envelope/2", "body": body, "id": id, "signatures": []any{map[string]any{"role": "author", "key": s.Registry, "sig": base64.RawURLEncoding.EncodeToString(make([]byte, 64))}}}
	encoded, e := record.Canonical(placeholder)
	if e != nil {
		return nil, fail(Malformed)
	}
	if len(encoded) > MaxBytes {
		return nil, fail(CapacityExhausted)
	}
	// Compare content at equal issue time before custody; signature placeholder
	// bytes are irrelevant to this content comparison.
	if issuedAt == old["issued_at"] {
		oldID, _ := record.ContentID("revocation/1", old)
		if id != oldID {
			return nil, fail(Fork)
		}
		return nil, fail(Rollback)
	}
	if issuedAt < stringValue(old["issued_at"]) {
		return nil, fail(Rollback)
	}
	if issuedAt > t.Watermark {
		return nil, fail(TimeUnavailable)
	}
	at, _ := time.Parse(time.RFC3339, issuedAt)
	now, _ := time.Parse(time.RFC3339, t.Now)
	if now.Sub(at) > 60*time.Second {
		return nil, fail(Code("journal_unavailable"))
	}
	if e = cumulative(oldRows, rows); e != nil {
		return nil, e
	}
	if signer == nil {
		return nil, fail(Code("signer_unavailable"))
	}
	rawBody, _ := record.Canonical(body)
	raw, e := signer.Sign(rawBody)
	if e != nil {
		return nil, upstream(e)
	}
	b, _, e := snapshot(raw, s.Registry)
	if e != nil {
		return nil, e
	}
	actual, _ := record.Canonical(b)
	if !bytes.Equal(actual, rawBody) {
		return nil, fail(Code("signer_signature_invalid"))
	}
	return &Issued{id, bytes.Clone(raw), warning(len(rows), len(raw))}, nil
}

// Prepare revalidates time, cumulative continuity and exact prior bindings, then
// admits the entire head change through BuildPublication/VerifyJournal only.
// The supplied snapshot normally comes from Issue; it must have a strictly
// greater issue time even if its cumulative set is identical. Received entries
// are never repaired or synthesized to conceal gaps. Refusal returns nil and
// exposes no persistence plan; all caller inputs and retained originals survive.
func Prepare(c verifier.Context, signer journal.Signer, s Retained, t TimeSample, p journal.Publication) (*Candidate, error) {
	c, e := timeContext(c, t)
	if e != nil {
		return nil, e
	}
	old, oldRows, e := retained(s)
	if e != nil {
		return nil, e
	}
	cb, e := readObject(p.CheckpointBody)
	if e != nil {
		return nil, e
	}
	prior, _ := readObject(s.Checkpoint)
	pb := mapValue(prior["body"])
	if cb["seq"] == pb["seq"] {
		a, _ := record.Canonical(cb)
		b, _ := record.Canonical(pb)
		if !bytes.Equal(a, b) || !bytes.Equal(p.Snapshot.Bytes, s.Snapshot) {
			return nil, fail(Fork)
		}
		return nil, fail(Rollback)
	}
	if e = transition(s, old, oldRows, p.Snapshot.Bytes, t); e != nil {
		return nil, e
	}
	prepared, r := journal.BuildPublication(c, signer, p)
	if r.Code != "accept" {
		return nil, fail(Code(r.Code))
	}
	// Bind protected cumulative history to the same exact prior checkpoint used
	// by journal admission. A different retained lineage cannot qualify this one.
	if !bytes.Equal(p.PriorCheckpoint, s.Checkpoint) || !bytes.Equal(p.Witness.Checkpoint(), s.Checkpoint) {
		return nil, fail(Gap)
	}
	_, rows, _ := snapshot(p.Snapshot.Bytes, s.Registry)
	return &Candidate{prepared, r, warning(len(rows), len(p.Snapshot.Bytes))}, nil
}

// Change supplies a caller-authorized record body and witnessed anchor for one
// real head change. Entries is the protected current complete cumulative set.
// The builder constructs the new record entry, snapshot-add entry and checkpoint
// together; callers cannot choose an old checkpoint snapshot reference here.
// Record authorization and acquiring the genuine latest witness remain external.
type Change struct {
	Anchor, RecordBody     []byte
	Entries                []Entry
	IssuedAt, CheckpointAt string
	Witness                journal.Projection
}

// Build constructs the entire anchored record/reissue transaction and admits it
// through Prepare. It never makes an idle publication or persists its candidate.
func Build(c verifier.Context, signer journal.Signer, s Retained, t TimeSample, change Change) (*Candidate, error) {
	if _, e := timeContext(c, t); e != nil {
		return nil, e
	}
	anchor, e := readObject(change.Anchor)
	if e != nil {
		return nil, e
	}
	ab := mapValue(anchor["body"])
	entry, e := journal.BuildEntry(change.RecordBody, stringValue(ab["at"]))
	if e != nil {
		return nil, upstream(e)
	}
	parsed, _ := readObject(entry)
	body := mapValue(parsed["body"])
	seq, _ := body["seq"].(int64)
	if seq >= journal.MaxSequence {
		return nil, fail(CapacityExhausted)
	}
	issued, e := Issue(signer, s, t, change.IssuedAt, change.Entries)
	if e != nil {
		return nil, e
	}
	reissueBody := map[string]any{"type": "journal-entry/1", "registry": s.Registry, "owner": s.Owner, "seq": seq + 1, "prev": parsed["id"], "at": body["at"], "op": "add", "record_type": "revocation/1", "record": issued.ID}
	raw, e := record.Canonical(reissueBody)
	if e != nil {
		return nil, fail(Malformed)
	}
	reissue, e := journal.BuildEntry(raw, stringValue(body["at"]))
	if e != nil {
		return nil, upstream(e)
	}
	last, _ := readObject(reissue)
	checkpoint := map[string]any{"type": "checkpoint/1", "registry": s.Registry, "owner": s.Owner, "seq": seq + 1, "head": last["id"], "at": change.CheckpointAt, "snapshot": issued.ID}
	cp, e := record.Canonical(checkpoint)
	if e != nil {
		return nil, fail(Malformed)
	}
	return Prepare(c, signer, s, t, journal.Publication{Entries: [][]byte{bytes.Clone(change.Anchor), entry, reissue}, CheckpointBody: cp, PriorCheckpoint: bytes.Clone(s.Checkpoint), Snapshot: journal.SnapshotFacts{Bytes: issued.Bytes, Available: true, RollbackSafe: true, MaxAge: 60}, Witness: change.Witness})
}
