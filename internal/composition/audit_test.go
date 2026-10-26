package composition

import (
	"github.com/relux-works/waggle/verifier"
	"testing"
)

// Absence is exact: nil audit differs from a present empty audit, and nil slice
// differs from present-empty, matching S8's reflect.DeepEqual set contract.
func TestAuditAbsenceIsExact(t *testing.T) {
	if !sameAudit(nil, nil) || sameAudit(nil, &verifier.FreshnessReanchorPlan{}) {
		t.Fatal("audit absence")
	}
	a := &verifier.FreshnessReanchorPlan{}
	b := &verifier.FreshnessReanchorPlan{OldSnapshots: []verifier.SnapshotBinding{}}
	if sameAudit(a, b) || !sameAudit(a, &verifier.FreshnessReanchorPlan{}) {
		t.Fatal("slice absence")
	}
}
