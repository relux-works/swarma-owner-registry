package grantattestation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/relux-works/swarma-envelope/record"
	"github.com/relux-works/swarma-owner-registry/internal/store"
)

// This file is an inert proposal experiment, compiled only by go test. None of
// these values authenticate a peer, prove publication, or supply a real fence.
// There is no constructor exported to production, socket, key operation, durable
// format, clock qualification, issuer appointment or consumer binding here.
const proposalOperation = time.Second
const proposalErrorCap = 1024
const proposalTransferCap = 2 * 1024 * 1024

type proposalSession struct {
	authenticated bool
	uid           uint32
	generation    string
	binding       proposalBinding
	expires       time.Time
}

type proposalDisclosure struct {
	kind, id, owner, subject, issuer string
}

// Model of the admitted session binding, not a new record or authority store.
type proposalBinding struct {
	uid                            int64
	generation, owner, path, epoch string
	number                         int64
}

// Injected premises for an ABSENT historical/inclusion verifier. These values
// authenticate nothing. In particular an empty store.Proof is not this proof.
type proposalArchivalModel struct {
	intakeVerified, inclusionVerified bool
}

type proposalSource struct {
	session                      proposalSession
	allow                        proposalDisclosure
	wire                         []byte
	p                            Provenance
	proof                        store.Proof
	archival                     *proposalArchivalModel
	resolved, challenged         bool
	excluded, pending            bool
	state, restoreFloor          uint64
	diagnostic                   []byte
	opens, reads, checks, closes int
	consumed, changedAtCheck     int
	bindingChangedAtCheck        int
	deadline                     time.Time
	readWait                     bool
	closeErr                     bool
}

type proposalLease struct {
	s   *proposalSource
	ctx context.Context
	r   *bytes.Reader
}

// Fixture-only projection to the external port documented in the component
// README, not a callable API implemented by either pinned repository. Construct
// one port per operation with its original context; never renew that deadline.
type proposalPort struct {
	reader *Reader
	ctx    context.Context
}

func (p proposalPort) Attestation(id string) ([]byte, error) {
	original, err := p.reader.Read(p.ctx, id)
	if err != nil {
		return nil, err
	}
	return original.Wire, nil
}

func proposalFixture(t *testing.T) *proposalSource {
	t.Helper()
	// Reuse the exact existing original and its pinned public issuer; fixture()
	// already verifies the original wire digest. It performs no signing.
	f := fixture(t)
	return &proposalSource{
		session: proposalSession{authenticated: true, uid: 1002, generation: "generation-2", expires: time.Now().Add(time.Hour),
			binding: proposalBinding{uid: 1002, generation: "generation-2", owner: "owner-a", path: "message/agent-2", number: 3, epoch: "epoch-1"}},
		// This binding-to-subject mapping is injected protected-source input, not
		// a field of Keymaker.Binding and not authenticated by the fixture.
		allow: proposalDisclosure{kind: "attestation/1", id: f.p.ContentID, owner: f.p.Owner, subject: f.p.Subject, issuer: f.p.Issuer},
		wire:  bytes.Clone(f.wire), p: f.p,
		resolved: true, challenged: true, excluded: true, state: 2, restoreFloor: 2,
		archival: &proposalArchivalModel{intakeVerified: true, inclusionVerified: true},
		// Empty proof is only a shape-budget input. The injected resolved flag is
		// not derived from it and must NEVER be used by a real connector.
		proof: store.Proof{Route: store.JournalProof},
	}
}

func (s *proposalSource) admitted(ctx context.Context, id string) bool {
	deadline, ok := ctx.Deadline()
	a := s.allow
	return ok && ctx.Err() == nil && time.Until(deadline) > 0 &&
		s.session.authenticated && time.Now().Before(s.session.expires) &&
		int64(s.session.uid) == s.session.binding.uid && s.session.generation == s.session.binding.generation &&
		a.kind == "attestation/1" && id == a.id && a.id == s.p.ContentID &&
		a.owner == s.p.Owner && a.subject == s.p.Subject && a.issuer == s.p.Issuer &&
		s.archival != nil && s.archival.intakeVerified && s.archival.inclusionVerified &&
		s.resolved && s.challenged && s.excluded && !s.pending &&
		s.state >= s.restoreFloor
}

