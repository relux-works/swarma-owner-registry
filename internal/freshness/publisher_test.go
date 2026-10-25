package freshness_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"

	f "github.com/relux-works/swarma-owner-registry/internal/freshness"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

// Global publication sequence survives a real head advance. The independent
// published rollback negative refuses, while the constructed advance is seq 2.
func TestFreshnessBackwardSeqRefused(t *testing.T) {
	fixtureVerdict(t, "N-FR-rollback", "freshness_rollback")
	p := publisher(t, initial(t), &persistence{})
	in := input(t)
	publish(t, p, in)
	b, _ := publish(t, p, advanced(t, in))
	body := object(parsed(t, b)["body"])
	if body["publication_seq"] != int64(2) || body["registry_seq"] != int64(3) {
		t.Fatal("head advance reset global publication sequence")
	}
}

// Same-head continuation needs its predecessor; an advanced head needs null.
// Both forbidden prev variants refuse; the actual publisher must produce null.
func TestFreshnessPredecessorMissingRefused(t *testing.T) {
	fixtureVerdict(t, "N-FR-prev-null", "freshness_gap")
	fixtureVerdict(t, "N-FR-head-prev", "freshness_gap")
	p := publisher(t, initial(t), &persistence{})
	in := input(t)
	publish(t, p, in)
	b, _ := publish(t, p, advanced(t, in))
	if object(parsed(t, b)["body"])["prev"] != nil {
		t.Fatal("advanced head kept predecessor")
	}
}

// Omitted acknowledged history cannot pass the independent exact-state floor;
// complete retained history can restart under an externally reconciled receipt.
func TestFreshnessHistoryOmissionRefused(t *testing.T) {
	p := publisher(t, initial(t), &persistence{})
	in := input(t)
	publish(t, p, in)
	publish(t, p, at(t, in, "2026-01-01T00:03:20Z"))
	state := p.LastResolved()
	floor, e := f.StateID(state)
	if e != nil {
		t.Fatal(e)
	}
	publisher(t, state, &persistence{})
	state.History = state.History[1:]
	denied, err := f.New(f.Trust{Registry: state.Genesis.RegistryID, Owner: state.Genesis.Owner, Signer: signer(t), Retained: state, Persistence: &persistence{}, Recovery: f.Recovery{StateID: floor, Reconciled: true}})
	stateRefused(t, denied, err)
}

// A local restored counter, absent independent reconciliation, and unknown or
// mismatched persistence acknowledgement all close publication. No durability
// or real off-node reconciliation is supplied by these test doubles.
func TestRestoredFreshnessStateFences(t *testing.T) {
	p := publisher(t, initial(t), &persistence{})
	stale := p.LastResolved()
	publish(t, p, input(t))
	latest := p.LastResolved()
	floor, _ := f.StateID(latest)
	for _, state := range []f.State{stale, latest} {
		recovered, e := f.New(f.Trust{Registry: state.Genesis.RegistryID, Owner: state.Genesis.Owner, Signer: signer(t), Retained: state, Persistence: &persistence{}, Recovery: f.Recovery{StateID: floor}})
		stateRefused(t, recovered, e)
	}
	restored, e := f.New(f.Trust{Registry: stale.Genesis.RegistryID, Owner: stale.Genesis.Owner, Signer: signer(t), Retained: stale, Persistence: &persistence{}, Recovery: f.Recovery{StateID: floor, Reconciled: true, GenesisAuthorized: true}})
	stateRefused(t, restored, e)
	for _, wrong := range []bool{false, true} {
		store := &persistence{fail: !wrong, wrong: wrong}
		x := publisher(t, initial(t), store)
		before := x.LastResolved()
		b, r := x.Publish(input(t))
		noCandidate(t, b, r, "freshness_persistence_unavailable")
		if !x.Fenced() || store.calls != 1 {
			t.Fatal("unknown durability did not fence")
		}
		unchanged(t, before, x.LastResolved())
		b, r = x.Publish(input(t))
		noCandidate(t, b, r, "freshness_state_unavailable")
		if store.calls != 1 {
			t.Fatal("fenced retry wrote")
		}
	}
	publisher(t, latest, &persistence{})
}

