package intake_test

import (
	"bytes"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/relux-works/swarma-owner-registry/internal/composition"
	"github.com/relux-works/swarma-owner-registry/internal/intake"
	"github.com/relux-works/swarma-owner-registry/internal/pins"
	"github.com/relux-works/swarma-owner-registry/internal/qtime"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

type registryFixture struct {
	s       sample
	r       *intake.Registry
	current intake.RegistryCurrent
	proof   composition.Proof
	fresh   []byte
}

// These synthetic intervals test projection and receipt binding, not an actual
// authenticated clock, witness client, durable store or final exclusion.
func registryClock(t *testing.T, when, prior string) composition.Clock {
	t.Helper()
	at, err := time.Parse(time.RFC3339, when)
	if err != nil {
		t.Fatal(err)
	}
	ns := func(x time.Time) string { return strconv.FormatInt(x.UnixNano(), 10) }
	n, floor := ns(at), ns(at.Add(-time.Second))
	return composition.Clock{Interval: qtime.Interval{Observed: n, Lower: n, Upper: n, AuthorityUntil: ns(at.Add(300 * time.Second)), PriorHighWater: floor, HighWater: n, JournalFloor: floor, Boot: "boot-a", SampleID: "sample-" + n}, PriorWatermark: prior, DecisionFloor: floor}
}

func registrySetup(t *testing.T) registryFixture {
	t.Helper()
	s, fresh, ctx, tip, cp := currentnessProof(t)
	for _, raw := range s.facts.Attestations {
		s.raw = raw
		break
	}
	s.kind = "attestation/1"
	r, err := intake.NewRegistry(s.config, s.facts.Snapshots)
	if err != nil {
		t.Fatal(err)
	}
	facts := cloneFacts(t, s.facts)
	facts.Snapshots = nil
	clock := registryClock(t, text(s.facts.Now), text(s.facts.Watermark))
	return registryFixture{s: s, r: r, fresh: fresh,
		current: intake.RegistryCurrent{Facts: facts, Clock: clock, JournalWitness: ctx.JournalWitness, WitnessReadAt: ctx.WitnessReadAt},
		proof:   composition.Proof{Context: ctx, Clock: clock, Chain: [][]byte{tip}, Checkpoint: cp}}
}

func registryMint(t *testing.T, f *registryFixture) *composition.Admission {
	t.Helper()
	a, result := f.r.VerifyFreshness(f.proof)
	if a == nil || result.Code != "accept" {
		t.Fatalf("full F control: %s", result.Code)
	}
	registryAcknowledge(f, a)
	return a
}

func registryAcknowledge(f *registryFixture, a *composition.Admission) {
	p, q := a.Plan(), a.Projection()
	f.current.Persisted = &composition.PersistenceAcknowledgement{Anchor: a.Anchor(), Watermark: p.Watermark, Reanchor: p.Reanchor, DecisionFloor: q.DecisionFloor, Boot: q.Boot, SnapshotJournal: p.SnapshotJournal, IDs: p.IDs}
	f.current.Clock.PriorWatermark = p.Watermark
}

func registryWant(t *testing.T, f registryFixture, want string) *intake.Accepted {
	t.Helper()
	a, err := f.r.Accept(f.s.kind, f.s.raw, f.current)
	got := "accept"
	if err != nil {
		var typed *intake.Error
		if !errors.As(err, &typed) || a != nil || err.Error() != string(typed.Code) {
			t.Fatalf("untyped refusal/nonzero admission: %v", err)
		}
		got = string(typed.Code)
	}
	if got != want {
		t.Fatalf("want %s got %s", want, got)
	}
	if err == nil && (a.ID != text(parse(t, f.s.raw)["id"]) || !bytes.Equal(a.Bytes, f.s.raw)) {
		t.Fatal("lost exact submitted bytes/id")
	}
	return a
}

// Required absence closes quiet registry intake; a full F admission plus exact
// acknowledged persistence permits the same >60-second-old signed base.
func TestQuietRegistryIntakeWithoutReceiptRefused(t *testing.T) {
	f := registrySetup(t)
	registryWant(t, f, "freshness_receipt_unavailable")
	registryMint(t, &f)
	registryWant(t, f, "accept")
}

// Identical reconstructed whole Evidence is a different premise. The positive
// neighbor also detects accidental rebuilding on each ordinary admission.
func TestQuietRegistryIntakeRebuiltEvidenceRefused(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	registryWant(t, f, "accept")
	if err := f.r.ReplaceSnapshots(f.s.facts.Snapshots); err != nil {
		t.Fatal(err)
	}
	registryWant(t, f, "freshness_receipt_binding_mismatch")
}

// Missing authenticated witness/read refuses rather than falling back; restoring
// current protected facts restores use inside the original receipt lifetime.
func TestRegistryIntakeWitnessUnavailableRefused(t *testing.T) {
	for _, slot := range []string{"witness", "read"} {
		t.Run(slot, func(t *testing.T) {
			f := registrySetup(t)
			registryMint(t, &f)
			good := f.current
			if slot == "witness" {
				f.current.JournalWitness = verifier.Evidence{}
			} else {
				f.current.WitnessReadAt = verifier.Evidence{}
			}
			registryWant(t, f, "freshness_receipt_unavailable")
			f.current = good
			registryWant(t, f, "accept")
		})
	}
}

// Full F without acknowledgement, or with a mismatched acknowledgement, cannot
// authorize intake. The exact caller acknowledgement is the valid neighbor.
func TestRegistryIntakePersistenceAcknowledgementRequired(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	good := *f.current.Persisted
	f.current.Persisted = nil
	registryWant(t, f, "freshness_receipt_unavailable")
	bad := good
	bad.Anchor.FreshnessID = record.Digest([]byte("other tip"))
	f.current.Persisted = &bad
	registryWant(t, f, "freshness_receipt_binding_mismatch")
	f.current.Persisted = &good
	registryWant(t, f, "accept")
}

// A changed qualification in another whole-map row invalidates the receipt,
// including when the row later returns to its original id/booleans. Full F
// against that newly frozen premise, not restoring booleans, is the remedy.
func TestRegistryIntakeOtherRowPremiseRequiresRemint(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	registryWant(t, f, "accept")
	root, _ := f.s.config.Pin(pins.GrantRoot)
	changed := cloneFacts(t, f.s.facts).Snapshots
	row := changed[root.Key]
	row.RollbackSafe = false
	changed[root.Key] = row
	if err := f.r.ReplaceSnapshots(changed); err != nil {
		t.Fatal(err)
	}
	registryWant(t, f, "freshness_receipt_binding_mismatch")
	if err := f.r.ReplaceSnapshots(f.s.facts.Snapshots); err != nil {
		t.Fatal(err)
	}
	registryWant(t, f, "freshness_receipt_binding_mismatch")
	registryAdvance(t, &f, false)
	registryMint(t, &f)
	registryWant(t, f, "accept")
}

// Persisted metadata alone after process reconstruction supplies no receipt.
// Full producer verification is required again, never a receipt deserialization.
func TestRegistryIntakeRestartRequiresFullProof(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	r, err := intake.NewRegistry(f.s.config, f.s.facts.Snapshots)
	if err != nil {
		t.Fatal(err)
	}
	f.r = r
	registryWant(t, f, "freshness_receipt_unavailable")
	registryAdvance(t, &f, false)
	registryMint(t, &f)
	registryWant(t, f, "accept")
}

// A successful early admission cannot authorize a later effect at exclusive
// expiry, with unavailable witness, changed premise, or a journal conflict.
// The hook is called once only for a successful recheck under the boundary.
func TestRegistryIntakeRefusalHasNoEffect(t *testing.T) {
	for _, name := range []string{"expiry", "witness", "premise", "conflict", "valid"} {
		t.Run(name, func(t *testing.T) {
			f := registrySetup(t)
			registryMint(t, &f)
			candidate := registryWant(t, f, "accept")
			calls, inside := 0, false
			want := map[string]string{"expiry": "freshness_receipt_expired", "witness": "freshness_receipt_unavailable", "premise": "freshness_receipt_binding_mismatch", "conflict": "envelope_conflict", "valid": "accept"}[name]
			boundary := func(check func(intake.RegistryCurrent) error) error {
				inside = true
				defer func() { inside = false }()
				switch name {
				case "expiry":
					f.current.Clock = registryClock(t, "2026-01-01T00:04:00Z", f.current.Persisted.Watermark)
				case "witness":
					f.current.JournalWitness = verifier.Evidence{}
				case "premise":
					if err := f.r.ReplaceSnapshots(f.s.facts.Snapshots); err != nil {
						t.Fatal(err)
					}
				case "conflict":
					f.current.Facts.Retained[candidate.ID] = []byte("different bytes")
				}
				return check(f.current)
			}
			err := f.r.Apply(candidate, boundary, func(a *intake.Accepted) error {
				calls++
				if !inside || !bytes.Equal(a.Bytes, candidate.Bytes) {
					t.Fatal("effect outside boundary or wrong bytes")
				}
				return nil
			})
			if want == "accept" {
				if err != nil || calls != 1 {
					t.Fatal(err, calls)
				}
				return
			}
			var typed *intake.Error
			if !errors.As(err, &typed) || string(typed.Code) != want || calls != 0 {
				t.Fatalf("want %s without effect, got %v calls=%d", want, err, calls)
			}
		})
	}
}

// Quiet means the unchanged signed base is already two minutes old at mint;
// repeated genuine clock/read advances retain that same whole Evidence and
// original receipt, without extending its fixed one-minute lifetime.
func TestQuietRegistryIntakeWithReceiptAccepted(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	for _, at := range []string{"2026-01-01T00:03:00Z", "2026-01-01T00:03:10Z", "2026-01-01T00:03:59Z"} {
		f.current.Clock = registryClock(t, at, f.current.Persisted.Watermark)
		f.current.WitnessReadAt = evidence(t, at)
		registryWant(t, f, "accept")
	}
	f.current.Clock = registryClock(t, "2026-01-01T00:04:00Z", f.current.Persisted.Watermark)
	registryWant(t, f, "freshness_receipt_expired")
}

// Replacement bytes cannot use the old receipt. Exact own-age submission is
// separately accepted without changing the current premise. Only full F over
// its committed new head, followed by acknowledgement, permits the new premise.
func TestReissuedRegistryIntakeEvidenceAccepted(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	f.s.kind, f.s.raw = "revocation/1", f.fresh
	f.current.Submission = intake.Snapshot{Available: true, RollbackSafe: true, MaxAge: 60}
	registryWant(t, f, "accept")
	for _, raw := range f.s.facts.Attestations {
		f.s.raw = raw
		break
	}
	f.s.kind = "attestation/1"
	registryWant(t, f, "accept")
	root, _ := f.s.config.Pin(pins.RegistryOwner)
	row := f.s.facts.Snapshots[root.Key]
	row.Bytes = f.fresh
	f.s.facts.Snapshots[root.Key] = row
	if err := f.r.ReplaceSnapshots(f.s.facts.Snapshots); err != nil {
		t.Fatal(err)
	}
	registryWant(t, f, "freshness_receipt_binding_mismatch")
	registryAdvance(t, &f, true)
	registryMint(t, &f)
	registryWant(t, f, "accept")
}

// Explicit recovery alone can cross an authenticated publication gap. Normal F
// refuses without recovery fallback and fences prior use; the recovery audit
// must be acknowledged exactly before its newly minted receipt can be consumed.
func TestRegistryIntakeReanchorRequiresFullProof(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	registryAdvance(t, &f, false)
	body := object(parse(t, f.proof.Chain[0])["body"])
	body["publication_seq"] = int64(4)
	body["prev"] = record.Digest([]byte("missing predecessor"))
	root, private := publicKey(85)
	f.proof.Chain = [][]byte{sign(t, body, root, private)}
	a, result := f.r.VerifyFreshness(f.proof)
	if a != nil || result.Code != "freshness_gap" || result.FreshnessReceipt != nil {
		t.Fatal("normal proof did not refuse gap", result.Code)
	}
	registryWant(t, f, "freshness_receipt_unavailable")
	a, result = f.r.VerifyFreshnessReanchor(f.proof)
	if a == nil || result.Code != "accept" || a.Plan().Reanchor == nil || a.Plan().Reanchor.SkippedPublications != 2 {
		t.Fatal("full recovery control", result.Code)
	}
	f.current.Persisted = nil
	registryWant(t, f, "freshness_receipt_unavailable")
	registryAcknowledge(&f, a)
	registryWant(t, f, "accept")
	f.current.Persisted.Reanchor = nil
	registryWant(t, f, "freshness_receipt_binding_mismatch")
}

// New submissions are checked from exact raw bytes before any receipt exists;
// neither the retained base nor metadata.Bytes can substitute a younger record.
// Submission never installs new Evidence or creates receipt authority.
func TestRegistryIntakeSubmissionUsesExactOwnAge(t *testing.T) {
	f := registrySetup(t)
	f.s.kind = "revocation/1"
	root, _ := f.s.config.Pin(pins.RegistryOwner)
	f.s.raw = f.s.facts.Snapshots[root.Key].Bytes
	f.current.Submission = intake.Snapshot{Bytes: f.fresh, Available: true, RollbackSafe: true, MaxAge: 60}
	registryWant(t, f, "key_binding_unavailable")
	f.s.raw = f.fresh
	registryWant(t, f, "accept")
	f.current.Submission.RollbackSafe = false
	registryWant(t, f, "key_binding_unavailable")
	for _, raw := range f.s.facts.Attestations {
		f.s.raw = raw
		break
	}
	f.s.kind = "attestation/1"
	registryWant(t, f, "freshness_receipt_unavailable")
	registryMint(t, &f)
	registryWant(t, f, "accept")
}

// A current registry receipt can authorize agent intake while an external-root
// submitted snapshot is too old; it never waives that root's own age boundary.
func TestRegistryIntakeExternalSnapshotOwnAgeUnchanged(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	registryAdvance(t, &f, false)
	registryMint(t, &f)
	root, _ := f.s.config.Pin(pins.GrantRoot)
	raw := f.s.facts.Snapshots[root.Key].Bytes
	f.s.kind, f.s.raw = "revocation/1", raw
	f.current.Submission = intake.Snapshot{Available: true, RollbackSafe: true, MaxAge: 60}
	f.current.Clock = registryClock(t, "2026-01-01T00:04:00Z", f.current.Persisted.Watermark)
	f.current.WitnessReadAt = evidence(t, "2026-01-01T00:04:00Z")
	registryWant(t, f, "accept") // The own-age boundary remains inclusive.
	f.current.Clock = registryClock(t, "2026-01-01T00:04:01Z", f.current.Persisted.Watermark)
	f.current.WitnessReadAt = evidence(t, "2026-01-01T00:04:01Z")
	registryWant(t, f, "key_binding_unavailable")
	for _, raw := range f.s.facts.Attestations {
		f.s.raw = raw
		break
	}
	f.s.kind = "attestation/1"
	registryWant(t, f, "accept")
}

// The retaining path cannot activate grant/service/device admission, accept
// ad-hoc snapshot facts, bypass its combined budget, or retain authority after
// a rejected premise update. Existing pure intake remains the separate route.
func TestRegistryIntakeRoutingAndBudgetRefused(t *testing.T) {
	for _, name := range []string{"grant", "device", "service", "facts", "budget", "update"} {
		t.Run(name, func(t *testing.T) {
			f := registrySetup(t)
			registryMint(t, &f)
			registryWant(t, f, "accept")
			switch name {
			case "grant":
				f.s.kind = "grant/1"
				registryWant(t, f, "record_type_unsupported")
			case "device", "service":
				body := object(parse(t, f.s.raw)["body"])
				class := name
				if name == "device" {
					class = "operator-device"
				}
				body["key_class"] = class
				root, private := publicKey(85)
				f.s.raw = sign(t, body, root, private)
				registryWant(t, f, "key_class_unsupported")
			case "facts":
				f.current.Facts.Snapshots = f.s.facts.Snapshots
				registryWant(t, f, "request_invalid")
			case "budget":
				// Every record meets its individual cap; the combined charge fails.
				f.current.Facts.Retained = map[string][]byte{}
				for i := range 8 {
					f.current.Facts.Retained["entry-"+strconv.Itoa(i)] = bytes.Repeat([]byte("x"), intake.MaxRecordBytes)
				}
				registryWant(t, f, "request_too_large")
			case "update":
				err := f.r.ReplaceSnapshots(map[string]intake.Snapshot{"other": {Bytes: bytes.Repeat([]byte("x"), intake.MaxIdentityBytes+1)}})
				var typed *intake.Error
				if !errors.As(err, &typed) || typed.Code != intake.RequestTooLarge {
					t.Fatal(err)
				}
				registryWant(t, f, "freshness_receipt_unavailable")
			}
		})
	}
}

// A caller effect failure is returned unchanged and is never retried; an
// accepting effect is the nearby boundary control in the no-effect test.
func TestRegistryIntakeEffectErrorNotRetried(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	want := errors.New("effect refused")
	calls := 0
	err := f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error { return check(f.current) }, func(*intake.Accepted) error { calls++; return want })
	if err != want || calls != 1 {
		t.Fatal("effect failure changed or retried", err, calls)
	}
}

