// Package freshness builds registry-owner publications and retains their complete
// accepted history. It supplies no witness client, scheduler or durable store.
package freshness

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/relux-works/swarma-owner-registry/internal/journal"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

// Anchor is exactly the eight-field S4 FreshnessAnchor. Only explicitly
// authorized genesis has PublicationSeq zero and nil publication ID/time.
type Anchor struct {
	RegistryID     string  `json:"registry_id"`
	Owner          string  `json:"owner"`
	PublicationSeq int64   `json:"publication_seq"`
	FreshnessID    *string `json:"freshness_id"`
	IssuedAt       *string `json:"issued_at"`
	Snapshot       string  `json:"snapshot"`
	RegistrySeq    int64   `json:"registry_seq"`
	CheckpointID   string  `json:"checkpoint_id"`
}

func (a Anchor) clone() Anchor {
	if a.FreshnessID != nil {
		x := *a.FreshnessID
		a.FreshnessID = &x
	}
	if a.IssuedAt != nil {
		x := *a.IssuedAt
		a.IssuedAt = &x
	}
	return a
}
func (a Anchor) fact() verifier.Evidence {
	var id, at any
	if a.FreshnessID != nil {
		id = *a.FreshnessID
	}
	if a.IssuedAt != nil {
		at = *a.IssuedAt
	}
	e, _ := verifier.Present(map[string]any{"registry_id": a.RegistryID, "owner": a.Owner, "publication_seq": a.PublicationSeq, "freshness_id": id, "issued_at": at, "snapshot": a.Snapshot, "registry_seq": a.RegistrySeq, "checkpoint_id": a.CheckpointID})
	return e
}

// Outcome retains the exact accepted code/plan, not an S5 consumer receipt.
// Receipt composition belongs to the separately qualified consumer adapter.
type Outcome struct {
	Code string        `json:"code"`
	Plan verifier.Plan `json:"plan"`
}

func (r Outcome) clone() Outcome {
	r.Plan.IDs = append([]string(nil), r.Plan.IDs...)
	if r.Plan.Reanchor != nil {
		x := *r.Plan.Reanchor
		r.Plan.Reanchor = &x
	}
	return r
}

// Publication archives exact accepted bytes and their original evidence/outcome.
// Archive membership records admission, never present or historical authority.
type Publication struct {
	Bytes      []byte  `json:"bytes"`
	Checkpoint []byte  `json:"checkpoint"`
	Snapshot   []byte  `json:"snapshot"`
	Witness    []byte  `json:"witness"`
	ReadAt     string  `json:"read_at"`
	Anchor     Anchor  `json:"anchor"`
	Outcome    Outcome `json:"outcome"`
}

func (p Publication) clone() Publication {
	p.Bytes = bytes.Clone(p.Bytes)
	p.Checkpoint = bytes.Clone(p.Checkpoint)
	p.Snapshot = bytes.Clone(p.Snapshot)
	p.Witness = bytes.Clone(p.Witness)
	p.Anchor = p.Anchor.clone()
	p.Outcome = p.Outcome.clone()
	return p
}

// State retains all publications, including prior heads, without truncation.
// Its authenticated genesis and watermark are independent of a local counter.
type State struct {
	Genesis   Anchor        `json:"genesis"`
	Anchor    Anchor        `json:"anchor"`
	History   []Publication `json:"history"`
	Watermark string        `json:"watermark"`
}

func (s State) Clone() State {
	s.Genesis = s.Genesis.clone()
	s.Anchor = s.Anchor.clone()
	if s.History != nil {
		history := make([]Publication, len(s.History))
		for i, p := range s.History {
			history[i] = p.clone()
		}
		s.History = history
	}
	return s
}

// StateID is a local inventory digest, not a witness signature or reconciliation
// proof. Recovery.StateID must come from independent qualified retained evidence.
func StateID(s State) (string, error) {
	raw, e := json.Marshal(s)
	if e != nil {
		return "", e
	}
	return record.Digest(append([]byte("freshness-state-v1\n"), raw...)), nil
}

