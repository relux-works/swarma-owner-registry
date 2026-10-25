//go:build linux

package store

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func id(s string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s))) }
func proposal() Request {
	return Request{Principal: "publisher-a", RequestID: "request-a", Operation: "add", Head: id("head-a"), Watermark: "2026-01-01T00:00:00Z", Records: []Record{{Type: "grant/1", ID: id("body-a"), Bytes: []byte("{\"body\":1}")}}, Journal: []byte("journal-a\n"), Checkpoint: []byte("checkpoint-a\n"), Result: []byte("accepted-a")}
}
func setup(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	s, e := Create(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, root
}
func mustCommit(t *testing.T, s *Store, r Request) Outcome {
	t.Helper()
	v, e := s.Commit(r)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func code(t *testing.T, e error, want Code) {
	t.Helper()
	var v *Error
	if !errors.As(e, &v) || v.Code != want || e.Error() != string(want) {
		t.Fatalf("want %s, got %v", want, e)
	}
}
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		n, _ := filepath.Rel(root, p)
		if d.IsDir() {
			m[n] = "dir"
			return nil
		}
		b, e := os.ReadFile(p)
		if e == nil {
			m[n] = string(b)
		}
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func unchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(before, tree(t, root)) {
		t.Fatal("refusal changed directory tree")
	}
}
func cloneRequest(r Request) Request { return clone(r) }

// A new request cannot replace the exact bytes of an existing content id; the original read and tree remain unchanged.
func TestChangedBytesSameIDRefused(t *testing.T) {
	s, root := setup(t)
	r := proposal()
	first := mustCommit(t, s, r)
	r.RequestID = "request-b"
	r.ExpectedHead = first.Head
	r.Head = id("head-b")
	r.Records[0].Bytes = []byte("changed signature order")
	before := tree(t, root)
	_, e := s.Commit(r)
	code(t, e, EnvelopeConflict)
	unchanged(t, root, before)
	b, e := s.ReadRecord(r.Records[0].Type, r.Records[0].ID)
	if e != nil || !bytes.Equal(b, proposal().Records[0].Bytes) {
		t.Fatal("original evidence overwritten")
	}
}

// Concurrent proposals for one expected head have exactly one winner, one typed head conflict and a single allocated sequence.
func TestConcurrentHeadConflict(t *testing.T) {
	s, _ := setup(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := proposal()
			r.RequestID = fmt.Sprint(i)
			_, e := s.Commit(r)
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	wins, conflicts := 0, 0
	for e := range errs {
		if e == nil {
			wins++
		} else {
			code(t, e, HeadConflict)
			conflicts++
		}
	}
	v, e := s.View()
	if e != nil || wins != 1 || conflicts != 1 || v.Sequence != 1 {
		t.Fatalf("wins=%d conflicts=%d view=%+v err=%v", wins, conflicts, v, e)
	}
}

// A file-sync failure fences both readers and writers until explicit local reconcile; a healthy commit controls the same path.
func TestSyncFailureFences(t *testing.T) {
	s, _ := setup(t)
	expected, e := s.Preview(proposal())
	if e != nil {
		t.Fatal(e)
	}
	s.disk.hooks.before = func(op, path string) error {
		if op == "file.sync" {
			return errors.New("injected")
		}
		return nil
	}
	_, e = s.Commit(proposal())
	code(t, e, StateUnavailable)
	s.disk.hooks = hooks{}
	_, e = s.View()
	code(t, e, StateUnavailable)
	_, e = s.Commit(proposal())
	code(t, e, StateUnavailable)
	code(t, s.Reconcile(expected.StateID), StateUnavailable)
	healthy, _ := setup(t)
	mustCommit(t, healthy, proposal())
}

// Capacity is checked before reservation, so an exhausted in-memory boundary cannot write or wrap. The pure successor's final valid value is also checked; this is not a fabricated full history.
func TestSequenceExhaustionNoWrap(t *testing.T) {
	s, root := setup(t)
	s.active.Sequence = MaxSequence
	before := tree(t, root)
	_, e := s.Commit(proposal())
	code(t, e, CapacityExhausted)
	unchanged(t, root, before)
	n, e := successor(MaxSequence - 1)
	if e != nil || n != MaxSequence {
		t.Fatal("last valid sequence unavailable")
	}
	_, e = successor(MaxSequence)
	code(t, e, CapacityExhausted)
}

func proofRecord() []byte { return []byte("{}" + strings.Repeat(" ", 8190)) }
func bounded(route Route) Proof {
	p := Proof{Route: route}
	if route == JournalProof {
		for i := 0; i < 64; i++ {
			p.Entries = append(p.Entries, proofRecord())
		}
		p.Checkpoints = [][]byte{proofRecord(), proofRecord()}
	} else {
		for i := 0; i < 64; i++ {
			p.Publications = append(p.Publications, proofRecord())
		}
		p.Checkpoints = [][]byte{proofRecord()}
		p.QueryIDs = make([]string, 64)
		p.QueryKeys = make([]string, 64)
	}
	p.Bundle = make([]byte, 2*1024*1024)
	return p
}

// Independent received-bundle, count, per-record, depth and query limits refuse before any file or intent is written, beside complete bound-sized controls.
func TestPublicationProofCapRefused(t *testing.T) {
	for _, name := range []string{"bundle", "j-count", "j-checkpoints", "j-record", "j-depth", "f-count", "f-checkpoints", "f-ids", "f-keys", "f-record"} {
		t.Run(name, func(t *testing.T) {
			s, root := setup(t)
			r := proposal()
			r.Proof = bounded(JournalProof)
			switch name {
			case "bundle":
				r.Proof.Bundle = make([]byte, 2*1024*1024+1)
			case "j-count":
				r.Proof.Entries = append(r.Proof.Entries, []byte("{}"))
			case "j-checkpoints":
				r.Proof.Checkpoints = append(r.Proof.Checkpoints, []byte("{}"))
			case "j-record":
				r.Proof.Entries[0] = make([]byte, 8193)
			case "j-depth":
				r.Proof.Entries[0] = []byte(strings.Repeat("[", 17) + "0" + strings.Repeat("]", 17))
			case "f-count":
				r.Proof = bounded(FreshnessProof)
				r.Proof.Publications = append(r.Proof.Publications, []byte("{}"))
			case "f-checkpoints":
				r.Proof = bounded(FreshnessProof)
				r.Proof.Checkpoints = append(r.Proof.Checkpoints, []byte("{}"))
			case "f-ids":
				r.Proof = bounded(FreshnessProof)
				r.Proof.QueryIDs = append(r.Proof.QueryIDs, "extra")
			case "f-keys":
				r.Proof = bounded(FreshnessProof)
				r.Proof.QueryKeys = append(r.Proof.QueryKeys, "extra")
			case "f-record":
				r.Proof = bounded(FreshnessProof)
				r.Proof.Publications[0] = make([]byte, 8193)
			}
			before := tree(t, root)
			_, e := s.Commit(r)
			code(t, e, ProofLimit)
			unchanged(t, root, before)
		})
	}
}

// The encoded WS wrapper, including its LF, cannot exceed 8,192 bytes; refusal creates no intent or file.
func TestWitnessWrapperCapRefused(t *testing.T) {
	s, root := setup(t)
	r := proposal()
	r.Proof.Wrapper = append(bytes.Repeat([]byte("x"), 8192), '\n')
	before := tree(t, root)
	_, e := s.Commit(r)
	code(t, e, ProofLimit)
	unchanged(t, root, before)
}

// A changed retained request wins over head and capacity conflicts; exact retries preserve original outcomes without renewing or appending state.
func TestRequestConflictAndGateOrder(t *testing.T) {
	s, root := setup(t)
	r := proposal()
	accepted := mustCommit(t, s, r)
	before := tree(t, root)
	for _, change := range []func(*Request){func(r *Request) { r.Operation = "revoke" }, func(r *Request) { r.Records[0].Bytes = []byte("other") }, func(r *Request) { r.Journal = []byte("other") }, func(r *Request) { r.ExpectedHead = id("other") }, func(r *Request) { r.Result = []byte("other") }} {
		n := cloneRequest(r)
		change(&n)
		s.active.Sequence = MaxSequence
		_, e := s.Commit(n)
		code(t, e, RequestConflict)
		unchanged(t, root, before)
	}
	n := cloneRequest(r)
	n.RequestID = "new"
	_, e := s.Commit(n)
	code(t, e, HeadConflict)
	n.ExpectedHead = accepted.Head
	_, e = s.Commit(n)
	code(t, e, CapacityExhausted)
	unchanged(t, root, before)
}

// Traversal, noncanonical ids and types fail without writes. Filesystem links/special files fence rather than being followed, and a regular tree accepts.
func TestPathSafety(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Request)
		want   Code
	}{
		{"traversal", func(r *Request) { r.Records[0].Type = "../grant/1" }, RecordTypeUnsupported},
		{"unknown-type", func(r *Request) { r.Records[0].Type = "freshness/1" }, RecordTypeUnsupported},
		{"id-traversal", func(r *Request) { r.Records[0].ID = "../file" }, RequestInvalid},
		{"id-uppercase", func(r *Request) { r.Records[0].ID = strings.ToUpper(r.Records[0].ID) }, RequestInvalid},
		{"id-length", func(r *Request) { r.Records[0].ID = id("x") + "a" }, RequestInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, root := setup(t)
			r := proposal()
			tc.change(&r)
			before := tree(t, root)
			_, e := s.Commit(r)
			code(t, e, tc.want)
			unchanged(t, root, before)
		})
	}
	for _, kind := range []string{"link", "fifo", "directory-link"} {
		t.Run(kind, func(t *testing.T) {
			s, root := setup(t)
			target := t.TempDir()
			var e error
			switch kind {
			case "fifo":
				e = syscall.Mkfifo(filepath.Join(root, "pipe"), 0600)
			case "link":
				e = os.Symlink(filepath.Join(target, "missing"), filepath.Join(root, "link"))
			case "directory-link":
				e = os.Symlink(target, filepath.Join(root, "records", "link"))
			}
			if e != nil {
				t.Fatal(e)
			}
			_, e = s.Commit(proposal())
			code(t, e, StateUnavailable)
			if len(mustReadDir(t, target)) != 0 {
				t.Fatal("link target changed")
			}
		})
	}
	s, _ := setup(t)
	mustCommit(t, s, proposal())
}
func mustReadDir(t *testing.T, p string) []os.DirEntry {
	t.Helper()
	v, e := os.ReadDir(p)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

// Exact retries, including after restart, return owned copies of the retained outcome and do not add history; missing authoritative bytes fence reads.
func TestExactRetrySameOutcome(t *testing.T) {
	s, root := setup(t)
	r := proposal()
	first := mustCommit(t, s, r)
	before := tree(t, root)
	second := mustCommit(t, s, r)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("retry outcome changed")
	}
	unchanged(t, root, before)
	second.Result[0] ^= 1
	third := mustCommit(t, s, r)
	if !reflect.DeepEqual(first, third) {
		t.Fatal("outcome aliases caller")
	}
	v, e := s.View()
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	restarted, e := Open(root, v.StateID)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if !reflect.DeepEqual(first, mustCommit(t, restarted, r)) {
		t.Fatal("restart lost outcome")
	}
	unchanged(t, root, before)
	if e = os.Remove(filepath.Join(root, recordPath(r.Records[0]))); e != nil {
		t.Fatal(e)
	}
	_, e = restarted.ReadRecord(r.Records[0].Type, r.Records[0].ID)
	code(t, e, StateUnavailable)
}

// Both independent J/F maxima fit the separate two-MiB received route; opaque proof records here prove budgets, not cryptographic validity.
func TestBoundedCompleteProofAccepted(t *testing.T) {
	for _, route := range []Route{JournalProof, FreshnessProof} {
		t.Run(string(route), func(t *testing.T) { s, _ := setup(t); r := proposal(); r.Proof = bounded(route); mustCommit(t, s, r) })
	}
}

// A whole wrapper exactly 8,192 bytes including LF fits before the first durable operation; these opaque bytes do not qualify WS transport.
func TestWrapperFitsBeforeIntent(t *testing.T) {
	s, _ := setup(t)
	r := proposal()
	r.Proof.Wrapper = append(bytes.Repeat([]byte("x"), 8191), '\n')
	mustCommit(t, s, r)
}
