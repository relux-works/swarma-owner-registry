//go:build linux

package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type boundary struct{ Operation, Path string }

func nextProposal(first Outcome) Request {
	r := proposal()
	r.RequestID = "request-b"
	r.ExpectedHead = first.Head
	r.Head = id("head-b")
	r.Watermark = "2026-01-01T00:00:01Z"
	r.Records[0].ID = id("body-b")
	r.Records[0].Bytes = []byte("{\"body\":2}")
	r.Journal = []byte("journal-b\n")
	r.Checkpoint = []byte("checkpoint-b\n")
	r.Result = []byte("accepted-b")
	return r
}
func prepareSecond(t *testing.T) (*Store, string, Request, View) {
	t.Helper()
	s, root := setup(t)
	r := nextProposal(mustCommit(t, s, proposal()))
	v, e := s.Preview(r)
	if e != nil {
		t.Fatal(e)
	}
	return s, root, r, v
}
func baseline(t *testing.T) []boundary {
	t.Helper()
	s, _, r, _ := prepareSecond(t)
	var events []boundary
	guard := newDurability(t)
	s.disk.hooks = hooks{before: guard.before, after: func(op, p string) error { guard.after(op, p); events = append(events, boundary{op, p}); return nil }}
	mustCommit(t, s, r)
	guard.resolved()
	s.disk.hooks = hooks{}
	if len(events) == 0 {
		t.Fatal("fault instrumentation was not applied")
	}
	return events
}

// durability checks the contract's ordering at real filesystem boundaries. It
// supplements, rather than qualifies, the bounded power-loss namespace model.
type durability struct {
	t                       *testing.T
	written, synced, closed map[string]bool
	dirty                   map[string]bool
}

func newDurability(t *testing.T) *durability {
	return &durability{t: t, written: map[string]bool{}, synced: map[string]bool{}, closed: map[string]bool{}, dirty: map[string]bool{}}
}
func (d *durability) before(op, p string) error {
	if op == "file.open" && p == "CURRENT" && len(d.dirty) != 0 {
		d.t.Errorf("publication before directory fsync: %v", d.dirty)
		return errors.New("unsynced rename")
	}
	return nil
}
func (d *durability) after(op, p string) {
	switch op {
	case "file.write":
		d.written[p] = true
	case "file.sync":
		d.synced[p] = true
	case "file.close":
		d.closed[p] = true
	case "file.rename":
		if !d.written[p] || !d.synced[p] || !d.closed[p] {
			d.t.Errorf("rename without full write/sync/close: %s", p)
		}
		d.dirty[filepath.Dir(p)] = true
	case "dir.sync":
		delete(d.dirty, p)
	}
}
func (d *durability) resolved() {
	if len(d.dirty) != 0 {
		d.t.Fatalf("acknowledgement before directory fsync: %v", d.dirty)
	}
}

// Each failing boundary, plus a real partial successful write, returns only
// state_unavailable and fences both read and write acceptance. Recovery either
// restores the exact independently expected candidate or remains fenced.
func TestDurabilityFaultMatrix(t *testing.T) {
	events := baseline(t)
	for i, event := range events {
		t.Run(fmt.Sprintf("%02d-%s", i, event.Operation), func(t *testing.T) {
			s, root, r, expected := prepareSecond(t)
			seen := 0
			injected := false
			s.disk.hooks.before = func(op, p string) error {
				if seen == i {
					injected = true
					return errors.New("injected boundary failure")
				}
				seen++
				return nil
			}
			_, e := s.Commit(r)
			if !injected {
				t.Fatal("fault not applied")
			}
			code(t, e, StateUnavailable)
			s.disk.hooks = hooks{}
			before := tree(t, root)
			_, e = s.View()
			code(t, e, StateUnavailable)
			_, e = s.Commit(r)
			code(t, e, StateUnavailable)
			unchanged(t, root, before)
			e = s.Reconcile(expected.StateID)
			if e != nil {
				code(t, e, StateUnavailable)
				_, e = s.View()
				code(t, e, StateUnavailable)
			} else {
				assertExact(t, s, r, expected)
			}
		})
	}
	t.Run("short-write", func(t *testing.T) {
		s, root, r, expected := prepareSecond(t)
		s.disk.hooks.write = func(f *os.File, b []byte) (int, error) { return f.Write(b[:len(b)/2]) }
		_, e := s.Commit(r)
		code(t, e, StateUnavailable)
		s.disk.hooks = hooks{}
		before := tree(t, root)
		_, e = s.Commit(r)
		code(t, e, StateUnavailable)
		unchanged(t, root, before)
		code(t, s.Reconcile(expected.StateID), StateUnavailable)
	})
}

