package composition_test

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	c "github.com/relux-works/swarma-owner-registry/internal/composition"
	"github.com/relux-works/waggle/verifier"
)

func consume(t *testing.T, f fixture, u c.Use) string {
	t.Helper()
	v, e := c.New(u)
	if e != nil {
		return code(t, e)
	}
	op := object(f.input["consume"])
	in := object(op["input"])
	checkResult := func(r verifier.Result) string {
		if r.FreshnessReceipt != nil {
			t.Fatal("non-F consumer minted receipt")
		}
		if r.Code != "accept" && !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
			t.Fatal("plan on refusal")
		}
		return r.Code
	}
	switch text(op["mode"]) {
	case "attestation", "device":
		a, e := v.VerifyAttestation(text(in["key"]), text(in["reference"]), op["mode"] == "device")
		if e != nil && a != (verifier.Attestation{}) {
			t.Fatal("identity on refusal")
		}
		return code(t, e)
	case "service":
		a, e := v.VerifyServiceAttestation(text(in["key"]))
		if e != nil && a != (verifier.Attestation{}) {
			t.Fatal("service identity on refusal")
		}
		return code(t, e)
	case "revocation":
		state, e := v.CheckRevocation(text(in["root"]), stringsOf(in["ids"]), stringsOf(in["keys"]))
		if e != nil && state != verifier.Unavailable {
			t.Fatal("state on refusal")
		}
		if e == nil && state == verifier.Unavailable {
			return "key_binding_unavailable"
		}
		return code(t, e)
	case "grant":
		var chain [][]byte
		for _, x := range in["chain"].([]any) {
			chain = append(chain, decode(t, x))
		}
		return checkResult(v.Authorize(chain, fact(t, f.facts["request"])))
	case "operator":
		return checkResult(v.VerifyOperator(decode(t, in["envelope"])))
	default:
		return checkResult(v.VerifyMessage(decode(t, in["envelope"])))
	}
}

// All consumer classes, including validate-only device/operator/service paths,
// preserve expiry and original-pointer binding; no non-F call issues a receipt.
func TestReceiptConsumerEntryPoints(t *testing.T) {
	for _, name := range []string{"P-RC-message", "P-RC-grant", "P-RC-revocation", "P-RC-device", "P-RC-service", "P-RC-operator"} {
		t.Run(name, func(t *testing.T) {
			f := setup(t, name)
			a := acceptedAdmission(t, f)
			u := use(f, a)
			u.Context.SignedSnapshots = fact(t, snapshots(t, f.facts))
			if got := consume(t, f, u); got != "freshness_receipt_binding_mismatch" {
				t.Fatal(got)
			}
			if got := consume(t, f, use(f, a)); got != "accept" {
				t.Fatal(got)
			}
			u = use(f, a)
			u.Clock = clockAt(t, "2026-01-01T00:04:00Z", a.Plan().Watermark)
			if got := consume(t, f, u); got != "freshness_receipt_expired" {
				t.Fatal(got)
			}
		})
	}
	f := setup(t, "P-RC-message")
	a := acceptedAdmission(t, f)
	v := consumer(t, use(f, a))
	raw := decode(t, object(object(f.input["consume"])["input"])["envelope"])
	r := v.VerifyEnvelope(raw, "message/1")
	if r.Code != "accept" || r.FreshnessReceipt != nil {
		t.Fatal(r.Code)
	}
	u := use(f, a)
	u.Context.SignedSnapshots = fact(t, snapshots(t, f.facts))
	r = consumer(t, u).VerifyEnvelope(raw, "message/1")
	if r.Code != "freshness_receipt_binding_mismatch" || !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
		t.Fatal(r.Code)
	}
}