// Parse the exact original bytes without reserialization. The unchanged core
// separately verifies their digest, content ID and signature. Equality here
// binds all six existing signed fields to the admitted session binding; it
// does not authenticate the protected source that supplied that binding.
func (s *proposalSource) signedBindingMatches() bool {
	v, err := record.Parse(s.wire, MaxWireBytes, 16)
	if err != nil {
		return false
	}
	b := object(object(object(v)["body"])["binding"])
	uid, uidOK := b["uid"].(int64)
	number, numberOK := b["registration_number"].(int64)
	a := s.session.binding
	return uidOK && numberOK && uid == a.uid &&
		b["generation"] == a.generation && b["owner"] == a.owner &&
		b["path"] == a.path && number == a.number && b["master_epoch"] == a.epoch
}

func (s *proposalSource) Open(ctx context.Context, id string) (Lease, error) {
	s.opens++
	s.deadline, _ = ctx.Deadline()
	// All checks precede opening original bytes. Core has no API for these
	// independent proof/transport/error budgets; this is fixture policy only.
	transfer := len(s.wire) + len(s.proof.Bundle) + len(s.proof.Wrapper) + len(s.diagnostic)
	if !s.admitted(ctx, id) || store.Preflight(s.proof) != nil ||
		len(s.diagnostic) > proposalErrorCap || transfer > proposalTransferCap || len(s.wire) > MaxWireBytes {
		return nil, errors.New("private diagnostic withheld")
	}
	if !s.signedBindingMatches() {
		return nil, ErrUnavailable
	}
	return &proposalLease{s: s, ctx: ctx, r: bytes.NewReader(s.wire)}, nil
}

func (l *proposalLease) Provenance() Provenance { return l.s.p }
func (l *proposalLease) Check(ctx context.Context) error {
	s := l.s
	s.checks++
	if s.changedAtCheck == s.checks {
		s.excluded = false
	}
	if s.bindingChangedAtCheck == s.checks {
		s.session.binding.path = "message/other-agent"
	}
	d, _ := ctx.Deadline()
	if !d.Equal(s.deadline) || !s.admitted(ctx, s.allow.id) || !s.signedBindingMatches() {
		return ErrUnavailable
	}
	return nil
}
func (l *proposalLease) Read(p []byte) (int, error) {
	l.s.reads++
	if l.s.readWait {
		<-l.ctx.Done()
		return 0, l.ctx.Err()
	}
	if l.ctx.Err() != nil {
		return 0, l.ctx.Err()
	}
	n, err := l.r.Read(p)
	l.s.consumed += n
	return n, err
}
func (l *proposalLease) Close() error {
	l.s.closes++
	if l.s.closeErr {
		return ErrUnavailable
	}
	return l.ctx.Err()
}

func proposalRead(t *testing.T, s *proposalSource, ctx context.Context, id string) (Original, error) {
	t.Helper()
	r, err := New(s, s.p.Issuer, proposalOperation)
	if err != nil {
		t.Fatal(err)
	}
	return r.Read(ctx, id)
}

func proposalContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 500*time.Millisecond)
}

func TestProposalExactOriginal(t *testing.T) {
	s := proposalFixture(t)
	ctx, cancel := proposalContext()
	defer cancel()
	out, err := proposalRead(t, s, ctx, s.allow.id)
	if err != nil || !bytes.Equal(out.Wire, s.wire) || out.Provenance != s.p {
		t.Fatalf("exact original control: %v", err)
	}
	if s.opens != 1 || s.closes != 1 || s.checks != 2 || s.consumed != len(s.wire) {
		t.Fatal("original operation lifecycle changed")
	}
	t.Logf("original_sha256=%s bytes=%d opens=%d checks=%d closes=%d; expired original accepted by SYNTHETIC archival model, no proof qualification", record.Digest(out.Wire), len(out.Wire), s.opens, s.checks, s.closes)
}

