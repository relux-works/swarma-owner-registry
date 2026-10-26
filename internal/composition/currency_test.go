package composition_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/relux-works/swarma-envelope/record"
	"github.com/relux-works/swarma-envelope/verifier"
	c "github.com/relux-works/swarma-owner-registry/internal/composition"
)

func consumeCurrency(t *testing.T, f fixture, u c.Use) string {
	t.Helper()
	v, e := c.New(u)
	if e != nil {
		return code(t, e)
	}
	in := object(object(f.input["consume"])["input"])
	switch object(f.input["consume"])["mode"] {
	case "grant":
		var chain [][]byte
		for _, raw := range in["chain"].([]any) {
			chain = append(chain, decode(t, raw))
		}
		r := v.Authorize(chain, fact(t, f.facts["request"]))
		if r.Code != "accept" && (!reflect.DeepEqual(r.Plan, verifier.Plan{}) || r.FreshnessReceipt != nil) {
			t.Fatal("refusal exposed authority", r)
		}
		return r.Code
	default:
		state, e := v.CheckRevocation(text(in["root"]), stringsOf(in["ids"]), stringsOf(in["keys"]))
		if e != nil && state != verifier.Unavailable {
			t.Fatal("state on refusal", state, e)
		}
		if e == nil && state != verifier.NotRevoked {
			return string(state)
		}
		return code(t, e)
	}
}
func checkCurrency(t *testing.T, name, want string) {
	t.Helper()
	f := setup(t, name)
	a := acceptedAdmission(t, f)
	u := use(f, a)
	u.Context.RequireFreshnessReceipt = f.input["require"] == true
	if got := consumeCurrency(t, f, u); got != want {
		t.Fatalf("%s: want %s got %s", name, want, got)
	}
}

// An optional /2 receipt covering only the registry cannot waive a stale grant
// root. The named fresh neighbor exercises the same ordinary own-age route.
func TestStaleGrantSnapshotWithRegistryReceiptRefused(t *testing.T) {
	checkCurrency(t, "N-CU-coverage-grant-optional", "revocation_unavailable")
}

// Missing grant coverage still permits a current grant's own-age check when
// receipt coverage is deliberately optional; required missing coverage refuses.
func TestCurrentGrantSnapshotWithRegistryReceiptAccepted(t *testing.T) {
	checkCurrency(t, "P-CU-optional-current-grant", "accept")
	checkCurrency(t, "N-CU-required-current-grant", "freshness_receipt_unavailable")
}

// Full F authenticates all journal positions and exact signed root originals;
// the covered aged grant then passes the actual grant-chain consumer.
func TestJournaledRootSnapshotWithRegistryReceiptAccepted(t *testing.T) {
	checkCurrency(t, "P-CU-receipt-grant", "accept")
}

// Omitting a journaled root add breaks the authenticated prefix, so no receipt,
// candidate Plan or consumer can be exposed; the intact neighbor succeeds.
func TestUnjournaledRootSnapshotWithRegistryReceiptRefused(t *testing.T) {
	f := setup(t, "P-CU-receipt-grant")
	acceptedAdmission(t, f)
	raw := object(f.facts["snapshot_journal"])
	entries := raw["entries"].([]any)
	values := make([]any, 0, len(entries)-1)
	for _, b := range entries[1:] {
		values = append(values, decode(t, b))
	}
	f.proof.Context.SnapshotJournal = fact(t, map[string]any{"anchor": raw["anchor"], "entries": values})
	a, r := admit(t, f)
	if a != nil || r.Code != "freshness_state_invalid" || !reflect.DeepEqual(r.Plan, verifier.Plan{}) || r.FreshnessReceipt != nil {
		t.Fatal(a, r)
	}
}

// Both original Evidence identities bind /2 receipts. Replacement, including
// byte-equivalent rebuilding, refuses globally even if coverage is optional.
func TestCurrencyOriginalPremisesAndForeignRootRefused(t *testing.T) {
	f := setup(t, "P-CU-check-grant")
	a := acceptedAdmission(t, f)
	for _, which := range []string{"snapshots", "journal", "root"} {
		u := use(f, a)
		u.Context.RequireFreshnessReceipt = false
		switch which {
		case "snapshots":
			u.Context.SignedSnapshots = fact(t, snapshots(t, f.facts))
		case "journal":
			j := object(f.facts["snapshot_journal"])
			entries := make([]any, 0)
			for _, raw := range j["entries"].([]any) {
				entries = append(entries, decode(t, raw))
			}
			u.Context.SnapshotJournal = fact(t, map[string]any{"anchor": j["anchor"], "entries": entries})
		case "root":
			u.Context.GrantRoot = fact(t, text(object(f.facts["agent_revocation_root"])["key"]))
		}
		want := "freshness_receipt_binding_mismatch"
		if which == "root" {
			want = "key_binding_invalid"
		}
		if got := consumeCurrency(t, f, u); got != want {
			t.Fatal(which, got)
		}
		if got := consumeCurrency(t, f, use(f, a)); got != "accept" {
			t.Fatal("positive control", got)
		}
	}
}

