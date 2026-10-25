package enrollment_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/contracts"
	admission "github.com/relux-works/swarma-owner-registry/internal/enrollment"
	"github.com/relux-works/swarma-owner-registry/internal/pins"
	"github.com/relux-works/swarma-registrar/ccj"
	"github.com/relux-works/swarma-registrar/envelope"
	"github.com/relux-works/swarma-registrar/registry"
)

type file struct {
	Path  string
	Bytes string `json:"bytes_base64"`
	Kind  string
}
type vector struct {
	Expected struct{ Code string }
	Fixture  struct {
		Audience string
		Registry struct {
			ObjectFormat      string `json:"object_format"`
			Publisher         string `json:"publisher_key"`
			PreviousPublisher string `json:"previous_publisher_key"`
			Revision          string
			PreviousRevision  string `json:"previous_revision"`
			Files             []file
			Previous          []file   `json:"previous_files"`
			Stations          []string `json:"station_hosts"`
			Descendant        bool
		}
	}
}

func fixture(t *testing.T, name string) vector {
	t.Helper()
	b, err := exec.Command("go", "list", "-m", "-json", "github.com/relux-works/swarma-registrar").Output()
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Path, Version, Dir string
		Replace            any
	}
	if err = json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Path != "github.com/relux-works/swarma-registrar" || m.Version != "v0.0.0-20261025040811-264660e5dabb" || m.Replace != nil {
		t.Fatal("unreviewed fixture module")
	}
	folder := "0-negative"
	if strings.HasPrefix(name, "P") {
		folder = "1-positive"
	}
	b, err = os.ReadFile(filepath.Join(m.Dir, "vectors/enrollment-1", folder, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f vector
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}
func projection(t *testing.T, revision string, files []file) admission.Projection {
	t.Helper()
	p := admission.Projection{Revision: revision, Records: map[string][]byte{}}
	for _, f := range files {
		b, err := base64.RawURLEncoding.DecodeString(f.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		kind := registry.Regular
		if f.Kind != "regular" {
			kind = registry.Other
		}
		p.Entries = append(p.Entries, registry.Entry{Path: f.Path, Bytes: b, Kind: kind})
		var e struct{ ID string }
		_ = json.Unmarshal(b, &e)
		p.Records[e.ID] = bytes.Clone(b)
	}
	return p
}
func current(t *testing.T, f vector) admission.Projection {
	return projection(t, f.Fixture.Registry.Revision, f.Fixture.Registry.Files)
}

// RFC 8032 section 7.1 public test seed, domain-separated for test roles only.
func private(n byte) ed25519.PrivateKey {
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	h := sha256.Sum256(append(seed, n))
	return ed25519.NewKeyFromSeed(h[:])
}
func public(n byte) string {
	return "ed25519:" + base64.RawURLEncoding.EncodeToString(private(n).Public().(ed25519.PublicKey))
}
func configuration(t *testing.T, publisher string, open bool) *pins.Config {
	t.Helper()
	return configurationWithFormat(t, publisher, open, "sha1")
}
func configurationWithFormat(t *testing.T, publisher string, open bool, objectFormat string) *pins.Config {
	t.Helper()
	roles := map[string]any{}
	purposes := []string{"registry-revocation", "git-commit", "enrollment/1", "purpose-5", "operator", "grant", "grant-service"}
	for i, r := range pins.Roles() {
		key := public(byte(i + 1))
		scope := "owner-a"
		if r == pins.EnrollmentPublisher {
			key = publisher
		}
		if r == pins.Purpose5Root {
			scope = "purpose-5"
		}
		if r == pins.RegistryOwner {
			scope = public(4)
		}
		roles[string(r)] = map[string]any{"key": key, "purpose": purposes[i], "scope": scope}
	}
	d := map[string]any{"revision": contracts.Revision, "owner": "owner-a", "object_format": objectFormat, "roles": roles, "record_types": []any{"attestation/1", "grant/1", "revocation/1", "enrollment/1", "journal-entry/1", "checkpoint/1"}, "key_classes": []any{"agent", "operator-device", "service"}, "service_profile": map[string]any{"key_class": "service", "purpose": "grant-service", "roles": []any{"author"}, "actions": []any{"grant.sign"}, "max_lifetime_seconds": int64(86400)}}
	if open {
		d["activation"] = map[string]any{"enrollment_intake": true}
	}
	b, err := ccj.Marshal(d, ccj.EnrollmentMalformed)
	if err != nil {
		t.Fatal(err)
	}
	c, err := pins.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// This instrument binds exact public fixture entries, not a real signed Git
// commit. The production verifier must check signature, approval and ancestry.
type verifier struct {
	projection        admission.Projection
	denied, unrelated bool
	calls             int
}

func (v *verifier) Verify(revision string, entries []registry.Entry) error {
	v.calls++
	want := append([]registry.Entry(nil), v.projection.Entries...)
	sort.Slice(want, func(i, j int) bool { return want[i].Path < want[j].Path })
	if v.denied || revision != v.projection.Revision || !reflect.DeepEqual(entries, want) {
		return errors.New("fixture approval refused")
	}
	return nil
}
func (v *verifier) Descends(candidate, previous string) (bool, error) { return !v.unrelated, nil }

// Test-only transaction double: no crash durability, witness authentication or
// real Git approval is claimed. It reports exact resolution only when configured.
type transaction struct {
	calls       []string
	candidates  []admission.Candidate
	fail, wrong string
	uncertain   bool
	mutate      bool
}

func (x *transaction) step(name string) error {
	x.calls = append(x.calls, name)
	if x.fail == name {
		return errors.New("instrument failure")
	}
	return nil
}
func (x *transaction) Preflight(c admission.Candidate) (admission.Binding, error) {
	x.candidates = append(x.candidates, c.Clone())
	err := x.step("preflight")
	b := admission.Binding{CandidateID: c.ID, StateID: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(c.ID)))}
	if x.wrong == "preflight" {
		b.CandidateID = "wrong"
	}
	if x.wrong == "state" {
		b.StateID = "wrong"
	}
	if x.mutate {
		clear(c.Projection.Records)
		clear(c.After.HighWater.Hosts)
		clear(c.Before.Exports)
	}
	return b, err
}
func (x *transaction) Intent(b admission.Binding) error { return x.step("intent") }
func (x *transaction) Commit(b admission.Binding) (admission.Binding, error) {
	err := x.step("commit")
	if x.wrong == "commit" {
		b.StateID = "wrong"
	}
	return b, err
}
func (x *transaction) Resolve(b admission.Binding) (admission.Resolution, error) {
	err := x.step("resolve")
	if x.wrong == "resolve" {
		b.CandidateID = "wrong"
	}
	return admission.Resolution{Binding: b, Resolved: !x.uncertain}, err
}
func adapter(t *testing.T, f vector, p admission.Projection, state admission.State, x *transaction) (*admission.Adapter, *verifier) {
	t.Helper()
	v := &verifier{projection: p}
	a, err := admission.New(admission.Trust{Config: configurationWithFormat(t, f.Fixture.Registry.Publisher, true, f.Fixture.Registry.ObjectFormat), Audience: f.Fixture.Audience, Stations: map[string]bool{"host-a": true, "host-b": true}, Verifier: v, Transaction: x, Retained: state, FirstUse: state.HighWater == nil, Reconciled: state.HighWater != nil})
	if err != nil {
		t.Fatal(err)
	}
	return a, v
}
func accepted(t *testing.T, a *admission.Adapter, p admission.Projection) *registry.Snapshot {
	t.Helper()
	s, err := a.Load(p)
	if err != nil || s == nil || a.Fenced() {
		t.Fatalf("valid projection refused: %v", err)
	}
	return s
}
func refused(t *testing.T, a *admission.Adapter, p admission.Projection, want registry.Code, x *transaction) {
	t.Helper()
	before := a.LastResolved()
	s, err := a.Load(p)
	var typed *admission.Error
	if s != nil || !errors.As(err, &typed) || typed.Code != want || err.Error() != string(want) || !a.Fenced() {
		t.Fatalf("want nil/%s/fenced, got snapshot=%v err=%v fenced=%v", want, s != nil, err, a.Fenced())
	}
	if !reflect.DeepEqual(before, a.LastResolved()) {
		t.Fatal("refusal replaced last-resolved evidence")
	}
	calls := len(x.calls)
	s, err = a.Load(p)
	if s != nil || !errors.As(err, &typed) || typed.Code != admission.StateUnavailable || len(x.calls) != calls {
		t.Fatal("fenced adapter retried")
	}
}
func positive(t *testing.T) {
	t.Helper()
	f := fixture(t, "PE1")
	p := current(t, f)
	a, _ := adapter(t, f, p, admission.State{}, &transaction{})
	accepted(t, a, p)
}

// One invalid entry or byte-unequal/missing/extra mirror refuses the whole load;
// a valid complete projection succeeds nearby, never a surviving lower subset.
func TestInvalidMirrorFencesWholeLoad(t *testing.T) {
	positive(t)
	for _, kind := range []string{"unequal", "missing", "extra", "malformed-entry"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, "PE1")
			p := current(t, f)
			switch kind {
			case "unequal":
				for id, b := range p.Records {
					p.Records[id] = append(b, ' ')
				}
			case "missing":
				clear(p.Records)
			case "extra":
				p.Records["other"] = []byte("{}")
			case "malformed-entry":
				p.Entries = append(p.Entries, registry.Entry{Path: "enrollments/z.json", Bytes: []byte("?")})
			}
			x := &transaction{}
			a, _ := adapter(t, f, p, admission.State{}, x)
			code := registry.RegistryUnavailable
			if kind == "malformed-entry" {
				code = registry.EnrollmentMalformed
			}
			refused(t, a, p, code, x)
			if len(x.calls) != 0 {
				t.Fatal("invalid mirror reached transaction")
			}
		})
	}
}

