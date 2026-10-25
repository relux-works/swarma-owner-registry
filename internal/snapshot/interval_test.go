package snapshot

import (
	"bytes"
	"strconv"
	"testing"
	"time"
)

// A full synthetic interval issues at floor(L), self-checks at ceil(U) and
// exposes the independent lower watermark/upper decision floor for atomic
// persistence. This tests the pure projection, not provider authentication.
func TestIntervalReceiptAccepted(t *testing.T) {
	c, p, s, clock, _ := publicationFixture(t, "P-JRN-publish-reissue")
	base, _ := time.Parse(time.RFC3339, "2026-01-01T00:01:20Z")
	l := base.UnixNano() + 250_000_000
	u := l + 2_000_000_000
	clock.Lower = strconv.FormatInt(l, 10)
	clock.Upper = strconv.FormatInt(u, 10)
	clock.Observed = strconv.FormatInt(l+1_000_000_000, 10)
	clock.HighWater = clock.Upper
	issued, e := Issue(fixtureSigner(t), s, clock, "2026-01-01T00:01:20Z", nil)
	if e != nil {
		t.Fatal(e)
	}
	if object(parseObject(t, issued.Bytes)["body"])["issued_at"] != "2026-01-01T00:01:20Z" {
		t.Fatal("issued_at not floor(L)")
	}
	change := Change{Anchor: p.Entries[0], RecordBody: wire(t, parseObject(t, p.Entries[1])["body"]), IssuedAt: "2026-01-01T00:01:20Z", CheckpointAt: "2026-01-01T00:01:20Z", Witness: p.Witness}
	got, e := Build(c, fixtureSigner(t), s, clock, change)
	if e != nil {
		t.Fatal(e)
	}
	accepted(t, got.Result)
	if !bytes.Equal(got.Publication.Snapshot, issued.Bytes) || got.Time.IssuedAt != change.IssuedAt || got.Time.Now != "2026-01-01T00:01:23Z" || got.Time.Watermark != change.IssuedAt || got.Time.Boot != clock.Boot || got.Time.SampleID != clock.SampleID {
		t.Fatal(got.Time)
	}
	if got.Time.DecisionFloor != strconv.FormatInt(base.Add(3*time.Second).UnixNano(), 10) {
		t.Fatal("wrong upper decision floor")
	}
}

// Invalid intervals and a regressed imported decision floor cannot reach
// custody or yield a candidate. The complete neighbouring interval accepts.
func TestIntervalRefusalHasNoEffects(t *testing.T) {
	c, p, s, good, _ := publicationFixture(t, "P-JRN-publish-reissue")
	for _, which := range []string{"authority", "boot", "sample", "provider", "journal", "highwater", "own-watermark", "retained-floor"} {
		t.Run(which, func(t *testing.T) {
			clock := good
			state := s
			switch which {
			case "authority":
				clock.AuthorityUntil = clock.Upper
			case "boot":
				clock.Boot = ""
			case "sample":
				clock.SampleID = ""
			case "provider":
				clock.PriorHighWater = clock.Lower
			case "journal":
				clock.JournalFloor = clock.Lower
			case "highwater":
				clock.HighWater = "0"
			case "own-watermark":
				state.Watermark = "2026-01-01T00:01:21Z"
			case "retained-floor":
				state.DecisionFloor = "1"
			}
			signer := &countSigner{Signer: fixtureSigner(t)}
			before := bytes.Clone(state.Snapshot)
			got, e := Issue(signer, state, clock, "2026-01-01T00:01:20Z", nil)
			errorCode(t, e, TimeUnavailable)
			if got != nil || signer.calls != 0 || !bytes.Equal(before, state.Snapshot) {
				t.Fatal("refusal produced effects")
			}
			prepared, e := Prepare(c, signer, state, clock, p)
			errorCode(t, e, TimeUnavailable)
			if prepared != nil || signer.calls != 0 {
				t.Fatal("invalid interval reached preparation")
			}
		})
	}
	if _, e := Prepare(c, fixtureSigner(t), s, good, p); e != nil {
		t.Fatal(e)
	}
}