// Quiet publications keep exact base/checkpoint bytes and the witnessed head;
// successful persistence precedes visibility and the complete archive grows.
func TestFreshnessKeepsJournalHead(t *testing.T) {
	store := &persistence{}
	p := publisher(t, initial(t), store)
	in := input(t)
	b, _ := publish(t, p, in)
	_, expected, _, _ := proof(t, "P-FR-genesis")
	if !bytes.Equal(b, expected[0]) {
		t.Fatal("built genesis differs from published exact bytes")
	}
	first := parsed(t, b)["id"]
	next, _ := publish(t, p, at(t, in, "2026-01-01T00:03:20Z"))
	body := object(parsed(t, next)["body"])
	if body["prev"] != first || body["publication_seq"] != int64(2) {
		t.Fatal("same-head chain broken")
	}
	state := p.LastResolved()
	if store.calls != 2 || len(state.History) != 2 {
		t.Fatal("missing persistence/history")
	}
	for _, entry := range state.History {
		if !bytes.Equal(entry.Checkpoint, in.Checkpoint) || !bytes.Equal(entry.Snapshot, in.Snapshot.Bytes) || !bytes.Equal(entry.Witness, in.Witness.Original) {
			t.Fatal("quiet tick changed retained base/head evidence")
		}
	}
	if state.Anchor.RegistrySeq != state.Genesis.RegistrySeq || state.Anchor.CheckpointID != state.Genesis.CheckpointID || state.Anchor.Snapshot != state.Genesis.Snapshot {
		t.Fatal("quiet publication changed journal position")
	}
	state.History[0].Bytes[0] = '!'
	if p.LastResolved().History[0].Bytes[0] == '!' {
		t.Fatal("accessor leaked owned history")
	}
}

// Exact retry returns the first accepted bytes and code/plan, without signing,
// a new position or persistence; a stale retry never renews that outcome.
func TestFreshnessAtomicRetry(t *testing.T) {
	store := &persistence{}
	s := &countingSigner{inner: signer(t)}
	p := publisherWithSigner(t, initial(t), store, s)
	in := input(t)
	b, r := publish(t, p, in)
	before := p.LastResolved()
	again, out := publish(t, p, in)
	if !bytes.Equal(b, again) || !reflect.DeepEqual(r, out) || store.calls != 1 || s.calls != 1 {
		t.Fatal("retry changed accepted position/outcome")
	}
	unchanged(t, before, p.LastResolved())
	stale := at(t, in, "2026-01-01T00:05:00Z")
	denied, refusal := p.Retry(stale)
	noCandidate(t, denied, refusal, "freshness_unavailable")
	unchanged(t, before, p.LastResolved())
	if store.calls != 1 || s.calls != 1 {
		t.Fatal("stale retry persisted")
	}
}

// The full freshness path admits a two-minute-old unchanged base; this does
// not waive the unchanged legacy attestation profile's own snapshot-age gate.
func TestAgedBaseAcceptedByVerifyFreshness(t *testing.T) {
	fixtureVerdict(t, "P-FR-old-snapshot", "accept")
	p := publisher(t, initial(t), &persistence{})
	publish(t, p, input(t))
}