func assertExact(t *testing.T, s *Store, r Request, expected View) {
	t.Helper()
	v, e := s.View()
	if e != nil || !reflect.DeepEqual(v, expected) {
		t.Fatalf("recovered wrong state: %+v != %+v: %v", v, expected, e)
	}
	before := tree(t, s.diskPathForTest())
	out, e := s.Commit(r)
	if e != nil || out.Sequence != v.Sequence || !bytes.Equal(out.Result, r.Result) {
		t.Fatalf("retained outcome changed: %+v %v", out, e)
	}
	unchanged(t, s.diskPathForTest(), before)
	b, e := s.ReadRecord(r.Records[0].Type, r.Records[0].ID)
	if e != nil || !bytes.Equal(b, r.Records[0].Bytes) {
		t.Fatal("record bytes not recovered exactly")
	}
}

// Linux procfs is used only to find our held temporary-directory descriptor for
// test snapshots; production never uses procfs to traverse caller paths.
func (s *Store) diskPathForTest() string {
	p, e := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", s.disk.root.Fd()))
	if e != nil {
		panic(e)
	}
	return p
}

// powerModel retains namespace entries only after their own directory sync.
// At a simulated power cut it discards unflushed names, including renames over
// CURRENT. It is deliberately a bounded model, not real hardware qualification.
type powerModel struct {
	t       *testing.T
	root    string
	durable map[string]string
}

func newPowerModel(t *testing.T, root string) *powerModel { return &powerModel{t, root, tree(t, root)} }
func (m *powerModel) sync(dir string) {
	for p, v := range m.durable {
		if v != "dir" && filepath.Dir(p) == dir {
			delete(m.durable, p)
		}
	}
	entries, e := os.ReadDir(filepath.Join(m.root, dir))
	if e != nil {
		m.t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		p := filepath.Join(dir, entry.Name())
		b, e := os.ReadFile(filepath.Join(m.root, p))
		if e != nil {
			m.t.Fatal(e)
		}
		m.durable[p] = string(b)
	}
}
func (m *powerModel) cut() {
	now := tree(m.t, m.root)
	for p, v := range now {
		if v != "dir" {
			if e := os.Remove(filepath.Join(m.root, p)); e != nil {
				m.t.Fatal(e)
			}
		}
	}
	for p, v := range m.durable {
		if v != "dir" {
			if e := os.WriteFile(filepath.Join(m.root, p), []byte(v), 0600); e != nil {
				m.t.Fatal(e)
			}
		}
	}
}

type crashSignal struct{}

func crashCall(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		p := recover()
		if _, ok := p.(crashSignal); !ok {
			t.Fatalf("expected injected crash, got %v", p)
		}
	}()
	fn()
	t.Fatal("crash did not occur")
}