func TestProposalPortProjection(t *testing.T) {
	s := proposalFixture(t)
	r, err := New(s, s.p.Issuer, proposalOperation)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := proposalContext()
	defer cancel()
	p := proposalPort{reader: r, ctx: ctx}
	wire, err := p.Attestation(s.allow.id)
	if err != nil || !bytes.Equal(wire, s.wire) {
		t.Fatal("original-only port projection failed", err)
	}
	// A fresh per-operation projection still cannot renew a cancelled context.
	cancel()
	p = proposalPort{reader: r, ctx: ctx}
	wire, err = p.Attestation(s.allow.id)
	if err != ErrUnavailable || wire != nil || s.opens != 1 {
		t.Fatal("port renewed caller operation or disclosed on refusal", err)
	}
	t.Log("fixture port returned only exact original bytes; cancelled original context was not renewed; no consumer installed")
}

func TestProposalAdmissionRefusals(t *testing.T) {
	cases := []struct {
		name   string
		change func(*proposalSource)
	}{
		{"unauthenticated-reader", func(s *proposalSource) { s.session.authenticated = false }},
		{"wrong-uid", func(s *proposalSource) { s.session.uid++ }},
		{"expired-reader-session", func(s *proposalSource) { s.session.expires = time.Now().Add(-time.Second) }},
		{"reused-uid-generation", func(s *proposalSource) { s.session.generation = "retired-generation" }},
		{"wrong-record-type-policy", func(s *proposalSource) { s.allow.kind = "grant/1" }},
		{"wrong-exact-record-policy", func(s *proposalSource) { s.allow.id = record.Digest([]byte("another record")) }},
		{"wrong-owner-policy", func(s *proposalSource) { s.allow.owner = "other-owner" }},
		{"wrong-subject-policy", func(s *proposalSource) { s.allow.subject = s.p.Issuer }},
		{"wrong-issuer-policy", func(s *proposalSource) { s.allow.issuer = s.p.Subject }},
		{"unpublished-record", func(s *proposalSource) { s.resolved = false }},
		{"missing-archival-proof-input", func(s *proposalSource) { s.archival = nil }},
		{"intake-not-verified", func(s *proposalSource) { s.archival.intakeVerified = false }},
		{"read-time-inclusion-not-verified", func(s *proposalSource) { s.archival.inclusionVerified = false }},
		{"stale-unchallenged-view", func(s *proposalSource) { s.challenged = false }},
		{"rollback-below-independent-floor", func(s *proposalSource) { s.state = 1 }},
		{"old-instance-not-excluded", func(s *proposalSource) { s.excluded = false }},
		{"unresolved-intent", func(s *proposalSource) { s.pending = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := proposalFixture(t)
			id := s.allow.id
			tc.change(s)
			ctx, cancel := proposalContext()
			defer cancel()
			out, err := proposalRead(t, s, ctx, id)
			refused(t, out, err)
			if s.reads != 0 || s.consumed != 0 || s.closes != 0 {
				t.Fatal("original opened before admission")
			}
			t.Log("fixture adapter refused before record read; no real peer/proof/fence qualification")
		})
	}
}