// Independent copies compare by exact content, including every new slice
// element and version. A changed acknowledgement exposes no Consumer/effect.
func TestReanchorSliceContentAcknowledgement(t *testing.T) {
	f := setup(t, "P-CU-reanchor-receipt")
	a := acceptedAdmission(t, f)
	u := use(f, a)
	u.Persisted.Reanchor = a.Plan().Reanchor
	if _, e := c.New(u); e != nil {
		t.Fatal("equal independent content", e)
	}
	for _, change := range []func(*verifier.FreshnessReanchorPlan){
		func(p *verifier.FreshnessReanchorPlan) { p.OldSnapshots[0].SnapshotID = record.Digest([]byte("other")) },
		func(p *verifier.FreshnessReanchorPlan) { p.NewSnapshots[0].RootKeyID = "changed" },
		func(p *verifier.FreshnessReanchorPlan) { p.OldRegistryCurrencyVersion++ },
		func(p *verifier.FreshnessReanchorPlan) { p.NewRegistryCurrencyVersion++ },
	} {
		u = use(f, a)
		change(u.Persisted.Reanchor)
		v, e := c.New(u)
		if v != nil || code(t, e) != "freshness_receipt_binding_mismatch" {
			t.Fatalf("changed audit exposed consumer: consumer=%t code=%s", v != nil, code(t, e))
		}
	}
}

// Mutating either the original verifier result or a returned Plan/Anchor cannot
// alter any held /2 slice, nested SnapshotID pointer, inventory, or audit binding.
func TestCurrencyAdmissionCopiesDoNotAlias(t *testing.T) {
	f := setup(t, "P-CU-reanchor-receipt")
	a, r := admit(t, f)
	if r.Code != "accept" {
		t.Fatal(r.Code)
	}
	expected, err := json.Marshal(a.Plan())
	if err != nil {
		t.Fatal(err)
	}
	originalAnchor := a.Anchor()
	mutate := func(p verifier.Plan) {
		p.Reanchor.OldSnapshots[0].SnapshotID = "changed"
		p.Reanchor.NewSnapshots[0].SnapshotID = "changed"
		*p.SnapshotJournal.Latest[0].SnapshotID = "changed"
		p.IDs[0] = "changed"
	}
	mutate(r.Plan)
	mutate(a.Plan())
	anchor := a.Anchor()
	anchor.Snapshots[0].SnapshotID = "changed"
	audit, _ := a.Audit()
	audit.Snapshots[0].SnapshotID = "changed"
	after, err := json.Marshal(a.Plan())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, after) || !reflect.DeepEqual(originalAnchor, a.Anchor()) {
		t.Fatal("retained plan alias")
	}
	if _, e := c.New(use(f, a)); e != nil {
		t.Fatal("original binding altered", e)
	}
}

// Missing external coverage refuses even though that exact root's own-age is
// current. The optional neighbor succeeds, detecting accidental policy downgrade.
func TestRequiredCurrencyCoverageCannotUseOwnAge(t *testing.T) {
	f := setup(t, "P-CU-optional-current-grant")
	a := acceptedAdmission(t, f)
	u := use(f, a)
	u.Context.RequireFreshnessReceipt = false
	v, e := c.NewRequired(u)
	if e != nil {
		t.Fatal(e)
	}
	in := object(object(f.input["consume"])["input"])
	state, e := v.CheckRevocation(text(in["root"]), stringsOf(in["ids"]), stringsOf(in["keys"]))
	if state != verifier.Unavailable || code(t, e) != "freshness_receipt_unavailable" {
		t.Fatal("missing required coverage", state, e)
	}
	if got := consumeCurrency(t, f, u); got != "accept" {
		t.Fatal("current optional neighbor", got)
	}
}

