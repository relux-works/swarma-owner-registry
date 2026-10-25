// Package enrollment orchestrates complete, approved enrollment exports through
// the pinned public registrar Loader. It supplies no Git, network, witness or
// transaction implementation. An open gate alone cannot qualify those adapters.
package enrollment

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"
	"sync"

	"github.com/relux-works/swarma-owner-registry/internal/pins"
	policy "github.com/relux-works/swarma-registrar/enrollment"
	"github.com/relux-works/swarma-registrar/registry"
)

const (
	IntakeClosed           registry.Code = "enrollment_intake_closed"
	TransactionUnavailable registry.Code = "transaction_unavailable"
	StateUnavailable       registry.Code = "state_unavailable"
)

// Error exposes only a typed code; restricted Git paths and transaction errors
// never become remote diagnostics.
type Error struct{ Code registry.Code }

func (e *Error) Error() string { return string(e.Code) }
func unavailable() error       { return &Error{Code: registry.RegistryUnavailable} }

// Projection is a complete separate enrollment export, never the general journal
// ref. Records maps content IDs to exact bytes from records/ for this export's
// enrollment history. It contains no other record types. Filenames grant no
// authority; the protected Verifier must bind the complete Entries to Revision.
type Projection struct {
	Revision string
	Entries  []registry.Entry
	Records  map[string][]byte
}

func (p Projection) Clone() Projection {
	out := p
	if p.Entries != nil {
		out.Entries = make([]registry.Entry, len(p.Entries))
		for i, e := range p.Entries {
			out.Entries[i] = e
			out.Entries[i].Bytes = bytes.Clone(e.Bytes)
		}
	}
	if p.Records != nil {
		out.Records = make(map[string][]byte, len(p.Records))
		for id, b := range p.Records {
			out.Records[id] = bytes.Clone(b)
		}
	}
	return out
}

// State is protected retained evidence. Exports includes every previous exact
// envelope revision, including the last accepted export. A new publisher may
// re-sign the candidate, but cannot overwrite these older signatures. Startup
// callers must supply independently reconciled, complete state, not request data.
type State struct {
	HighWater *registry.HighWater
	Exports   []Projection
}

func (s State) Clone() State {
	out := State{HighWater: s.HighWater.Clone()}
	if s.Exports != nil {
		out.Exports = make([]Projection, len(s.Exports))
		for i, p := range s.Exports {
			out.Exports[i] = p.Clone()
		}
	}
	return out
}

// Candidate binds the complete before/after history and exact mirror bytes.
// ID is a local inventory binding, not an enrollment ID, protocol witness digest
// or publication proof. Preflight receives an independent owned copy.
type Candidate struct {
	ID         string
	Before     State
	Projection Projection
	After      State
}

func (c Candidate) Clone() Candidate {
	return Candidate{ID: c.ID, Before: c.Before.Clone(), Projection: c.Projection.Clone(), After: c.After.Clone()}
}

// Binding connects the exact candidate to the independently previewed local
// durable state. Both IDs are sha256: plus 64 lowercase hex digits.
type Binding struct{ CandidateID, StateID string }

// Resolution reports matching durable resolution and final revalidation. A nil
// error with Resolved false is uncertainty, never success.
type Resolution struct {
	Binding  Binding
	Resolved bool
}

// Transaction is a required, caller-qualified external boundary. There is no
// production implementation here. Calls are serialized and must not re-enter
// the Adapter. All methods need bounded deadlines in a qualified implementation.
//
// Preflight quiesces publication/excludes older instances, validates the complete
// recoverable inventory and proof budget, and previews the exact internal/store
// commit without writing authority. It must retain the candidate before intent.
// Intent obtains the matching durable external acknowledgement before local
// writes. Commit uses internal/store to sync the exact candidate inventory,
// high-water, prior export revisions, outcome and watermark; it reports the exact
// previewed state ID only after durability. Resolve independently verifies that
// local commit, obtains matching committed resolution, revalidates bytes/time/
// authority and publishes the resolved head. A signature or intent alone is not
// resolution. On error/uncertainty it retains durable pending evidence for exact
// recovery; it must never roll back, cancel an intent or silently downgrade.
//
// Successful test doubles prove only ordering and bindings, not qualification.
type Transaction interface {
	Preflight(Candidate) (Binding, error)
	Intent(Binding) error
	Commit(Binding) (Binding, error)
	Resolve(Binding) (Resolution, error)
}

