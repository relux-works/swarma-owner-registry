package freshness_test

import (
	"bytes"
	f "github.com/relux-works/swarma-owner-registry/internal/freshness"
	snap "github.com/relux-works/swarma-owner-registry/internal/snapshot"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// The same explicit interval shape as snapshot issuance supplies the exact
// lower issue time and upper verifier time; acknowledged state retains both
// distinct floors and boot, while the pinned verifier plan is preserved.
func TestIntervalReceiptAccepted(t *testing.T) {
	store := &persistence{}
	p := publisher(t, initial(t), store)
	in := input(t)
	base, _ := time.Parse(time.RFC3339, "2026-01-01T00:03:00Z")
	l := base.UnixNano() + 250_000_000
	u := l + 2_000_000_000
	in.Time.Lower = strconv.FormatInt(l, 10)
	in.Time.Upper = strconv.FormatInt(u, 10)
	in.Time.Observed = strconv.FormatInt(l+1_000_000_000, 10)
	in.Time.HighWater = in.Time.Upper
	// Stale point fields cannot override the new interval projection.
	in.Context.Now = fact(t, "2026-01-01T00:00:00Z")
	in.Context.Watermark = fact(t, "2099-01-01T00:00:00Z")
	stateBefore := p.LastResolved()
	issued, e := snap.Issue(signer(t), snap.Retained{Available: true, RollbackSafe: true, Registry: stateBefore.Genesis.RegistryID, Owner: stateBefore.Genesis.Owner, Snapshot: in.Snapshot.Bytes, Checkpoint: in.Checkpoint, Watermark: stateBefore.Watermark, DecisionFloor: stateBefore.DecisionFloor}, in.Time, "2026-01-01T00:03:00Z", nil)
	if e != nil {
		t.Fatal(e)
	}
	if object(parsed(t, issued.Bytes)["body"])["issued_at"] != "2026-01-01T00:03:00Z" {
		t.Fatal("snapshot did not derive floor(L)")
	}
	b, r := publish(t, p, in)
	if object(parsed(t, b)["body"])["issued_at"] != "2026-01-01T00:03:00Z" || r.Plan.Watermark != "2026-01-01T00:03:03Z" || issued.Time.Now != r.Plan.Watermark {
		t.Fatal(r)
	}
	state := p.LastResolved()
	if state.Watermark != "2026-01-01T00:03:00Z" || state.DecisionFloor != strconv.FormatInt(base.Add(3*time.Second).UnixNano(), 10) || state.Boot != in.Time.Boot || state.History[0].Time != in.Time {
		t.Fatal("time evidence not retained separately")
	}
	publisher(t, state, &persistence{})
	// The published state is the exact acknowledged update, not later mutation.
	if !reflect.DeepEqual(store.updates[0].After, state) {
		t.Fatal("time floors escaped acknowledgement")
	}
}

// A provider's newly consumed U cannot repair a stale original interval or be
// substituted for the registry's previous watermark. Complete newer evidence
// passes and its high water is not mistaken for the previous registry floor.
func TestConsumedHighWaterNotPriorWatermark(t *testing.T) {
	for _, tc := range []struct {
		l, u int64
		good bool
	}{{170, 172, false}, {201, 203, false}, {202, 204, true}} {
		store := &persistence{}
		p := publisher(t, initial(t), store)
		in := at(t, input(t), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(tc.l)*time.Second).Format(time.RFC3339))
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
		in.Time.Observed = strconv.FormatInt(base+(tc.l+tc.u)*500_000_000, 10)
		in.Time.Upper = strconv.FormatInt(base+tc.u*1_000_000_000, 10)
		in.Time.HighWater = in.Time.Upper
		in.Time.PriorHighWater = strconv.FormatInt(base+201*1_000_000_000, 10)
		in.Time.JournalFloor = strconv.FormatInt(base+140*1_000_000_000, 10)
		in.Context.Watermark = fact(t, time.Unix(0, base+tc.u*1_000_000_000).UTC().Format(time.RFC3339))
		before := p.LastResolved()
		b, r := p.Publish(in)
		if tc.good {
			if r.Code != "accept" || len(b) == 0 || p.LastResolved().Watermark != "2026-01-01T00:03:22Z" {
				t.Fatal(r)
			}
		} else {
			noCandidate(t, b, r, "time_unavailable")
			unchanged(t, before, p.LastResolved())
			if store.calls != 0 {
				t.Fatal("stale receipt persisted")
			}
		}
	}
}

