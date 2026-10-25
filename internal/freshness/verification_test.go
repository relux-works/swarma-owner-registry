package freshness_test

import "testing"

// Missing proof does not become publication genesis. A nearby full-profile control accepts.
func TestFreshnessMissingRefused(t *testing.T) {
	fixtureVerdict(t, "N-FR-missing", "freshness_unavailable")
	fixtureVerdict(t, "P-FR-genesis", "accept")
}

// A stale tip cannot renew an otherwise valid old base. A nearby full-profile control accepts.
func TestFreshnessStaleRefused(t *testing.T) {
	fixtureVerdict(t, "N-FR-stale", "freshness_unavailable")
	fixtureVerdict(t, "P-FR-genesis", "accept")
}

// An operator signature cannot replace the protected registry-owner root. A nearby full-profile control accepts.
func TestFreshnessWrongRootRefused(t *testing.T) {
	fixtureVerdict(t, "N-FR-signer-operator", "freshness_signature_invalid")
	fixtureVerdict(t, "P-FR-genesis", "accept")
}

// The tip must name the exact authenticated snapshot and checkpoint snapshot. A nearby full-profile control accepts.
func TestFreshnessWrongSnapshotRefused(t *testing.T) {
	fixtureVerdict(t, "N-FR-snapshot-binding", "freshness_binding_mismatch")
	fixtureVerdict(t, "P-FR-genesis", "accept")
}

// The publication must bind the positive checkpoint sequence. A nearby full-profile control accepts.
func TestFreshnessWrongHeadRefused(t *testing.T) {
	fixtureVerdict(t, "N-FR-registry-seq-binding", "freshness_binding_mismatch")
	fixtureVerdict(t, "P-FR-genesis", "accept")
}

// A different publication at an accepted sequence cannot replace it. A nearby full-profile control accepts.
func TestFreshnessSameSeqDifferentBindingRefused(t *testing.T) {
	fixtureVerdict(t, "N-FR-fork", "freshness_fork")
	fixtureVerdict(t, "P-FR-genesis", "accept")
}

// An adjacent publication must advance its actual predecessor time. A nearby full-profile control accepts.
func TestFreshnessBackwardTimeRefused(t *testing.T) {
	fixtureVerdict(t, "N-FR-time-backward", "freshness_time_invalid")
	fixtureVerdict(t, "P-FR-genesis", "accept")
}

// A fresh authenticated chain cannot revive an effective revoked queried key. A nearby full-profile control accepts.
func TestFreshnessCannotReviveAuthority(t *testing.T) {
	fixtureVerdict(t, "N-FR-revoked", "revoked")
	fixtureVerdict(t, "P-FR-genesis", "accept")
	in := inputNamed(t, "N-FR-revoked")
	state := initial(t)
	cp := parsed(t, in.Checkpoint)
	body := object(cp["body"])
	state.Genesis.RegistrySeq = body["seq"].(int64)
	state.Genesis.CheckpointID = text(cp["id"])
	state.Genesis.Snapshot = text(body["snapshot"])
	state.Anchor = state.Genesis
	store := &persistence{}
	p := publisher(t, state, store)
	before := p.LastResolved()
	b, r := p.Publish(in)
	noCandidate(t, b, r, "revoked")
	unchanged(t, before, p.LastResolved())
	if store.calls != 0 {
		t.Fatal("revoked query persisted")
	}
}