// S3-compatible legacy verification still checks the registry snapshot's own
// age. This tests the retained legacy path in the pinned build, not an old binary.
func TestOldS3RejectsAgedBase(t *testing.T) {
	c, _, _, d := proof(t, "P-FR-genesis")
	facts := object(d["fixture"])
	refs := object(facts["agent_attestation_ids"])
	var key, reference string
	for k, v := range refs {
		key, reference = k, text(v)
	}
	_, err := verifier.New(c).VerifyAttestation(key, reference, false)
	var typed *record.Error
	if !errors.As(err, &typed) || typed.Code != "key_binding_unavailable" {
		t.Fatalf("aged legacy base: %v", err)
	}
	in := input(t)
	body := object(parsed(t, in.Snapshot.Bytes)["body"])
	body["issued_at"] = issueAt(t, in)
	fresh, e := signer(t).Sign(wire(t, body))
	if e != nil {
		t.Fatal(e)
	}
	snapshots := map[string]any{}
	for root, row := range object(facts["revocations"]) {
		copy := map[string]any{}
		for k, v := range object(row) {
			copy[k] = v
		}
		copy["snapshot"] = decode(t, copy["snapshot"])
		snapshots[root] = copy
	}
	root := initial(t).Genesis.RegistryID
	object(snapshots[root])["snapshot"] = fresh
	c.SignedSnapshots = fact(t, snapshots)
	if _, err = verifier.New(c).VerifyAttestation(key, reference, false); err != nil {
		t.Fatalf("current legacy control: %v", err)
	}
}

// Refused self-checks produce no candidate/plan, no persistence and no state
// advance. A future publication must not roll back global issued_at on advance.
func TestPublisherRefusalHasNoEffects(t *testing.T) {
	store := &persistence{}
	p := publisher(t, initial(t), store)
	in := input(t)
	publish(t, p, in)
	before := p.LastResolved()
	bad := advanced(t, in)
	bad.Time = in.Time
	bad.Time.SampleID = "sample-other"
	snap := object(parsed(t, bad.Snapshot.Bytes)["body"])
	snap["issued_at"] = issueAt(t, in)
	var e error
	bad.Snapshot.Bytes, e = signer(t).Sign(wire(t, snap))
	if e != nil {
		t.Fatal(e)
	}
	cp := object(parsed(t, bad.Checkpoint)["body"])
	cp["at"] = issueAt(t, in)
	cp["snapshot"] = parsed(t, bad.Snapshot.Bytes)["id"]
	bad.Checkpoint, e = signer(t).Sign(wire(t, cp))
	if e != nil {
		t.Fatal(e)
	}
	witness := parsed(t, bad.Witness.Original)
	witness["checkpoint_id"] = parsed(t, bad.Checkpoint)["id"]
	bad.Witness.Fact = fact(t, witness)
	bad.Witness.Original = wire(t, witness)
	b, r := p.Publish(bad)
	noCandidate(t, b, r, "time_unavailable")
	unchanged(t, before, p.LastResolved())
	if store.calls != 1 {
		t.Fatal("refusal persisted")
	}
	publish(t, p, advanced(t, in))
}

// Explicit genesis can publish at Unix epoch; a real epoch predecessor still
// requires strictly greater time. The caller-owned anchor remains untouched.
func TestFreshnessEpochGenesis(t *testing.T) {
	c, _, cp, d := proof(t, "P-FR-genesis-epoch")
	v := object(d["fixture"])
	a := anchor(t, object(v["freshness_anchor"]))
	state := f.State{Genesis: a, Anchor: a, Watermark: text(v["watermark"]), DecisionFloor: "-1000000000", Boot: "boot-a"}
	before := state.Clone()
	p := publisher(t, state, &persistence{})
	root := a.RegistryID
	row := object(object(v["revocations"])[root])
	in := f.Input{Context: c, Time: intervalAt(t, text(v["now"])), Checkpoint: cp, Snapshot: input(t).Snapshot, Witness: f.WitnessRead{Fact: c.JournalWitness, ReadAt: text(v["witness_read_at"]), Original: wire(t, v["journal_witness"])}}
	in.Snapshot.Bytes = decode(t, row["snapshot"])
	first, _ := publish(t, p, in)
	if object(parsed(t, first)["body"])["issued_at"] != "1970-01-01T00:00:00Z" {
		t.Fatal("epoch genesis changed")
	}
	publish(t, p, at(t, in, "1970-01-01T00:00:01Z"))
	unchanged(t, before, state)
}