// Protected approval cannot be replaced by envelope validity or object existence.
func TestUnapprovedCommitRefused(t *testing.T) {
	positive(t)
	f := fixture(t, "PE1")
	p := current(t, f)
	x := &transaction{}
	a, v := adapter(t, f, p, admission.State{}, x)
	v.denied = true
	refused(t, a, p, registry.RegistryUnavailable, x)
	if len(x.calls) != 0 {
		t.Fatal("unapproved projection reached transaction")
	}
}
func previous(t *testing.T, f vector) admission.State {
	t.Helper()
	old := f
	if f.Fixture.Registry.PreviousPublisher != "" {
		old.Fixture.Registry.Publisher = f.Fixture.Registry.PreviousPublisher
	}
	p := projection(t, f.Fixture.Registry.PreviousRevision, f.Fixture.Registry.Previous)
	a, _ := adapter(t, old, p, admission.State{}, &transaction{})
	accepted(t, a, p)
	return a.LastResolved()
}

// Published dropped-tombstone and dropped-sequence witnesses retain no authority;
// loading the complete retained projection at the new revision succeeds.
func TestTombstoneHistoryCannotDisappear(t *testing.T) {
	for _, name := range []string{"E76", "E74"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t, name)
			state := previous(t, f)
			p := current(t, f)
			full := projection(t, p.Revision, f.Fixture.Registry.Previous)
			control, _ := adapter(t, f, full, state, &transaction{})
			accepted(t, control, full)
			x := &transaction{}
			a, _ := adapter(t, f, p, state, x)
			refused(t, a, p, registry.RegistryRollback, x)
			if len(x.calls) != 0 {
				t.Fatal("history omission reached transaction")
			}
		})
	}
}

