package grantsnapshot

import (
	"bytes"
	"sort"
	"time"
)

// Analysis contains proposed receiver observations even when Refusal is set.
// Persist their union before the nonfuture mark through the existing receiver
// journal, never a new sidecar. Mark is only an analysis result, not a protected
// publication watermark or authority. No field grants RollbackSafe.
type Analysis struct {
	Observation *Observation
	Union       []Entry
	Mark        time.Time
	Refusal     error
}

// Analyze compares one signer with its COMPLETE authenticated historical
// originals. nil history means unavailable; an explicit empty slice is only a
// hypothetical bootstrap premise until a custodian admits it. This function
// cannot authenticate completeness, independent floors, restart exclusion or
// currentness. A caller must not turn success into a protected Snapshot result.
// now and watermark must be qualified externally. Full nanoseconds are retained
// for all comparisons. No host wall clock or cache TTL substitutes for them.
func Analyze(history []*Observation, next *Observation, now, watermark time.Time) Analysis {
	a := Analysis{Observation: next}
	if next == nil || next.root == "" || len(next.wire) == 0 {
		a.Observation = nil
		a.Refusal = ErrInvalid
		return a
	}
	if history == nil {
		a.Refusal = ErrContinuity
		return a
	}
	size := len(next.wire)
	if len(history) > MaxHistoryBytes {
		a.Refusal = ErrCapacity
		return a
	}
	for _, o := range history {
		if o == nil || o.root != next.root || len(o.wire) == 0 {
			a.Refusal = ErrContinuity
			return a
		}
		if len(o.wire) > MaxHistoryBytes-size {
			a.Refusal = ErrCapacity
			return a
		}
		size += len(o.wire)
	}
	union := map[string]Entry{}
	at := map[time.Time][]byte{}
	// The prior inventory is not assumed to be ordered. Every retained entry,
	// including future snapshots, constrains the proposed complete next set.
	for _, o := range history {
		if old, ok := at[o.issued]; ok && !bytes.Equal(old, o.wire) {
			a.Refusal = ErrFork
		}
		at[o.issued] = o.wire
		if !now.IsZero() && !o.issued.After(now) && o.issued.After(a.Mark) {
			a.Mark = o.issued
		}
		for _, r := range o.entries {
			k := r.Kind + ":" + r.ID
			if old, ok := union[k]; !ok || r.From.Before(old.From) {
				union[k] = r
			}
		}
	}
	priorMark := a.Mark
	present := map[string]Entry{}
	for _, r := range next.entries {
		k := r.Kind + ":" + r.ID
		if old, ok := present[k]; !ok || r.From.Before(old.From) {
			present[k] = r
		}
	}
	if a.Refusal == nil {
		if old, ok := at[next.issued]; ok && !bytes.Equal(old, next.wire) {
			a.Refusal = ErrFork
		} else if !priorMark.IsZero() && next.issued.Before(priorMark) {
			a.Refusal = ErrRollback
		}
		for k, old := range union {
			r, ok := present[k]
			if !ok || r.From.After(old.From) {
				if a.Refusal == nil {
					a.Refusal = ErrRollback
				}
			}
		}
	}
	// Static-valid evidence survives a freshness or continuity refusal. Entries
	// precede marks in the later durable integration; this call writes nothing.
	for k, r := range present {
		if old, ok := union[k]; !ok || r.From.Before(old.From) {
			union[k] = r
		}
	}
	if len(union) > MaxEntries {
		a.Refusal = ErrCapacity
		return a
	}
	for _, r := range union {
		a.Union = append(a.Union, r)
	}
	sort.Slice(a.Union, func(i, j int) bool {
		x, y := a.Union[i], a.Union[j]
		if x.Kind != y.Kind {
			return x.Kind < y.Kind
		}
		return x.ID < y.ID
	})
	if !now.IsZero() && !next.issued.After(now) && next.issued.After(a.Mark) {
		a.Mark = next.issued
	}
	if a.Refusal != nil {
		return a
	}
	if now.IsZero() || watermark.IsZero() {
		a.Refusal = ErrUnavailable
		return a
	}
	lower, upper := now, watermark
	if lower.After(upper) {
		lower, upper = upper, lower
	}
	if next.issued.After(lower) || upper.Sub(next.issued) > 60*time.Second {
		a.Refusal = ErrUnavailable
	}
	return a
}
