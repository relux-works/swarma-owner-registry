//go:build linux

package store

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// Changed, missing and swapped delta evidence fences without writes; the same
// untouched chain reopens at the protected latest id (not directory order).
func TestDeltaChainTamper(t *testing.T) {
	for _, mode := range []string{"changed", "missing", "reordered"} {
		t.Run(mode, func(t *testing.T) {
			s, root, r, expected := prepareSecond(t)
			first := s.stateID
			mustCommit(t, s, r)
			s.Close()
			control, e := Open(root, expected.StateID)
			if e != nil {
				t.Fatal(e)
			}
			control.Close()
			p := filepath.Join(root, "transactions", first+".json")
			raw, e := os.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "changed":
				var m manifest
				if !decode(raw, &m) {
					t.Fatal("decode")
				}
				// Replace this delta with a semantically valid different operation and
				// exact blob. Only its protected chained identity authorizes the original.
				requestBytes, e := os.ReadFile(filepath.Join(root, "blobs", m.Retained.Request+".blob"))
				if e != nil {
					t.Fatal(e)
				}
				var request packedRequest
				if !decode(requestBytes, &request) {
					t.Fatal("request decode")
				}
				request.Arguments.Operation = "different-valid-operation"
				changed := canonical(request)
				m.Retained.Request = byteID(changed)
				if e = os.WriteFile(filepath.Join(root, "blobs", m.Retained.Request+".blob"), changed, 0600); e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(p, canonical(m), 0600); e != nil {
					t.Fatal(e)
				}
			case "missing":
				if e = os.Remove(p); e != nil {
					t.Fatal(e)
				}
			case "reordered":
				q := filepath.Join(root, "transactions", expected.StateID+".json")
				other, e := os.ReadFile(q)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(p, other, 0600); e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(q, raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			before := tree(t, root)
			bad, e := Open(root, expected.StateID)
			if bad != nil {
				bad.Close()
				t.Fatal("tampered chain admitted")
			}
			code(t, e, StateUnavailable)
			unchanged(t, root, before)
		})
	}
}

// A retained blob with changed bytes never yields a retry or a recovered state;
// the byte-exact original opens and retries before corruption.
func TestBlobDigestMismatchFences(t *testing.T) {
	s, root := setup(t)
	r := proposal()
	r.Proof.Bundle = bytes.Repeat([]byte("b"), 65536)
	out := mustCommit(t, s, r)
	expected := s.stateID
	path := filepath.Join(root, "blobs", byteID(r.Proof.Bundle)+".blob")
	again := mustCommit(t, s, r)
	if !reflect.DeepEqual(out, again) {
		t.Fatal("control retry")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	raw[len(raw)/2] ^= 1
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	before := tree(t, root)
	_, e = s.View()
	code(t, e, StateUnavailable)
	unchanged(t, root, before)
	s.Close()
	bad, e := Open(root, expected)
	if bad != nil {
		bad.Close()
		t.Fatal("changed blob admitted")
	}
	code(t, e, StateUnavailable)
	unchanged(t, root, before)
}

// Total logical capacity is charged before the intent seam, not after a write;
// the same request with adequate capacity succeeds, and its retry needs no space.
func TestTotalCapacityNoIntent(t *testing.T) {
	s, root := setup(t)
	r := proposal()
	before := tree(t, root)
	s.maxBytes = uint64(len(canonical(genesis())) + 72)
	calls := 0
	s.disk.hooks.before = func(op, p string) error { calls++; return nil }
	_, e := s.Preview(r)
	code(t, e, CapacityExhausted)
	_, e = s.Commit(r)
	code(t, e, CapacityExhausted)
	if calls != 0 || s.pendingID != "" || s.fenced {
		t.Fatal("capacity refusal crossed intent")
	}
	unchanged(t, root, before)
	s.disk.hooks = hooks{}
	s.maxBytes = DefaultMaxBytes
	want := mustCommit(t, s, r)
	s.maxBytes = 1
	got := mustCommit(t, s, r)
	if !reflect.DeepEqual(want, got) {
		t.Fatal("retry charged again")
	}
}

func seriesRequest(i int, head string, bundle int) Request {
	r := proposal()
	r.RequestID = fmt.Sprintf("request-%d", i)
	r.Head = id(fmt.Sprintf("head-%d", i))
	r.ExpectedHead = head
	r.Records[0].ID = id(fmt.Sprintf("record-%d", i))
	r.Proof.Bundle = bytes.Repeat([]byte{byte(i)}, bundle)
	if bundle >= 8 {
		binary.LittleEndian.PutUint64(r.Proof.Bundle, uint64(i))
	}
	return r
}
func storageBytes(t *testing.T, root string) uint64 {
	t.Helper()
	var n uint64
	e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			st, e := d.Info()
			if e != nil {
				return e
			}
			n += uint64(st.Size())
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return n
}

// A 1,000-commit real-filesystem history with distinct 64 KiB carriers uses less
// than 128 MiB and less than 1/100 of the former full-image carrier estimate.
// Exact original retries remain byte-identical after those later deltas/restart.
func TestLinearRetainedBundleSize(t *testing.T) {
	s, root := setup(t)
	var first Outcome
	var original Request
	head := ""
	for i := 0; i < 1000; i++ {
		r := seriesRequest(i, head, 65536)
		out := mustCommit(t, s, r)
		if i%25 == 24 && storageBytes(t, root) > 128*1024*1024 {
			t.Fatalf("linear size ceiling exceeded at commit %d: %d", i+1, storageBytes(t, root))
		}
		head = out.Head
		if i == 0 {
			first = out
			original = r
		}
	}
	total := storageBytes(t, root)
	old := uint64(1000*1001/2) * uint64(((65536+2)/3)*4)
	t.Logf("commits=1000 bundle=65536 total=%d old-carrier-estimate=%d ratio=%.5f", total, old, float64(total)/float64(old))
	if total > 128*1024*1024 || total*100 >= old {
		t.Fatal("retained history is not linear within the declared bound")
	}
	wantID := s.stateID
	s.Close()
	restarted, e := Open(root, wantID)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	before := storageBytes(t, root)
	again := mustCommit(t, restarted, original)
	if !reflect.DeepEqual(first, again) || storageBytes(t, root) != before {
		t.Fatal("old retry changed outcome or appended")
	}
}

// Opening 2,000 small real commits holds digest/path indexes, not historical
// payloads; sampled additional live heap stays below 64 MiB. This is a measured
// workload bound, not a constant-space claim for arbitrarily large indexes.
func TestBoundedMemoryOpen(t *testing.T) {
	s, root := setup(t)
	head := ""
	for i := 0; i < 2000; i++ {
		head = mustCommit(t, s, seriesRequest(i, head, 0)).Head
	}
	expected := s.stateID
	s.Close()
	s = nil
	runtime.GC()
	var start runtime.MemStats
	runtime.ReadMemStats(&start)
	var peak atomic.Uint64
	peak.Store(start.HeapAlloc)
	stop := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		for {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			for old := peak.Load(); m.HeapAlloc > old; old = peak.Load() {
				if peak.CompareAndSwap(old, m.HeapAlloc) {
					break
				}
			}
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	opened, e := Open(root, expected)
	close(stop)
	<-joined
	if e != nil {
		t.Fatal(e)
	}
	defer opened.Close()
	var end runtime.MemStats
	runtime.ReadMemStats(&end)
	high := peak.Load()
	if end.HeapAlloc > high {
		high = end.HeapAlloc
	}
	growth := high - start.HeapAlloc
	t.Logf("commits=2000 baseline=%d peak=%d extra=%d limit=%d", start.HeapAlloc, high, growth, 64*1024*1024)
	if growth > 64*1024*1024 {
		t.Fatal("Open exceeded measured working-heap bound")
	}
	if opened.active.Sequence != 2000 {
		t.Fatal("missing fold entries")
	}
}

// Configured bootstrap and commit budgets accept their exact reserved byte
// boundary, refuse one byte below without even mkdir/intent, and persist no
// request/candidate on refusal. Zero is documented as the default, not no space.
func TestCapacityOptionsExactBoundary(t *testing.T) {
	root := emptyRoot(t)
	before := tree(t, root)
	initial := uint64(len(canonical(genesis())) + 72)
	s, e := CreateWithOptions(root, Options{MaxBytes: initial - 1})
	if s != nil {
		s.Close()
		t.Fatal("undersized bootstrap succeeded")
	}
	code(t, e, CapacityExhausted)
	unchanged(t, root, before)
	s, e = CreateWithOptions(root, Options{MaxBytes: initial})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := proposal()
	next, _, e := candidate(s.active, r)
	if e != nil {
		t.Fatal(e)
	}
	m, blobs := delta(s.stateID, next, r)
	needed := initial + uint64(len(canonical(m))+72)
	for _, b := range blobs {
		needed += uint64(len(b))
	}
	for _, id := range m.Files {
		needed += uint64(len(blobs[id]))
	}
	before = tree(t, root)
	s.maxBytes = needed - 1
	_, e = s.Commit(r)
	code(t, e, CapacityExhausted)
	unchanged(t, root, before)
	s.maxBytes = needed
	mustCommit(t, s, r)
	if storageBytes(t, root) != needed-72 {
		t.Fatal("capacity accounting differs from final regular bytes plus CURRENT staging reserve")
	}
	expected := s.stateID
	s.Close()
	opened, e := OpenWithOptions(root, expected, Options{MaxBytes: needed})
	if e != nil {
		t.Fatal(e)
	}
	defer opened.Close()
	before = tree(t, root)
	_, e = opened.Commit(nextProposal(Outcome{Head: r.Head}))
	code(t, e, CapacityExhausted)
	unchanged(t, root, before)
}

// Nil and empty payloads remain distinct retained arguments after reopening;
// both valid forms independently commit, and altered retries append nothing.
func TestRetainedNilAndEmptyPayloads(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			s, root := setup(t)
			r := proposal()
			r.Result = nil
			r.Proof.Route = JournalProof
			if empty {
				r.Result = []byte{}
				r.Proof.Bundle = []byte{}
				r.Proof.Entries = [][]byte{}
			}
			out := mustCommit(t, s, r)
			expected := s.stateID
			s.Close()
			opened, e := Open(root, expected)
			if e != nil {
				t.Fatal(e)
			}
			defer opened.Close()
			before := tree(t, root)
			changed := cloneRequest(r)
			if empty {
				changed.Result = nil
			} else {
				changed.Result = []byte{}
			}
			_, e = opened.Commit(changed)
			code(t, e, RequestConflict)
			unchanged(t, root, before)
			again := mustCommit(t, opened, r)
			if !reflect.DeepEqual(out, again) {
				t.Fatal("nil/empty retry changed")
			}
			unchanged(t, root, before)
		})
	}
}