// Recovery is supplied only after an independently qualified complete-history
// comparison to the published floor, real-head reconciliation, time continuity
// and old-instance exclusion. Local restored bytes cannot supply this evidence.
// GenesisAuthorized is separate explicit authorization, not missing-state repair.
type Recovery struct {
	StateID                       string
	Reconciled, GenesisAuthorized bool
}

// Update binds a complete retained outcome/history to its exact predecessor.
type Update struct {
	Before string
	After  State
}

func (u Update) Clone() Update { u.After = u.After.Clone(); return u }

// Persistence must atomically compare Before, durably retain all exact history,
// publication/base/checkpoint/witness/read/outcome/watermark and candidate pointer,
// and acknowledge the exact StateID(After) after checked sync. This includes
// retention/budget preflight and the shared real-publication exclusion boundary.
// Errors or an uncertain/misbound acknowledgement fence; no implementation ships
// here. Implementing this interface alone does not qualify crash durability.
type Persistence interface{ Commit(Update) (string, error) }
type Trust struct {
	Registry, Owner string
	Signer          journal.Signer
	Retained        State
	Recovery        Recovery
	Persistence     Persistence
}

// WitnessRead contains already authenticated original evidence and the exact
// protected read timestamp/fact. The later witness client must bind all three,
// exclude unresolved intent and revalidate the resolved head under serialization.
// This package authenticates no witness transport or challenge.
type WitnessRead struct {
	Fact     verifier.Evidence
	ReadAt   string
	Original []byte
}

// Input supplies protected policy/time and current resolved evidence. Context's
// anchor, watermark, witness, read and registry snapshot are replaced from the
// typed fields and retained State. Watermark is a protected time-provider value;
// it is clamped to the retained floor, never inferred from IssuedAt.
type Input struct {
	Context             verifier.Context
	IssuedAt, Watermark string
	Checkpoint          []byte
	Snapshot            journal.SnapshotFacts
	Witness             WitnessRead
	IDs, Keys           []string
}

// Publisher serializes local calls. External serialization/exclusion remains a
// Persistence/witness integration obligation. A durability failure fences all
// later calls; refusal plans and uncommitted candidate bytes never escape.
type Publisher struct {
	mu              sync.Mutex
	registry, owner string
	signer          journal.Signer
	store           Persistence
	state           State
	stateID         string
	fenced          bool
}
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }
func unavailable() error       { return &Error{Code: "freshness_state_unavailable"} }
func absent(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}
func timestamp(s string) bool {
	v, e := time.Parse("2006-01-02T15:04:05Z", s)
	return e == nil && v.Year() >= 1970 && v.Year() <= 9999 && v.Format("2006-01-02T15:04:05Z") == s
}
func validAnchor(a Anchor) bool {
	if a.RegistryID == "" || a.Owner == "" || len(a.Owner) > 255 || !utf8.ValidString(a.Owner) || a.PublicationSeq < 0 || a.PublicationSeq > journal.MaxSequence || a.RegistrySeq < 1 || a.RegistrySeq > journal.MaxSequence || !record.IsContentID(a.Snapshot) || !record.IsContentID(a.CheckpointID) {
		return false
	}
	if a.PublicationSeq == 0 {
		return a.FreshnessID == nil && a.IssuedAt == nil
	}
	return a.FreshnessID != nil && record.IsContentID(*a.FreshnessID) && a.IssuedAt != nil && timestamp(*a.IssuedAt)
}