// Receipt-backed authority checks cannot use a registry receipt for another
// domain even if that domain's exact fresh submitted snapshot passes own-age.
func TestExternalRootReceiptRefused(t *testing.T) {
	f := setup(t, "P-RC-current")
	a := acceptedAdmission(t, f)
	v := consumer(t, use(f, a))
	for root, row := range snapshots(t, f.facts) {
		if root == f.root {
			continue
		}
		state, e := v.CheckRevocation(root, nil, nil)
		if state != verifier.Unavailable || code(t, e) != "freshness_receipt_binding_mismatch" {
			t.Fatal(state, e)
		}
		s := c.Submission{Context: f.proof.Context, Clock: f.proof.Clock, Root: root, Snapshot: object(row)["snapshot"].([]byte), Available: true, RollbackSafe: true, MaxAge: 60}
		state, e = c.CheckSubmittedSnapshot(s, nil, nil)
		if state != verifier.NotRevoked || e != nil {
			t.Fatal(state, e)
		}
	}
}

// Altering another row or its qualification changes the entire Evidence premise;
// byte-identical rebuilding is not a supported repair of an old receipt.
func TestWholeEvidencePremiseRefused(t *testing.T) {
	f := setup(t, "P-RC-current")
	a := acceptedAdmission(t, f)
	for _, which := range []string{"other-row", "qualification", "replacement-snapshot"} {
		u := use(f, a)
		m := snapshots(t, f.facts)
		for root, v := range m {
			row := object(v)
			switch which {
			case "other-row":
				if root != f.root {
					row["max_age"] = int64(59)
				}
			case "qualification":
				if root != f.root {
					row["rollback_safe"] = false
				}
			case "replacement-snapshot":
				if root == f.root {
					row["snapshot"] = []byte("replacement")
				}
			}
		}
		u.Context.SignedSnapshots = fact(t, m)
		wantCode(t, f, u, "freshness_receipt_binding_mismatch")
		wantCode(t, f, use(f, a), "accept")
	}
}

// Projection overrides stale point fields with ceil(U), floor(L), protected
// prior watermark and exclusive floor(A); nanosecond/coarse refusals remain.
func TestReceiptIntervalProjection(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	base, _ := time.Parse(time.RFC3339, "2026-01-01T00:03:00Z")
	ns := func(n int64) string { return strconv.FormatInt(n, 10) }
	f.proof.Clock.Interval.Lower = ns(base.UnixNano() + 250_000_000)
	f.proof.Clock.Interval.Observed = ns(base.UnixNano() + 1_250_000_000)
	f.proof.Clock.Interval.Upper = ns(base.UnixNano() + 2_250_000_000)
	f.proof.Clock.Interval.HighWater = f.proof.Clock.Interval.Upper
	f.proof.Context.Now = fact(t, "2099-01-01T00:00:00Z")
	f.proof.Context.Watermark = f.proof.Context.Now
	a := acceptedAdmission(t, f)
	p := a.Projection()
	if p.IssuedAt != "2026-01-01T00:03:00Z" || p.Now != "2026-01-01T00:03:03Z" || p.Watermark != "2026-01-01T00:03:00Z" || a.Plan().Watermark != p.Now {
		t.Fatal("wrong interval projection")
	}
	u := use(f, a)
	u.Clock.PriorWatermark = a.Plan().Watermark
	wantCode(t, f, u, "accept")
	u.Clock.Interval.AuthorityUntil = u.Clock.Interval.Upper
	wantCode(t, f, u, "time_unavailable")
	u = use(f, a)
	u.Clock.PriorWatermark = a.Plan().Watermark
	u.Clock.DecisionFloor = u.Clock.Interval.Upper
	wantCode(t, f, u, "time_unavailable")
	// A malformed signed envelope must still win over invalid projected time.
	r := consumer(t, u).VerifyMessage([]byte("not-json"))
	if r.Code != "envelope_malformed" {
		t.Fatal(r.Code)
	}
}

// F's inherited inclusive age boundary can issue an already unusable receipt;
// receipt consumption retains its stricter exclusive boundary and 0..60 margin.
func TestMintAtInclusiveBoundaryCannotBeUsed(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	f.proof.Clock = clockAt(t, "2026-01-01T00:04:00Z", "2026-01-01T00:03:00Z")
	a := acceptedAdmission(t, f)
	u := use(f, a)
	u.Clock.PriorWatermark = a.Plan().Watermark
	wantCode(t, f, u, "freshness_receipt_expired")
	f = setup(t, "P-RC-attestation")
	a = acceptedAdmission(t, f)
	u = use(f, a)
	u.Context.FreshnessMarginSeconds = 60
	wantCode(t, f, u, "freshness_receipt_expired")
	u.Context.FreshnessMarginSeconds = 59
	wantCode(t, f, u, "accept")
	u.Context.FreshnessMarginSeconds = -1
	wantCode(t, f, u, "freshness_receipt_invalid")
}

