package snapshot

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/swarma-owner-registry/internal/journal"
	"github.com/relux-works/swarma-owner-registry/internal/qtime"
	"github.com/relux-works/waggle/record"
)

// Each shared negative must retain its pinned refusal with no candidate; the
// actual three-entry publication is the nearby positive, not a two-entry proxy.
func TestHeadChangeOldSnapshotRefRefused(t *testing.T) {
	fixtureNegative(t, "old-snapshot", "checkpoint_snapshot_invalid")
}
func TestHeadChangeMissingRecordRefused(t *testing.T) {
	fixtureNegative(t, "missing-record", "journal_chain_broken")
}
func TestHeadChangeMissingReissueRefused(t *testing.T) {
	fixtureNegative(t, "missing-reissue", "journal_chain_broken")
}
func TestHeadChangeMissingPriorCheckpointRefused(t *testing.T) {
	fixtureNegative(t, "missing-prior", "journal_unavailable")
}
func TestHeadChangeWrongPriorCheckpointRefused(t *testing.T) {
	fixtureNegative(t, "wrong-prior", "checkpoint_invalid")
}
func TestCheckpointTimeRegressionRefused(t *testing.T) {
	fixtureNegative(t, "backward-time", "journal_rollback")
}
func fixtureNegative(t *testing.T, suffix, want string) {
	t.Helper()
	c, p, s, clock, _ := publicationFixture(t, "N-JRN-publish-reissue-"+suffix)
	prepareRefused(t, c, p, s, clock, want)
	c, p, s, clock, _ = publicationFixture(t, "P-JRN-publish-reissue")
	if _, e := Prepare(c, fixtureSigner(t), s, clock, p); e != nil {
		t.Fatal(e)
	}
	if suffix == "old-snapshot" {
		// This positive also requires construction to use the new reference;
		// substituting the old reference must not satisfy a head-change build.
		built, e := Build(c, fixtureSigner(t), s, clock, Change{Anchor: p.Entries[0], RecordBody: wire(t, parseObject(t, p.Entries[1])["body"]), IssuedAt: "2026-01-01T00:01:20Z", CheckpointAt: "2026-01-01T00:01:20Z", Witness: p.Witness})
		if e != nil {
			t.Fatalf("head-change construction positive: %v", e)
		}
		expectedSnapshot, _, _ := projectedFixture(t, p, "2026-01-01T00:01:20Z")
		if object(parseObject(t, built.Publication.Candidate)["body"])["snapshot"] != parseObject(t, expectedSnapshot)["id"] {
			t.Fatal("head-change construction reused old reference")
		}
	}
}

// A current checkpoint cannot refresh an aged base; the snapshot's own age is
// still required through journal admission, even with fresh qualified time.
func TestHeadChangeStaleSnapshotRefused(t *testing.T) {
	c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	clock = intervalAt(t, "2026-01-01T00:02:06Z")
	b := parseObject(t, p.CheckpointBody)
	b["at"] = "2026-01-01T00:02:06Z"
	p.CheckpointBody = wire(t, b)
	prepareRefused(t, c, p, s, clock, "journal_unavailable")
}

// An old caller timestamp cannot override a newer interval to issue an aged
// candidate. Final preparation separately preserves the verifier age boundary.
func TestStaleSnapshotRefused(t *testing.T) {
	_, _, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	clock = intervalAt(t, "2026-01-01T00:02:06Z")
	got, e := Issue(fixtureSigner(t), s, clock, "2026-01-01T00:01:05Z", nil)
	errorCode(t, e, TimeUnavailable)
	if got != nil {
		t.Fatal("aged candidate returned")
	}
}

// Equal-time changed content is a fork; equal-time exact content is not an idle
// issuance. Greater issue time is required even when the complete set is empty.
func TestEqualIssueTimeDifferentBytesRefused(t *testing.T) {
	_, _, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	got, e := Issue(fixtureSigner(t), s, clock, "2026-01-01T00:01:00Z", []Entry{{"record", record.Digest([]byte("revoked")), "2026-01-01T00:00:00Z"}})
	errorCode(t, e, Fork)
	if got != nil || !e.(*Error).Signal {
		t.Fatal("fork not signaled")
	}
	got, e = Issue(fixtureSigner(t), s, clock, "2026-01-01T00:01:00Z", nil)
	errorCode(t, e, Rollback)
	if got != nil {
		t.Fatal("equal-time candidate")
	}
}

