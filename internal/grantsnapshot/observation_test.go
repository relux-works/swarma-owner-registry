package grantsnapshot

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type vector struct{ Fixture, Wire, Root string }

func vectors(t *testing.T) []vector {
	t.Helper()
	b, e := os.ReadFile("testdata/snapshots.json")
	if e != nil {
		t.Fatal(e)
	}
	var v []vector
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func pins(t *testing.T) Pins {
	t.Helper()
	v := vectors(t)
	return Pins{AgentRoot: v[0].Root, PublisherRoot: v[2].Root, ServiceRoot: v[36].Root, GrantRoot: v[1].Root}
}
func original(t *testing.T, index int) ([]byte, string) {
	t.Helper()
	v := vectors(t)[index]
	raw, e := base64.StdEncoding.DecodeString(v.Wire)
	if e != nil {
		t.Fatal(e)
	}
	return raw, v.Root
}
func observed(t *testing.T, index int) *Observation {
	t.Helper()
	raw, root := original(t, index)
	o, e := Observe(pins(t), root, raw)
	if e != nil {
		t.Fatalf("vector %d: %v", index, e)
	}
	return o
}
func stamp(t *testing.T, s string) time.Time {
	t.Helper()
	v, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestOriginalSignatureAndOwnedBytes(t *testing.T) {
	for _, index := range []int{0, 2, 3, 4, 9, 10, 11, 12, 21, 28, 31, 32, 33, 34, 37, 38, 45, 47, 48, 49, 53, 56, 65} {
		t.Run(vectors(t)[index].Fixture, func(t *testing.T) {
			raw, root := original(t, index)
			o, e := Observe(pins(t), root, raw)
			if e != nil {
				t.Fatal(e)
			}
			saved := bytes.Clone(raw)
			raw[0] ^= 1
			if !bytes.Equal(saved, o.Wire()) {
				t.Fatal("caller mutated retained original")
			}
			returned := o.Wire()
			returned[0] ^= 1
			if !bytes.Equal(saved, o.Wire()) {
				t.Fatal("returned slice aliases original")
			}
			rows := o.Entries()
			if len(rows) > 0 {
				rows[0].ID = "changed"
				if o.Entries()[0].ID == "changed" {
					t.Fatal("entry alias")
				}
			}
		})
	}
}
func TestNonObservations(t *testing.T) {
	for _, index := range []int{5, 6, 13, 14, 16, 17, 18, 19, 20, 22, 35, 36, 39, 40} {
		t.Run(vectors(t)[index].Fixture, func(t *testing.T) {
			raw, root := original(t, index)
			o, e := Observe(pins(t), root, raw)
			if e == nil || o != nil {
				t.Fatalf("invalid vector %d observed", index)
			}
		})
	}
	raw, root := original(t, 0)
	for name, wire := range map[string][]byte{
		"oversize":         bytes.Repeat([]byte(" "), MaxBytes+1),
		"unknown-member":   bytes.Replace(raw, []byte(`"complete":true`), []byte(`"complete":true,"extra":true`), 1),
		"duplicate-member": bytes.Replace(raw, []byte(`"complete":true`), []byte(`"complete":true,"complete":true`), 1),
		"changed-issuer":   bytes.Replace(raw, []byte(root), []byte(pins(t).PublisherRoot), 1),
		"truncated":        raw[:len(raw)-1],
		"signature-role":   bytes.Replace(raw, []byte(`"role":"author"`), []byte(`"role":"approver"`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if o, e := Observe(pins(t), root, wire); e == nil || o != nil {
				t.Fatal("invalid data observed")
			}
		})
	}
	p := pins(t)
	p.AgentRoot = p.ServiceRoot
	if o, e := Observe(p, root, raw); o != nil || !errors.Is(e, ErrUnavailable) {
		t.Fatal("overlapping root policy")
	}
	if o, e := Observe(pins(t), pins(t).PublisherRoot, raw); o != nil || e == nil {
		t.Fatal("wrong signer domain")
	}
}

func TestFullNanosecondAgeAndWatermark(t *testing.T) {
	o := observed(t, 0)
	at := o.IssuedAt()
	for _, c := range []struct {
		name   string
		now, w time.Time
		want   error
	}{
		{"60s-inclusive", at.Add(60 * time.Second), at.Add(60 * time.Second), nil},
		{"60s-plus-one-ns", at.Add(60*time.Second + 1), at.Add(60*time.Second + 1), ErrUnavailable},
		{"watermark-ahead-age", at, at.Add(60*time.Second + 1), ErrUnavailable},
		{"watermark-behind-issued", at, at.Add(-1), ErrUnavailable},
		{"now-behind-issued", at.Add(-1), at, ErrUnavailable},
		{"missing-watermark", at, time.Time{}, ErrUnavailable},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := Analyze([]*Observation{}, o, c.now, c.w)
			if !errors.Is(a.Refusal, c.want) {
				t.Fatalf("got %v want %v", a.Refusal, c.want)
			}
			if a.Observation != o {
				t.Fatal("lost static observation")
			}
		})
	}
}
func TestHistoricalUnionSurvivesRefusals(t *testing.T) {
	revoked := observed(t, 32)
	clear := observed(t, 31)
	postponed := observed(t, 33)
	now := stamp(t, "2026-01-01T00:02:00Z")
	a := Analyze([]*Observation{}, revoked, now, now)
	if !errors.Is(a.Refusal, ErrUnavailable) || len(a.Union) != 1 || a.Mark.IsZero() {
		t.Fatal("stale signed revocation lost")
	}
	// The caller retains the same signed original, including after reopen. This
	// is an in-memory causal replay, not a durability or protected-restart claim.
	for _, next := range []*Observation{clear, postponed} {
		a = Analyze([]*Observation{revoked}, next, next.IssuedAt(), next.IssuedAt())
		if !errors.Is(a.Refusal, ErrRollback) || len(a.Union) != 1 || !a.Union[0].From.Equal(revoked.entries[0].From) {
			t.Fatal("historical hold cleared or postponed")
		}
	}
	future := observed(t, 56)
	before := future.IssuedAt().Add(-1)
	a = Analyze([]*Observation{}, future, before, before)
	if a.Refusal != ErrUnavailable || len(a.Union) != 1 || !a.Mark.IsZero() {
		t.Fatal("future evidence discarded or future mark raised")
	}
	a = Analyze([]*Observation{future}, observed(t, 0), observed(t, 0).IssuedAt(), observed(t, 0).IssuedAt())
	if a.Refusal != ErrRollback || len(a.Union) != 1 {
		t.Fatal("future observed union cleared")
	}
}
func TestForkRollbackAndSeparateRoots(t *testing.T) {
	first := observed(t, 0)
	changed := observed(t, 3)
	newer := observed(t, 38)
	for _, c := range []struct {
		name    string
		history []*Observation
		next    *Observation
		want    error
	}{
		{"exact-retransmit", []*Observation{first}, first, nil},
		{"same-time-fork", []*Observation{first}, changed, ErrFork},
		{"lower-time", []*Observation{newer}, first, ErrRollback},
		{"missing-history", nil, first, ErrContinuity},
		{"different-root", []*Observation{observed(t, 2)}, first, ErrContinuity},
		{"conflicting-history", []*Observation{first, changed}, newer, ErrFork},
	} {
		t.Run(c.name, func(t *testing.T) {
			now := stamp(t, "2026-01-01T00:02:00Z")
			a := Analyze(c.history, c.next, now, now)
			if !errors.Is(a.Refusal, c.want) {
				t.Fatalf("got %v want %v", a.Refusal, c.want)
			}
		})
	}
	// Independent domains are analyzed separately; the registry never re-signs
	// publisher observations or combines their cumulative sets.
	publisher := observed(t, 2)
	if a := Analyze([]*Observation{}, publisher, publisher.issued, publisher.issued); a.Refusal != nil {
		t.Fatal(a.Refusal)
	}
}
func TestCapacityAndZeroValues(t *testing.T) {
	o := observed(t, 0)
	history := make([]*Observation, MaxHistoryBytes/len(o.wire)+1)
	for i := range history {
		history[i] = o
	}
	if a := Analyze(history, o, o.issued, o.issued); a.Refusal != ErrCapacity {
		t.Fatal("history silently truncated")
	}
	if a := Analyze([]*Observation{}, &Observation{}, o.issued, o.issued); a.Refusal != ErrInvalid || a.Observation != nil {
		t.Fatal("zero observation used")
	}
	// 8192 signed bytes is a distinct positive bound, never inferred from count.
	all := vectors(t)
	for _, v := range all {
		if strings.HasPrefix(v.Fixture, "PH-revocations-bytes-8192") {
			raw, e := base64.StdEncoding.DecodeString(v.Wire)
			if e != nil {
				t.Fatal(e)
			}
			if len(raw) != 8192 {
				t.Fatal("fixture bound")
			}
			if _, e = Observe(pins(t), v.Root, raw); e != nil {
				t.Fatal(e)
			}
			return
		}
	}
	t.Fatal("missing exact byte boundary fixture")
}