// Both path orders preserve snapshot-wide reserved-sigs precedence; malformed
// CCJ in a later file still wins at phase 1. A complete PE1 load accepts.
func TestReservedSigsWinsSnapshotWide(t *testing.T) {
	positive(t)
	for _, name := range []string{"E107", "E108", "E109"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t, name)
			p := current(t, f)
			x := &transaction{}
			a, _ := adapter(t, f, p, admission.State{}, x)
			refused(t, a, p, registry.Code(f.Expected.Code), x)
			if len(x.calls) != 0 {
				t.Fatal("invalid envelope reached transaction")
			}
		})
	}
}

// Success uses the public Loader and returns authority only after the test
// transaction reports exact resolution. This is no live Git/witness qualification.
func TestOfflineEnrollmentSignedCommitAccepted(t *testing.T) {
	f := fixture(t, "PE1")
	p := current(t, f)
	original := current(t, f)
	x := &transaction{}
	a, v := adapter(t, f, p, admission.State{}, x)
	s := accepted(t, a, p)
	if !reflect.DeepEqual(x.calls, []string{"preflight", "intent", "commit", "resolve"}) || v.calls != 1 {
		t.Fatalf("order: %v", x.calls)
	}
	r, ok := s.Current("host-a")
	if !ok || r.Sequence() != 1 || r.ID() == "" {
		t.Fatal("snapshot lacks enrollment")
	}
	state := a.LastResolved()
	if state.HighWater.Revision != p.Revision || len(state.Exports) != 1 || !reflect.DeepEqual(state.Exports[0], original) || !reflect.DeepEqual(p, original) {
		t.Fatal("exact export not retained or input mutated")
	}
	c := x.candidates[0]
	if c.ID == "" || c.Before.HighWater != nil || c.After.HighWater.Revision != p.Revision || !reflect.DeepEqual(c.Projection, original) {
		t.Fatal("candidate did not bind exact projection/history")
	}
	clear(state.Exports[0].Records)
	clear(state.HighWater.Hosts)
	if !reflect.DeepEqual(a.LastResolved().Exports[0], original) {
		t.Fatal("state accessor aliases adapter")
	}
}

