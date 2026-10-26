// Package plan detaches verifier candidates without changing absence semantics.
package plan

import "github.com/relux-works/swarma-envelope/verifier"

func bindings(s []verifier.SnapshotBinding) []verifier.SnapshotBinding {
	if s == nil {
		return nil
	}
	out := make([]verifier.SnapshotBinding, len(s))
	copy(out, s)
	return out
}

// Clone preserves nil versus present-empty slices, including both reanchor sets.
func Clone(p verifier.Plan) verifier.Plan {
	if p.IDs != nil {
		ids := make([]string, len(p.IDs))
		copy(ids, p.IDs)
		p.IDs = ids
	}
	if p.Reanchor != nil {
		v := *p.Reanchor
		v.OldSnapshots = bindings(v.OldSnapshots)
		v.NewSnapshots = bindings(v.NewSnapshots)
		p.Reanchor = &v
	}
	if p.SnapshotJournal != nil {
		v := *p.SnapshotJournal
		if v.Latest != nil {
			v.Latest = make([]verifier.SnapshotJournalEntry, len(p.SnapshotJournal.Latest))
			copy(v.Latest, p.SnapshotJournal.Latest)
			for i := range v.Latest {
				if v.Latest[i].SnapshotID != nil {
					id := *v.Latest[i].SnapshotID
					v.Latest[i].SnapshotID = &id
				}
			}
		}
		p.SnapshotJournal = &v
	}
	return p
}
