package freshness_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/swarma-envelope/record"
	f "github.com/relux-works/swarma-owner-registry/internal/freshness"
	"github.com/relux-works/swarma-owner-registry/internal/journal"
)

type currencyStore struct {
	persistence
	preflights    int
	capacityError bool
}

func (p *currencyStore) Preflight(_ string, _ int) error {
	p.preflights++
	if p.capacityError {
		return errors.New("capacity unavailable")
	}
	return nil
}
func currencyInput(t *testing.T) (f.Input, f.State, map[string]string, journal.Signer) {
	t.Helper()
	in := inputNamed(t, "P-CU-grant")
	_, _, _, d := proof(t, "P-CU-grant")
	facts := object(d["fixture"])
	j := object(facts["snapshot_journal"])
	p := &f.CurrencyProof{Anchor: wire(t, j["anchor"]), Entries: make([][]byte, 0), Originals: map[string][]byte{}, Snapshots: map[string]journal.SnapshotFacts{}}
	for _, b := range j["entries"].([]any) {
		p.Entries = append(p.Entries, decode(t, b))
	}
	for id, b := range object(facts["seen"]) {
		p.Originals[id] = decode(t, b)
	}
	for k, v := range object(facts["revocations"]) {
		r := object(v)
		p.Snapshots[k] = journal.SnapshotFacts{Bytes: decode(t, r["snapshot"]), Available: r["availability"] == "available", RollbackSafe: r["rollback_safe"] == true, MaxAge: r["max_age"].(int64)}
	}
	p.Originals[text(parsed(t, in.Checkpoint)["id"])] = bytes.Clone(in.Checkpoint)
	in.Currency = p
	a := anchor(t, object(facts["freshness_anchor"]))
	a.RegistryCurrencyVersion = 2
	roots := map[string]string{"registry-owner": a.RegistryID, "grant": text(facts["root"]), "operator": text(facts["operator_root"])}
	pin, e := f.CurrencyPinIdentity(a.RegistryID, a.Owner, roots)
	if e != nil {
		t.Fatal(e)
	}
	state := f.State{Version: 2, PinIdentity: pin, Currency: p, Genesis: a, Anchor: a, Watermark: text(facts["watermark"]), DecisionFloor: "-1000000000", Boot: "boot-a"}
	seed := sha256.Sum256([]byte("waggle public test registry revocation seed"))
	key := ed25519.NewKeyFromSeed(seed[:])
	s, e := journal.NewCurrencySigner(a.RegistryID, a.Owner, roots, service(func(b []byte) ([]byte, error) { return ed25519.Sign(key, b), nil }))
	if e != nil {
		t.Fatal(e)
	}
	return in, state, roots, s
}
func currencyPublisher(t *testing.T, state f.State, roots map[string]string, s journal.Signer, p *currencyStore) *f.Publisher {
	t.Helper()
	id, e := f.StateID(state)
	if e != nil {
		t.Fatal(e)
	}
	x, e := f.New(f.Trust{Registry: state.Genesis.RegistryID, Owner: state.Genesis.Owner, Signer: s, Retained: state, Persistence: p, CurrencyRoots: roots, Recovery: f.Recovery{StateID: id, Reconciled: true, GenesisAuthorized: state.Anchor.PublicationSeq == 0}})
	if e != nil {
		t.Fatal(e)
	}
	return x
}

// The real closed-profile signer/publisher emits the exact published /2 vector,
// retains every root original/index proof, and repeats the checkpoint coverage
// on idle ticks without adding journal positions. Capacity/commit are test
// doubles; no actual durability, root application or activation is qualified.
func TestCurrencyPublisherExactCoverageAndDetachedState(t *testing.T) {
	in, state, roots, s := currencyInput(t)
	store := &currencyStore{}
	count := &countingSigner{inner: s}
	p := currencyPublisher(t, state, roots, count, store)
	raw, out := publish(t, p, in)
	_, expected, _, _ := proof(t, "P-CU-grant")
	if !bytes.Equal(raw, expected[0]) || out.Plan.SnapshotJournal == nil || store.calls != 1 || store.preflights != 1 {
		t.Fatal("producer differs from S8", out, store.calls, store.preflights)
	}
	saved := p.LastResolved()
	returned := p.LastResolved()
	*returned.History[0].Outcome.Plan.SnapshotJournal.Latest[0].SnapshotID = "changed"
	returned.History[0].Currency.Entries[0][0] = '!'
	returned.Anchor.Snapshots[0].SnapshotID = "changed"
	*out.Plan.SnapshotJournal.Latest[0].SnapshotID = "changed"
	if !reflect.DeepEqual(saved, p.LastResolved()) {
		t.Fatal("retained publisher alias")
	}
	again, _ := publish(t, p, in)
	if !bytes.Equal(raw, again) || count.calls != 1 || store.calls != 1 {
		t.Fatal("exact retry renewed")
	}
	next := at(t, in, "2026-01-01T00:01:01Z")
	later, _ := publish(t, p, next)
	if !reflect.DeepEqual(object(parsed(t, raw)["body"])["snapshots"], object(parsed(t, later)["body"])["snapshots"]) || p.LastResolved().Anchor.RegistrySeq != state.Anchor.RegistrySeq {
		t.Fatal("quiet publication changed coverage/head")
	}
	// Versioned codec restores byte arrays and nullable journal endpoints exactly.
	encoded, e := json.Marshal(saved)
	if e != nil {
		t.Fatal(e)
	}
	var restored f.State
	if e = json.Unmarshal(encoded, &restored); e != nil {
		t.Fatal(e)
	}
	a, _ := f.StateID(saved)
	b, _ := f.StateID(restored)
	if a != b {
		t.Fatal("versioned codec changed identity")
	}
	currencyPublisher(t, restored, roots, s, &currencyStore{})
}