// Real published publisher rotation keeps canonical body/id/sequence and archives
// the old exact envelope. New valid signatures cannot rewrite old bodies/IDs.
func TestApprovedPublisherRotationPreservesHistory(t *testing.T) {
	f := fixture(t, "PE9")
	state := previous(t, f)
	p := current(t, f)
	x := &transaction{}
	a, _ := adapter(t, f, p, state, x)
	s := accepted(t, a, p)
	r, _ := s.Current("host-a")
	oldID := state.HighWater.Hosts["host-a"].ID
	if r.ID() != oldID || r.Sequence() != state.HighWater.Hosts["host-a"].Seq {
		t.Fatal("rotation changed id/sequence")
	}
	next := a.LastResolved()
	if len(next.Exports) != 2 || !reflect.DeepEqual(next.Exports[0], state.Exports[0]) || bytes.Equal(next.Exports[0].Entries[0].Bytes, p.Entries[0].Bytes) {
		t.Fatal("old envelope revision was not preserved")
	}
	var oldEnvelope, newEnvelope map[string]any
	_ = json.Unmarshal(next.Exports[0].Entries[0].Bytes, &oldEnvelope)
	_ = json.Unmarshal(p.Entries[0].Bytes, &newEnvelope)
	if !reflect.DeepEqual(oldEnvelope["body"], newEnvelope["body"]) {
		t.Fatal("published rotation changed body")
	}
	for _, kind := range []string{"body", "id"} {
		t.Run(kind, func(t *testing.T) {
			changed := current(t, f)
			value, err := ccj.Parse(changed.Entries[0].Bytes, ccj.EnrollmentMalformed)
			if err != nil {
				t.Fatal(err)
			}
			e := value.(map[string]any)
			body := e["body"].(map[string]any)
			pub := public(80)
			if kind == "body" {
				body["keys"].([]any)[0].(map[string]any)["key"] = public(81)
			}
			pre, id, err := envelope.SigningInput(envelope.Enrollment, body)
			if err != nil {
				t.Fatal(err)
			}
			e["id"] = id
			e["sig"] = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private(80), pre))
			if kind == "id" {
				e["id"] = "sha256:" + strings.Repeat("0", 64)
			}
			b, err := ccj.Marshal(e, ccj.EnrollmentMalformed)
			if err != nil {
				t.Fatal(err)
			}
			changed.Entries[0].Bytes = b
			changed.Records = map[string][]byte{e["id"].(string): bytes.Clone(b)}
			altered := f
			altered.Fixture.Registry.Publisher = pub
			x := &transaction{}
			a, _ := adapter(t, altered, changed, state, x)
			want := registry.RegistryRollback
			if kind == "id" {
				want = registry.EnrollmentIDMismatch
			}
			refused(t, a, changed, want, x)
			if len(x.calls) != 0 {
				t.Fatal("changed historical body reached transaction")
			}
		})
	}
}