// The pinned verifier admits high-issued_at=max_age exactly and refuses one
// second beyond, using the projected ceil(U), not O or floor(U). A retry keeps
// the old publication/position and original outcome; only time state advances.
func TestIntervalAgeBoundary(t *testing.T) {
	for _, age := range []int{60, 61} {
		store := &persistence{}
		p := publisher(t, initial(t), store)
		original := input(t)
		b, out := publish(t, p, original)
		base, _ := time.Parse(time.RFC3339, issueAt(t, original))
		lower := base.Add(time.Duration(age-2) * time.Second)
		in := at(t, original, lower.Format(time.RFC3339))
		u := base.Add(time.Duration(age) * time.Second).UnixNano()
		in.Time.Upper = strconv.FormatInt(u, 10)
		in.Time.HighWater = in.Time.Upper
		in.Time.Observed = strconv.FormatInt(lower.Add(time.Second).UnixNano(), 10)
		before := p.LastResolved()
		got, r := p.Retry(in)
		if age == 60 {
			if !bytes.Equal(got, b) || !reflect.DeepEqual(r, out) || store.calls != 2 || p.LastResolved().Anchor.PublicationSeq != 1 || p.LastResolved().DecisionFloor != in.Time.Upper {
				t.Fatal(r)
			}
			publisher(t, p.LastResolved(), &persistence{})
		} else {
			noCandidate(t, got, r, "freshness_unavailable")
			unchanged(t, before, p.LastResolved())
			if store.calls != 1 {
				t.Fatal("stale retry advanced time state")
			}
		}
	}
}

// Missing identities, inconsistent bounds and a regressed consumer floor refuse
// before signing/persistence. A complete interval is the neighbouring control.
func TestIntervalRefusalHasNoEffects(t *testing.T) {
	for _, which := range []string{"authority", "boot", "sample", "highwater", "prior", "journal"} {
		store := &persistence{}
		s := &countingSigner{inner: signer(t)}
		state := initial(t)
		in := input(t)
		switch which {
		case "authority":
			in.Time.AuthorityUntil = in.Time.Upper
		case "boot":
			in.Time.Boot = ""
		case "sample":
			in.Time.SampleID = ""
		case "highwater":
			in.Time.HighWater = "0"
		case "prior":
			state.Watermark = "2026-01-01T00:03:01Z"
		case "journal":
			state.DecisionFloor = in.Time.Upper
		}
		p := publisherWithSigner(t, state, store, s)
		before := p.LastResolved()
		b, r := p.Publish(in)
		noCandidate(t, b, r, "time_unavailable")
		unchanged(t, before, p.LastResolved())
		if s.calls != 0 || store.calls != 0 {
			t.Fatal("invalid time reached effects")
		}
	}
	publish(t, publisher(t, initial(t), &persistence{}), input(t))
}

// Restored time evidence must be complete and agree with the exact accepted
// verifier plan and retained floors; independent recovery cannot excuse an
// internally inconsistent archive. The intact state remains a restart control.
func TestRestoredIntervalEvidenceRefused(t *testing.T) {
	p := publisher(t, initial(t), &persistence{})
	publish(t, p, input(t))
	state := p.LastResolved()
	for _, which := range []string{"boot", "floor", "watermark", "interval", "projection", "plan"} {
		changed := state.Clone()
		switch which {
		case "boot":
			changed.Boot = ""
		case "floor":
			changed.DecisionFloor = "0"
		case "watermark":
			changed.Watermark = "1970-01-01T00:00:00Z"
		case "interval":
			changed.History[0].Time.SampleID = ""
		case "projection":
			changed.History[0].Projection.Now = "1970-01-01T00:00:00Z"
		case "plan":
			changed.History[0].Outcome.Plan.Watermark = "1970-01-01T00:00:00Z"
		}
		id, e := f.StateID(changed)
		if e != nil {
			t.Fatal(e)
		}
		denied, e := f.New(f.Trust{Registry: changed.Genesis.RegistryID, Owner: changed.Genesis.Owner, Signer: signer(t), Retained: changed, Persistence: &persistence{}, Recovery: f.Recovery{StateID: id, Reconciled: true}})
		stateRefused(t, denied, e)
	}
	publisher(t, state, &persistence{})
}