// Every actual boundary is crashed both with surviving process-cache bytes and
// with the bounded namespace power-loss model. A complete expected candidate is
// recovered exactly; missing candidate evidence fences, never returns an older
// state as success. Directory barriers are asserted before visibility and ack.
func TestCrashReconcilesExactCommit(t *testing.T) {
	events := baseline(t)
	t.Logf("%d commit boundaries, two crash models", len(events))
	for _, power := range []bool{false, true} {
		for i, event := range events {
			t.Run(fmt.Sprintf("power-%t/%02d-%s", power, i, event.Operation), func(t *testing.T) {
				s, root, r, expected := prepareSecond(t)
				model := newPowerModel(t, root)
				guard := newDurability(t)
				seen := 0
				s.disk.hooks = hooks{before: guard.before, after: func(op, p string) error {
					guard.after(op, p)
					if op == "dir.sync" {
						model.sync(p)
					}
					if seen == i {
						panic(crashSignal{})
					}
					seen++
					return nil
				}}
				crashCall(t, func() { s.Commit(r) })
				s.disk.hooks = hooks{}
				s.Close()
				if power {
					model.cut()
				}
				before := tree(t, root)
				restarted, e := Open(root, expected.StateID)
				if e != nil {
					code(t, e, StateUnavailable)
					unchanged(t, root, before)
					return
				}
				defer restarted.Close()
				assertExact(t, restarted, r, expected)
			})
		}
	}
}

// Even total loss of a pending candidate cannot turn the previous acknowledged
// state into an approved restart; the caller's expected state is mandatory.
func TestMissingCandidateCannotRollback(t *testing.T) {
	s, root, _, expected := prepareSecond(t)
	s.Close()
	before := tree(t, root)
	v, e := Open(root, expected.StateID)
	if v != nil {
		v.Close()
		t.Fatal("lost candidate silently rolled back")
	}
	code(t, e, StateUnavailable)
	unchanged(t, root, before)
	v, e = Open(root, "")
	if v != nil {
		v.Close()
		t.Fatal("restart omitted protected expected identity")
	}
	code(t, e, StateUnavailable)
}

// New files from an unresolved candidate cannot be disclosed through the store;
// exact-candidate reconciliation makes them visible only after checked durability.
func TestUnresolvedCandidateNotVisible(t *testing.T) {
	s, root, r, expected := prepareSecond(t)
	s.disk.hooks.before = func(op, p string) error {
		if op == "file.open" && p == "CURRENT" {
			return errors.New("publication unavailable")
		}
		return nil
	}
	_, e := s.Commit(r)
	code(t, e, StateUnavailable)
	s.disk.hooks = hooks{}
	_, e = s.ReadRecord(r.Records[0].Type, r.Records[0].ID)
	code(t, e, StateUnavailable)
	e = s.Reconcile(expected.StateID)
	if e != nil {
		t.Fatal(e)
	}
	assertExact(t, s, r, expected)
	if len(tree(t, root)) == 0 {
		t.Fatal("missing retained evidence")
	}
}

// Bootstrap cannot reuse populated storage; a second local writer, root link,
// or changed acknowledged manifest refuses. A normal closed/reopened store works.
func TestBootstrapAndStateSafety(t *testing.T) {
	s, root := setup(t)
	before := tree(t, root)
	other, e := Open(root, s.stateID)
	if other != nil {
		other.Close()
		t.Fatal("second writer admitted")
	}
	code(t, e, StateUnavailable)
	unchanged(t, root, before)
	v := mustCommit(t, s, proposal())
	_ = v
	stateID := s.stateID
	s.Close()
	other, e = Create(root)
	if other != nil {
		other.Close()
		t.Fatal("nonempty bootstrap admitted")
	}
	code(t, e, StateUnavailable)
	linked := filepath.Join(t.TempDir(), "link")
	if e = os.Symlink(root, linked); e != nil {
		t.Fatal(e)
	}
	other, e = Open(linked, stateID)
	if other != nil {
		other.Close()
		t.Fatal("linked root admitted")
	}
	code(t, e, StateUnavailable)
	other, e = Open(root, stateID)
	if e != nil {
		t.Fatal(e)
	}
	other.Close()
	path := filepath.Join(root, "transactions", stateID+".json")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	raw = append(raw, ' ')
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	before = tree(t, root)
	other, e = Open(root, stateID)
	if other != nil {
		other.Close()
		t.Fatal("changed manifest admitted")
	}
	code(t, e, StateUnavailable)
	unchanged(t, root, before)
}