// Each transaction failure stops later calls, returns no snapshot, and stays
// fenced even when the double is repaired; uncertainty and wrong bindings fail too.
func TestTransactionFailuresFence(t *testing.T) {
	positive(t)
	for _, mode := range []string{"preflight", "intent", "commit", "resolve", "uncertain", "wrong-preflight", "wrong-state", "wrong-commit", "wrong-resolve"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture(t, "PE1")
			p := current(t, f)
			x := &transaction{}
			switch {
			case mode == "uncertain":
				x.uncertain = true
			case strings.HasPrefix(mode, "wrong-"):
				x.wrong = strings.TrimPrefix(mode, "wrong-")
			default:
				x.fail = mode
			}
			a, _ := adapter(t, f, p, admission.State{}, x)
			refused(t, a, p, registry.RegistryUnavailable, x)
			stops := map[string]int{"preflight": 1, "intent": 2, "commit": 3, "resolve": 4, "uncertain": 4, "wrong-preflight": 1, "wrong-state": 1, "wrong-commit": 3, "wrong-resolve": 4}
			if len(x.calls) != stops[mode] {
				t.Fatalf("calls after failure: %v", x.calls)
			}
		})
	}
}

// Default-closed protected enrollment gate and absent transaction/verifier deny
// construction; an explicit gate plus the qualified-interface test double works.
func TestEnrollmentGateClosedByDefault(t *testing.T) {
	positive(t)
	f := fixture(t, "PE1")
	p := current(t, f)
	for _, kind := range []string{"gate", "transaction", "typed-nil", "verifier"} {
		t.Run(kind, func(t *testing.T) {
			trust := admission.Trust{Config: configuration(t, f.Fixture.Registry.Publisher, true), Audience: f.Fixture.Audience, Stations: map[string]bool{"host-a": true}, Verifier: &verifier{projection: p}, Transaction: &transaction{}, FirstUse: true}
			want := admission.TransactionUnavailable
			switch kind {
			case "gate":
				trust.Config = configuration(t, f.Fixture.Registry.Publisher, false)
				want = admission.IntakeClosed
			case "transaction":
				trust.Transaction = nil
			case "typed-nil":
				trust.Transaction = (*transaction)(nil)
			case "verifier":
				trust.Verifier = nil
				want = registry.RegistryUnavailable
			}
			a, err := admission.New(trust)
			var e *admission.Error
			if a != nil || !errors.As(err, &e) || e.Code != want {
				t.Fatalf("construction not closed: %v", err)
			}
		})
	}
}

// A malformed projection cannot get as far as preflight/intent/local commit.
func TestMalformedProjectionNeverCommits(t *testing.T) {
	positive(t)
	f := fixture(t, "PE1")
	p := current(t, f)
	p.Entries[0].Bytes = []byte("?")
	x := &transaction{}
	a, _ := adapter(t, f, p, admission.State{}, x)
	refused(t, a, p, registry.EnrollmentMalformed, x)
	if len(x.calls) != 0 {
		t.Fatal("malformed projection reached transaction")
	}
}

// Mutating the preflight copy cannot change either later output or retained bytes.
func TestTransactionCannotMutateCandidate(t *testing.T) {
	f := fixture(t, "PE1")
	p := current(t, f)
	x := &transaction{mutate: true}
	a, _ := adapter(t, f, p, admission.State{}, x)
	accepted(t, a, p)
	if !reflect.DeepEqual(a.LastResolved().Exports[0], p) || len(a.LastResolved().HighWater.Hosts) != 1 {
		t.Fatal("transaction mutated adapter state")
	}
}

// Export-only paths/types are prerequisites, not paths silently ignored by the
// Loader. The nearby valid export succeeds and none of these reaches preflight.
func TestProjectionPrerequisitesRefused(t *testing.T) {
	positive(t)
	for _, kind := range []string{"outside", "nested", "traversal", "symlink", "duplicate", "record-type"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, "PE1")
			p := current(t, f)
			switch kind {
			case "outside":
				p.Entries[0].Path = "records/a.json"
			case "nested":
				p.Entries[0].Path = "enrollments/nested/a.json"
			case "traversal":
				p.Entries[0].Path = "enrollments/../a.json"
			case "symlink":
				p.Entries[0].Kind = registry.Symlink
			case "duplicate":
				p.Entries = append(p.Entries, p.Entries[0])
			case "record-type":
				value, err := ccj.Parse(p.Entries[0].Bytes, ccj.EnrollmentMalformed)
				if err != nil {
					t.Fatal(err)
				}
				e := value.(map[string]any)
				e["body"].(map[string]any)["type"] = "grant/1"
				b, err := ccj.Marshal(e, ccj.EnrollmentMalformed)
				if err != nil {
					t.Fatal(err)
				}
				p.Entries[0].Bytes = b
			}
			want := registry.RegistryUnavailable
			if kind == "record-type" {
				want = registry.EnrollmentMalformed
			}
			x := &transaction{}
			a, _ := adapter(t, f, p, admission.State{}, x)
			refused(t, a, p, want, x)
			if len(x.calls) != 0 {
				t.Fatal("invalid projection reached preflight")
			}
		})
	}
}

