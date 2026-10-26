package journal

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/relux-works/swarma-envelope/verifier"
)

func currencyJournal(t *testing.T) (verifier.Context, CurrencyPublication, Signer, []byte, map[string]any) {
	t.Helper()
	d := loadFixture(t, "P-CU-journal-argument")
	facts := object(d["fixture"])
	input := object(d["input"])
	cp := decode(t, input["checkpoint"])
	body := object(parseObject(t, cp)["body"])
	j := object(facts["snapshot_journal"])
	c, entries, _ := fixtureProof(t, d)
	c.RegistryCurrencyVersion = 2
	root := text(body["registry"])
	owner := text(body["owner"])
	originals := map[string][]byte{}
	for id, b := range object(facts["seen"]) {
		originals[id] = decode(t, b)
	}
	roots := map[string]string{"registry-owner": root, "grant": text(facts["root"]), "operator": text(facts["operator_root"])}
	seed := sha256.Sum256([]byte("waggle public test registry revocation seed"))
	key := ed25519.NewKeyFromSeed(seed[:])
	s, e := NewCurrencySigner(root, owner, roots, serviceFunc(func(b []byte) ([]byte, error) { return ed25519.Sign(key, b), nil }))
	if e != nil {
		t.Fatal(e)
	}
	// Add the application's mandatory registry snapshot reissue to the S8 vector's
	// external-root update. All test keys derive from published fixed seeds.
	old := object(object(facts["revocations"])[root])
	snapshotBody := object(parseObject(t, decode(t, old["snapshot"]))["body"])
	snapshotBody["issued_at"] = "2026-01-01T00:00:59Z"
	snapshot, e := s.Sign(wire(t, snapshotBody))
	if e != nil {
		t.Fatal(e)
	}
	snapshotID := text(parseObject(t, snapshot)["id"])
	originals[snapshotID] = snapshot
	last := parseObject(t, entries[len(entries)-1])
	entry, e := BuildEntry(wire(t, map[string]any{"type": "journal-entry/1", "registry": root, "owner": owner, "seq": int64(5), "prev": last["id"], "at": "2026-01-01T00:00:59Z", "op": "add", "record_type": "revocation/1", "record": snapshotID}), "2026-01-01T00:00:59Z")
	if e != nil {
		t.Fatal(e)
	}
	entries = append(entries, entry)
	body["seq"] = int64(5)
	body["head"] = parseObject(t, entry)["id"]
	for _, v := range body["snapshots"].([]any) {
		r := object(v)
		if r["root_role"] == "registry-owner" {
			r["snapshot_id"] = snapshotID
		}
	}
	old["snapshot"] = base64.RawURLEncoding.EncodeToString(snapshot)
	snapshots := map[string]any{}
	for k, v := range object(facts["revocations"]) {
		r := object(v)
		copy := map[string]any{}
		for n, x := range r {
			copy[n] = x
		}
		copy["snapshot"] = decode(t, r["snapshot"])
		snapshots[k] = copy
	}
	c.SignedSnapshots = present(t, snapshots)
	cp, e = s.Sign(wire(t, body))
	if e != nil {
		t.Fatal(e)
	}

	return c, CurrencyPublication{Entries: entries, CheckpointBody: wire(t, body), JournalAnchor: wire(t, j["anchor"]), Originals: originals, Capacity: func(int) error { return nil }}, s, cp, body
}

// The actual closed signer and J builder authenticate a derived /2 checkpoint
// with the registry reissue and the vector's external update at positions 5 and 4.
// A missing position exposes neither Prepared state nor a candidate Plan.
func TestCurrencyJournalPublicationAuthenticatesCompletePrefix(t *testing.T) {
	c, p, s, expected, _ := currencyJournal(t)
	built, r := BuildCurrencyPublication(c, s, p)
	if r.Code != "accept" || !bytes.Equal(built.Candidate, expected) || r.Plan.SnapshotJournal == nil || len(r.Plan.SnapshotJournal.Latest) != 3 || r.Plan.SnapshotJournal.Latest[0].RecordSeq != 4 || r.Plan.SnapshotJournal.Latest[2].RecordSeq != 5 {
		t.Fatal("S8 journal control", r)
	}
	p.Entries = append(p.Entries[:1], p.Entries[2:]...)
	denied, r := BuildCurrencyPublication(c, s, p)
	if r.Code != "journal_chain_broken" || !reflect.DeepEqual(denied, Prepared{}) || !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
		t.Fatal(denied, r)
	}
}

