// Package grantattestation implements a bounded original-by-content-ID read
// component. It supplies no authenticated session, publication authority,
// transport, protected view, clock, consumer adapter or production constructor.
package grantattestation

import (
	"context"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/relux-works/swarma-envelope/record"
)

const MaxWireBytes = 8192

// These are internal component errors, not new platform or transport codes.
// Source errors and unauthorized record metadata are never returned to readers.
var (
	ErrUnavailable = errors.New("attestation read unavailable")
	ErrReference   = errors.New("invalid attestation reference")
)

// Provenance is internal retained publication/custody evidence identity. The
// identifiers do not authenticate themselves. A qualified Lease must bind all
// fields to the exact original wire under its independently admitted view.
type Provenance struct {
	ContentID, WireSHA256, StateID, JournalID, CheckpointID string
	Owner, Subject, Issuer                                  string
}

// AuthorizedSource is a proposed local integration boundary, not a wire API.
// It must be bound to a transport-authenticated reader session, independently
// authorize attestation/1, exact owner/subject/issuer and disclosure scope BEFORE
// opening any record, and reject missing or expired principal authority. A
// publishing identity is not implicitly a reader. No request principal string
// is accepted here. Nil leaves the component unavailable.
//
// Open must acquire a finite, context-bounded local protected publication view.
// Unpublished, unresolved, fenced, rolled-back or conflicting originals refuse.
// No latest-for-key fallback, remote lookup under a receiver journal lock,
// automatic retry, decompression or redirect is permitted by this interface.
type AuthorizedSource interface {
	Open(context.Context, string) (Lease, error)
}

// Lease keeps read/disclosure authority, original publication proof and
// independent rollback/exclusion admission valid throughout this local read.
// Check revalidates those facts; a saved boolean or local checksum is not proof.
// All methods, including Read and Close, MUST obey the original context deadline
// passed to Open. The core starts no timeout goroutine to conceal a stuck source.
// Implementations must preflight their own framing/proof/error/storage bounds;
// the core's 8193-byte sentinel bound applies only to original wire consumption.
// Close releases the protected view. Even on Open error a returned lease is
// owned by the core and closed. No qualified implementation is supplied here.
type Lease interface {
	io.Reader
	Provenance() Provenance
	Check(context.Context) error
	Close() error
}

// Reader has one nonblocking operation slot and no queue or cache. Its immutable
// issuer pin and maximum elapsed operation duration are protected configuration.
// New checks wiring only; it does not qualify an injected source as a provider.
type Reader struct {
	source       AuthorizedSource
	issuer       string
	maxOperation time.Duration
	slot         chan struct{}
}

func New(source AuthorizedSource, issuer string, maxOperation time.Duration) (*Reader, error) {
	algorithm, _, err := record.KeyBytes(issuer)
	if absent(source) || err != nil || algorithm != "ed25519" || record.ValidateKey(issuer) != nil || maxOperation <= 0 {
		return nil, ErrUnavailable
	}
	return &Reader{source: source, issuer: issuer, maxOperation: maxOperation, slot: make(chan struct{}, 1)}, nil
}

// Original contains only exact retained original bytes and private provenance. It is
// an archival read, so signature/profile validity does not assert current-use
// validity: expiry, revocation, active generation, time and A2 remain consumer
// gates. The existing Attestation(contentID) port may project only Wire after a
// separately qualified connector supplies its finite operation context.
type Original struct {
	Wire       []byte
	Provenance Provenance
}

func (r *Reader) Read(ctx context.Context, contentID string) (out Original, err error) {
	if !record.IsContentID(contentID) {
		return Original{}, ErrReference
	}
	if r == nil || absent(r.source) || expired(ctx) {
		return Original{}, ErrUnavailable
	}
	deadline, ok := ctx.Deadline()
	// Wall/monotonic time here bounds resource use only, never signed evidence
	// freshness or qualified authority. The caller's deadline is never renewed.
	remaining := time.Until(deadline)
	if !ok || remaining <= 0 || remaining > r.maxOperation {
		return Original{}, ErrUnavailable
	}
	select {
	case r.slot <- struct{}{}:
		defer func() { <-r.slot }()
	default:
		return Original{}, ErrUnavailable
	}
	lease, openErr := r.source.Open(ctx, contentID)
	if !absent(lease) {
		defer func() {
			if lease.Close() != nil || expired(ctx) {
				out, err = Original{}, ErrUnavailable
			}
		}()
	}
	if openErr != nil || absent(lease) || expired(ctx) || lease.Check(ctx) != nil {
		return Original{}, ErrUnavailable
	}
	p := lease.Provenance()
	if p.ContentID != contentID || p.Issuer != r.issuer || !validProvenance(p) {
		return Original{}, ErrUnavailable
	}
	wire, readErr := boundedRead(ctx, lease)
	if readErr != nil || record.Digest(wire) != p.WireSHA256 || !original(wire, p) {
		return Original{}, ErrUnavailable
	}
	if lease.Check(ctx) != nil || expired(ctx) || lease.Provenance() != p || expired(ctx) {
		return Original{}, ErrUnavailable
	}
	return Original{Wire: wire, Provenance: p}, nil
}

func expired(ctx context.Context) bool {
	if absent(ctx) || ctx.Err() != nil {
		return true
	}
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) <= 0
}

func absent(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

func validProvenance(p Provenance) bool {
	return record.IsContentID(p.ContentID) && record.IsContentID(p.WireSHA256) &&
		record.IsContentID(p.StateID) && record.IsContentID(p.JournalID) && record.IsContentID(p.CheckpointID) &&
		text(p.Owner) && record.ValidateKey(p.Subject) == nil
}

func boundedRead(ctx context.Context, source io.Reader) ([]byte, error) {
	// Fixed allocation before reading. At most one sentinel byte beyond the
	// signed-evidence cap is consumed; oversized bodies are never parsed.
	buf := make([]byte, MaxWireBytes+1)
	n := 0
	for n < len(buf) {
		if expired(ctx) {
			return nil, ErrUnavailable
		}
		count, err := source.Read(buf[n:])
		if count < 0 || count > len(buf)-n || expired(ctx) {
			return nil, ErrUnavailable
		}
		n += count
		if n > MaxWireBytes {
			return nil, ErrUnavailable
		}
		if err == io.EOF {
			return buf[:n:n], nil
		}
		if err != nil || count == 0 {
			return nil, ErrUnavailable
		}
	}
	return nil, ErrUnavailable
}