// Both removal and postponing a previously retained (including future)
// revocation refuse. Adding entries and moving an effective time earlier pass.
func TestCumulativeRemovalRefused(t *testing.T) {
	_, _, s, _, _ := publicationFixture(t, "P-JRN-publish-reissue")
	retained := []Entry{{"record", record.Digest([]byte("revoked")), "2026-01-02T00:00:00Z"}}
	s = withRetainedEntries(t, s, retained)
	original := bytes.Clone(s.Snapshot)
	for _, rows := range [][]Entry{nil, {{"record", retained[0].ID, "2026-01-03T00:00:00Z"}}} {
		got, e := Issue(fixtureSigner(t), s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", rows)
		errorCode(t, e, CumulativeRemoval)
		if got != nil || !bytes.Equal(s.Snapshot, original) {
			t.Fatal("retained set changed")
		}
	}
	retained[0].From = "2026-01-01T00:00:00Z"
	if _, e := Issue(fixtureSigner(t), s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", retained); e != nil {
		t.Fatal(e)
	}
}

// The final boundary repeats cumulative checks even for a signed snapshot that
// did not come through Issue; bypassing the issuance helper cannot drop history.
func TestPreparedCumulativeRemovalRefused(t *testing.T) {
	c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	rows := []Entry{{"record", record.Digest([]byte("revoked")), "2026-01-02T00:00:00Z"}}
	s = withRetainedEntries(t, s, rows)
	prepareRefused(t, c, p, s, clock, string(CumulativeRemoval))
	bindPrior(t, &p, s)
	reissue := signFixture(t, map[string]any{"type": "revocation/1", "issuer": s.Registry, "complete": true, "issued_at": "2026-01-01T00:01:05Z", "entries": entryValues(rows)})
	bindSnapshot(t, &p, reissue)
	got, e := Prepare(c, fixtureSigner(t), s, clock, p)
	if e != nil {
		t.Fatal(e)
	}
	accepted(t, got.Result)
}

// Missing independently reconciled history is never automatic genesis; neither
// issuance nor journal preparation may replace the missing set with empty.
func TestGapRefused(t *testing.T) {
	c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	s.Available = false
	got, e := Issue(fixtureSigner(t), s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", nil)
	errorCode(t, e, Gap)
	if got != nil {
		t.Fatal("missing history issued")
	}
	prepareRefused(t, c, p, s, clock, string(Gap))
}

// Same-position changed head, checkpoint or snapshot is operator-visible fork
// evidence. Original inputs remain retained; no alternative head is selected.
func TestSplitHeadSignalsAndRefuses(t *testing.T) {
	for _, field := range []string{"head", "at", "snapshot"} {
		t.Run(field, func(t *testing.T) {
			c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
			b := object(parseObject(t, s.Checkpoint)["body"])
			if field == "at" {
				b[field] = "2026-01-01T00:01:11Z"
			} else {
				b[field] = record.Digest([]byte("split"))
			}
			p.CheckpointBody = wire(t, b)
			got, e := Prepare(c, fixtureSigner(t), s, clock, p)
			errorCode(t, e, Fork)
			if got != nil || !e.(*Error).Signal {
				t.Fatal("split not signaled")
			}
		})
	}
}

// Both independent capacity bounds refuse before custody, preserve the complete
// last-good bytes and signal; no lower subset is returned or re-signed.
func TestSnapshotCapacitySignalsWithoutDrop(t *testing.T) {
	_, _, s, _, _ := publicationFixture(t, "P-JRN-publish-reissue")
	for _, count := range []int{64, 65} {
		t.Run(strings.Repeat("x", count-63), func(t *testing.T) {
			rows := make([]Entry, count)
			for i := range rows {
				rows[i] = Entry{"record", record.Digest([]byte{byte(i)}), "2026-01-01T00:00:00Z"}
			}
			original := bytes.Clone(s.Snapshot)
			signer := &countSigner{Signer: fixtureSigner(t)}
			got, e := Issue(signer, s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", rows)
			errorCode(t, e, CapacityExhausted)
			if count == 64 {
				body := map[string]any{"type": "revocation/1", "issuer": s.Registry, "complete": true, "issued_at": "2026-01-01T00:01:05Z", "entries": entryValues(rows)}
				// 64 is count-valid but this exact payload already exceeds the
				// byte budget before even adding envelope/signature overhead.
				if len(wire(t, body)) <= MaxBytes {
					t.Fatal("byte-bound witness did not exceed byte limit")
				}
			}
			if got != nil || !e.(*Error).Signal || signer.calls != 0 || !bytes.Equal(original, s.Snapshot) {
				t.Fatal("capacity lost state or called custody")
			}
		})
	}

	c, p, retainedState, sample, _ := publicationFixture(t, "P-JRN-publish-reissue")
	p.Snapshot.Bytes = make([]byte, MaxBytes+1)
	signer := &countSigner{Signer: fixtureSigner(t)}
	prepared, e := Prepare(c, signer, retainedState, sample, p)
	errorCode(t, e, CapacityExhausted)
	if prepared != nil || !e.(*Error).Signal || signer.calls != 0 {
		t.Fatal("over-budget prepared snapshot reached custody")
	}
	if _, e := Issue(fixtureSigner(t), s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", []Entry{{"record", record.Digest([]byte("one")), "2026-01-01T00:00:00Z"}}); e != nil {
		t.Fatal(e)
	}
}

// Missing confidence, watermark or exclusive time authority forbids issuance
// and final preparation even if Context contains older healthy time facts.
func TestTimeLossNoPublication(t *testing.T) {
	for _, which := range []string{"absent", "confidence", "end", "past-end", "watermark", "backward"} {
		t.Run(which, func(t *testing.T) {
			c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
			switch which {
			case "absent":
				clock = qtime.Interval{}
			case "confidence":
				clock.Boot = ""
			case "end":
				clock.AuthorityUntil = clock.Upper
			case "past-end":
				clock.AuthorityUntil = intervalAt(t, "2026-01-01T00:01:19Z").Upper
			case "watermark":
				clock.JournalFloor = ""
			case "backward":
				clock.PriorHighWater = intervalAt(t, "2026-01-01T00:01:21Z").Upper
			}
			signer := &countSigner{Signer: fixtureSigner(t)}
			got, e := Issue(signer, s, clock, "2026-01-01T00:01:05Z", nil)
			errorCode(t, e, TimeUnavailable)
			if got != nil || signer.calls != 0 {
				t.Fatal("unqualified issuance")
			}
			prepared, e := Prepare(c, signer, s, clock, p)
			errorCode(t, e, TimeUnavailable)
			if prepared != nil || signer.calls != 0 {
				t.Fatal("unqualified publication")
			}
		})
	}
}

// The actual shared S4 three-entry fixture is reproduced exactly, including its
// reissue bytes, signed checkpoint, prior ID lookup and pinned verifier plan.
// New Build issuance uses its own interval timestamp and a later reissue entry.
func TestHeadChangeReissueAccepted(t *testing.T) {
	c, p, s, clock, expected := publicationFixture(t, "P-JRN-publish-reissue")
	issued, e := Issue(fixtureSigner(t), s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", nil)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(issued.Bytes, p.Snapshot.Bytes) {
		t.Fatal("reissue differs from shared snapshot")
	}
	got, e := Prepare(c, fixtureSigner(t), s, clock, p)
	if e != nil {
		t.Fatal(e)
	}
	accepted(t, got.Result)
	expectedPlan := object(object(loadFixture(t, "P-JRN-publish-reissue")["expected"])["plan"])
	if !bytes.Equal(got.Publication.Candidate, expected) || got.Result.Plan.IDs[0] != expectedPlan["ids"].([]any)[0] || got.Result.Plan.Watermark != expectedPlan["watermark"] || !got.Publication.IDJournalAvailable {
		t.Fatal("proof differs from original")
	}
	accepted(t, journal.Verify(got.Publication.Context, got.Publication.Entries, got.Publication.Candidate, nil))
	built, e := Build(c, fixtureSigner(t), s, clock, Change{Anchor: p.Entries[0], RecordBody: wire(t, parseObject(t, p.Entries[1])["body"]), IssuedAt: "2026-01-01T00:01:20Z", CheckpointAt: "2026-01-01T00:01:20Z", Witness: p.Witness})
	if e != nil {
		t.Fatal(e)
	}
	accepted(t, built.Result)
	expectedSnapshot, expectedEntry, expectedCheckpoint := projectedFixture(t, p, "2026-01-01T00:01:20Z")
	if !bytes.Equal(built.Publication.Snapshot, expectedSnapshot) || !bytes.Equal(built.Publication.Candidate, expectedCheckpoint) || !bytes.Equal(built.Publication.Entries[2], expectedEntry) {
		t.Fatal("projected construction differs from exact expected bytes")
	}
	for i := 0; i < 2; i++ {
		if !bytes.Equal(built.Publication.Entries[i], p.Entries[i]) {
			t.Fatal("original record entries differ")
		}
	}

}

// Fresh complete construction preserves exact closed profile/ID and owns its
// bytes; it does not claim durable publication or external time qualification.
func TestFreshCompleteSnapshotAccepted(t *testing.T) {
	_, _, s, _, _ := publicationFixture(t, "P-JRN-publish-reissue")
	got, e := Issue(fixtureSigner(t), s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", nil)
	if e != nil {
		t.Fatal(e)
	}
	envelope := parseObject(t, got.Bytes)
	b := object(envelope["body"])
	id, _ := record.ContentID("revocation/1", b)
	if len(b) != 5 || b["complete"] != true || len(b["entries"].([]any)) != 0 || id != envelope["id"] || got.ID != id {
		t.Fatal("not exact complete profile")
	}
}

// The legacy consumer admits age 60 inclusive and refuses 61 seconds using the
// snapshot's own issue time; no freshness-receipt bypass is installed.
func TestConsumerAgeBoundary(t *testing.T) {
	for _, age := range []int{60, 61} {
		t.Run((time.Duration(age) * time.Second).String(), func(t *testing.T) {
			c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
			at, _ := time.Parse(time.RFC3339, "2026-01-01T00:01:05Z")
			now := at.Add(time.Duration(age) * time.Second).Format(time.RFC3339)
			clock = intervalAt(t, now)
			b := parseObject(t, p.CheckpointBody)
			b["at"] = now
			p.CheckpointBody = wire(t, b)
			got, e := Prepare(c, fixtureSigner(t), s, clock, p)
			if age == 60 {
				if e != nil {
					t.Fatal(e)
				}
				accepted(t, got.Result)
			} else {
				errorCode(t, e, Code("journal_unavailable"))
				if got != nil {
					t.Fatal("aged proof returned")
				}
			}
		})
	}
}

// There is no authenticated gossip source in this slice; a fake tuple cannot
// qualify matching-head discovery or absence of undisclosed publication.
func TestMatchingGossipHeadAccepted(t *testing.T) {
	t.Skip("authenticated gossip source is not implemented; no matching-head qualification claimed")
}

type countSigner struct {
	journal.Signer
	calls int
}

func (s *countSigner) Sign(b []byte) ([]byte, error) { s.calls++; return s.Signer.Sign(b) }
func withRetainedEntries(t *testing.T, s Retained, rows []Entry) Retained {
	t.Helper()
	b := object(parseObject(t, s.Snapshot)["body"])
	b["entries"] = entryValues(rows)
	s.Snapshot = signFixture(t, b)
	cp := object(parseObject(t, s.Checkpoint)["body"])
	cp["snapshot"] = parseObject(t, s.Snapshot)["id"]
	s.Checkpoint = signFixture(t, cp)
	return s
}

// Input and profile failures are typed, bounded and have no returned candidate;
// the valid key/record set is a nearby positive rather than rejection of all.
func TestInvalidSnapshotEntriesRefused(t *testing.T) {
	_, _, s, _, _ := publicationFixture(t, "P-JRN-publish-reissue")
	good := Entry{"record", record.Digest([]byte("one")), "2026-01-01T00:00:00Z"}
	for _, rows := range [][]Entry{{{"other", good.ID, good.From}}, {{"key", "invalid", good.From}}, {{good.Kind, "sha256:invalid", good.From}}, {{good.Kind, good.ID, "2026-02-30T00:00:00Z"}}, {good, good}} {
		got, e := Issue(fixtureSigner(t), s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", rows)
		errorCode(t, e, Malformed)
		if got != nil {
			t.Fatal("invalid issued")
		}
	}
	rows := []Entry{good, {"key", s.Registry, "2026-01-02T00:00:00Z"}}
	if _, e := Issue(fixtureSigner(t), s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", rows); e != nil {
		t.Fatal(e)
	}
}

// Reaching the advisory byte threshold still retains and signs all entries;
// exceeding the hard bound is tested separately and never yields this candidate.
func TestSnapshotCapacityWarningAccepted(t *testing.T) {
	_, _, s, _, _ := publicationFixture(t, "P-JRN-publish-reissue")
	rows := make([]Entry, 56)
	for i := range rows {
		rows[i] = Entry{"record", record.Digest([]byte{byte(i)}), "2026-01-01T00:00:00Z"}
	}
	got, e := Issue(fixtureSigner(t), s, intervalAt(t, "2026-01-01T00:01:05Z"), "2026-01-01T00:01:05Z", rows)
	if e != nil {
		t.Fatal(e)
	}
	if !got.CapacityWarning || len(object(parseObject(t, got.Bytes)["body"])["entries"].([]any)) != len(rows) {
		t.Fatal("warning lost entries")
	}
}

// A new signature at a lower/equal issue time cannot revive authority; future
// issuance fails at the conservative lower bound. The exact old bytes survive.
func TestIssueTimeOrderingRefused(t *testing.T) {
	_, _, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	for _, tc := range []struct {
		at   string
		code Code
	}{{"2026-01-01T00:00:59Z", Rollback}, {"2026-01-01T00:01:21Z", TimeUnavailable}, {"2026-02-30T00:00:00Z", Malformed}} {
		got, e := Issue(fixtureSigner(t), s, clock, tc.at, nil)
		errorCode(t, e, tc.code)
		if got != nil {
			t.Fatal("invalid issue time")
		}
	}
	c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	p.Snapshot.Bytes = bytes.Clone(s.Snapshot)
	prepareRefused(t, c, p, s, clock, string(Rollback))
	b := object(parseObject(t, s.Snapshot)["body"])
	b["entries"] = entryValues([]Entry{{"record", record.Digest([]byte("changed")), "2026-01-01T00:00:00Z"}})
	p.Snapshot.Bytes = signFixture(t, b)
	prepareRefused(t, c, p, s, clock, string(Fork))
}

// Private candidate copies cannot overwrite the retained immutable base or
// alias caller buffers when a downstream adapter edits its owned output.
func TestCandidateOwnsBytes(t *testing.T) {
	c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	original := bytes.Clone(s.Snapshot)
	got, e := Prepare(c, fixtureSigner(t), s, clock, p)
	if e != nil {
		t.Fatal(e)
	}
	got.Publication.Snapshot[0] = ' '
	got.Publication.Entries[0][0] = ' '
	if !bytes.Equal(original, s.Snapshot) || p.Snapshot.Bytes[0] == ' ' || p.Entries[0][0] == ' ' {
		t.Fatal("candidate aliases inputs")
	}
}
func bindSnapshot(t *testing.T, p *journal.Publication, raw []byte) {
	t.Helper()
	p.Snapshot.Bytes = raw
	last := object(parseObject(t, p.Entries[2])["body"])
	last["record"] = parseObject(t, raw)["id"]
	var e error
	p.Entries[2], e = journal.BuildEntry(wire(t, last), text(object(parseObject(t, p.Entries[1])["body"])["at"]))
	if e != nil {
		t.Fatal(e)
	}
	cp := parseObject(t, p.CheckpointBody)
	cp["snapshot"] = parseObject(t, raw)["id"]
	cp["head"] = parseObject(t, p.Entries[2])["id"]
	p.CheckpointBody = wire(t, cp)
}
func bindPrior(t *testing.T, p *journal.Publication, s Retained) {
	t.Helper()
	p.PriorCheckpoint = s.Checkpoint
	w := parseObject(t, p.Witness.Bytes())
	w["checkpoint_id"] = parseObject(t, s.Checkpoint)["id"]
	var e error
	p.Witness, e = journal.ReduceProjection(wire(t, w), journal.Originals{Registry: s.Registry, Owner: s.Owner, Checkpoint: s.Checkpoint, CandidateDigest: text(w["candidate_digest"])}, nil)
	if e != nil {
		t.Fatal(e)
	}
}

// A fork detected by the pinned verifier also signals even if the caller's
// independently retained base and the challenged witness view differ.
func TestJournalForkSignalPreserved(t *testing.T) {
	c, p, s, clock, cp := publicationFixture(t, "P-JRN-publish-reissue")
	cb := object(parseObject(t, cp)["body"])
	w := parseObject(t, p.Witness.Bytes())
	w["registry_seq"] = cb["seq"]
	w["registry_head"] = cb["head"]
	w["checkpoint_id"] = parseObject(t, cp)["id"]
	w["checkpoint_time"] = cb["at"]
	priorID := text(parseObject(t, s.Checkpoint)["id"])
	w["previous_checkpoint_id"] = priorID
	var e error
	p.Witness, e = journal.ReduceProjection(wire(t, w), journal.Originals{Registry: s.Registry, Owner: s.Owner, Checkpoint: cp, CandidateDigest: text(w["candidate_digest"]), PreviousCheckpointID: &priorID}, nil)
	if e != nil {
		t.Fatal(e)
	}
	p.PriorCheckpoint = cp
	cb["at"] = "2026-01-01T00:01:21Z"
	p.CheckpointBody = wire(t, cb)
	clock = intervalAt(t, text(cb["at"]))
	got, e := Prepare(c, fixtureSigner(t), s, clock, p)
	errorCode(t, e, Code("journal_fork"))
	if got != nil || !e.(*Error).Signal {
		t.Fatal("pinned fork signal lost")
	}
}

// Broken anchor links cannot be repaired by the transaction builder. A zero
// record body and an unavailable qualified time also yield no private candidate.
func TestBuildMissingLinksRefused(t *testing.T) {
	c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	body := object(parseObject(t, p.Entries[1])["body"])
	body["prev"] = record.Digest([]byte("wrong anchor"))
	change := Change{Anchor: p.Entries[0], RecordBody: wire(t, body), IssuedAt: "2026-01-01T00:01:20Z", CheckpointAt: "2026-01-01T00:01:20Z", Witness: p.Witness}
	got, e := Build(c, fixtureSigner(t), s, clock, change)
	errorCode(t, e, Code("journal_chain_broken"))
	if got != nil {
		t.Fatal("broken link returned candidate")
	}
	change.RecordBody = nil
	got, e = Build(c, fixtureSigner(t), s, clock, change)
	errorCode(t, e, Code("journal_malformed"))
	if got != nil {
		t.Fatal("missing record returned candidate")
	}
	got, e = Build(c, fixtureSigner(t), s, qtime.Interval{}, change)
	errorCode(t, e, TimeUnavailable)
	if got != nil {
		t.Fatal("missing time returned candidate")
	}
}

// Independently encode the published fixture with the new lower issue second
// and same-transaction reissue timestamp, without using the candidate builder.
func projectedFixture(t *testing.T, p journal.Publication, at string) ([]byte, []byte, []byte) {
	t.Helper()
	body := object(parseObject(t, p.Snapshot.Bytes)["body"])
	body["issued_at"] = at
	snapshot := signFixture(t, body)
	snapshotID := parseObject(t, snapshot)["id"]
	entry := object(parseObject(t, p.Entries[2])["body"])
	entry["record"], entry["at"] = snapshotID, at
	id, e := record.ContentID("journal-entry/1", entry)
	if e != nil {
		t.Fatal(e)
	}
	rawEntry := wire(t, map[string]any{"version": "swarm-envelope/2", "body": entry, "id": id})
	cp := parseObject(t, p.CheckpointBody)
	cp["snapshot"], cp["head"] = snapshotID, id
	return snapshot, rawEntry, signFixture(t, cp)
}