// Invalid reference counts reject before the first payload lookup; a valid
// descriptor expands through individually bounded reads. A single oversized
// referenced result refuses before allocation of that file's contents.
func TestRetainedReferencesBoundedBeforeExpansion(t *testing.T) {
	packed, blobs := packRequest(proposal())
	for _, name := range []string{"records", "entries", "publications", "checkpoints", "ids", "keys"} {
		t.Run(name, func(t *testing.T) {
			p := clone(packed)
			switch name {
			case "records":
				p.Records = make([]string, 65)
				p.Arguments.Records = make([]Record, 65)
			case "entries":
				p.Entries = make([]string, 65)
			case "publications":
				p.Publications = make([]string, 65)
			case "checkpoints":
				p.Checkpoints = make([]string, 3)
			case "ids":
				p.Arguments.Proof.QueryIDs = make([]string, 65)
			case "keys":
				p.Arguments.Proof.QueryKeys = make([]string, 65)
			}
			calls := 0
			_, e := expandRequest(canonical(p), func(id string, limit int) ([]byte, error) { calls++; return nil, nil })
			if e == nil || e.Error() != "retained reference bounds" || calls != 0 {
				t.Fatalf("reference bounds did not precede expansion: calls=%d error=%v", calls, e)
			}
		})
	}
	calls := 0
	r, e := expandRequest(canonical(packed), func(id string, limit int) ([]byte, error) {
		calls++
		b := blobs[id]
		if len(b) > limit {
			t.Fatal("wrong per-field limit")
		}
		return b, nil
	})
	if e != nil || calls == 0 || !reflect.DeepEqual(r, proposal()) {
		t.Fatalf("valid bounded expansion refused: %v", e)
	}
	s, root := setup(t)
	large := bytes.Repeat([]byte("x"), 65537)
	digest := byteID(large)
	if e = os.WriteFile(filepath.Join(root, "blobs", digest+".blob"), large, 0600); e != nil {
		t.Fatal(e)
	}
	before := tree(t, root)
	if _, e = s.readBlobLimit(digest, 65536); e == nil {
		t.Fatal("oversized result read accepted")
	}
	unchanged(t, root, before)
	exact, e := s.readBlobLimit(digest, 65537)
	if e != nil || !bytes.Equal(exact, large) {
		t.Fatal("exact sized blob refused")
	}
}
