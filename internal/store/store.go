//go:build linux

// Package store provides local serialized durability, not signature admission,
// witness resolution, old-instance exclusion or permission to publish externally.
package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/relux-works/swarma-envelope/record"
	"github.com/relux-works/swarma-owner-registry/internal/contracts"
)

const MaxSequence uint64 = 9007199254740991
const MaxBundleBytes = 2 * 1024 * 1024
const MaxProofRecordBytes = 8192

type Code string

const (
	RequestInvalid        Code = "request_invalid"
	RequestConflict       Code = "request_conflict"
	HeadConflict          Code = "head_conflict"
	CapacityExhausted     Code = "capacity_exhausted"
	EnvelopeConflict      Code = "envelope_conflict"
	RecordTypeUnsupported Code = "record_type_unsupported"
	ProofLimit            Code = "proof_limit"
	StateUnavailable      Code = "state_unavailable"
	NotFound              Code = "not_found"
)

type Error struct{ Code Code }

func (e *Error) Error() string { return string(e.Code) }
func refuse(c Code) error      { return &Error{Code: c} }

type Route string

const (
	JournalProof   Route = "journal"
	FreshnessProof Route = "freshness"
)

// Proof carries already selected proof originals and the ENTIRE received wire
// bundle (including context/control/encoding). The caller must supply that full
// carrier, not a size claim or an excerpt. Bounds do not authenticate its contents.
// Wrapper is the complete optional WS checkpoint wrapper, including its LF.
type Proof struct {
	Route                              Route
	Entries, Publications, Checkpoints [][]byte
	QueryIDs, QueryKeys                []string
	Bundle, Wrapper                    []byte
}
type Record struct {
	Type, ID string
	Bytes    []byte
}

// Request is prepared by a qualified caller after admission. Journal/checkpoint
// bytes remain opaque; Head, watermark and outcome are caller-protected facts.
// The store allocates the sequence; the caller must prepare bytes for that next
// position. This package neither builds nor verifies signed record bodies.
type Request struct {
	Principal, RequestID, Operation string
	ExpectedHead, Head, Watermark   string
	Records                         []Record
	Journal, Checkpoint, Result     []byte
	Proof                           Proof
}
type Outcome struct {
	Sequence        uint64
	Head, Watermark string
	Result          []byte
}
type View struct {
	StateID, Head, Watermark string
	Sequence                 uint64
}

// Transactions is the local storage boundary a future Git adapter can wrap.
// Commit's local resolution supplies no external witness or publication claim.
type Transactions interface {
	Commit(Request) (Outcome, error)
	Preview(Request) (View, error)
	View() (View, error)
	ReadRecord(kind, id string) ([]byte, error)
	Reconcile(expectedStateID string) error
	Close() error
}

// Store owns one lifetime directory lock and serializes all its readers/writers.
// The lock prevents cooperating local opens, not an old clone or hostile owner.
type Store struct {
	mu             sync.Mutex
	disk           *disk
	active         image
	stateID        string
	pendingID      string
	manifests      map[string]bool
	maxBytes       uint64
	blobs          map[string]bool
	fenced, closed bool
}

// Retained indexes contain digests and positions only, never historical payloads.
type retained struct {
	Request, Outcome string
	Sequence         uint64
}
type image struct {
	Format          string
	Sequence        uint64
	Head, Watermark string
	Files           map[string]string
	Requests        map[string]retained
}

// A state identity hashes this entire delta, including its predecessor identity.
// Exact file/request/outcome bytes are bound through immutable blob digests.
type manifest struct {
	Base            string
	Sequence        uint64
	Head, Watermark string
	Files           map[string]string
	Key             string
	Retained        retained
}

const DefaultMaxBytes uint64 = 1 << 30

// Options is a local logical regular-file byte budget, not a wire limit.
// Zero selects the default. Directory/inode metadata is not charged.
type Options struct{ MaxBytes uint64 }

func budget(o Options) uint64 {
	if o.MaxBytes == 0 {
		return DefaultMaxBytes
	}
	return o.MaxBytes
}
func byteID(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }

// The immutable anchor binds the local format into every descendant state ID.
func genesis() image {
	return image{Format: "delta-1", Files: map[string]string{}, Requests: map[string]retained{}}
}
func canonical(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic("local state encoding failed")
	}
	return b
}
func digest(v any) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(canonical(v))) }
func clone[T any](v T) T {
	var out T
	if json.Unmarshal(canonical(v), &out) != nil {
		panic("local state copy failed")
	}
	return out
}
func requestKey(r Request) string { return digest([]string{r.Principal, r.RequestID}) }
func validID(id string) bool {
	if len(id) != 71 || !strings.HasPrefix(id, "sha256:") {
		return false
	}
	for _, c := range id[7:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func typeDir(kind string) (string, bool) {
	for _, v := range contracts.RecordTypes() {
		if kind == v {
			return strings.ReplaceAll(v, "/", "-"), true
		}
	}
	return "", false
}
func recordPath(r Record) string {
	d, _ := typeDir(r.Type)
	return "records/" + d + "/" + r.ID + ".json"
}
func successor(n uint64) (uint64, error) {
	if n >= MaxSequence {
		return 0, refuse(CapacityExhausted)
	}
	return n + 1, nil
}
func validTime(s string) bool {
	t, e := time.Parse("2006-01-02T15:04:05Z", s)
	return e == nil && len(s) == 20 && t.Year() >= 1970 && t.Year() <= 9999 && t.Format("2006-01-02T15:04:05Z") == s
}

// Preflight checks independently charged wire, pure-proof and wrapper limits.
// It performs no filesystem operation and never truncates a complete proof.
func Preflight(p Proof) error {
	if len(p.Bundle) > MaxBundleBytes || len(p.Wrapper) > 8192 {
		return refuse(ProofLimit)
	}
	if len(p.Wrapper) > 0 && p.Wrapper[len(p.Wrapper)-1] != '\n' {
		return refuse(RequestInvalid)
	}
	for _, query := range append(append([]string{}, p.QueryIDs...), p.QueryKeys...) {
		if len(query) > 8192 || !utf8.ValidString(query) {
			return refuse(RequestInvalid)
		}
	}
	var list [][]byte
	switch p.Route {
	case "":
		if len(p.Entries)+len(p.Publications)+len(p.Checkpoints)+len(p.QueryIDs)+len(p.QueryKeys) != 0 {
			return refuse(RequestInvalid)
		}
	case JournalProof:
		if len(p.Entries) > 64 || len(p.Checkpoints) > 2 {
			return refuse(ProofLimit)
		}
		if len(p.Publications)+len(p.QueryIDs)+len(p.QueryKeys) != 0 {
			return refuse(RequestInvalid)
		}
		list = append(append([][]byte{}, p.Entries...), p.Checkpoints...)
	case FreshnessProof:
		if len(p.Publications) > 64 || len(p.Checkpoints) > 1 || len(p.QueryIDs) > 64 || len(p.QueryKeys) > 64 {
			return refuse(ProofLimit)
		}
		if len(p.Entries) != 0 {
			return refuse(RequestInvalid)
		}
		list = append(append([][]byte{}, p.Publications...), p.Checkpoints...)
	default:
		return refuse(RequestInvalid)
	}
	total := 0
	for _, raw := range list {
		if len(raw) > 8192 {
			return refuse(ProofLimit)
		}
		total += len(raw)
		// The pinned bounded reader also rejects depth/string/array overflows. This
		// layer reports all unadmittable proof records as proof_limit, not a wire verdict.
		if _, e := record.Parse(raw, 8192, 16); e != nil {
			return refuse(ProofLimit)
		}
	}
	if p.Route == JournalProof && total > 540672 || p.Route == FreshnessProof && total > 532480 {
		return refuse(ProofLimit)
	}
	if len(p.Bundle) < total {
		return refuse(RequestInvalid)
	}
	return nil
}
func validate(r Request) error {
	if e := Preflight(r.Proof); e != nil {
		return e
	}
	for _, s := range []string{r.Principal, r.RequestID, r.Operation} {
		if s == "" || len(s) > 255 || !utf8.ValidString(s) {
			return refuse(RequestInvalid)
		}
	}
	if !validID(r.Head) || (r.ExpectedHead != "" && !validID(r.ExpectedHead)) || !validTime(r.Watermark) || len(r.Records) == 0 || len(r.Records) > 64 || len(r.Result) > 65536 {
		return refuse(RequestInvalid)
	}
	if len(r.Journal) == 0 || len(r.Journal) > 8192 || len(r.Checkpoint) == 0 || len(r.Checkpoint) > 8192 {
		return refuse(RequestInvalid)
	}
	total := 0
	seen := map[string]bool{}
	idBytes := map[string][]byte{}
	for _, v := range r.Records {
		if _, ok := typeDir(v.Type); !ok {
			return refuse(RecordTypeUnsupported)
		}
		if !validID(v.ID) {
			return refuse(RequestInvalid)
		}
		limit := 8192
		if v.Type == "grant/1" || v.Type == "enrollment/1" {
			limit = 65536
		}
		if len(v.Bytes) == 0 || len(v.Bytes) > limit {
			return refuse(RequestInvalid)
		}
		path := recordPath(v)
		if seen[path] {
			return refuse(RequestInvalid)
		}
		if previous, ok := idBytes[v.ID]; ok && !bytes.Equal(previous, v.Bytes) {
			return refuse(EnvelopeConflict)
		}
		idBytes[v.ID] = v.Bytes
		seen[path] = true
		total += len(v.Bytes)
	}
	if total > MaxBundleBytes {
		return refuse(ProofLimit)
	}
	return nil
}

func candidate(base image, r Request) (image, Outcome, error) {
	key := requestKey(r)
	if prior, ok := base.Requests[key]; ok {
		if prior.Request != requestDigest(r) {
			return image{}, Outcome{}, refuse(RequestConflict)
		}
		return image{}, Outcome{Sequence: prior.Sequence, Head: r.Head, Watermark: r.Watermark, Result: append([]byte{}, r.Result...)}, nil
	}
	if r.ExpectedHead != base.Head {
		return image{}, Outcome{}, refuse(HeadConflict)
	}
	seq, e := successor(base.Sequence)
	if e != nil {
		return image{}, Outcome{}, e
	}
	if r.Watermark < base.Watermark || r.Head == base.Head {
		return image{}, Outcome{}, refuse(RequestInvalid)
	}
	for _, v := range r.Records {
		for path, old := range base.Files {
			if strings.HasPrefix(path, "records/") && strings.HasSuffix(path, "/"+v.ID+".json") && old != byteID(v.Bytes) {
				return image{}, Outcome{}, refuse(EnvelopeConflict)
			}
		}
	}
	next := genesis()
	for _, v := range r.Records {
		next.Files[recordPath(v)] = byteID(v.Bytes)
	}
	next.Files[fmt.Sprintf("journal/%016d.json", seq)] = byteID(r.Journal)
	next.Files[fmt.Sprintf("checkpoints/%016d.json", seq)] = byteID(r.Checkpoint)
	next.Sequence, next.Head, next.Watermark = seq, r.Head, r.Watermark
	out := Outcome{Sequence: seq, Head: r.Head, Watermark: r.Watermark, Result: append([]byte{}, r.Result...)}
	next.Requests[key] = retained{Request: requestDigest(r), Outcome: byteID(canonical(outcomeRef(out))), Sequence: seq}
	return next, out, nil
}

// Create is explicit bootstrap of an existing empty private directory. Open
// never treats absent state as genesis. The caller owns bootstrap authorization.
func Create(root string) (*Store, error) { return CreateWithOptions(root, Options{}) }
func CreateWithOptions(root string, options Options) (*Store, error) {
	d, e := openDisk(root)
	if e != nil {
		return nil, refuse(StateUnavailable)
	}
	return createDisk(d, budget(options))
}

// The disk seam makes bootstrap boundaries observable without global hooks.
func createDisk(d *disk, maxBytes uint64) (*Store, error) {
	fail := func(e error) (*Store, error) { d.close(); return nil, e }
	paths, e := d.scan()
	if e != nil || len(paths) != 0 {
		return fail(refuse(StateUnavailable))
	}
	initial := genesis()
	stateID := digest(initial)
	if uint64(len(canonical(initial))+72) > maxBytes {
		return fail(refuse(CapacityExhausted))
	}
	for _, dir := range []string{"records", "journal", "checkpoints", "transactions", "blobs"} {
		if e = d.mkdir(dir); e != nil {
			return fail(refuse(StateUnavailable))
		}
	}
	if e = d.atomic("anchor.json", canonical(initial), stateID, false); e != nil {
		return fail(refuse(StateUnavailable))
	}
	if e = d.atomic("CURRENT", []byte(stateID+"\n"), stateID, true); e != nil {
		return fail(refuse(StateUnavailable))
	}
	return &Store{disk: d, active: initial, stateID: stateID, manifests: map[string]bool{}, blobs: map[string]bool{}, maxBytes: maxBytes}, nil
}

// Open validates/reconciles only local evidence. Service startup still requires
// independently qualified exclusion, continuity and witness barriers.
func Open(root, expectedStateID string) (*Store, error) {
	return OpenWithOptions(root, expectedStateID, Options{})
}
func OpenWithOptions(root, expectedStateID string, options Options) (*Store, error) {
	d, e := openDisk(root)
	if e != nil {
		return nil, refuse(StateUnavailable)
	}
	s := &Store{disk: d, fenced: true, maxBytes: budget(options)}
	if e = s.Reconcile(expectedStateID); e != nil {
		d.close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.fenced = true
	if s.disk.close() != nil {
		return refuse(StateUnavailable)
	}
	return nil
}
func (s *Store) unavailable() error { s.fenced = true; return refuse(StateUnavailable) }
func (s *Store) check() error {
	if s.closed || s.fenced {
		return refuse(StateUnavailable)
	}
	if e := s.audit(); e != nil {
		return s.unavailable()
	}
	return nil
}

// Preview supplies an immutable local candidate identity before external intent.
// It reserves nothing: Commit repeats the same serialized checks. The caller must
// retain this identity independently for crash recovery; disk state cannot prove
// the absence of a completely lost candidate or detect whole-directory rollback.
func (s *Store) Preview(input Request) (View, error) {
	if e := validate(input); e != nil {
		return View{}, e
	}
	r := clone(input)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(); e != nil {
		return View{}, e
	}
	next, _, e := candidate(s.active, r)
	if e != nil {
		return View{}, e
	}
	if next.Files == nil {
		return View{StateID: s.stateID, Head: s.active.Head, Sequence: s.active.Sequence, Watermark: s.active.Watermark}, nil
	}
	m, blobs := delta(s.stateID, next, r)
	if e = s.capacity(m, blobs); e != nil {
		return View{}, e
	}
	return View{StateID: digest(m), Head: next.Head, Sequence: next.Sequence, Watermark: next.Watermark}, nil
}

func (s *Store) Commit(input Request) (Outcome, error) {
	// Copy only bounded inputs, before intent. Caller mutation during a call is
	// outside Go's ownership contract; returned/stored values never alias inputs.
	if e := validate(input); e != nil {
		return Outcome{}, e
	}
	r := clone(input)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(); e != nil {
		return Outcome{}, e
	}
	next, out, e := candidate(s.active, r)
	if e != nil {
		return Outcome{}, e
	}
	if next.Files == nil {
		return out, nil
	} // exact retained outcome, no reservation
	m, blobs := delta(s.stateID, next, r)
	if e = s.capacity(m, blobs); e != nil {
		return Outcome{}, e
	}
	stateID := digest(m)
	// Fence before the first uncertain operation, including a crash/panic. Only
	// the final checked namespace sync below can reopen local readers/acceptance.
	s.fenced = true
	s.pendingID = stateID
	// The manifest is durable first. Missing blobs after a crash fence; complete
	// surviving blobs permit exact reconstruction of materialized paths.
	if e = s.disk.atomic("transactions/"+stateID+".json", canonical(m), stateID, false); e != nil {
		return Outcome{}, refuse(StateUnavailable)
	}
	for _, id := range sortedKeys(blobs) {
		if e = s.disk.atomic("blobs/"+id+".blob", blobs[id], stateID, false); e != nil {
			return Outcome{}, refuse(StateUnavailable)
		}
	}
	if e = s.materialize(m.Files, stateID); e != nil {
		return Outcome{}, refuse(StateUnavailable)
	}
	if e = s.disk.atomic("CURRENT", []byte(stateID+"\n"), stateID, true); e != nil {
		return Outcome{}, refuse(StateUnavailable)
	}
	s.active, s.stateID = fold(s.active, next), stateID
	s.manifests[stateID] = true
	for id := range blobs {
		s.blobs[id] = true
	}
	s.pendingID = ""
	s.fenced = false
	return clone(out), nil
}
func (s *Store) View() (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(); e != nil {
		return View{}, e
	}
	return View{StateID: s.stateID, Head: s.active.Head, Watermark: s.active.Watermark, Sequence: s.active.Sequence}, nil
}
func (s *Store) ReadRecord(kind, id string) ([]byte, error) {
	if _, ok := typeDir(kind); !ok {
		return nil, refuse(RecordTypeUnsupported)
	}
	if !validID(id) {
		return nil, refuse(RequestInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(); e != nil {
		return nil, e
	}
	v, ok := s.active.Files[recordPath(Record{Type: kind, ID: id})]
	if !ok {
		return nil, refuse(NotFound)
	}
	raw, e := s.disk.read("blobs/" + v + ".blob")
	if e != nil || byteID(raw) != v {
		return nil, s.unavailable()
	}
	return raw, nil
}
