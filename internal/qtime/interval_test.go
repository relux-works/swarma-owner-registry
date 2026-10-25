package qtime

import (
	"math"
	"strconv"
	"testing"
)

func ns(n int64) string { return strconv.FormatInt(n, 10) }
func sample(l, u, a, w, j int64) Interval {
	return Interval{Observed: ns(l + (u-l)/2), Lower: ns(l), Upper: ns(u), AuthorityUntil: ns(a), PriorHighWater: ns(w), HighWater: ns(u), JournalFloor: ns(j), Boot: "boot-a", SampleID: "sample-a"}
}
func project(v Interval) (Projection, error) {
	return v.Project("1970-01-01T00:00:00Z", v.JournalFloor)
}

// U=A is refused by the original nanosecond gate itself, even though O<A.
// Increasing A by a whole second supplies a nearby accepted control.
func TestPointTimeViewCannotAccept(t *testing.T) {
	v := sample(99*second, 101*second, 101*second, 0, 0)
	if _, e := v.validate("0"); e != ErrInterval {
		t.Fatal("original U=A accepted")
	}
	if _, e := project(v); e != ErrInterval {
		t.Fatal("point view accepted")
	}
	v.AuthorityUntil = ns(102 * second)
	if _, e := project(v); e != nil {
		t.Fatal(e)
	}
}

// Provider prior W is checked strictly, while its newly consumed bound is U
// and never a prior registry watermark. A stale interval cannot be repaired by
// supplying U as the registry watermark.
func TestConsumedHighWaterNotPriorWatermark(t *testing.T) {
	for _, tc := range []struct {
		l, u int64
		good bool
	}{{170, 172, false}, {201, 203, false}, {202, 204, true}} {
		v := sample(tc.l*second, tc.u*second, 300*second, 201*second, 140*second)
		p, e := project(v)
		if (e == nil) != tc.good {
			t.Fatalf("[%d,%d]: %v", tc.l, tc.u, e)
		}
		if tc.good && p.Watermark != "1970-01-01T00:03:22Z" {
			t.Fatal(p)
		}
		if !tc.good {
			if _, e = v.Project("1970-01-01T00:03:24Z", ns(140*second)); e == nil {
				t.Fatal("watermark repaired stale receipt")
			}
		}
	}
}

// Original refusals remain refusals at subsecond A/W/J boundaries; conservative
// conversion can additionally refuse a valid original interval, never accept it.
func TestCoarseRoundingNeverAccepts(t *testing.T) {
	for _, offset := range []int64{-1, 0, 1, second - 1} {
		for _, which := range []string{"authority", "provider", "journal"} {
			v := sample(100*second, 102*second, 104*second, 90*second, 90*second)
			switch which {
			case "authority":
				v.AuthorityUntil = ns(102*second + offset)
			case "provider":
				v.PriorHighWater = ns(100*second + offset)
			case "journal":
				v.JournalFloor = ns(100*second + offset)
			}
			_, raw := v.validate(v.JournalFloor)
			_, rounded := project(v)
			if raw != nil && rounded == nil {
				t.Fatalf("original refusal lost: %s %d", which, offset)
			}
		}
	}
	v := sample(100*second, 101*second, 101*second+second/2, 90*second, 90*second)
	if _, e := v.validate(v.JournalFloor); e != nil {
		t.Fatal(e)
	}
	if _, e := project(v); e != ErrInterval {
		t.Fatal("rounded authority endpoint accepted")
	}
	// With fractional U just below an integral A, rounding U down would accept.
	v = sample(100*second, 101*second+second/2, 102*second, 90*second, 90*second)
	if _, e := v.validate(v.JournalFloor); e != nil {
		t.Fatal(e)
	}
	if _, e := project(v); e != ErrInterval {
		t.Fatal("U rounded down")
	}
	v.AuthorityUntil = ns(103 * second)
	if _, e := project(v); e != nil {
		t.Fatal(e)
	}
	for _, which := range []string{"provider", "journal"} {
		v = sample(100*second+second/2, 102*second+second/2, 105*second, 99*second, 99*second)
		if which == "provider" {
			v.PriorHighWater = ns(100*second + 1)
		} else {
			v.JournalFloor = ns(100*second + 1)
		}
		if _, e := v.validate(v.JournalFloor); e != nil {
			t.Fatal(e)
		}
		if _, e := project(v); e != ErrInterval {
			t.Fatalf("rounded %s floor ignored", which)
		}
	}
}