// Trust is caller-protected configuration and reconciled retained state, never
// supplied by online enrollment requests. Verifier owns exact signed-commit
// approval and ancestry using Config's CommitSigner pin and the external
// maintainer allowlist/approved revision. Audience must be canonical and station labels
// independently classified, as required by the public Policy constructor.
type Trust struct {
	Config               *pins.Config
	Audience             string
	Stations             map[string]bool
	Verifier             registry.Verifier
	Transaction          Transaction
	Retained             State
	FirstUse, Reconciled bool
}

// Adapter serializes complete loads and permanently fences after any failed
// load. Creating a replacement requires external reconciliation, not a reset flag.
// Previously returned snapshots MUST NOT be used after a failure. The caller
// must carry this fence into every downstream signing/publication operation.
type Adapter struct {
	mu                           sync.Mutex
	policy                       *policy.Policy
	verifier                     registry.Verifier
	transaction                  Transaction
	state                        State
	firstUse, reconciled, fenced bool
}

func nilInterface(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return r.IsNil()
	}
	return false
}

// New copies protected state/policy. It refuses the default-closed gate or an
// absent transaction. No concrete transaction is installed automatically.
func New(t Trust) (*Adapter, error) {
	if t.Config.Require(pins.EnrollmentIntake) != nil {
		return nil, &Error{Code: IntakeClosed}
	}
	if nilInterface(t.Transaction) {
		return nil, &Error{Code: TransactionUnavailable}
	}
	if nilInterface(t.Verifier) {
		return nil, unavailable()
	}
	publisher, ok := t.Config.Pin(pins.EnrollmentPublisher)
	if !ok {
		return nil, unavailable()
	}
	p, err := policy.NewPolicy(t.Audience, publisher.Key, t.Stations)
	if err != nil {
		return nil, unavailable()
	}
	if t.FirstUse {
		if t.Retained.HighWater != nil || len(t.Retained.Exports) != 0 {
			return nil, unavailable()
		}
	} else {
		if !t.Reconciled || t.Retained.HighWater == nil || len(t.Retained.Exports) == 0 || t.Retained.Exports[len(t.Retained.Exports)-1].Revision != t.Retained.HighWater.Revision {
			return nil, unavailable()
		}
	}
	return &Adapter{policy: p, verifier: t.Verifier, transaction: t.Transaction, state: t.Retained.Clone(), firstUse: t.FirstUse, reconciled: t.Reconciled}, nil
}