// Identifiers bind exact bytes globally, even if a request chooses a different
// supported storage type. Matching evidence under its original type controls it.
func TestContentIDBindingAcrossTypes(t *testing.T) {
	s, root := setup(t)
	r := proposal()
	out := mustCommit(t, s, r)
	r = nextProposal(out)
	r.Records[0].Type = "attestation/1"
	r.Records[0].ID = proposal().Records[0].ID
	before := tree(t, root)
	_, e := s.Commit(r)
	code(t, e, EnvelopeConflict)
	unchanged(t, root, before)
}

// Invalid proof route/shape, truncated carrier and malformed wrapper are
// refused before any intent; six closed storage kinds remain independently usable.
func TestPreflightShapeNoWrite(t *testing.T) {
	for _, p := range []Proof{{Route: "unknown"}, {Entries: [][]byte{[]byte("{}")}}, {Route: JournalProof, Entries: [][]byte{[]byte("{}")}}, {Wrapper: []byte("no LF")}, {Route: FreshnessProof, Entries: [][]byte{[]byte("{}")}}} {
		s, root := setup(t)
		r := proposal()
		r.Proof = p
		before := tree(t, root)
		_, e := s.Commit(r)
		code(t, e, RequestInvalid)
		unchanged(t, root, before)
	}
	for _, kind := range []string{"attestation/1", "grant/1", "revocation/1", "enrollment/1", "journal-entry/1", "checkpoint/1"} {
		s, _ := setup(t)
		r := proposal()
		r.Records[0].Type = kind
		mustCommit(t, s, r)
	}
}

// Exact JSON-local bytes include all request arguments; another principal's
// same request id is a separate key, and request strings are never path pieces.
func TestRetainedArgumentsAndPrincipalScope(t *testing.T) {
	s, root := setup(t)
	r := proposal()
	first := mustCommit(t, s, r)
	for _, mutate := range []func(*Request){func(r *Request) { r.Proof.Bundle = []byte("other carrier") }, func(r *Request) { r.Checkpoint = []byte("other checkpoint") }, func(r *Request) { r.Head = id("different head") }, func(r *Request) { r.Watermark = "2026-01-01T00:00:01Z" }, func(r *Request) { r.Records[0].Type = "enrollment/1" }} {
		n := cloneRequest(r)
		mutate(&n)
		before := tree(t, root)
		_, e := s.Commit(n)
		code(t, e, RequestConflict)
		unchanged(t, root, before)
	}
	r = nextProposal(first)
	r.Principal = "../publisher-b"
	r.RequestID = proposal().RequestID
	mustCommit(t, s, r)
	before := tree(t, root)
	again := mustCommit(t, s, proposal())
	if !reflect.DeepEqual(first, again) {
		t.Fatal("later head changed original outcome")
	}
	unchanged(t, root, before)
	preview, e := s.Preview(proposal())
	if e != nil || preview.StateID != s.stateID {
		t.Fatal("retry preview lost current recovery identity")
	}
	if strings.Contains(recordPath(r.Records[0]), "publisher") {
		t.Fatal("principal used as path")
	}
}

// Opaque bytes remain unchanged at the independently specified paths and
// sixteen-digit sequence names; returned values do not alias caller buffers.
func TestSequencePathsAndOpaqueBytes(t *testing.T) {
	s, root := setup(t)
	r := proposal()
	out := mustCommit(t, s, r)
	for path, want := range map[string][]byte{
		"journal/0000000000000001.json":                r.Journal,
		"checkpoints/0000000000000001.json":            r.Checkpoint,
		"records/grant-1/" + r.Records[0].ID + ".json": r.Records[0].Bytes,
	} {
		got, e := os.ReadFile(filepath.Join(root, path))
		if e != nil || !bytes.Equal(got, want) {
			t.Fatalf("exact storage bytes differ at %s", path)
		}
	}
	r.Result[0] ^= 1
	r.Journal[0] ^= 1
	r.Records[0].Bytes[0] ^= 1
	if bytes.Equal(out.Result, r.Result) {
		t.Fatal("result aliases input")
	}
	original := proposal()
	firstTree := tree(t, root)
	retry := mustCommit(t, s, original)
	if !reflect.DeepEqual(out, retry) {
		t.Fatal("caller mutation changed retained state")
	}
	unchanged(t, root, firstTree)
}

