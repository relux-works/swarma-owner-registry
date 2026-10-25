package freshness_test

import (
	"fmt"
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/journal"
)

// Direct and wrapped signer errors forward only the journal signer's five
// failure codes. Every refusal has zero output/plan and no state or persistence
// effect; a real signer still verifies and commits a valid publication.
func TestSignerErrorCodeAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name string
		code journal.Code
		want string
	}{
		{"malformed", journal.Malformed, "journal_malformed"},
		{"profile-refused", journal.ProfileRefused, "signer_profile_refused"},
		{"unavailable", journal.SignerUnavailable, "signer_unavailable"},
		{"invalid", journal.SignerInvalid, "signer_signature_invalid"},
		{"rollback", journal.Rollback, "journal_rollback"},
		{"accept", journal.Code("accept"), "signer_unavailable"},
		{"empty", journal.Code(""), "signer_unavailable"},
		{"unknown", journal.Code("provider-diagnostic"), "signer_unavailable"},
		{"projection", journal.ProjectionInvalid, "signer_unavailable"},
		{"fork", journal.Fork, "signer_unavailable"},
	} {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/wrapped=%t", tc.name, wrapped), func(t *testing.T) {
				store := &persistence{}
				s := signerFunc(func([]byte) ([]byte, error) {
					var err error = &journal.Error{Code: tc.code}
					if wrapped {
						err = fmt.Errorf("signing failed: %w", err)
					}
					return []byte("discarded output"), err
				})
				p := publisherWithSigner(t, initial(t), store, s)
				before := p.LastResolved()
				b, r := p.Publish(input(t))
				unchanged(t, before, p.LastResolved())
				if store.calls != 0 {
					t.Fatal("failed signer persisted")
				}
				noCandidate(t, b, r, tc.want)
			})
		}
	}
	t.Run("valid-signer", func(t *testing.T) {
		store := &persistence{}
		p := publisher(t, initial(t), store)
		_, r := publish(t, p, input(t))
		if store.calls != 1 || len(store.updates) != 1 || p.LastResolved().Anchor.PublicationSeq != 1 || r.Plan.Watermark == "" {
			t.Fatal("valid signer did not verify and persist publication")
		}
	})
}