// Closed /2 custody rejects foreign pins, missing registry coverage, unknown
// members, unsorted sets and /1 downgrade before invoking its signature service.
// The valid neighboring request does invoke real signature math exactly once.
func TestCurrencySignerClosedProfiles(t *testing.T) {
	_, p, _, _, body := currencyJournal(t)
	root := text(body["registry"])
	owner := text(body["owner"])
	roots := map[string]string{}
	for _, v := range body["snapshots"].([]any) {
		r := object(v)
		roots[text(r["root_role"])] = text(r["root_key_id"])
	}
	seed := sha256.Sum256([]byte("waggle public test registry revocation seed"))
	key := ed25519.NewKeyFromSeed(seed[:])
	calls := 0
	s, e := NewCurrencySigner(root, owner, roots, serviceFunc(func(b []byte) ([]byte, error) { calls++; return ed25519.Sign(key, b), nil }))
	if e != nil {
		t.Fatal(e)
	}
	for _, which := range []string{"foreign", "missing", "unknown", "order", "downgrade"} {
		b := parseObject(t, p.CheckpointBody)
		rows := b["snapshots"].([]any)
		switch which {
		case "foreign":
			object(rows[0])["root_key_id"] = root
		case "missing":
			b["snapshots"] = rows[:len(rows)-1]
		case "unknown":
			b["extra"] = true
		case "order":
			rows[0], rows[1] = rows[1], rows[0]
		case "downgrade":
			b["type"] = "checkpoint/1"
			delete(b, "snapshots")
			b["snapshot"] = object(rows[len(rows)-1])["snapshot_id"]
		}
		raw, e := s.Sign(wire(t, b))
		if raw != nil {
			t.Fatal("candidate on refusal")
		}
		errorCode(t, e, ProfileRefused)
	}
	if calls != 0 {
		t.Fatal("refusal contacted custody")
	}
	if _, e = s.Sign(p.CheckpointBody); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
}

// A repeated registry snapshot is insufficient for a real publication, and raw
// record bounds are checked before custody. No candidate escapes either refusal.
func TestCurrencyJournalRequiresRegistryReissueAndBoundedInputs(t *testing.T) {
	c, p, s, _, _ := currencyJournal(t)
	anchor := parseObject(t, p.JournalAnchor)
	rows := anchor["latest"].([]any)
	for _, v := range parseObject(t, p.CheckpointBody)["snapshots"].([]any) {
		r := object(v)
		if r["root_role"] == "registry-owner" {
			object(rows[len(rows)-1])["snapshot_id"] = r["snapshot_id"]
		}
	}
	p.JournalAnchor = wire(t, anchor)
	denied, r := BuildCurrencyPublication(c, s, p)
	if r.Code != "checkpoint_snapshot_invalid" || !reflect.DeepEqual(denied, Prepared{}) {
		t.Fatal(denied, r)
	}
	c, p, s, _, _ = currencyJournal(t)
	p.Entries = append(p.Entries, make([]byte, 8193))
	denied, r = BuildCurrencyPublication(c, s, p)
	if r.Code != "journal_malformed" || !reflect.DeepEqual(denied, Prepared{}) {
		t.Fatal(denied, r)
	}
}

// The caller's actual-history capacity veto and absent capacity hook close
// before custody. A valid reservation permits the same authenticated candidate.
// Oversized unused originals retain malformed priority under another profile.
func TestCurrencyJournalCapacityBeforeCustody(t *testing.T) {
	c, p, s, _, _ := currencyJournal(t)
	calls := 0
	spy := signerFunc(func(b []byte) ([]byte, error) { calls++; return s.Sign(b) })
	for _, capacity := range []func(int) error{nil, func(int) error { return &Error{Code: SignerUnavailable} }} {
		p.Capacity = capacity
		candidate, r := BuildCurrencyPublication(c, spy, p)
		if r.Code != "journal_capacity_unavailable" || !reflect.DeepEqual(candidate, Prepared{}) || calls != 0 {
			t.Fatal(r, calls)
		}
	}
	p.Capacity = func(maximum int) error {
		if maximum < 1 || maximum > 16777216 {
			t.Fatal("invalid bound", maximum)
		}
		return nil
	}
	_, r := BuildCurrencyPublication(c, spy, p)
	if r.Code != "accept" || calls != 1 {
		t.Fatal(r, calls)
	}
	c.RegistryCurrencyVersion = 1
	p.Originals["unused"] = make([]byte, 8193)
	_, r = BuildCurrencyPublication(c, spy, p)
	if r.Code != "journal_malformed" || calls != 1 {
		t.Fatal(r, calls)
	}
}