// Setup cannot bypass admitted persistence with a raw original pointer, roll
// back the acknowledged watermark, or substitute another authentic admission.
func TestReceiptSetupCannotBypassAcknowledgement(t *testing.T) {
	f := setup(t, "P-RC-current")
	a, r := admit(t, f)
	u := use(f, nil)
	u.Context.RequireFreshnessReceipt = false
	u.Context.FreshnessReceipt = r.FreshnessReceipt
	wantCode(t, f, u, "freshness_receipt_unavailable")
	u = use(f, a)
	u.Clock.PriorWatermark = "2026-01-01T00:02:59Z"
	wantCode(t, f, u, "freshness_receipt_binding_mismatch")
	_, other := admit(t, f)
	u = use(f, a)
	u.Context.FreshnessReceipt = other.FreshnessReceipt
	wantCode(t, f, u, "freshness_receipt_binding_mismatch")
	wantCode(t, f, use(f, a), "accept")
}

// Submission preserves unavailable/invalid versus valid own-age behavior and
// never returns a positive state for unavailable or unqualified snapshot facts.
func TestSubmissionMissingFactsRefused(t *testing.T) {
	f := setup(t, "P-RC-current")
	row := object(snapshots(t, f.facts)[f.root])
	valid := c.Submission{Context: f.proof.Context, Clock: f.proof.Clock, Root: f.root, Snapshot: row["snapshot"].([]byte), Available: true, RollbackSafe: true, MaxAge: 60}
	for _, which := range []string{"unavailable", "rollback", "age", "missing", "oversize"} {
		s := valid
		switch which {
		case "unavailable":
			s.Available = false
		case "rollback":
			s.RollbackSafe = false
		case "age":
			s.MaxAge = 61
		case "missing":
			s.Snapshot = nil
		case "oversize":
			s.Snapshot = make([]byte, 1<<20+1)
		}
		state, e := c.CheckSubmittedSnapshot(s, nil, nil)
		if state != verifier.Unavailable {
			t.Fatal(which, state)
		}
		want := "accept" // Unavailable with nil error is the legacy availability refusal.
		if which == "age" || which == "oversize" {
			want = "key_binding_invalid"
		}
		if got := code(t, e); got != want {
			t.Fatalf("%s: want %s got %s", which, want, got)
		}

		state, e = c.CheckSubmittedSnapshot(valid, nil, nil)
		if state != verifier.NotRevoked || e != nil {
			t.Fatal(state, e)
		}
	}
}

// Invalid projected time cannot mint authority or mask earlier producer wire
// failure; the intact proof accepts on both explicit producer choices.
func TestProducerTimeAndWirePrecedence(t *testing.T) {
	for _, name := range []string{"P-RC-attestation", "P-RC-reanchor"} {
		f := setup(t, name)
		bad := f
		bad.proof.Clock.Interval.Boot = ""
		a, r := admit(t, bad)
		if a != nil || r.Code != "time_unavailable" || r.FreshnessReceipt != nil || !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
			t.Fatal(r.Code)
		}
		bad.proof.Chain = [][]byte{[]byte("not-json")}
		a, r = admit(t, bad)
		if a != nil || r.Code != "freshness_malformed" || r.FreshnessReceipt != nil || !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
			t.Fatal(r.Code)
		}
		acceptedAdmission(t, f)
	}
	f := setup(t, "P-RC-reanchor")
	for _, chain := range [][][]byte{nil, {f.proof.Chain[0], f.proof.Chain[0]}} {
		bad := f.proof
		bad.Chain = chain
		a, r := c.VerifyFreshnessReanchor(bad)
		if a != nil || r.Code != "freshness_malformed" || r.FreshnessReceipt != nil || !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
			t.Fatal(r.Code)
		}
	}
}