// Canonical but changed recovery instructions cannot be accepted by a live
// writer merely because the embedded candidate still has its original digest.
func TestManifestMutationFencesLiveWriter(t *testing.T) {
	s, root := setup(t)
	mustCommit(t, s, proposal())
	path := "transactions/" + s.stateID + ".json"
	raw, e := s.disk.read(path)
	if e != nil {
		t.Fatal(e)
	}
	var m manifest
	if !decode(raw, &m) {
		t.Fatal("healthy manifest unreadable")
	}
	m.Base = id("foreign-base")
	if e = os.WriteFile(filepath.Join(root, path), canonical(m), 0600); e != nil {
		t.Fatal(e)
	}
	before := tree(t, root)
	_, e = s.View()
	code(t, e, StateUnavailable)
	_, e = s.Commit(nextProposal(Outcome{Head: proposal().Head}))
	code(t, e, StateUnavailable)
	unchanged(t, root, before)
}

// Two complete descendants of one base are not resolved by choosing a filename,
// sequence or CURRENT. Both originals survive the typed fenced refusal.
func TestForkedRecoveryFenced(t *testing.T) {
	s, root, r, expected := prepareSecond(t)
	firstBase := s.stateID
	base := clone(s.active)
	mustCommit(t, s, r)
	rival := cloneRequest(r)
	rival.RequestID = "rival"
	rival.Head = id("rival-head")
	rival.Result = []byte("rival-outcome")
	next, _, e := candidate(base, rival)
	if e != nil {
		t.Fatal(e)
	}
	m := manifest{Base: firstBase, Request: rival, Candidate: next}
	if e = os.WriteFile(filepath.Join(root, "transactions", digest(next)+".json"), canonical(m), 0600); e != nil {
		t.Fatal(e)
	}
	before := tree(t, root)
	code(t, s.Reconcile(expected.StateID), StateUnavailable)
	unchanged(t, root, before)
	_, e = s.View()
	code(t, e, StateUnavailable)
}

// Recovery's own durability failures keep the store fenced; repeating local
// reconciliation with the same protected candidate can then expose exact bytes.
func TestRecoverySyncFailureFences(t *testing.T) {
	s, _, r, expected := prepareSecond(t)
	mustCommit(t, s, r)
	applied := false
	s.disk.hooks.before = func(op, p string) error {
		if op == "file.sync" {
			applied = true
			return errors.New("recovery sync failure")
		}
		return nil
	}
	code(t, s.Reconcile(expected.StateID), StateUnavailable)
	if !applied {
		t.Fatal("recovery fault not applied")
	}
	s.disk.hooks = hooks{}
	_, e := s.View()
	code(t, e, StateUnavailable)
	if e = s.Reconcile(expected.StateID); e != nil {
		t.Fatal(e)
	}
	assertExact(t, s, r, expected)
}

// Even failure before creating the first intent file cannot let this instance
// clear its uncertain candidate by reconciling to the previous local state.
func TestInMemoryIntentCannotRollback(t *testing.T) {
	s, root, r, expected := prepareSecond(t)
	previous := s.stateID
	before := tree(t, root)
	s.disk.hooks.before = func(op, p string) error {
		if op == "file.open" {
			return errors.New("intent open failure")
		}
		return nil
	}
	_, e := s.Commit(r)
	code(t, e, StateUnavailable)
	s.disk.hooks = hooks{}
	unchanged(t, root, before)
	code(t, s.Reconcile(previous), StateUnavailable)
	unchanged(t, root, before)
	code(t, s.Reconcile(expected.StateID), StateUnavailable)
	unchanged(t, root, before)
}
