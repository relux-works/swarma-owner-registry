package journal

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

// An advancing suffix must carry its prior head entry; the anchored suffix is
// the nearby control. Exact verifier refusals must leave no candidate plan.
func TestAdvancingAnchorMissingRefused(t *testing.T) {
	f := loadFixture(t, "P-JRN-publish-reissue")
	c, entries, cp := fixtureProof(t, f)
	accepted(t, Verify(c, entries, cp, nil))
	refusedResult(t, Verify(c, entries[1:], cp, nil), "journal_anchor_required")
}

// A signed fork at the same sequence/head still differs by checkpoint identity;
// only the exact witnessed checkpoint is an accepted zero-entry retry.
func TestSameHeadCheckpointForkRefused(t *testing.T) {
	f := loadFixture(t, "P-JRN-retransmit")
	c, entries, cp := fixtureProof(t, f)
	accepted(t, Verify(c, entries, cp, nil))
	changed := parseObject(t, cp)
	b := changed["body"].(map[string]any)
	at, err := time.Parse("2006-01-02T15:04:05Z", text(b["at"]))
	if err != nil {
		t.Fatal(err)
	}
	b["at"] = at.Add(time.Second).Format("2006-01-02T15:04:05Z")
	c.Now = present(t, b["at"])
	c.Watermark = present(t, b["at"])
	cp = signFixture(t, b)
	refusedResult(t, Verify(c, nil, cp, nil), "journal_fork")
}

// A six-field received tuple is never a validated projection. Supplying all
// eight fields to the six-field verifier API also refuses with no plan.
func TestEightFieldProjectionRefused(t *testing.T) {
	raw, originals, _ := projectionFixture(t)
	p, e := ReduceProjection(raw, originals, nil)
	if e != nil {
		t.Fatal(e)
	}
	six := p.FactBytes()
	if got, e := ReduceProjection(six, originals, nil); e == nil || got.Valid() {
		t.Fatal("received six-field tuple admitted")
	}
	f := loadFixture(t, "P-JRN-publish-reissue")
	c, es, cp := fixtureProof(t, f)
	c.JournalWitness = present(t, parseObject(t, raw))
	refusedResult(t, Verify(c, es, cp, nil), "journal_witness_invalid")
}

// The shared suffix supplies the authenticated raw anchor and preserves its
// exact checkpoint ID/watermark. It conveys no authority for referenced content.
func TestAnchoredSuffixAccepted(t *testing.T) {
	f := loadFixture(t, "P-JRN-publish-reissue")
	c, entries, cp := fixtureProof(t, f)
	r := Verify(c, entries, cp, nil)
	accepted(t, r)
	expected := object(f["expected"])
	if r.Code != expected["code"] || r.Plan.Watermark != object(expected["plan"])["watermark"] || r.Plan.IDs[0] != object(expected["plan"])["ids"].([]any)[0] {
		t.Fatalf("fixture plan differs: %+v", r)
	}
}

// Nondecreasing entry time includes the raw anchor; an authenticated suffix
// cannot conceal a first new entry predating that anchor.
func TestAnchorTimeRegressionRefused(t *testing.T) {
	f := loadFixture(t, "P-JRN-publish-reissue")
	c, entries, cp := fixtureProof(t, f)
	accepted(t, Verify(c, entries, cp, nil))
	first := parseObject(t, entries[1])
	body := object(first["body"])
	body["at"] = "1970-01-01T00:00:00Z"
	changed, e := BuildEntry(wire(t, body), "1970-01-01T00:00:00Z")
	if e != nil {
		t.Fatal(e)
	}
	entries[1] = changed
	refusedResult(t, Verify(c, entries, cp, nil), "journal_chain_broken")
}

// Prior checkpoint time is a separate retained floor. Equal time passes and
// backward time refuses without a plan through the pinned verifier.
func TestCheckpointTimeRegressionRefused(t *testing.T) {
	for _, tc := range []struct{ name, want string }{{"P-JRN-prior-time-equal", "accept"}, {"N-JRN-prior-time", "journal_rollback"}} {
		f := loadFixture(t, tc.name)
		c, es, cp := fixtureProof(t, f)
		r := Verify(c, es, cp, nil)
		if tc.want == "accept" {
			accepted(t, r)
		} else {
			refusedResult(t, r, tc.want)
		}
	}
}

// Actual D39 candidate construction reproduces the shared signed checkpoint,
// exact prior lookup and oldest-first entries. Six independent fixture negatives
// retain their original typed refusals, not a second local admission policy.
func TestHeadChangeReissueAccepted(t *testing.T) {
	c, p, expected := publicationFixture(t)
	plan, r := BuildPublication(c, fixtureSigner(t), p)
	accepted(t, r)
	exactCandidate(t, plan, expected)
	if !plan.IDJournalAvailable || !bytes.Equal(plan.IDJournal[text(p.Witness.fields["checkpoint_id"])], p.PriorCheckpoint) || !reflect.DeepEqual(plan.Entries, p.Entries) {
		t.Fatal("prepared exact proof differs")
	}
	accepted(t, Verify(plan.Context, plan.Entries, plan.Candidate, nil))
	for _, suffix := range []string{"old-snapshot", "missing-record", "missing-reissue", "missing-prior", "wrong-prior", "backward-time"} {
		t.Run(suffix, func(t *testing.T) {
			f := loadFixture(t, "N-JRN-publish-reissue-"+suffix)
			c, es, cp := fixtureProof(t, f)
			want := text(object(f["expected"])["code"])
			refusedResult(t, Verify(c, es, cp, nil), want)
			_, n, _ := publicationFixture(t)
			n.Entries = es
			n.CheckpointBody = wire(t, parseObject(t, cp)["body"])
			facts := object(f["fixture"])
			w := object(facts["journal_witness"])
			n.PriorCheckpoint = nil
			if value, ok := object(facts["seen"])[text(w["checkpoint_id"])]; ok {
				n.PriorCheckpoint = decode(t, value)
			}
			row := object(object(facts["revocations"])[text(w["registry_id"])])
			n.Snapshot = SnapshotFacts{decode(t, row["snapshot"]), row["availability"] == "available", row["rollback_safe"] == true, number(row["max_age"])}
			prepared, built := BuildPublication(c, fixtureSigner(t), n)
			assertPreparedEmpty(t, prepared)
			refusedResult(t, built, want)

		})
	}
}

// A validated projection retains all eight members and exact originals; fact
// derivation removes only the two excluded fields and exposes owned copies.
func TestValidatedProjectionReduced(t *testing.T) {
	raw, o, p := projectionFixture(t)
	if !bytes.Equal(p.Bytes(), raw) || !bytes.Equal(p.Checkpoint(), o.Checkpoint) {
		t.Fatal("retained originals differ")
	}
	six := parseObject(t, p.FactBytes())
	if len(six) != 6 || six["checkpoint_time"] != nil || six["candidate_digest"] != nil || !p.Fact().IsAvailable() {
		t.Fatal("wrong reduction")
	}
	raw[0] = ' '
	o.Checkpoint[0] = ' '
	owned := p.Bytes()
	owned[0] = ' '
	if p.Bytes()[0] == ' ' || p.Checkpoint()[0] == ' ' {
		t.Fatal("retained evidence aliases caller")
	}
}