// Omitting a configured, unselected operator row is not qualified absence. Full
// F rejects the incomplete index; the complete three-role prefix is accepted.
func TestCurrencyCompleteIndexCannotInferAbsence(t *testing.T) {
	f := setup(t, "P-CU-receipt-grant")
	acceptedAdmission(t, f)
	j := object(f.facts["snapshot_journal"])
	anchor := object(j["anchor"])
	rows := anchor["latest"].([]any)
	kept := make([]any, 0)
	for _, r := range rows {
		if object(r)["root_role"] != "operator" {
			kept = append(kept, r)
		}
	}
	anchor["latest"] = kept
	entries := make([]any, 0)
	for _, r := range j["entries"].([]any) {
		entries = append(entries, decode(t, r))
	}
	f.proof.Context.SnapshotJournal = fact(t, map[string]any{"anchor": anchor, "entries": entries})
	a, r := admit(t, f)
	if a != nil || r.Code != "freshness_state_invalid" || !reflect.DeepEqual(r.Plan, verifier.Plan{}) || r.FreshnessReceipt != nil {
		t.Fatal(a, r)
	}
}

// A later authenticated read never extends a /2 receipt's fixed exclusive E.
// The same retained two-premise receipt remains valid one second before E.
func TestCurrencyReceiptExpiryDoesNotRenew(t *testing.T) {
	f := setup(t, "P-CU-check-grant")
	a := acceptedAdmission(t, f)
	u := use(f, a)
	u.Context.WitnessReadAt = fact(t, "2026-01-01T00:01:59Z")
	u.Clock = clockAt(t, "2026-01-01T00:02:00Z", a.Plan().Watermark)
	if got := consumeCurrency(t, f, u); got != "freshness_receipt_expired" {
		t.Fatal(got)
	}
	u.Clock = clockAt(t, "2026-01-01T00:01:59Z", a.Plan().Watermark)
	if got := consumeCurrency(t, f, u); got != "accept" {
		t.Fatal(got)
	}
}

// A valid /2 receipt authorizes its aged covered root, but cannot authenticate
// newly submitted bytes through that old authority. The explicit submission
// path checks exact own-age bytes and leaves the original admission usable.
func TestCurrencySubmittedSnapshotUsesExactOwnAge(t *testing.T) {
	f := setup(t, "P-CU-check-grant")
	a, r := admit(t, f)
	if r.Code != "accept" {
		t.Fatal(r.Code)
	}
	root := text(object(object(f.input["consume"])["input"])["root"])
	ctx := f.proof.Context
	ctx.FreshnessReceipt = r.FreshnessReceipt
	ctx.RequireFreshnessReceipt = true
	old := object(snapshots(t, f.facts)[root])["snapshot"].([]byte)
	in := c.Submission{Context: ctx, Clock: f.proof.Clock, Root: root, Snapshot: old, Available: true, RollbackSafe: true, MaxAge: 60}
	state, e := c.CheckSubmittedSnapshot(in, nil, nil)
	if state != verifier.Unavailable || e != nil {
		t.Fatal("aged submission", state, e)
	}
	fresh := setup(t, "P-CU-optional-current-grant")
	in.Snapshot = object(snapshots(t, fresh.facts)[root])["snapshot"].([]byte)
	state, e = c.CheckSubmittedSnapshot(in, nil, nil)
	if state != verifier.NotRevoked || e != nil {
		t.Fatal("fresh exact submission", state, e)
	}
	if got := consumeCurrency(t, f, use(f, a)); got != "accept" {
		t.Fatal("submission mutated original premise", got)
	}
}

// Explicit 0/1 preserve the S5 receipt-backed registry-only path and legacy
// candidate shape; selecting /2 never silently accepts those same /1 bytes.
func TestExplicitLegacyCurrencyProfilesKeepS5Behavior(t *testing.T) {
	for _, version := range []int{0, 1} {
		f := setup(t, "P-RC-attestation")
		f.proof.Context.RegistryCurrencyVersion = version
		a := acceptedAdmission(t, f)
		if a.Plan().SnapshotJournal != nil || a.Anchor().RegistryCurrencyVersion != 0 {
			t.Fatal("legacy shape changed")
		}
		wantCode(t, f, use(f, a), "accept")
	}
	f := setup(t, "P-RC-attestation")
	f.proof.Context.RegistryCurrencyVersion = 2
	a, r := admit(t, f)
	if a != nil || r.Code != "registry_currency_version_mismatch" || !reflect.DeepEqual(r.Plan, verifier.Plan{}) || r.FreshnessReceipt != nil {
		t.Fatal(a, r)
	}
}