// Original publisher equations remain mandatory. A host-signed or wrong-publisher
// envelope cannot become enrollment authority even with approval and an open gate.
func TestEnrollmentPublisherSignatureRequired(t *testing.T) {
	positive(t)
	for _, name := range []string{"E33", "E34"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t, name)
			p := current(t, f)
			x := &transaction{}
			a, _ := adapter(t, f, p, admission.State{}, x)
			refused(t, a, p, registry.EnrollmentSignatureInvalid, x)
			if len(x.calls) != 0 {
				t.Fatal("bad publisher signature reached transaction")
			}
		})
	}
}

// Candidate binding includes exact envelope bytes, not only the stable body ID.
func TestCandidateBindsExactInventory(t *testing.T) {
	f := fixture(t, "PE1")
	p := current(t, f)
	first := &transaction{}
	a, _ := adapter(t, f, p, admission.State{}, first)
	accepted(t, a, p)
	changed := current(t, f)
	changed.Entries[0].Bytes = append(changed.Entries[0].Bytes, ' ')
	for id := range changed.Records {
		changed.Records[id] = bytes.Clone(changed.Entries[0].Bytes)
	}
	second := &transaction{}
	b, _ := adapter(t, f, changed, admission.State{}, second)
	accepted(t, b, changed)
	if first.candidates[0].ID == second.candidates[0].ID {
		t.Fatal("exact envelope difference did not change candidate binding")
	}
	if !reflect.DeepEqual(a.LastResolved().HighWater, b.LastResolved().HighWater) {
		t.Fatal("control changed body identity instead of envelope bytes")
	}
}

// Missing/recovered rollback evidence cannot become implicit first use; retained
// exact export evidence is required alongside the reconciled high-water.
func TestReconciledHistoryRequired(t *testing.T) {
	f := fixture(t, "PE1")
	p := current(t, f)
	control, _ := adapter(t, f, p, admission.State{}, &transaction{})
	accepted(t, control, p)
	state := control.LastResolved()
	for _, kind := range []string{"missing-state", "not-reconciled", "bootstrap-with-history", "missing-archive", "wrong-archive"} {
		t.Run(kind, func(t *testing.T) {
			trust := admission.Trust{Config: configuration(t, f.Fixture.Registry.Publisher, true), Audience: f.Fixture.Audience, Stations: map[string]bool{"host-a": true}, Verifier: &verifier{projection: p}, Transaction: &transaction{}, Retained: state.Clone(), Reconciled: true}
			switch kind {
			case "missing-state":
				trust.Retained = admission.State{}
			case "not-reconciled":
				trust.Reconciled = false
			case "bootstrap-with-history":
				trust.FirstUse = true
			case "missing-archive":
				trust.Retained.Exports = nil
			case "wrong-archive":
				trust.Retained.Exports[0].Revision = strings.Repeat("b", 40)
			}
			a, err := admission.New(trust)
			var e *admission.Error
			if a != nil || !errors.As(err, &e) || e.Code != registry.RegistryUnavailable {
				t.Fatalf("invalid rollback evidence accepted: %v", err)
			}
		})
	}
}

// A failed reload fences further use even after a previously resolved load;
// retained evidence remains available without being claimed as current authority.
func TestReloadFailureFencesOldAuthority(t *testing.T) {
	f := fixture(t, "PE1")
	p := current(t, f)
	x := &transaction{}
	a, v := adapter(t, f, p, admission.State{}, x)
	accepted(t, a, p)
	v.denied = true
	refused(t, a, p, registry.RegistryUnavailable, x)
	if len(x.calls) != 4 {
		t.Fatal("failed reload reached a new transaction")
	}
}
