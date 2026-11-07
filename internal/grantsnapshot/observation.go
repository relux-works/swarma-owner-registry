// Package grantsnapshot validates original grant and agent-revocation snapshots
// and prepares read-side continuity analysis. It performs no I/O, persistence,
// clock sampling, disclosure authorization or effect admission.
package grantsnapshot

import (
	"bytes"
	"errors"
	"time"

	"github.com/relux-works/swarma-envelope/record"
)

const (
	MaxBytes   = 8192
	MaxEntries = 64
	// MaxHistoryBytes bounds this analysis call, independently of transport and
	// durable inventory limits. Exceeding it refuses; it never evicts history.
	MaxHistoryBytes = 2 << 20
)

var (
	ErrInvalid     = errors.New("snapshot_invalid")
	ErrUnavailable = errors.New("snapshot_unavailable")
	ErrContinuity  = errors.New("snapshot_continuity_unavailable")
	ErrRollback    = errors.New("snapshot_rollback")
	ErrFork        = errors.New("snapshot_fork")
	ErrCapacity    = errors.New("snapshot_capacity")
)

// Pins is installed policy, never values learned from the received record.
// The publisher's appointment and the agent root's service scope must already
// be authenticated by the embedding service. Validation does not establish that.
type Pins struct {
	AgentRoot, PublisherRoot, ServiceRoot, GrantRoot string
}

func (p Pins) valid() bool {
	seen := map[string]bool{}
	for _, key := range []string{p.AgentRoot, p.PublisherRoot, p.ServiceRoot, p.GrantRoot} {
		if seen[key] || len(key) > 128 || record.ValidateKey(key) != nil {
			return false
		}
		seen[key] = true
	}
	return true
}

// Entry retains From even though the Linux MU receiver applies identity-only
// immediate holds. Analysis never discards future-effective or historical rows.
type Entry struct {
	Kind, ID string
	From     time.Time
}

// Observation is a static-shape-valid, pinned-signed original. It is NOT a
// protected read, proof of currentness, publication or rollback safety.
// Fields are private to prevent mutation after signature verification.
type Observation struct {
	root, id string
	wire     []byte
	issued   time.Time
	entries  []Entry
}

func (o *Observation) Wire() []byte {
	if o == nil {
		return nil
	}
	return bytes.Clone(o.wire)
}
func (o *Observation) Root() string {
	if o == nil {
		return ""
	}
	return o.root
}
func (o *Observation) IssuedAt() time.Time {
	if o == nil {
		return time.Time{}
	}
	return o.issued
}
func (o *Observation) Entries() []Entry {
	if o == nil {
		return nil
	}
	return append([]Entry(nil), o.entries...)
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func text(v any) string           { s, _ := v.(string); return s }
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
func timestamp(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok || len(s) != 20 {
		return time.Time{}, false
	}
	t, e := time.Parse("2006-01-02T15:04:05Z", s)
	return t, e == nil && t.Year() >= 1970 && t.Format("2006-01-02T15:04:05Z") == s
}

// Observe verifies bounded closed schema, content ID and original signature for
// exactly one installed root. It intentionally accepts stale and future-issued
// valid observations: the existing receiver must retain them even on refusal.
// No wire is rewritten, re-signed or upgraded to another profile.
func Observe(p Pins, root string, raw []byte) (*Observation, error) {
	if !p.valid() || (root != p.AgentRoot && root != p.PublisherRoot) {
		return nil, ErrUnavailable
	}
	if len(raw) > MaxBytes {
		return nil, ErrCapacity
	}
	v, e := record.Parse(raw, MaxBytes, 16)
	if e != nil {
		return nil, ErrInvalid
	}
	m := object(v)
	b := object(m["body"])
	if !exact(m, "version", "body", "id", "signatures") || m["version"] != "swarm-envelope/2" || !exact(b, "type", "issuer", "issued_at", "complete", "entries") || b["type"] != "revocation/1" || b["issuer"] != root || b["complete"] != true {
		return nil, ErrInvalid
	}
	issued, ok := timestamp(b["issued_at"])
	if !ok || issued.Unix() <= 0 {
		return nil, ErrInvalid
	}
	id, e := record.ContentID("revocation/1", b)
	if e != nil || m["id"] != id {
		return nil, ErrInvalid
	}
	rows, ok := b["entries"].([]any)
	if !ok {
		return nil, ErrInvalid
	}
	if len(rows) > MaxEntries {
		return nil, ErrCapacity
	}
	entries := make([]Entry, 0, len(rows))
	for _, v := range rows {
		r := object(v)
		kind, id := text(r["kind"]), text(r["id"])
		from, ok := timestamp(r["from"])
		if !exact(r, "kind", "id", "from") || !ok {
			return nil, ErrInvalid
		}
		if kind == "key" {
			if _, _, e := record.KeyBytes(id); e != nil {
				return nil, ErrInvalid
			}
		} else if kind != "record" || !record.IsContentID(id) {
			return nil, ErrInvalid
		}
		entries = append(entries, Entry{kind, id, from})
	}
	sigs, ok := m["signatures"].([]any)
	if !ok || len(sigs) != 1 {
		return nil, ErrInvalid
	}
	s := object(sigs[0])
	if !exact(s, "role", "key", "sig") || s["role"] != "author" || s["key"] != root {
		return nil, ErrInvalid
	}
	sig, e := record.DecodeBase64(text(s["sig"]), 64)
	if e != nil {
		return nil, ErrInvalid
	}
	pre, e := record.SigningPreimage("revocation/1", root, "author", b)
	if e != nil || record.VerifySignature(root, sig, pre) != nil {
		return nil, ErrInvalid
	}
	return &Observation{root: root, id: id, wire: bytes.Clone(raw), issued: issued, entries: entries}, nil
}