func TestProposalExactnessAndReleaseRefusals(t *testing.T) {
	for _, name := range []string{"wrong-request-content-id", "same-id-different-original", "wrong-envelope-record", "exclusion-lost-at-release", "close-failed"} {
		t.Run(name, func(t *testing.T) {
			s := proposalFixture(t)
			id := s.allow.id
			switch name {
			case "wrong-request-content-id":
				id = record.Digest([]byte("requested other original"))
			case "same-id-different-original":
				s.wire = append(s.wire, '\n') // Do not update original-wire proof.
			case "wrong-envelope-record":
				s.wire = bytes.Replace(s.wire, []byte("attestation/1"), []byte("revocation/1"), 1)
				s.p.WireSHA256 = record.Digest(s.wire)
			case "exclusion-lost-at-release":
				s.changedAtCheck = 2
			case "close-failed":
				s.closeErr = true
			}
			ctx, cancel := proposalContext()
			defer cancel()
			out, err := proposalRead(t, s, ctx, id)
			refused(t, out, err)
			if s.opens != 1 {
				t.Fatal("retried open")
			}
			if name != "wrong-request-content-id" && s.closes != 1 {
				t.Fatal("lease leaked")
			}
		})
	}
}

func TestProposalOriginalDeadline(t *testing.T) {
	for _, name := range []string{"missing", "past", "beyond-resource-budget", "slow-body-original-expiry"} {
		t.Run(name, func(t *testing.T) {
			s := proposalFixture(t)
			ctx := context.Background()
			cancel := func() {}
			switch name {
			case "past":
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
			case "beyond-resource-budget":
				ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
			case "slow-body-original-expiry":
				ctx, cancel = context.WithTimeout(ctx, 25*time.Millisecond)
				s.readWait = true
			}
			defer cancel()
			out, err := proposalRead(t, s, ctx, s.allow.id)
			refused(t, out, err)
			if name == "slow-body-original-expiry" {
				deadline, _ := ctx.Deadline()
				if !s.deadline.Equal(deadline) || ctx.Err() != context.DeadlineExceeded || s.closes != 1 || s.opens != 1 {
					t.Fatal("deadline replaced or lease leaked")
				}
			} else if s.opens != 0 {
				t.Fatal("opened before original deadline admission")
			}
		})
	}
}

func TestProposalBudgetPreflight(t *testing.T) {
	for _, name := range []string{"record", "proof-record", "proof-count", "wrapper", "error", "total-transfer"} {
		t.Run(name, func(t *testing.T) {
			s := proposalFixture(t)
			switch name {
			case "record":
				s.wire = append(s.wire, bytes.Repeat([]byte(" "), MaxWireBytes+1-len(s.wire))...)
			case "proof-record":
				s.proof.Entries = [][]byte{bytes.Repeat([]byte(" "), 8193)}
			case "proof-count":
				for i := 0; i < 65; i++ {
					s.proof.Entries = append(s.proof.Entries, []byte("{}"))
				}
				s.proof.Bundle = bytes.Repeat([]byte(" "), 130)
			case "wrapper":
				s.proof.Wrapper = bytes.Repeat([]byte("\n"), 8193)
			case "error":
				s.diagnostic = bytes.Repeat([]byte("!"), proposalErrorCap+1)
			case "total-transfer":
				s.proof.Bundle = bytes.Repeat([]byte(" "), store.MaxBundleBytes)
			}
			if name == "total-transfer" && store.Preflight(s.proof) != nil {
				t.Fatal("aggregate witness must pass individual existing proof preflight")
			}
			ctx, cancel := proposalContext()
			defer cancel()
			out, err := proposalRead(t, s, ctx, s.allow.id)
			refused(t, out, err)
			if s.reads != 0 || s.consumed != 0 {
				t.Fatal("budget checked after original allocation/read")
			}
			t.Logf("original=%d carrier=%d wrapper=%d diagnostic=%d; original bytes consumed=%d", len(s.wire), len(s.proof.Bundle), len(s.proof.Wrapper), len(s.diagnostic), s.consumed)
		})
	}
}

