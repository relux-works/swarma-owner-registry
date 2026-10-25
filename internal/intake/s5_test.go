package intake_test

import (
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/pins"
	"github.com/relux-works/waggle/verifier"
)

// S5 can mint a real receipt for an aged registry base, but receipt-free intake
// retains own-age refusal on all three legacy paths and accepts each fresh neighbor.
func TestIntakeLegacyAgeWithoutReceipt(t *testing.T) {
	s, fresh, c, tip, cp := currentnessProof(t)
	proof := verifier.New(c).VerifyFreshness([][]byte{tip}, cp, nil, nil)
	if proof.Code != "accept" || proof.FreshnessReceipt == nil {
		t.Fatalf("S5 producer control: %s", proof.Code)
	}
	registry, _ := s.config.Pin(pins.RegistryOwner)
	for _, kind := range []string{"attestation/1", "revocation/1", "grant/1"} {
		t.Run(kind, func(t *testing.T) {
			old := s
			old.kind, old.facts = kind, cloneFacts(t, s.facts)
			if kind == "attestation/1" {
				for _, raw := range old.facts.Attestations {
					old.raw = raw
					break
				}
			}
			current := old
			current.facts = cloneFacts(t, old.facts)
			snapshot := current.facts.Snapshots[registry.Key]
			snapshot.Bytes = fresh
			current.facts.Snapshots[registry.Key] = snapshot
			if kind == "revocation/1" {
				old.raw, current.raw = old.facts.Snapshots[registry.Key].Bytes, fresh
			}
			check(t, old, "key_binding_unavailable")
			check(t, current, "accept")
		})
	}
}