// New admits independently reconciled state, never an implicit bootstrap. It
// checks local archive completeness, not historical signature/currentness again;
// the externally qualified Recovery contract is mandatory even for intact bytes.
func New(t Trust) (*Publisher, error) {
	s := t.Retained
	if absent(t.Signer) || absent(t.Persistence) || record.ValidateKey(t.Registry) != nil || t.Owner == "" || !t.Recovery.Reconciled || !validAnchor(s.Genesis) || !validAnchor(s.Anchor) || s.Genesis.PublicationSeq != 0 || s.Anchor.RegistryID != t.Registry || s.Genesis.RegistryID != t.Registry || s.Anchor.Owner != t.Owner || s.Genesis.Owner != t.Owner || !timestamp(s.Watermark) || int64(len(s.History)) != s.Anchor.PublicationSeq {
		return nil, unavailable()
	}
	if s.Anchor.PublicationSeq == 0 && (!t.Recovery.GenesisAuthorized || !reflect.DeepEqual(s.Genesis, s.Anchor)) {
		return nil, unavailable()
	}
	for i, p := range s.History {
		if p.Anchor.PublicationSeq != int64(i)+1 || !validAnchor(p.Anchor) || p.Anchor.RegistryID != t.Registry || p.Anchor.Owner != t.Owner || len(p.Bytes) == 0 || len(p.Bytes) > journal.MaxRecordBytes || len(p.Checkpoint) == 0 || len(p.Checkpoint) > journal.MaxRecordBytes || len(p.Snapshot) == 0 || len(p.Snapshot) > journal.MaxRecordBytes || len(p.Witness) == 0 || !timestamp(p.ReadAt) || p.Outcome.Code != "accept" || p.Outcome.Plan.Reanchor != nil {
			return nil, unavailable()
		}
	}
	if len(s.History) > 0 {
		last := s.History[len(s.History)-1]
		if !reflect.DeepEqual(last.Anchor, s.Anchor) || last.Outcome.Plan.Watermark != s.Watermark {
			return nil, unavailable()
		}
	}
	id, e := StateID(s)
	if e != nil || id != t.Recovery.StateID {
		return nil, unavailable()
	}
	return &Publisher{registry: t.Registry, owner: t.Owner, signer: t.Signer, store: t.Persistence, state: s.Clone(), stateID: id}, nil
}
func (p *Publisher) Fenced() bool {
	if p == nil {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fenced
}

// LastResolved is copied recovery evidence. After uncertain persistence it does
// not prove durable storage is unchanged, nor authorize serving the old position.
func (p *Publisher) LastResolved() State {
	if p == nil {
		return State{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state.Clone()
}

// Verify forwards only normal pinned freshness admission. It never falls back
// to re-anchor or changes a refusal code/zero plan.
func Verify(c verifier.Context, chain [][]byte, checkpoint []byte, ids, keys []string) verifier.Result {
	return verifier.New(c).VerifyFreshness(chain, checkpoint, ids, keys)
}
func no(code string) ([]byte, Outcome) { return nil, Outcome{Code: code} }
func obj(v any) map[string]any         { m, _ := v.(map[string]any); return m }
func text(v any) string                { s, _ := v.(string); return s }
func number(v any) int64               { n, _ := v.(int64); return n }
func parse(raw []byte) (map[string]any, error) {
	v, e := record.Parse(raw, journal.MaxRecordBytes, 16)
	if e != nil {
		return nil, e
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("not an object")
	}
	return m, nil
}

// Publish derives global seq/prev, signs via the existing closed-profile signer,
// and self-checks against the exact retained anchor. Accepted bytes/code/plan are
// exposed only after exact persistence acknowledgement. Exact retransmission is
// rechecked for currentness but returns its original outcome without renewal.
func (p *Publisher) Publish(in Input) ([]byte, Outcome) {
	if p == nil {
		return no("freshness_state_unavailable")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fenced {
		return no("freshness_state_unavailable")
	}
	if len(in.Snapshot.Bytes) > journal.MaxRecordBytes || len(in.Witness.Original) > journal.MaxRecordBytes {
		return no("freshness_malformed")
	}
	if len(in.Witness.Original) == 0 {
		return no("freshness_unavailable")
	}
	if !timestamp(in.Watermark) {
		return no("watermark_unavailable")
	}
	checkpoint, e := parse(in.Checkpoint)
	if e != nil {
		return no("freshness_malformed")
	}
	cb := obj(checkpoint["body"])
	c := in.Context
	c.FreshnessAnchor = p.state.Anchor.fact()
	watermark := in.Watermark
	if watermark < p.state.Watermark {
		watermark = p.state.Watermark
	}
	c.Watermark, _ = verifier.Present(watermark)
	c.WatermarkAvailable, _ = verifier.Present(true)
	c.JournalWitness = in.Witness.Fact
	c.WitnessReadAt, _ = verifier.Present(in.Witness.ReadAt)
	availability := "unavailable"
	if in.Snapshot.Available {
		availability = "available"
	}
	c.SignedSnapshots, e = verifier.Present(map[string]any{p.registry: map[string]any{"snapshot": in.Snapshot.Bytes, "availability": availability, "max_age": in.Snapshot.MaxAge, "rollback_safe": in.Snapshot.RollbackSafe}})
	if e != nil {
		return no("freshness_malformed")
	}
	var raw, encoded []byte
	retry := false
	if len(p.state.History) > 0 {
		last := p.state.History[len(p.state.History)-1]
		if p.state.Anchor.IssuedAt != nil && in.IssuedAt == *p.state.Anchor.IssuedAt && bytes.Equal(in.Checkpoint, last.Checkpoint) && bytes.Equal(in.Snapshot.Bytes, last.Snapshot) {
			raw = bytes.Clone(last.Bytes)
			retry = true
		}
	}
	if !retry {
		if p.state.Anchor.PublicationSeq == journal.MaxSequence {
			return no("freshness_state_invalid")
		}
		seq := p.state.Anchor.PublicationSeq + 1
		var prev any
		if number(cb["seq"]) == p.state.Anchor.RegistrySeq && p.state.Anchor.FreshnessID != nil {
			prev = *p.state.Anchor.FreshnessID
		}
		body := map[string]any{"type": "freshness/1", "registry": p.registry, "owner": p.owner, "publication_seq": seq, "prev": prev, "issued_at": in.IssuedAt, "snapshot": cb["snapshot"], "registry_seq": cb["seq"], "checkpoint_id": checkpoint["id"]}
		encoded, e = record.Canonical(body)
		if e != nil {
			return no("freshness_malformed")
		}
		raw, e = p.signer.Sign(encoded)
		if e != nil {
			var typed *journal.Error
			if errors.As(e, &typed) {
				return no(string(typed.Code))
			}
			return no(string(journal.SignerUnavailable))
		}
	}
	r := Verify(c, [][]byte{raw}, in.Checkpoint, in.IDs, in.Keys)
	if r.Code != "accept" {
		return nil, Outcome{Code: r.Code, Plan: r.Plan}
	}
	if retry {
		return bytes.Clone(raw), p.state.History[len(p.state.History)-1].Outcome.clone()
	}
	env, e := parse(raw)
	if e != nil {
		return no("freshness_malformed")
	}
	// Preserve full verifier refusal order, then require the signer to have
	// returned exactly the requested body rather than another valid publication.
	signed, e := record.Canonical(env["body"])
	if e != nil || !bytes.Equal(encoded, signed) {
		return no(string(journal.SignerInvalid))
	}
	b := obj(env["body"])
	id, issued := text(env["id"]), text(b["issued_at"])
	a := Anchor{p.registry, p.owner, number(b["publication_seq"]), &id, &issued, text(b["snapshot"]), number(b["registry_seq"]), text(b["checkpoint_id"])}
	outcome := Outcome{Code: r.Code, Plan: r.Plan}.clone()
	next := p.state.Clone()
	next.Anchor = a
	next.Watermark = r.Plan.Watermark
	next.History = append(next.History, Publication{bytes.Clone(raw), bytes.Clone(in.Checkpoint), bytes.Clone(in.Snapshot.Bytes), bytes.Clone(in.Witness.Original), in.Witness.ReadAt, a.clone(), outcome.clone()})
	nextID, e := StateID(next)
	if e != nil {
		p.fenced = true
		return no("freshness_state_unavailable")
	}
	ack, e := p.store.Commit(Update{p.stateID, next.Clone()})
	if e != nil || ack != nextID {
		p.fenced = true
		return no("freshness_persistence_unavailable")
	}
	p.state, p.stateID = next, nextID
	return bytes.Clone(raw), outcome
}
