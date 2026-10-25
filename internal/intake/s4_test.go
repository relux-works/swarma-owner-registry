package intake_test

import (
	"errors"
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/intake"
	"github.com/relux-works/swarma-owner-registry/internal/pins"
	"github.com/relux-works/waggle/record"
	"github.com/relux-works/waggle/verifier"
)

func evidence(t *testing.T, value any) verifier.Evidence {
	t.Helper()
	e, err := verifier.Present(value)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// currentnessProof uses published fixed-seed test keys and an explicit genesis
// anchor. The current tip binds the exact two-minute-old registry snapshot;
// all grant/identity lifetimes and the consumed grant-root snapshot stay current.
func currentnessProof(t *testing.T) (sample, []byte, verifier.Context, []byte, []byte) {
	t.Helper()
	s, rootPrivate := syntheticService(t)
	registry, registryPrivate := publicKey(85)
	object(object(s.document["roles"])[string(pins.RegistryOwner)])["key"] = registry
	s.config = config(t, s.document)
	now := "2026-01-01T00:03:00Z"
	s.facts.Now, s.facts.Watermark = now, now
	s.facts.Time = map[string]any{"trusted": true, "fresh": true, "authority_until": "2026-01-01T00:04:00Z"}
	root, _ := s.config.Pin(pins.GrantRoot)
	snapshot := func(issuer, issued string, private []byte) []byte {
		return sign(t, map[string]any{"type": "revocation/1", "issuer": issuer, "issued_at": issued, "complete": true, "entries": []any{}}, issuer, private)
	}
	s.facts.Snapshots[root.Key] = intake.Snapshot{Bytes: snapshot(root.Key, now, rootPrivate), Available: true, MaxAge: 60, RollbackSafe: true}
	old := snapshot(registry, "2026-01-01T00:01:00Z", registryPrivate)
	fresh := snapshot(registry, now, registryPrivate)
	s.facts.Snapshots[registry] = intake.Snapshot{Bytes: old, Available: true, MaxAge: 60, RollbackSafe: true}
	base := load(t, "P-SVC-chain")
	grant := object(parse(t, base.raw)["body"])
	grant["issuer"], grant["parent"] = root.Key, nil
	grant["exp"], grant["expires"] = "2026-01-01T00:10:00Z", "2026-01-01T00:10:00Z"
	s.kind, s.raw, s.facts.Parents = "grant/1", sign(t, grant, root.Key, rootPrivate), nil
	snapshotID := text(parse(t, old)["id"])
	entry := map[string]any{"type": "journal-entry/1", "registry": registry, "owner": "owner-a", "seq": int64(1), "prev": nil, "at": "2026-01-01T00:01:00Z", "op": "add", "record_type": "revocation/1", "record": snapshotID}
	head, err := record.ContentID("journal-entry/1", entry)
	if err != nil {
		t.Fatal(err)
	}
	cp := sign(t, map[string]any{"type": "checkpoint/1", "registry": registry, "owner": "owner-a", "seq": int64(1), "head": head, "at": "2026-01-01T00:01:00Z", "snapshot": snapshotID}, registry, registryPrivate)
	cpID := text(parse(t, cp)["id"])
	tip := sign(t, map[string]any{"type": "freshness/1", "registry": registry, "owner": "owner-a", "publication_seq": int64(1), "prev": nil, "issued_at": now, "snapshot": snapshotID, "registry_seq": int64(1), "checkpoint_id": cpID}, registry, registryPrivate)
	c, err := s.config.VerifierContext()
	if err != nil {
		t.Fatal(err)
	}
	c.Now, c.Time = evidence(t, s.facts.Now), evidence(t, s.facts.Time)
	c.Watermark, c.WatermarkAvailable = evidence(t, s.facts.Watermark), evidence(t, s.facts.WatermarkAvailable)
	c.SignedSnapshots = evidence(t, map[string]any{registry: map[string]any{"snapshot": old, "availability": "available", "max_age": int64(60), "rollback_safe": true}})
	c.JournalWitness = evidence(t, map[string]any{"registry_id": registry, "owner": "owner-a", "registry_seq": int64(1), "registry_head": head, "checkpoint_id": cpID, "previous_checkpoint_id": nil})
	c.FreshnessAnchor = evidence(t, map[string]any{"registry_id": registry, "owner": "owner-a", "publication_seq": int64(0), "freshness_id": nil, "issued_at": nil, "snapshot": snapshotID, "registry_seq": int64(1), "checkpoint_id": cpID})
	c.WitnessReadAt = evidence(t, now)
	return s, fresh, c, tip, cp
}

func requirePinRefusal(t *testing.T, d map[string]any, want pins.Code) {
	t.Helper()
	c, err := pins.Parse(wire(t, d))
	var typed *pins.Error
	if c != nil || !errors.As(err, &typed) || typed.Code != want || err.Error() != string(want) {
		t.Fatalf("want nil configuration/%s, got config=%v error=%v", want, c != nil, err)
	}
}

// S3 policy cannot coexist with the current build; real signed freshness bytes still have no intake route under valid policy.
func TestS3PinCannotAdmitFreshness(t *testing.T) {
	s := load(t, "P-SVC-chain")
	check(t, s, "accept")
	s.document["revision"] = "9b42338b8a57d12a21da4db4375b269deacdd6bf"
	requirePinRefusal(t, s.document, pins.RevisionUnsupported)
	s, _, _, tip, _ := currentnessProof(t)
	s.kind, s.raw = "freshness/1", tip
	check(t, s, "record_type_unsupported")
}

// Opening each S4 gate preserves intake routing, compiled type/class sets and the closed signed class profile.
func TestS4ProfileWideningRefused(t *testing.T) {
	for _, gate := range []pins.Gate{pins.Freshness, pins.VerifyFreshness, pins.Reanchor} {
		t.Run(string(gate), func(t *testing.T) {
			s, fresh, _, tip, _ := currentnessProof(t)
			s.document["activation"] = map[string]any{string(gate): true}
			s.config = config(t, s.document)
			registry, _ := s.config.Pin(pins.RegistryOwner)
			snap := s.facts.Snapshots[registry.Key]
			snap.Bytes = fresh
			s.facts.Snapshots[registry.Key] = snap
			check(t, s, "accept")
			for _, kind := range []string{"freshness/1", "contact/1", "message/1", "operator-act/1", "new/1"} {
				n := s
				n.kind, n.raw = kind, tip
				check(t, n, "record_type_unsupported")
			}
			class := s
			class.kind = "attestation/1"
			service, _ := s.config.Pin(pins.GrantService)
			body := object(parse(t, s.facts.Services[service.Key])["body"])
			body["exp"] = "2026-01-01T00:10:00Z"
			root, private := publicKey(80)
			class.raw = sign(t, body, root, private)
			check(t, class, "accept")
			body["key_class"] = "robot"
			class.raw = sign(t, body, root, private)
			check(t, class, "key_class_unsupported")
			widened := parse(t, wire(t, s.document))
			widened["record_types"] = append(widened["record_types"].([]any), "freshness/1")
			requirePinRefusal(t, widened, pins.ProfileUnsupported)
			widened = parse(t, wire(t, s.document))
			widened["key_classes"] = append(widened["key_classes"].([]any), "robot")
			requirePinRefusal(t, widened, pins.KeyClassUnsupported)
		})
	}
}

// Valid freshness for the exact aged registry base cannot waive own-age through any of the three legacy intake paths.
func TestLegacyCurrentnessNotWeakened(t *testing.T) {
	s, fresh, c, tip, cp := currentnessProof(t)
	proof := verifier.New(c).VerifyFreshness([][]byte{tip}, cp, nil, nil)
	if proof.Code != "accept" || proof.Plan.Watermark != s.facts.Now || len(proof.Plan.IDs) != 3 {
		t.Fatalf("valid aged-base freshness control refused: %+v", proof)
	}
	registry, _ := s.config.Pin(pins.RegistryOwner)
	for _, kind := range []string{"attestation/1", "revocation/1", "grant/1"} {
		t.Run(kind, func(t *testing.T) {
			n := s
			n.kind = kind
			n.facts = cloneFacts(t, s.facts)
			if kind == "attestation/1" {
				for _, raw := range n.facts.Attestations {
					n.raw = raw
					break
				}
			}
			old := n.facts.Snapshots[registry.Key]
			positive := n
			positive.facts = cloneFacts(t, n.facts)
			current := old
			current.Bytes = fresh
			positive.facts.Snapshots[registry.Key] = current
			if kind == "revocation/1" {
				positive.raw, n.raw = fresh, old.Bytes
			}
			check(t, positive, "accept")
			check(t, n, "key_binding_unavailable")
		})
	}
}
