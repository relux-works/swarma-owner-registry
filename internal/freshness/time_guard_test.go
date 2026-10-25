package freshness

import (
	"strconv"
	"testing"
	"time"

	"github.com/relux-works/swarma-owner-registry/internal/qtime"
)

// Equal or lower issue seconds cannot create another publication. This guard
// is tested directly: even subsecond uncertainty cannot make strict second
// continuity admit two consumptions with the same floor(L).
func TestEqualConsecutiveIssuedAtRefused(t *testing.T) {
	previous := "2026-01-01T00:03:00Z"
	at, _ := time.Parse(time.RFC3339, previous)
	ns := func(offset time.Duration) string { return strconv.FormatInt(at.Add(offset).UnixNano(), 10) }
	// A 100ms uncertainty interval shares the predecessor's issue second.
	// Project it against an earlier protected floor to isolate the independent
	// issue-time guard; two real consecutive converted consumptions cannot do so.
	interval := qtime.Interval{Observed: ns(300 * time.Millisecond), Lower: ns(200 * time.Millisecond), Upper: ns(400 * time.Millisecond), AuthorityUntil: ns(300 * time.Second), PriorHighWater: ns(-time.Second), HighWater: ns(400 * time.Millisecond), JournalFloor: ns(-time.Second), Boot: "boot-a", SampleID: "sample-a"}
	clock, err := interval.Project(previous, interval.JournalFloor)
	if err != nil || clock.IssuedAt != previous || advances(clock.IssuedAt, &previous) {
		t.Fatal("subsecond interval bypassed strict increase", err)
	}
	if advances(previous, &previous) || advances("2026-01-01T00:02:59Z", &previous) {
		t.Fatal("non-increasing issue time accepted")
	}
	if !advances("2026-01-01T00:03:01Z", &previous) || !advances("1970-01-01T00:00:00Z", nil) {
		t.Fatal("valid issue time refused")
	}
}
