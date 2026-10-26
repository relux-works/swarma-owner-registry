// Package composition connects full freshness admission to process-local /1 and /2
// consumers after a caller acknowledges persistence. It performs no I/O, clock
// collection, durability, witness authentication or intake activation.
package composition

import (
	"encoding/json"
	"reflect"

	"github.com/relux-works/swarma-owner-registry/internal/plan"

	"github.com/relux-works/swarma-owner-registry/internal/qtime"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

// Clock contains one caller-qualified interval and independently retained
// registry state. PriorWatermark is the admitted-plan watermark, never a provider
// consumed bound. DecisionFloor is the registry's protected retained upper floor.
// Project checks the interval; it does not authenticate its source or continuity.
type Clock struct {
	Interval                      qtime.Interval
	PriorWatermark, DecisionFloor string
}

// Proof supplies the complete signed producer proof and protected facts. Receipt
// consumption fields do not affect these independent library producer gates.
// VerifyFreshnessReanchor accepts exactly one tip in Chain, by explicit choice.
type Proof struct {
	Context    verifier.Context
	Clock      Clock
	Chain      [][]byte
	Checkpoint []byte
	IDs, Keys  []string
}

// Anchor is the local typed projection of an admitted positive freshness tip,
// not a new closed record or a genesis/missing-state bootstrap.
type Anchor struct {
	RegistryID, Owner               string
	PublicationSeq                  int64
	FreshnessID, IssuedAt, Snapshot string
	RegistrySeq                     int64
	CheckpointID                    string
	RegistryCurrencyVersion         int                        `json:"registry_currency_version,omitempty"`
	Snapshots                       []verifier.SnapshotBinding `json:"snapshots,omitempty"`
}

func (a Anchor) clone() Anchor {
	if a.Snapshots != nil {
		rows := make([]verifier.SnapshotBinding, len(a.Snapshots))
		copy(rows, a.Snapshots)
		a.Snapshots = rows
	}
	return a
}
func (a Anchor) evidence() verifier.Evidence {
	m := map[string]any{"registry_id": a.RegistryID, "owner": a.Owner, "publication_seq": a.PublicationSeq, "freshness_id": a.FreshnessID, "issued_at": a.IssuedAt, "registry_seq": a.RegistrySeq, "checkpoint_id": a.CheckpointID}
	if a.RegistryCurrencyVersion == 2 {
		var rows []any
		for _, r := range a.Snapshots {
			rows = append(rows, map[string]any{"root_role": r.RootRole, "root_key_id": r.RootKeyID, "snapshot_id": r.SnapshotID})
		}
		m["snapshots"] = rows
	} else {
		m["snapshot"] = a.Snapshot
	}
	e, _ := verifier.Present(m)
	return e
}

// PersistenceAcknowledgement is supplied only after the caller atomically
// persists the admitted tip, watermark and any re-anchor audit, and retains the
// projected DecisionFloor/Boot outside rollback. It contains no receipt. Equality
// checks bind the claim to this admission; they cannot establish real durability.
type PersistenceAcknowledgement struct {
	Anchor              Anchor
	Watermark           string
	Reanchor            *verifier.FreshnessReanchorPlan
	DecisionFloor, Boot string
	// In /2 the same atomic commitment also covers the complete derived index and inventory.
	SnapshotJournal *verifier.SnapshotJournalState `json:"snapshot_journal,omitempty"`
	IDs             []string                       `json:"ids,omitempty"`
}

// Admission retains the original pointer and whole immutable Evidence premise.
// Only full F success initializes it. It is process-local; persist Plan/Anchor
// and recovery audit, never Admission or a receipt. Its zero value has no authority.
type Admission struct {
	receipt    *verifier.FreshnessReceipt
	snapshots  verifier.Evidence
	anchor     Anchor
	plan       verifier.Plan
	projection qtime.Projection
}

// Plan returns a detached candidate transaction, not a persistence acknowledgement.
func (a *Admission) Plan() verifier.Plan {
	if a == nil {
		return verifier.Plan{}
	}
	return clonePlan(a.plan)
}
func (a *Admission) Anchor() Anchor {
	if a == nil {
		return Anchor{}
	}
	return a.anchor.clone()
}
func (a *Admission) Projection() qtime.Projection {
	if a == nil {
		return qtime.Projection{}
	}
	return a.projection
}

// Audit reports detached origin only, even after expiry; it is never a currency,
// effect or persistence check. A new process must perform full F admission again.
func (a *Admission) Audit() (verifier.FreshnessAudit, bool) {
	if a == nil {
		return verifier.FreshnessAudit{}, false
	}
	return a.receipt.Audit()
}

// VerifyFreshness performs only normal full admission; it never retries recovery.
// Refusal returns no Admission and the library's zero Plan/nil receipt result.
func VerifyFreshness(in Proof) (*Admission, verifier.Result) {
	ctx, p := project(in.Context, in.Clock)
	r := verifier.New(ctx).VerifyFreshness(in.Chain, in.Checkpoint, in.IDs, in.Keys)
	return admitted(in, ctx, p, r)
}

// VerifyFreshnessReanchor is an explicit recovery choice, retaining the library's
// skipped-span audit and limitations; normal continuity is never silently waived.
func VerifyFreshnessReanchor(in Proof) (*Admission, verifier.Result) {
	if len(in.Chain) != 1 {
		return nil, verifier.Result{Code: "freshness_malformed"}
	}
	ctx, p := project(in.Context, in.Clock)
	r := verifier.New(ctx).VerifyFreshnessReanchor(in.Chain[0], in.Checkpoint, in.IDs, in.Keys)
	return admitted(in, ctx, p, r)
}

func admitted(in Proof, ctx verifier.Context, p qtime.Projection, r verifier.Result) (*Admission, verifier.Result) {
	if r.Code != "accept" {
		return nil, r
	}
	// Parse only the already fully verified tip, never use audit alone to admit it.
	var tip struct {
		ID   string `json:"id"`
		Body struct {
			Registry       string                     `json:"registry"`
			Owner          string                     `json:"owner"`
			PublicationSeq int64                      `json:"publication_seq"`
			IssuedAt       string                     `json:"issued_at"`
			Snapshot       string                     `json:"snapshot"`
			RegistrySeq    int64                      `json:"registry_seq"`
			CheckpointID   string                     `json:"checkpoint_id"`
			Type           string                     `json:"type"`
			Snapshots      []verifier.SnapshotBinding `json:"snapshots"`
		} `json:"body"`
	}
	if len(in.Chain) == 0 || json.Unmarshal(in.Chain[len(in.Chain)-1], &tip) != nil || r.FreshnessReceipt == nil {
		return nil, verifier.Result{Code: "freshness_malformed"}
	}
	b := tip.Body
	a := &Admission{receipt: r.FreshnessReceipt, snapshots: ctx.SignedSnapshots, plan: clonePlan(r.Plan), projection: p, anchor: Anchor{RegistryID: b.Registry, Owner: b.Owner, PublicationSeq: b.PublicationSeq, FreshnessID: tip.ID, IssuedAt: b.IssuedAt, Snapshot: b.Snapshot, RegistrySeq: b.RegistrySeq, CheckpointID: b.CheckpointID, Snapshots: b.Snapshots}}
	if b.Type == "freshness/2" {
		a.anchor.RegistryCurrencyVersion = 2
	}
	return a, r
}

func clonePlan(p verifier.Plan) verifier.Plan             { return plan.Clone(p) }
func sameAudit(a, b *verifier.FreshnessReanchorPlan) bool { return reflect.DeepEqual(a, b) }
func refusal(code string) error                           { return &record.Error{Code: code} }

// project replaces all point-time inputs with one conservative projection. An
// invalid interval leaves time unavailable, allowing the library's earlier wire
// and signed-evidence failures to retain precedence before its trusted-time gate.
func project(ctx verifier.Context, clock Clock) (verifier.Context, qtime.Projection) {
	p, e := clock.Interval.Project(clock.PriorWatermark, clock.DecisionFloor)
	if e != nil {
		ctx.Time = verifier.Evidence{}
		ctx.Now = verifier.Evidence{}
		ctx.Watermark = verifier.Evidence{}
		return ctx, qtime.Projection{}
	}
	ctx.Now, _ = verifier.Present(p.Now)
	ctx.Watermark, _ = verifier.Present(p.Watermark)
	ctx.Time, _ = verifier.Present(map[string]any{"trusted": true, "fresh": true, "authority_until": p.AuthorityUntil})
	// Availability is still the caller's protected state claim, not inferred here.
	return ctx, p
}

// Use supplies current protected facts and genuine time. Both whole Evidence
// identities, SignedSnapshots and (in /2) SnapshotJournal, must stay unchanged.
// Context.SignedSnapshots
// must be the same entire Evidence supplied to F, including all other rows and
// qualification premises. New retains that Evidence without Present/rebuilding.
// The current authenticated JournalWitness/read remain caller inputs.
// RequireFreshnessReceipt and FreshnessMarginSeconds retain their Context
// semantics. A supplied Admission always selects receipt mode, even if optional.
type Use struct {
	Context   verifier.Context
	Clock     Clock
	Admission *Admission
	Persisted *PersistenceAcknowledgement
}

// Consumer offers the library consumer entry points without fallback. Construction
// checks the caller's persistence claim; entry-point R1–R3 and their ordering
// remain exclusively in the pinned verifier. Each method invokes it once.
type Consumer struct{ context verifier.Context }

// NewRequired selects receipt-backed authority before construction. It lets
// adapters keep ordinary untyped facts separate from receipt policy; omission
// of an Admission can never silently choose legacy own-age verification.
func NewRequired(in Use) (*Consumer, error) {
	in.Context.RequireFreshnessReceipt = true
	return New(in)
}

// New binds an admitted original receipt only after its exact acknowledgement.
// Nil Admission with Context.RequireFreshnessReceipt=false is deliberate legacy
// own-age verification.
// Pre-attached raw receipts cannot bypass the admitted-plan acknowledgement.
// Acknowledgement errors are setup errors before any verification entry point.
func New(in Use) (*Consumer, error) {
	ctx := in.Context
	a := in.Admission
	if a != nil {
		if a.receipt == nil {
			return nil, refusal("freshness_receipt_unavailable")
		}
		ack := in.Persisted
		if ack == nil {
			return nil, refusal("freshness_receipt_unavailable")
		}
		if !reflect.DeepEqual(ack.Anchor, a.anchor) || ack.Watermark != a.plan.Watermark || !sameAudit(ack.Reanchor, a.plan.Reanchor) || ack.DecisionFloor != a.projection.DecisionFloor || ack.Boot != a.projection.Boot {
			return nil, refusal("freshness_receipt_binding_mismatch")
		}
		if a.anchor.RegistryCurrencyVersion == 2 && (!reflect.DeepEqual(ack.SnapshotJournal, a.plan.SnapshotJournal) || !reflect.DeepEqual(ack.IDs, a.plan.IDs)) {
			return nil, refusal("freshness_receipt_binding_mismatch")
		}
		if ctx.FreshnessReceipt != nil && ctx.FreshnessReceipt != a.receipt {
			return nil, refusal("freshness_receipt_binding_mismatch")
		}
		// Preserve the caller's whole current Evidence so changes cannot be
		// concealed. The library's ordered R2 gate checks its identity; this
		// adapter never rebuilds or substitutes the original snapshots.
		ctx.FreshnessReceipt = a.receipt
		ctx.FreshnessAnchor = ack.Anchor.evidence()
		// A later retained admitted watermark is permitted, a rolled-back one is not.
		if in.Clock.PriorWatermark < ack.Watermark {
			return nil, refusal("freshness_receipt_binding_mismatch")
		}
	} else if ctx.FreshnessReceipt != nil {
		return nil, refusal("freshness_receipt_unavailable")
	}
	ctx, _ = project(ctx, in.Clock)
	return &Consumer{context: ctx}, nil
}

func (c *Consumer) VerifyAttestation(key, reference string, device bool) (verifier.Attestation, error) {
	return verifier.New(c.context).VerifyAttestation(key, reference, device)
}
func (c *Consumer) VerifyServiceAttestation(key string) (verifier.Attestation, error) {
	return verifier.New(c.context).VerifyServiceAttestation(key)
}
func (c *Consumer) VerifyMessage(raw []byte) verifier.Result {
	return verifier.New(c.context).VerifyMessage(raw)
}
func (c *Consumer) VerifyOperator(raw []byte) verifier.Result {
	return verifier.New(c.context).VerifyOperator(raw)
}
func (c *Consumer) VerifyEnvelope(raw []byte, expectedType string) verifier.Result {
	return verifier.New(c.context).VerifyEnvelope(raw, expectedType)
}
func (c *Consumer) Authorize(chain [][]byte, request verifier.Evidence) verifier.Result {
	return verifier.New(c.context).Authorize(chain, request)
}
func (c *Consumer) CheckRevocation(root string, ids, keys []string) (verifier.RevocationState, error) {
	return verifier.New(c.context).CheckRevocation(root, ids, keys)
}

// Submission explicitly selects own-age validation of exact new snapshot bytes,
// separate from receipt-backed current-authority checks. Qualification booleans
// remain protected caller claims; they do not prove cumulative history.
type Submission struct {
	Context                 verifier.Context
	Clock                   Clock
	Root                    string
	Snapshot                []byte
	Available, RollbackSafe bool
	MaxAge                  int64
}

// CheckSubmittedSnapshot selects the no-receipt path before any verification,
// including for replacement registry and external-root snapshots. It never
// retries a receipt refusal or authenticates replacement bytes with an old receipt.
// Only this exact issuer row is supplied; other Context evidence stays protected.
func CheckSubmittedSnapshot(in Submission, ids, keys []string) (verifier.RevocationState, error) {
	ctx, _ := project(in.Context, in.Clock)
	ctx.FreshnessReceipt = nil
	ctx.RequireFreshnessReceipt = false
	ctx.FreshnessMarginSeconds = 0
	availability := "unavailable"
	if in.Available {
		availability = "available"
	}
	var e error
	ctx.SignedSnapshots, e = verifier.Present(map[string]any{in.Root: map[string]any{"snapshot": in.Snapshot, "availability": availability, "rollback_safe": in.RollbackSafe, "max_age": in.MaxAge}})
	if e != nil {
		return verifier.Unavailable, refusal("key_binding_invalid")
	}
	return verifier.New(ctx).CheckRevocation(in.Root, ids, keys)
}
