package grantattestation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/swarma-envelope/record"
)

// All protected-view/session behavior below is injected test behavior, not an
// authenticated provider. The only signed bytes are an unchanged public pinned
// vector: no key is generated, loaded, activated or used to sign by these tests.
type testSource struct {
	closeDelay                                                                                     time.Duration
	readFailure, noProgress                                                                        bool
	wire                                                                                           []byte
	p                                                                                              Provenance
	deny, unpublished, rollback, errorWithLease, closeError, finalRollback, changeProvenance, slow bool
	opens, reads, consumed, closes, checks                                                         int
	entered, release                                                                               chan struct{}
}
type testLease struct {
	source *testSource
	ctx    context.Context
	offset int
}

func (s *testSource) Open(ctx context.Context, id string) (Lease, error) {
	s.opens++
	if s.entered != nil {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.deny {
		return nil, errors.New("private principal metadata")
	}
	l := &testLease{source: s, ctx: ctx}
	if s.errorWithLease {
		return l, errors.New(strings.Repeat("private", 10000))
	}
	return l, nil
}
func (l *testLease) Provenance() Provenance {
	p := l.source.p
	if l.source.changeProvenance && l.source.checks >= 2 {
		p.StateID = record.Digest([]byte("changed view"))
	}
	return p
}
func (l *testLease) Check(ctx context.Context) error {
	s := l.source
	s.checks++
	if ctx.Err() != nil || s.unpublished || s.rollback || (s.finalRollback && s.checks%2 == 0) {
		return ErrUnavailable
	}
	return nil
}
func (l *testLease) Read(p []byte) (int, error) {
	s := l.source
	s.reads++
	if s.noProgress {
		return 0, nil
	}
	if s.readFailure && l.offset > 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if s.slow {
		<-l.ctx.Done()
		return 0, l.ctx.Err()
	}
	if l.offset == len(s.wire) {
		return 0, io.EOF
	}
	n := copy(p, s.wire[l.offset:])
	l.offset += n
	s.consumed += n
	return n, nil
}
func (l *testLease) Close() error {
	l.source.closes++
	time.Sleep(l.source.closeDelay)
	if l.source.closeError {
		return ErrUnavailable
	}
	return nil
}
func fixture(t *testing.T) *testSource {
	t.Helper()
	wire, err := os.ReadFile("testdata/pinned-agent.json")
	if err != nil {
		t.Fatal(err)
	}
	if record.Digest(wire) != "sha256:471799e158df60c2b89455b8685d64559bd41a6965693225e49e82c46447e9af" {
		t.Fatal("changed pinned fixture")
	}
	v, err := record.Parse(wire, 8192, 16)
	if err != nil {
		t.Fatal(err)
	}
	e := object(v)
	b := object(e["body"])
	return &testSource{wire: wire, p: Provenance{ContentID: stringOf(e["id"]), WireSHA256: record.Digest(wire), StateID: record.Digest([]byte("synthetic protected state")), JournalID: record.Digest([]byte("synthetic journal")), CheckpointID: record.Digest([]byte("synthetic checkpoint")), Owner: stringOf(object(b["binding"])["owner"]), Subject: stringOf(object(b["subject"])["key_id"]), Issuer: stringOf(b["issuer"])}}
}
func reader(t *testing.T, s *testSource) *Reader {
	t.Helper()
	r, err := New(s, s.p.Issuer, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func read(t *testing.T, r *Reader, id string) (Original, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	return r.Read(ctx, id)
}
func refused(t *testing.T, out Original, err error) {
	t.Helper()
	if err != ErrUnavailable || out.Wire != nil || out.Provenance != (Provenance{}) {
		t.Fatalf("disclosed on refusal: error=%v bytes=%d", err, len(out.Wire))
	}
}

func TestOriginalBytesAndReopen(t *testing.T) {
	s := fixture(t)
	r := reader(t, s)
	// Deliberately retain noncanonical envelope whitespace. The original wire
	// and its separate publication digest must survive without reserialization.
	s.wire = append(s.wire, '\n')
	s.p.WireSHA256 = record.Digest(s.wire)
	for i := 0; i < 2; i++ {
		out, err := read(t, r, s.p.ContentID)
		if err != nil || !bytes.Equal(out.Wire, s.wire) || out.Provenance != s.p {
			t.Fatalf("original read: %v", err)
		}
		out.Wire[0] = 'X'
		if s.wire[0] == 'X' {
			t.Fatal("returned bytes alias source")
		}
	}
	if s.opens != 2 || s.closes != 2 || s.checks != 4 {
		t.Fatalf("revalidation counts: %+v", s)
	}
}

func TestOneFaultRefusalAndSameIDRepair(t *testing.T) {
	for _, name := range []string{"reader denied", "unpublished", "rollback", "final rollback", "changed provenance", "close failure", "wrong wire id", "newer for key", "same id changed bytes", "bad issuer", "bad signature", "wrong owner", "wrong subject", "unresolved checkpoint", "oversized error", "partial read failure", "no progress"} {
		t.Run(name, func(t *testing.T) {
			s := fixture(t)
			originalWire := bytes.Clone(s.wire)
			originalP := s.p
			r := reader(t, s)
			switch name {
			case "reader denied":
				s.deny = true
			case "unpublished":
				s.unpublished = true
			case "rollback":
				s.rollback = true
			case "final rollback":
				s.finalRollback = true
			case "changed provenance":
				s.changeProvenance = true
			case "close failure":
				s.closeError = true
			case "wrong wire id":
				v, _ := record.Parse(s.wire, 8192, 16)
				e := object(v)
				e["id"] = record.Digest([]byte("different envelope id"))
				s.wire, _ = record.Canonical(e)
				s.p.WireSHA256 = record.Digest(s.wire)
			case "newer for key":
				// A substituted candidate with a newer binding is still not the
				// requested original. No signing operation is performed.
				v, _ := record.Parse(s.wire, 8192, 16)
				e := object(v)
				b := object(e["body"])
				object(b["binding"])["generation"] = "newer-generation"
				e["id"], _ = record.ContentID("attestation/1", b)
				s.wire, _ = record.Canonical(e)
				s.p.WireSHA256 = record.Digest(s.wire)
			case "same id changed bytes":
				s.wire = append(s.wire, '\n') // same signed ID, different original wire
			case "bad issuer":
				s.p.Issuer = s.p.Subject
			case "bad signature":
				v, _ := record.Parse(s.wire, 8192, 16)
				e := object(v)
				sig := object(e["signatures"].([]any)[0])
				sig["sig"] = strings.Repeat("A", 86)
				s.wire, _ = record.Canonical(e)
				s.p.WireSHA256 = record.Digest(s.wire)
			case "wrong owner":
				s.p.Owner = "different-owner"
			case "wrong subject":
				s.p.Subject = s.p.Issuer
			case "unresolved checkpoint":
				s.p.CheckpointID = ""
			case "oversized error":
				s.errorWithLease = true
			case "partial read failure":
				s.readFailure = true
			case "no progress":
				s.noProgress = true
			}
			out, err := read(t, r, originalP.ContentID)
			refused(t, out, err)
			if (s.deny || s.unpublished || s.rollback || s.errorWithLease) && s.consumed != 0 {
				t.Fatal("read before authorization/publication")
			}
			// Repair only source fault; the original Reader, exact request ID and
			// original wire remain. There is no reservation or effect API here.
			s.wire = originalWire
			s.p = originalP
			s.deny = false
			s.unpublished = false
			s.rollback = false
			s.finalRollback = false
			s.changeProvenance = false
			s.closeError = false
			s.errorWithLease = false
			s.readFailure = false
			s.noProgress = false
			s.checks = 0
			out, err = read(t, r, originalP.ContentID)
			if err != nil || !bytes.Equal(out.Wire, originalWire) {
				t.Fatalf("same-ID repair: %v", err)
			}
		})
	}
}

// Deadline passage must refuse even if cancellation notification has not yet
// been scheduled. This context deliberately never reports Err().
type delayedCancellation struct {
	context.Context
	deadline time.Time
}

func (c delayedCancellation) Deadline() (time.Time, bool) { return c.deadline, true }
func TestAbsoluteDeadlineAfterClose(t *testing.T) {
	s := fixture(t)
	s.closeDelay = 30 * time.Millisecond
	r := reader(t, s)
	ctx := delayedCancellation{Context: context.Background(), deadline: time.Now().Add(15 * time.Millisecond)}
	out, err := r.Read(ctx, s.p.ContentID)
	refused(t, out, err)
	if s.closes != 1 {
		t.Fatal("lease not closed")
	}
}

type sourceFunc func(context.Context, string) (Lease, error)

func (f sourceFunc) Open(ctx context.Context, id string) (Lease, error) { return f(ctx, id) }
func TestTypedNilLeaseIsUnavailable(t *testing.T) {
	s := fixture(t)
	var lease *testLease
	r, err := New(sourceFunc(func(context.Context, string) (Lease, error) { return lease, nil }), s.p.Issuer, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	out, err := read(t, r, s.p.ContentID)
	refused(t, out, err)
}

func TestByteCapsBeforeParsing(t *testing.T) {
	for _, size := range []int{8192, 8193, 1 << 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			s := fixture(t)
			s.wire = append(s.wire, bytes.Repeat([]byte(" "), size-len(s.wire))...)
			s.p.WireSHA256 = record.Digest(s.wire)
			out, err := read(t, reader(t, s), s.p.ContentID)
			if size == 8192 {
				if err != nil || len(out.Wire) != 8192 {
					t.Fatalf("boundary: %v", err)
				}
			} else {
				refused(t, out, err)
			}
			if s.consumed > MaxWireBytes+1 {
				t.Fatalf("unbounded transfer: %d", s.consumed)
			}
		})
	}
}

func TestDeadlinesAndSlowSource(t *testing.T) {
	s := fixture(t)
	r := reader(t, s)
	for _, ctx := range []context.Context{nil, context.Background()} {
		out, err := r.Read(ctx, s.p.ContentID)
		refused(t, out, err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	out, err := r.Read(ctx, s.p.ContentID)
	refused(t, out, err)
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err = r.Read(ctx, s.p.ContentID)
	refused(t, out, err)
	if s.opens != 0 {
		t.Fatal("opened with invalid deadline")
	}
	s.slow = true
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, err = r.Read(ctx, s.p.ContentID)
	refused(t, out, err)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("slow refusal: %v", elapsed)
	}
	if s.consumed != 0 || s.closes != 1 {
		t.Fatal("slow source cleanup")
	}
	s.slow = false
	out, err = read(t, r, s.p.ContentID)
	if err != nil || !bytes.Equal(out.Wire, s.wire) {
		t.Fatalf("deadline repair: %v", err)
	}
}

func TestMissingBindingsAndReference(t *testing.T) {
	s := fixture(t)
	var typedNil *testSource
	for _, src := range []AuthorizedSource{nil, typedNil} {
		if r, err := New(src, s.p.Issuer, time.Second); r != nil || err != ErrUnavailable {
			t.Fatal("nil source accepted")
		}
	}
	for _, pin := range []string{"", s.p.Issuer + "="} {
		if _, err := New(s, pin, time.Second); err != ErrUnavailable {
			t.Fatal("bad pin accepted")
		}
	}
	if _, err := New(s, s.p.Issuer, 0); err != ErrUnavailable {
		t.Fatal("unbounded duration")
	}
	var nilReader *Reader
	out, err := read(t, nilReader, s.p.ContentID)
	refused(t, out, err)
	for _, id := range []string{"", strings.ToUpper(s.p.ContentID), "../" + s.p.ContentID, s.p.ContentID + "\n"} {
		out, err = read(t, reader(t, s), id)
		if err != ErrReference || out.Wire != nil {
			t.Fatal("bad reference accepted")
		}
	}
	if s.opens != 0 {
		t.Fatal("opened invalid reference")
	}
}

func TestConcurrentCallHasNoQueue(t *testing.T) {
	s := fixture(t)
	s.entered = make(chan struct{})
	s.release = make(chan struct{})
	r := reader(t, s)
	done := make(chan error, 1)
	go func() {
		out, err := read(t, r, s.p.ContentID)
		if err == nil && !bytes.Equal(out.Wire, s.wire) {
			err = ErrUnavailable
		}
		done <- err
	}()
	<-s.entered
	out, err := read(t, r, s.p.ContentID)
	refused(t, out, err)
	close(s.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.opens != 1 {
		t.Fatal("queued/opened concurrent request")
	}
}