func TestProposalCoreCannotAuthenticateFixture(t *testing.T) {
	// Deliberately omit the proposed adapter. The unchanged core accepts a source
	// reporting valid test facts, even though there is no kernel session, real
	// publication proof or independent restore/exclusion authority. This positive
	// counterexample prevents presenting injected guards as core qualification.
	s := fixture(t)
	out, err := read(t, reader(t, s), s.p.ContentID)
	if err != nil || !bytes.Equal(out.Wire, s.wire) {
		t.Fatal("expected documented core trust boundary", err)
	}
	t.Log("LIMITATION: core trusts AuthorizedSource/Lease; synthetic success cannot qualify protected admission")
}

func TestProposalSignedBinding(t *testing.T) {
	cases := []struct {
		name   string
		change func(*proposalSource)
	}{
		{"uid", func(s *proposalSource) { s.session.uid++; s.session.binding.uid++ }},
		{"generation", func(s *proposalSource) {
			s.session.generation = "another-generation"
			s.session.binding.generation = s.session.generation
		}},
		{"owner", func(s *proposalSource) { s.session.binding.owner = "another-owner" }},
		{"path", func(s *proposalSource) { s.session.binding.path = "message/another-agent" }},
		{"registration_number", func(s *proposalSource) { s.session.binding.number++ }},
		{"master_epoch", func(s *proposalSource) { s.session.binding.epoch = "epoch-2" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := proposalFixture(t)
			ctx, cancel := proposalContext()
			defer cancel()
			originalWire := bytes.Clone(s.wire)
			if !s.admitted(ctx, s.allow.id) || !s.signedBindingMatches() || !original(s.wire, s.p) {
				t.Fatal("valid signed-binding neighbor unavailable")
			}
			out, err := proposalRead(t, s, ctx, s.allow.id)
			if err != nil || !bytes.Equal(out.Wire, originalWire) {
				t.Fatal("valid self-reader neighbor failed", err)
			}
			// Change admitted session inputs, NEVER the original or its signature.
			// UID/generation changes agree with each other so that the independent
			// session identity guard cannot mask the signed-field comparison.
			tc.change(s)
			if !s.admitted(ctx, s.allow.id) || !original(s.wire, s.p) || !bytes.Equal(s.wire, originalWire) {
				t.Fatal("wrong-binding witness masked by another admission/original guard")
			}
			beforeReads, beforeCloses := s.reads, s.closes
			out, err = proposalRead(t, s, ctx, s.allow.id)
			refused(t, out, err)
			if s.reads != beforeReads || s.closes != beforeCloses {
				t.Fatal("wrong signed binding opened a body lease")
			}
		})
	}
}

func TestProposalBindingRecheckedAtRelease(t *testing.T) {
	s := proposalFixture(t)
	s.bindingChangedAtCheck = 2
	ctx, cancel := proposalContext()
	defer cancel()
	out, err := proposalRead(t, s, ctx, s.allow.id)
	refused(t, out, err)
	if s.checks != 2 || s.consumed != len(s.wire) || s.closes != 1 || !s.admitted(ctx, s.allow.id) {
		t.Fatal("final signed-binding witness masked or lease leaked")
	}
}

func TestProposalPublicationDoesNotSupplyReaderIdentity(t *testing.T) {
	// No publisher-permission boolean is modeled. Publication success and reader
	// admission are independent premises. A publisher may read only when it also
	// meets the self-reader policy; publication is never a blanket read denial.
	for _, validReader := range []bool{false, true} {
		name := "published-original-without-reader"
		if validReader {
			name = "publisher-with-independently-valid-self-reader"
		}
		t.Run(name, func(t *testing.T) {
			s := proposalFixture(t)
			s.session.authenticated = validReader
			ctx, cancel := proposalContext()
			defer cancel()
			out, err := proposalRead(t, s, ctx, s.allow.id)
			if !validReader {
				refused(t, out, err)
				if s.reads != 0 {
					t.Fatal("publication conferred reader identity")
				}
			} else if err != nil || !bytes.Equal(out.Wire, s.wire) {
				t.Fatal("valid self-reader was denied on publisher semantics", err)
			}
		})
	}
}