// Exact projection floors fractional L, ceils fractional U, leaves integral U
// unchanged and preserves an independently stricter registry watermark.
func TestProjectionExactBounds(t *testing.T) {
	for _, u := range []int64{102 * second, 102*second + second/2} {
		l := 100*second + second/2
		v := sample(l, u, 105*second, 90*second, 90*second)
		p, e := project(v)
		if e != nil {
			t.Fatal(e)
		}
		want := "1970-01-01T00:01:42Z"
		if u%second != 0 {
			want = "1970-01-01T00:01:43Z"
		}
		if p.IssuedAt != "1970-01-01T00:01:40Z" || p.Now != want || p.Watermark != p.IssuedAt || p.Boot != v.Boot || p.SampleID != v.SampleID {
			t.Fatal(p)
		}
		p, e = v.Project("1970-01-01T00:01:41Z", v.JournalFloor)
		if e != nil || p.Watermark != "1970-01-01T00:01:41Z" {
			t.Fatal(p, e)
		}
		if _, e = v.Project("1970-01-01T00:01:44Z", v.JournalFloor); e != ErrInterval {
			t.Fatal("prior watermark above U accepted")
		}
	}
}

// Absent/noncanonical facts, inconsistent intervals, authority ceiling overflow,
// out-of-range seconds and regressed imported floors refuse; valid ceilings and
// the 1.5-second uncertainty boundary have nearby controls.
func TestInvalidIntervalRefused(t *testing.T) {
	base := sample(100*second, 102*second, 200*second, 90*second, 90*second)
	mutations := map[string]func(*Interval){
		"absent": func(v *Interval) { *v = Interval{} }, "boot": func(v *Interval) { v.Boot = "" }, "sample": func(v *Interval) { v.SampleID = "" },
		"lower": func(v *Interval) { v.Lower = "" }, "overflow": func(v *Interval) { v.Upper = "9223372036854775808" },
		"noncanonical": func(v *Interval) { v.Lower = "0100" }, "inverted": func(v *Interval) { v.Lower = ns(103 * second) },
		"highwater": func(v *Interval) { v.HighWater = ns(103 * second) }, "asymmetric": func(v *Interval) { v.Observed = ns(100 * second) },
		"negative": func(v *Interval) { v.Lower = "-1" }, "width": func(v *Interval) { *v = sample(100*second, 103*second+2, 200*second, 0, 0) },
		"ceiling":           func(v *Interval) { v.AuthorityUntil = ns(401*second + 1) },
		"reference":         func(v *Interval) { v.Reference = ns(-2000 * second) },
		"future-reference":  func(v *Interval) { v.Reference = ns(102 * second) },
		"addition-overflow": func(v *Interval) { *v = sample(math.MaxInt64-2*second, math.MaxInt64, math.MaxInt64, 0, 0) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := base
			mutate(&v)
			if _, e := project(v); e != ErrInterval {
				t.Fatal("invalid interval accepted", e)
			}
		})
	}
	for _, delta := range []int64{0, 1_499_999_999, 1_500_000_000} {
		v := sample(101*second-delta, 101*second+delta, 401*second, 0, 0)
		v.Reference = ns(101 * second)
		if _, e := project(v); e != nil {
			t.Fatal(delta, e)
		}
	}
	if _, e := base.Project("1970-01-01T00:00:00Z", ns(91*second)); e != ErrInterval {
		t.Fatal("regressed imported J accepted")
	}
	for _, prior := range []string{"", "not-time", "9999-12-31T23:59:59Z"} {
		if _, e := base.Project(prior, base.JournalFloor); e != ErrInterval {
			t.Fatal("bad prior accepted")
		}
	}
}