func (a *Adapter) Fenced() bool {
	if a == nil {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fenced
}

// LastResolved returns recovery evidence only. After a failed transaction it
// does NOT establish unchanged durable state or permit reuse of old authority.
func (a *Adapter) LastResolved() State {
	if a == nil {
		return State{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state.Clone()
}

// Load delegates all seven snapshot-wide phases to the public Loader. The mirror
// and archive checks run inside its Store boundary, after those phases and before
// any transaction effect. Every refusal returns nil and sets a sticky fence.
func (a *Adapter) Load(input Projection) (*registry.Snapshot, error) {
	if a == nil {
		return nil, &Error{Code: StateUnavailable}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fenced {
		return nil, &Error{Code: StateUnavailable}
	}
	fail := func(err error) (*registry.Snapshot, error) {
		a.fenced = true
		var typed *registry.Error
		if errors.As(err, &typed) {
			return nil, &Error{Code: typed.Code}
		}
		return nil, unavailable()
	}
	if !boundedProjection(input) {
		return fail(unavailable())
	}
	p := input.Clone()
	bridge := &storeBridge{adapter: a, projection: p}
	loader := registry.Loader{Policy: a.policy, Verifier: a.verifier, Store: bridge}
	snapshot, _, err := loader.Load(registry.Input{Revision: p.Revision, Entries: p.Entries, Retained: a.state.HighWater.Clone(), FirstUse: a.firstUse, Reconciled: a.reconciled})
	if err != nil {
		return fail(err)
	}
	if !bridge.resolved {
		return fail(unavailable())
	}
	a.state = bridge.after.Clone()
	a.firstUse = false
	a.reconciled = true
	return snapshot, nil
}

func boundedProjection(p Projection) bool {
	if len(p.Entries) > registry.MaxEntries || len(p.Records) > registry.MaxEntries {
		return false
	}
	total := 0
	for _, e := range p.Entries {
		if !strings.HasPrefix(e.Path, "enrollments/") || !strings.HasSuffix(e.Path, ".json") || strings.Contains(e.Path[len("enrollments/"):], "/") || strings.ContainsAny(e.Path, "\\\x00") || path.Clean(e.Path) != e.Path || e.Kind != registry.Regular {
			return false
		}
		if len(e.Bytes) > registry.MaxBytes-total {
			return false
		}
		total += len(e.Bytes)
	}
	total = 0
	for _, b := range p.Records {
		if len(b) > registry.MaxBytes-total {
			return false
		}
		total += len(b)
	}
	return true
}

type storeBridge struct {
	adapter    *Adapter
	projection Projection
	after      State
	resolved   bool
}

// Commit is the public Loader's sole transition boundary; it never installs
// high-water before exact resolution or uses a throwaway successful Store.
func (b *storeBridge) Commit(previous *registry.HighWater, next registry.HighWater) error {
	a := b.adapter
	if !reflect.DeepEqual(previous, a.state.HighWater) || b.resolved || len(b.projection.Records) != len(b.projection.Entries) {
		return unavailable()
	}
	for _, e := range b.projection.Entries {
		// All records have already passed all seven Loader phases. Reusing the public
		// Policy here obtains the verified ID without local schema/signature logic.
		r, err := a.policy.Parse(e.Bytes)
		if err != nil || !bytes.Equal(b.projection.Records[r.ID()], e.Bytes) {
			return unavailable()
		}
	}
	after := a.state.Clone()
	after.HighWater = next.Clone()
	known := false
	for _, old := range after.Exports {
		if old.Revision == b.projection.Revision {
			if !reflect.DeepEqual(old, b.projection) {
				return unavailable()
			}
			known = true
		}
	}
	if !known {
		after.Exports = append(after.Exports, b.projection.Clone())
	}
	c := Candidate{Before: a.state.Clone(), Projection: b.projection.Clone(), After: after}
	raw, err := json.Marshal(c)
	if err != nil {
		return unavailable()
	}
	c.ID = fmt.Sprintf("sha256:%x", sha256.Sum256(append([]byte("enrollment-candidate-v1\x00"), raw...)))
	binding, err := a.transaction.Preflight(c.Clone())
	if err != nil || binding.CandidateID != c.ID || !validID(binding.StateID) {
		return unavailable()
	}
	if err = a.transaction.Intent(binding); err != nil {
		return unavailable()
	}
	committed, err := a.transaction.Commit(binding)
	if err != nil || committed != binding {
		return unavailable()
	}
	resolution, err := a.transaction.Resolve(binding)
	if err != nil || !resolution.Resolved || resolution.Binding != binding {
		return unavailable()
	}
	b.after = after.Clone()
	b.resolved = true
	return nil
}
func validID(s string) bool {
	if len(s) != 71 || !strings.HasPrefix(s, "sha256:") {
		return false
	}
	for _, c := range s[7:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