func TestProposalLiteralOneSecondBudget(t *testing.T) {
	// Independent literal pins the selected policy even if both helper uses of
	// proposalOperation are changed together. This is resource time, not trust time.
	if proposalOperation != time.Duration(1000000000) {
		t.Fatal("profile must remain exactly one second")
	}
	for _, tc := range []struct {
		name      string
		remaining time.Duration
		accept    bool
	}{
		{"within-one-second", 900 * time.Millisecond, true},
		{"over-one-second", 1500 * time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := proposalFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), tc.remaining)
			defer cancel()
			out, err := proposalRead(t, s, ctx, s.allow.id)
			if tc.accept {
				if err != nil || !bytes.Equal(out.Wire, s.wire) {
					t.Fatal("valid budget neighbor failed", err)
				}
			} else {
				refused(t, out, err)
				if s.opens != 0 {
					t.Fatal("over-budget operation opened source")
				}
			}
		})
	}
}

func TestProposalCheckDeadlineIdentity(t *testing.T) {
	s := proposalFixture(t)
	ctx, cancel := proposalContext()
	defer cancel()
	l, err := s.Open(ctx, s.allow.id)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	d, _ := ctx.Deadline()
	for _, tc := range []struct {
		name     string
		deadline time.Time
		want     error
	}{
		{"original", d, nil},
		{"same-deadline-neighbor", d, nil},
		{"changed-still-valid-deadline", d.Add(50 * time.Millisecond), ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkCtx, checkCancel := context.WithDeadline(context.Background(), tc.deadline)
			defer checkCancel()
			if !s.admitted(checkCtx, s.allow.id) || !s.signedBindingMatches() {
				t.Fatal("deadline identity witness masked by admission or binding")
			}
			if got := l.Check(checkCtx); got != tc.want {
				t.Fatalf("Check deadline identity: got %v want %v", got, tc.want)
			}
		})
	}
}

func TestProposalPreflightBundleComponent(t *testing.T) {
	// Component-only witness: full operations also charge the 840-byte original,
	// so an oversized bundle necessarily violates the aggregate bound as well.
	// No aggregate limit is relaxed or bypassed in proposalSource.Open.
	for _, extra := range []int{0, 1} {
		p := store.Proof{Route: store.JournalProof, Bundle: make([]byte, store.MaxBundleBytes+extra)}
		err := store.Preflight(p)
		if extra == 0 {
			if err != nil {
				t.Fatal("exact bundle-cap neighbor failed", err)
			}
		} else {
			var e *store.Error
			if !errors.As(err, &e) || e.Code != store.ProofLimit {
				t.Fatal("expected exact Preflight bundle proof_limit", err)
			}
		}
	}
}

func TestProposalAggregateTransferNeighbors(t *testing.T) {
	for _, extra := range []int{0, 1} {
		s := proposalFixture(t)
		s.proof.Bundle = make([]byte, proposalTransferCap-len(s.wire)+extra)
		if store.Preflight(s.proof) != nil || len(s.wire)+len(s.proof.Bundle) != proposalTransferCap+extra || !s.signedBindingMatches() {
			t.Fatal("aggregate witness masked by individual proof or binding check")
		}
		ctx, cancel := proposalContext()
		out, err := proposalRead(t, s, ctx, s.allow.id)
		cancel()
		if extra == 0 {
			if err != nil || !bytes.Equal(out.Wire, s.wire) {
				t.Fatal("exact aggregate-cap neighbor failed", err)
			}
		} else {
			refused(t, out, err)
			if s.reads != 0 {
				t.Fatal("aggregate cap checked after body read")
			}
		}
	}
}

// Compile-time seam check only. It grants no qualification or installation.
var _ AuthorizedSource = (*proposalSource)(nil)
var _ Lease = (*proposalLease)(nil)
var _ io.Reader = (*proposalLease)(nil)
