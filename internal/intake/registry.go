package intake

import (
	"bytes"
	"sync"

	"github.com/relux-works/swarma-owner-registry/internal/composition"
	"github.com/relux-works/swarma-owner-registry/internal/pins"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

// Registry retains one entire immutable snapshot premise, not a per-root cache.
// Replacement always creates a new Evidence identity, even for identical input.
// No receipt survives restart. Qualification, clock/witness authentication and
// actual durable persistence remain the caller's responsibilities.
type Registry struct {
	mu           sync.RWMutex
	config       *pins.Config
	snapshots    verifier.Evidence
	snapshotCost int
	admission    *composition.Admission
}

// NewRegistry starts without receipt authority. Full F and an exact persistence
// acknowledgement are required before agent intake, even for a current base.
func NewRegistry(config *pins.Config, snapshots map[string]Snapshot) (*Registry, error) {
	if _, err := config.VerifierContext(); err != nil {
		return nil, refuse(string(VerificationUnavailable))
	}
	r := &Registry{config: config}
	if err := r.ReplaceSnapshots(snapshots); err != nil {
		return nil, err
	}
	return r, nil
}

// ReplaceSnapshots is mandatory after ANY snapshot/qualification change,
// including other rows and a change back to earlier ids or booleans. Call it
// under the caller's premise-update exclusion. It freezes the complete map once;
// an old admission cannot authorize the new identity. A rejected update fences
// the previous premise too. It does not establish cumulative history.
func (r *Registry) ReplaceSnapshots(snapshots map[string]Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots = verifier.Evidence{}
	r.snapshotCost = 0
	f := Facts{Snapshots: snapshots}
	if err := f.preflight(0); err != nil {
		return err
	}
	for key, s := range snapshots {
		r.snapshotCost += len(key) + 128 + len(s.Bytes)
	}
	if snapshots == nil {
		return nil
	}
	e, err := verifier.Present(snapshotRecords(snapshots))
	if err != nil {
		return refuse(string(VerificationUnavailable))
	}
	r.snapshots = e
	return nil
}

// VerifyFreshness performs full normal F against the retained whole premise.
// Failed attempts remove prior authority; this never retries re-anchor or legacy.
// Persist the returned Admission's Plan/Anchor before supplying its acknowledgement.
func (r *Registry) VerifyFreshness(proof composition.Proof) (*composition.Admission, verifier.Result) {
	return r.verify(proof, false)
}

// VerifyFreshnessReanchor explicitly selects full recovery F and its audit cost.
func (r *Registry) VerifyFreshnessReanchor(proof composition.Proof) (*composition.Admission, verifier.Result) {
	return r.verify(proof, true)
}

func (r *Registry) verify(proof composition.Proof, reanchor bool) (*composition.Admission, verifier.Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.admission = nil
	ctx, err := r.protected(proof.Context)
	if err != nil {
		return nil, verifier.Result{Code: string(VerificationUnavailable)}
	}
	ctx.SignedSnapshots = r.snapshots
	proof.Context = ctx
	var a *composition.Admission
	var result verifier.Result
	if reanchor {
		a, result = composition.VerifyFreshnessReanchor(proof)
	} else {
		a, result = composition.VerifyFreshness(proof)
	}
	if result.Code == "accept" {
		r.admission = a
	}
	return a, result
}

// protected preserves genuine dynamic evidence while always taking authority
// pins from Config, on both producer and submission paths.
func (r *Registry) protected(ctx verifier.Context) (verifier.Context, error) {
	static, err := r.config.VerifierContext()
	if err != nil {
		return verifier.Context{}, refuse(string(VerificationUnavailable))
	}
	ctx.ServiceRoot, ctx.AgentRevocationRoot = static.ServiceRoot, static.AgentRevocationRoot
	ctx.CommitSigningKey, ctx.EnrollmentPublisher = static.CommitSigningKey, static.EnrollmentPublisher
	ctx.GrantRoot, ctx.OperatorRoot = static.GrantRoot, static.OperatorRoot
	ctx.RegistryOwner, ctx.OperatorOwner, ctx.ServiceAuthority = static.RegistryOwner, static.OperatorOwner, static.ServiceAuthority
	return ctx, nil
}

// RegistryCurrent supplies current protected inputs, never snapshot replacements.
// Facts.Snapshots must be nil: use ReplaceSnapshots for every premise change.
// Clock supplies genuine interval facts; Facts' point-time values cannot replace
// that projection. JournalWitness and WitnessReadAt must be freshly authenticated.
// Submission supplies only issuer qualification metadata for exact submitted
// revocation bytes; its Bytes field is ignored. No acknowledgement is required
// for this explicitly separate own-age submission path.
type RegistryCurrent struct {
	Facts                         Facts
	Clock                         composition.Clock
	JournalWitness, WitnessReadAt verifier.Evidence
	Persisted                     *composition.PersistenceAcknowledgement
	Submission                    Snapshot
}

// Accept admits registry-owner agent attestations or exact new revocation/1
// submissions. Grants, devices and services retain their existing separate gates.
// It performs no effects and does not update snapshots or mint a receipt.
func (r *Registry) Accept(kind string, raw []byte, current RegistryCurrent) (*Accepted, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.accept(kind, raw, current)
}

func (r *Registry) accept(kind string, raw []byte, in RegistryCurrent) (*Accepted, error) {
	if len(raw) > MaxIdentityBytes {
		return nil, refuse(string(RequestTooLarge))
	}
	if kind != "attestation/1" && kind != "revocation/1" {
		return nil, refuse(string(RecordTypeUnsupported))
	}
	value, err := record.Parse(raw, MaxIdentityBytes, 16)
	if err != nil {
		return nil, upstream(err)
	}
	envelope, _ := value.(map[string]any)
	body, _ := envelope["body"].(map[string]any)
	if body == nil {
		return nil, refuse(string(RequestInvalid))
	}
	if body["type"] != kind {
		return nil, refuse("envelope_type_mismatch")
	}
	id, ok := envelope["id"].(string)
	if !ok {
		return nil, refuse(string(RequestInvalid))
	}
	if kind == "attestation/1" && body["key_class"] != "agent" {
		return nil, refuse(string(KeyClassUnsupported))
	}
	if in.Facts.Snapshots != nil {
		return nil, refuse(string(RequestInvalid))
	}
	if len(raw)+r.snapshotCost > MaxEvidenceBytes {
		return nil, refuse(string(RequestTooLarge))
	}
	if err := in.Facts.preflight(len(raw) + r.snapshotCost); err != nil {
		return nil, err
	}
	ctx, err := r.protected(verifier.Context{})
	if err != nil {
		return nil, err
	}
	if err = in.Facts.fill(&ctx); err != nil {
		return nil, err
	}
	ctx.SignedSnapshots = r.snapshots // identity retained, never Present per admission
	ctx.JournalWitness, ctx.WitnessReadAt = in.JournalWitness, in.WitnessReadAt
	service, _ := r.config.Pin(pins.GrantService)
	ctx.ServiceAttestations, err = serviceRecords(in.Facts.Services, service.Key, "", nil)
	if err != nil {
		return nil, err
	}
	issuer, _ := body["issuer"].(string)
	if kind == "revocation/1" {
		m := in.Submission
		state, e := composition.CheckSubmittedSnapshot(composition.Submission{Context: ctx, Clock: in.Clock, Root: issuer, Snapshot: raw, Available: m.Available, RollbackSafe: m.RollbackSafe, MaxAge: m.MaxAge}, nil, nil)
		if e != nil {
			return nil, upstream(e)
		}
		if state != verifier.NotRevoked {
			return nil, refuse("key_binding_unavailable")
		}
	} else {
		ctx.Attestations, err = withRecord(in.Facts.Attestations, id, raw)
		if err != nil {
			return nil, err
		}
		consumer, e := composition.NewRequired(composition.Use{Context: ctx, Clock: in.Clock, Admission: r.admission, Persisted: in.Persisted})
		if e != nil {
			return nil, upstream(e)
		}
		subject, _ := body["subject"].(map[string]any)
		key, _ := subject["key_id"].(string)
		if _, e = consumer.VerifyAttestation(key, id, false); e != nil {
			return nil, upstream(e)
		}
	}
	if err = in.Facts.conflict(id, raw); err != nil {
		return nil, err
	}
	return &Accepted{ID: id, Type: kind, IssuerRoot: issuer, Bytes: bytes.Clone(raw)}, nil
}

// RegistryBoundary runs its callback once under the caller's serialized final
// exclusion, with freshly collected current facts, and holds that exclusion
// through callback return (including the effect). It must propagate refusal and
// must not perform an effect itself. No witness/clock collection is implemented.
type RegistryBoundary func(check func(RegistryCurrent) error) error

// RegistryEffect is the caller's effect hook, invoked only after the repeated
// admission succeeds under RegistryBoundary. The hook owns actual effects and
// durability; its error is propagated unchanged, without retry.
type RegistryEffect func(*Accepted) error

// Apply re-verifies the exact candidate at the serialized effect boundary. Even
// a fabricated/mutated candidate has no authority without this complete check.
// Snapshot replacement/full F cannot interleave between this check and effect.
func (r *Registry) Apply(candidate *Accepted, boundary RegistryBoundary, effect RegistryEffect) error {
	if candidate == nil || boundary == nil || effect == nil {
		return refuse(string(RequestInvalid))
	}
	kind, raw := candidate.Type, bytes.Clone(candidate.Bytes)
	return boundary(func(current RegistryCurrent) error {
		r.mu.RLock()
		defer r.mu.RUnlock()
		accepted, err := r.accept(kind, raw, current)
		if err != nil {
			return err
		}
		return effect(accepted)
	})
}
