// Package qtime projects caller-qualified intervals without clocks, I/O or a
// provider dependency. It authenticates no receipt, boot, sample or continuity.
package qtime

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

const second int64 = 1_000_000_000

var ErrInterval = errors.New("invalid qualified interval")

// Interval preserves canonical signed decimal nanoseconds, including explicit
// zero. Empty required values are absent, not epoch or a bootstrap permission.
// Reference is optional; all other fields must come from protected qualification.
// Provider floors are never the registry's admitted-plan watermark.
type Interval struct {
	Observed, Lower, Upper, AuthorityUntil  string
	PriorHighWater, HighWater, JournalFloor string
	Boot, SampleID                          string
	Reference                               string
}

// Projection is conservative whole-second UTC evidence. DecisionFloor remains
// decimal nanoseconds for the next consumption. Persist it and Boot outside
// rollback before acknowledging a decision; this pure package persists nothing.
type Projection struct {
	IssuedAt, Now, Watermark, AuthorityUntil string
	DecisionFloor, Boot, SampleID            string
}

func integer(s string) (int64, error) {
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || strconv.FormatInt(n, 10) != s {
		return 0, ErrInterval
	}
	return n, nil
}

// ValidFloor checks a retained floor's representation without narrowing it to
// seconds. A pre-epoch genesis floor is permitted only as protected input.
func ValidFloor(s string) bool { _, e := integer(s); return e == nil }

func ceil(n int64) int64 {
	q := n / second
	if n > 0 && n%second != 0 {
		q++
	}
	return q
}

func stamp(s int64) string { return time.Unix(s, 0).UTC().Format("2006-01-02T15:04:05Z") }

// validate preserves the original nanosecond predicates before any conversion.
func (v Interval) validate(retainedFloor string) ([]int64, error) {
	bad := func() ([]int64, error) { return nil, ErrInterval }
	if strings.TrimSpace(v.Boot) == "" || strings.TrimSpace(v.SampleID) == "" {
		return bad()
	}
	values := []string{v.Observed, v.Lower, v.Upper, v.AuthorityUntil, v.PriorHighWater, v.HighWater, v.JournalFloor, retainedFloor}
	n := make([]int64, len(values))
	for i, s := range values {
		var e error
		n[i], e = integer(s)
		if e != nil {
			return bad()
		}
	}
	o, l, u, a, w, h, j, retained := n[0], n[1], n[2], n[3], n[4], n[5], n[6], n[7]
	if l < 0 || u < l || o < l || o > u || u-l > 3*second || o-l != u-o || o-l > 1_500_000_000 {
		return bad()
	}
	if o > math.MaxInt64-300*second || a > o+300*second {
		return bad()
	}
	if v.Reference != "" {
		r, e := integer(v.Reference)
		if e != nil || r > o || r > math.MaxInt64-2048*second || a > r+2048*second {
			return bad()
		}
	}
	if u >= a || l <= w || l <= j || h != u || j < retained {
		return bad()
	}
	return n, nil
}

// Project checks original predicates before rounding, then repeats them on the
// conservative seconds. prior is only this registry's admitted-plan watermark;
// retainedFloor is its independently retained decision floor. Neither argument
// may be reconstructed from the provider's consumed high water.
func (v Interval) Project(prior, retainedFloor string) (Projection, error) {
	bad := func() (Projection, error) { return Projection{}, ErrInterval }
	n, e := v.validate(retainedFloor)
	if e != nil {
		return bad()
	}
	l, u, a, w, j := n[1], n[2], n[3], n[4], n[6]
	low, high, end := l/second, ceil(u), a/second
	if high >= end || low <= ceil(w) || low <= ceil(j) || high > math.MaxInt64/second {
		return bad()
	}
	p, e := time.Parse("2006-01-02T15:04:05Z", prior)
	if e != nil || p.Year() < 1970 || p.Year() > 9999 || p.Format("2006-01-02T15:04:05Z") != prior || p.Unix() > high {
		return bad()
	}
	watermark := max(p.Unix(), low)
	return Projection{stamp(low), stamp(high), stamp(watermark), stamp(end), strconv.FormatInt(high*second, 10), v.Boot, v.SampleID}, nil
}

// FloorAtLeast compares only canonical nanosecond floors, without rounding.
func FloorAtLeast(current, previous string) bool {
	a, e := integer(current)
	if e != nil {
		return false
	}
	b, e := integer(previous)
	return e == nil && a >= b
}