// Constructor/accessor copies isolate caller-owned history, anchors and plans;
// mutating returned recovery evidence cannot alter the live publisher.
func TestFreshnessStateCopies(t *testing.T) {
	state := initial(t)
	before := state.Clone()
	p := publisher(t, state, &persistence{})
	publish(t, p, input(t))
	unchanged(t, before, state)
	retained := p.LastResolved()
	id, e := f.StateID(retained)
	if e != nil {
		t.Fatal(e)
	}
	copy := retained.Clone()
	copy.History[0].Outcome.Plan.IDs[0] = "changed"
	*copy.Anchor.FreshnessID = "changed"
	unchanged(t, retained, p.LastResolved())
	current, e := f.StateID(p.LastResolved())
	if e != nil || id != current {
		t.Fatal("accessor mutated manifest")
	}
}

// Complete retained history is not the verifier's bounded per-call proof. More
// than 64 publications remain archived and restart against an exact floor.
func TestFreshnessCompleteHistoryRetained(t *testing.T) {
	p := publisher(t, initial(t), &persistence{})
	in := input(t)
	start, _ := time.Parse(time.RFC3339, issueAt(t, in))
	for i := 0; i < 65; i++ {
		publish(t, p, at(t, in, start.Add(time.Duration(i)*20*time.Second).Format(time.RFC3339)))
	}
	state := p.LastResolved()
	if len(state.History) != 65 || state.Anchor.PublicationSeq != 65 {
		t.Fatal("archive was truncated to a proof window")
	}
	publisher(t, state, &persistence{})
}

// A substituted valid body, invalid signature, malformed envelope and custody
// failure expose no candidate/plan or persistence. The real closed signer works.
func TestPublisherChecksSignerOutput(t *testing.T) {
	_, bad, _, _ := proof(t, "N-FR-signer-operator")
	for _, tc := range []struct {
		name, code string
		sign       signerFunc
	}{
		{"substituted-body", "signer_signature_invalid", func(raw []byte) ([]byte, error) {
			b := parsed(t, raw)
			b["issued_at"] = "2026-01-01T00:02:59Z"
			return signer(t).Sign(wire(t, b))
		}},
		{"wrong-root", "freshness_signature_invalid", func([]byte) ([]byte, error) { return bad[0], nil }},
		{"malformed", "freshness_malformed", func([]byte) ([]byte, error) { return []byte("{"), nil }},
		{"unavailable", "signer_unavailable", func([]byte) ([]byte, error) { return nil, errors.New("custody diagnostics") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &persistence{}
			p := publisherWithSigner(t, initial(t), store, tc.sign)
			before := p.LastResolved()
			b, r := p.Publish(input(t))
			noCandidate(t, b, r, tc.code)
			unchanged(t, before, p.LastResolved())
			if store.calls != 0 {
				t.Fatal("signer refusal persisted")
			}
		})
	}
	publish(t, publisher(t, initial(t), &persistence{}), input(t))
}

// Missing independently reconciled state and missing persistence are closed;
// neither an empty configuration nor absent history silently authorizes genesis.
func TestFreshnessExplicitGenesisRequired(t *testing.T) {
	p, e := f.New(f.Trust{})
	stateRefused(t, p, e)
	state := initial(t)
	id, _ := f.StateID(state)
	for _, authorized := range []bool{false, true} {
		trust := f.Trust{Registry: state.Genesis.RegistryID, Owner: state.Genesis.Owner, Signer: signer(t), Retained: state, Recovery: f.Recovery{StateID: id, Reconciled: true, GenesisAuthorized: authorized}}
		if !authorized {
			trust.Persistence = &persistence{}
		}
		p, e = f.New(trust)
		stateRefused(t, p, e)
	}
	publish(t, publisher(t, state, &persistence{}), input(t))
}
