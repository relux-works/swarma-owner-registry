package plan

import (
	"github.com/relux-works/waggle/verifier"
	"reflect"
	"testing"
)

// Nil and present-empty sets are distinct S8 content. Copying preserves both,
// detaches populated sets and nested nullable index pointers, and preserves /1.
func TestCloneCurrencyAbsenceAndBoundaries(t *testing.T) {
	for _, rows := range [][]verifier.SnapshotBinding{nil, {}, {{RootRole: "grant", RootKeyID: "key-a", SnapshotID: "id-a"}}} {
		id := "id-a"
		p := verifier.Plan{Reanchor: &verifier.FreshnessReanchorPlan{OldSnapshots: rows, NewSnapshots: rows}, SnapshotJournal: &verifier.SnapshotJournalState{Version: 2, Latest: []verifier.SnapshotJournalEntry{{SnapshotID: nil}, {SnapshotID: &id}}}}
		q := Clone(p)
		if !reflect.DeepEqual(p, q) {
			t.Fatal("absence changed", p, q)
		}
		if len(rows) > 0 {
			q.Reanchor.OldSnapshots[0].SnapshotID = "changed"
			q.Reanchor.NewSnapshots[0].RootRole = "changed"
			if !reflect.DeepEqual(p.Reanchor.OldSnapshots, rows) || p.Reanchor.NewSnapshots[0].RootRole == "changed" {
				t.Fatal("binding alias")
			}
		}
		*q.SnapshotJournal.Latest[1].SnapshotID = "changed"
		if id != "id-a" || q.SnapshotJournal.Latest[0].SnapshotID != nil {
			t.Fatal("nullable index alias")
		}
	}
	if !reflect.DeepEqual(Clone(verifier.Plan{}), verifier.Plan{}) {
		t.Fatal("legacy changed")
	}
}