func registryAdvance(t *testing.T, f *registryFixture, replacement bool) {
	t.Helper()
	old := f.current.Persisted.Anchor
	anchor := map[string]any{"registry_id": old.RegistryID, "owner": old.Owner, "publication_seq": old.PublicationSeq, "freshness_id": old.FreshnessID, "issued_at": old.IssuedAt, "snapshot": old.Snapshot, "registry_seq": old.RegistrySeq, "checkpoint_id": old.CheckpointID}
	f.proof.Context.FreshnessAnchor = evidence(t, anchor)
	cp := parse(t, f.proof.Checkpoint)
	cpBody := object(cp["body"])
	_, private := publicKey(85)
	if replacement {
		cpBody["seq"] = int64(2)
		cpBody["snapshot"] = text(parse(t, f.fresh)["id"])
		cpBody["head"] = record.Digest([]byte("replacement journal head"))
		cpBody["at"] = "2026-01-01T00:03:00Z"
		f.proof.Checkpoint = sign(t, cpBody, old.RegistryID, private)
	}
	cp = parse(t, f.proof.Checkpoint)
	cpBody = object(cp["body"])
	at := "2026-01-01T00:03:10Z"
	var prev any = old.FreshnessID
	if replacement {
		prev = nil
	} // A changed journal head starts its new freshness segment.
	tip := sign(t, map[string]any{"type": "freshness/1", "registry": old.RegistryID, "owner": old.Owner, "publication_seq": old.PublicationSeq + 1, "prev": prev, "issued_at": at, "snapshot": cpBody["snapshot"], "registry_seq": cpBody["seq"], "checkpoint_id": cp["id"]}, old.RegistryID, private)
	f.proof.Chain = [][]byte{tip}
	f.proof.Context.JournalWitness = evidence(t, map[string]any{"registry_id": old.RegistryID, "owner": old.Owner, "registry_seq": cpBody["seq"], "registry_head": cpBody["head"], "checkpoint_id": cp["id"], "previous_checkpoint_id": old.CheckpointID})
	if !replacement {
		f.proof.Context.JournalWitness = f.current.JournalWitness
	}
	f.proof.Context.WitnessReadAt = evidence(t, at)
	f.proof.Clock = registryClock(t, at, f.current.Persisted.Watermark)
	f.current.Clock, f.current.JournalWitness, f.current.WitnessReadAt = f.proof.Clock, f.proof.Context.JournalWitness, f.proof.Context.WitnessReadAt
}
