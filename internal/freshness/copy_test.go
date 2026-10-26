package freshness

import (
	"github.com/relux-works/swarma-envelope/verifier"
	"testing"
)

// The archive Outcome copier detaches both new audit slices independently,
// including when a caller originally shares their backing array.
func TestOutcomeReanchorCopiesDoNotAlias(t *testing.T) {
	rows := []verifier.SnapshotBinding{{SnapshotID: "original"}}
	p := Outcome{Plan: verifier.Plan{Reanchor: &verifier.FreshnessReanchorPlan{OldSnapshots: rows, NewSnapshots: rows}}}
	q := p.clone()
	q.Plan.Reanchor.OldSnapshots[0].SnapshotID = "changed"
	q.Plan.Reanchor.NewSnapshots[0].SnapshotID = "other"
	if rows[0].SnapshotID != "original" || p.Plan.Reanchor.NewSnapshots[0].SnapshotID != "original" {
		t.Fatal("audit slice alias")
	}
}