// Missing history/old policy cannot seed /2 recovery; capacity refusal invokes
// neither custody nor Commit, while an uncertain Commit fences and exposes no
// candidate. The nearby successful case exercises those same real code paths.
func TestCurrencyPublisherCapacityRecoveryAndCommitRefused(t *testing.T) {
	in, state, roots, s := currencyInput(t)
	store := &currencyStore{capacityError: true}
	count := &countingSigner{inner: s}
	p := currencyPublisher(t, state, roots, count, store)
	before := p.LastResolved()
	raw, out := p.Publish(in)
	noCandidate(t, raw, out, "freshness_capacity_unavailable")
	if count.calls != 0 || store.calls != 0 {
		t.Fatal("capacity refusal had effects")
	}
	unchanged(t, before, p.LastResolved())
	store.capacityError = false
	publish(t, p, in)
	floor, _ := f.StateID(state)
	wrong := state
	wrong.PinIdentity = "changed"
	denied, e := f.New(f.Trust{Registry: state.Genesis.RegistryID, Owner: state.Genesis.Owner, Signer: s, Retained: wrong, Persistence: store, CurrencyRoots: roots, Recovery: f.Recovery{StateID: floor, Reconciled: true, GenesisAuthorized: true}})
	stateRefused(t, denied, e)
	badStore := &currencyStore{persistence: persistence{wrong: true}}
	p = currencyPublisher(t, state, roots, s, badStore)
	raw, out = p.Publish(in)
	noCandidate(t, raw, out, "freshness_persistence_unavailable")
	if !p.Fenced() {
		t.Fatal("uncertain commit did not fence")
	}
	bad := in
	bad.Currency = nil
	p = currencyPublisher(t, state, roots, s, &currencyStore{})
	raw, out = p.Publish(bad)
	noCandidate(t, raw, out, "freshness_unavailable")
	bad = in
	bad.Context.RegistryCurrencyVersion = 1
	raw, out = p.Publish(bad)
	noCandidate(t, raw, out, "registry_currency_version_mismatch")
}

// An incomplete prefix cannot mint a /2 outcome, and unused oversized retained
// originals hit the capacity gate before signing; neither failure commits.
func TestCurrencyPublisherIncompleteAndOversizedProofRefused(t *testing.T) {
	for _, which := range []string{"gap", "oversized"} {
		in, state, roots, s := currencyInput(t)
		store := &currencyStore{}
		count := &countingSigner{inner: s}
		p := currencyPublisher(t, state, roots, count, store)
		if which == "gap" {
			in.Currency.Entries = in.Currency.Entries[1:]
		} else {
			in.Currency.Originals["unused"] = make([]byte, 8193)
		}
		raw, out := p.Publish(in)
		want := "freshness_state_invalid"
		if which == "oversized" {
			want = "freshness_malformed"
		}
		noCandidate(t, raw, out, want)
		if store.calls != 0 || which == "oversized" && count.calls != 0 {
			t.Fatal("refusal effects", store.calls, count.calls)
		}
	}
}

// The original /1 JSON members and local digest prefix survive the repin,
// including zero values. /2 selects a distinct codec and never emits snapshot.
func TestLegacyStateCodecBytesUnchanged(t *testing.T) {
	anchor := `{"registry_id":"","owner":"","publication_seq":0,"freshness_id":null,"issued_at":null,"snapshot":"","registry_seq":0,"checkpoint_id":""}`
	encoded, e := json.Marshal(f.Anchor{})
	if e != nil || string(encoded) != anchor {
		t.Fatal("legacy anchor bytes", string(encoded), e)
	}
	want := `{"genesis":` + anchor + `,"anchor":` + anchor + `,"history":null,"watermark":"","decision_floor":"","boot":""}`
	encoded, e = json.Marshal(f.State{})
	if e != nil || string(encoded) != want {
		t.Fatal("legacy state bytes", string(encoded), e)
	}
	got, e := f.StateID(f.State{})
	if e != nil || got != record.Digest(append([]byte("freshness-state-v1\n"), []byte(want)...)) {
		t.Fatal("legacy digest", got, e)
	}
	_, s, _, _ := currencyInput(t)
	encoded, e = json.Marshal(s.Anchor)
	if e != nil || bytes.Contains(encoded, []byte(`"snapshot":`)) || !bytes.Contains(encoded, []byte(`"snapshots":`)) {
		t.Fatal("/2 codec", string(encoded), e)
	}
}
