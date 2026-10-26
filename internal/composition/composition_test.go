package composition_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/relux-works/swarma-envelope/record"
	"github.com/relux-works/swarma-envelope/verifier"
	c "github.com/relux-works/swarma-owner-registry/internal/composition"
)

// Every changed binding refuses exactly, while the original admitted proof
// remains usable. Rebuilt identical whole Evidence is intentionally distinct.
func TestReceiptWrongBindingRefused(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	a := acceptedAdmission(t, f)
	tests := map[string]func(*c.Use){
		"root": func(u *c.Use) {
			m := object(f.facts["agent_revocation_root"])
			row := map[string]any{}
			for k, v := range m {
				row[k] = v
			}
			row["key"] = f.key
			u.Context.AgentRevocationRoot = fact(t, row)
		},
		"owner":    func(u *c.Use) { u.Context.RegistryOwner = fact(t, "owner-b") },
		"snapshot": func(u *c.Use) { u.Persisted.Anchor.Snapshot = record.Digest([]byte("other snapshot")) },
		"checkpoint": func(u *c.Use) {
			m := map[string]any{}
			for k, v := range object(f.facts["journal_witness"]) {
				m[k] = v
			}
			m["checkpoint_id"] = record.Digest([]byte("other checkpoint"))
			u.Context.JournalWitness = fact(t, m)
		},
		"tip": func(u *c.Use) { u.Persisted.Anchor.FreshnessID = record.Digest([]byte("other tip")) },
		"old-mint-anchor": func(u *c.Use) {
			u.Persisted.Anchor.FreshnessID = text(object(f.facts["freshness_anchor"])["freshness_id"])
		},
		"Evidence": func(u *c.Use) { u.Context.SignedSnapshots = fact(t, snapshots(t, f.facts)) },
		"read":     func(u *c.Use) { u.Context.WitnessReadAt = fact(t, "2026-01-01T00:02:59Z") },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			u := use(f, a)
			change(&u)
			wantCode(t, f, u, "freshness_receipt_binding_mismatch")
			wantCode(t, f, use(f, a), "accept")
		})
	}
}

// Equality at the exclusive fixed deadline (including a tightening margin)
// refuses; a use one second earlier accepts with the same original receipt.
func TestReceiptAtExpiryRefused(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	a := acceptedAdmission(t, f)
	for _, tt := range []struct {
		margin     int64
		before, at string
	}{{0, "2026-01-01T00:03:59Z", "2026-01-01T00:04:00Z"}, {1, "2026-01-01T00:03:58Z", "2026-01-01T00:03:59Z"}, {30, "2026-01-01T00:03:29Z", "2026-01-01T00:03:30Z"}} {
		u := use(f, a)
		u.Context.FreshnessMarginSeconds = tt.margin
		u.Clock = clockAt(t, tt.at, a.Plan().Watermark)
		wantCode(t, f, u, "freshness_receipt_expired")
		u.Clock = clockAt(t, tt.before, a.Plan().Watermark)
		wantCode(t, f, u, "accept")
	}
	audit, ok := a.Audit()
	if !ok {
		t.Fatal("expiry erased origin")
	}
	audit.Owner = "changed"
	again, ok := a.Audit()
	if !ok || again.Owner == audit.Owner {
		t.Fatal("mutable audit origin")
	}

}

// Updating authenticated read/time, with or without Require, cannot renew mint
// expiry. The newer read is valid inside the original lifetime.
func TestReceiptExtensionRefused(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	a := acceptedAdmission(t, f)
	for _, required := range []bool{false, true} {
		u := use(f, a)
		u.Context.RequireFreshnessReceipt = required
		u.Context.WitnessReadAt = fact(t, "2026-01-01T00:03:59Z")
		u.Clock = clockAt(t, "2026-01-01T00:04:00Z", a.Plan().Watermark)
		wantCode(t, f, u, "freshness_receipt_expired")
		u.Clock = clockAt(t, "2026-01-01T00:03:59Z", a.Plan().Watermark)
		wantCode(t, f, u, "accept")
	}
}

// Persisted acknowledgement/origin metadata alone supplies no runtime receipt
// after restart. A new full admission against retained facts restores use.
func TestReceiptAfterRestartRefused(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	a := acceptedAdmission(t, f)
	raw, e := json.Marshal(acknowledged(a))
	if e != nil {
		t.Fatal(e)
	}
	var restored c.PersistenceAcknowledgement
	if e = json.Unmarshal(raw, &restored); e != nil {
		t.Fatal(e)
	}
	u := use(f, nil)
	u.Persisted = &restored
	wantCode(t, f, u, "freshness_receipt_unavailable")
	fresh := acceptedAdmission(t, f)
	wantCode(t, f, use(f, fresh), "accept")
}

// An accepting legacy revocation check cannot produce an Admission; a zero
// admission cannot inject receipt authority. Only full F succeeds as issuer.
func TestNonFreshnessReceiptIssuerRefused(t *testing.T) {
	f := setup(t, "P-RC-current")
	u := use(f, nil)
	u.Context.RequireFreshnessReceipt = false
	state, e := consumer(t, u).CheckRevocation(f.root, nil, []string{f.key})
	if state != verifier.NotRevoked || e != nil {
		t.Fatal(state, e)
	}
	var empty c.Admission
	u = use(f, &empty)
	wantCode(t, f, u, "freshness_receipt_unavailable")
	wantCode(t, f, use(f, acceptedAdmission(t, f)), "accept")
}

// Required absence never authorizes an aged base via legacy verification.
func TestLegacyOldBaseCompositionRefused(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	wantCode(t, f, use(f, nil), "freshness_receipt_unavailable")
	u := use(f, nil)
	u.Context.RequireFreshnessReceipt = false
	wantCode(t, f, u, "key_binding_unavailable")
	wantCode(t, f, use(f, acceptedAdmission(t, f)), "accept")
}

// An old signed base is usable only with its original receipt and admitted
// persistence acknowledgement, on both normal and explicit re-anchor paths.
func TestFreshnessReceiptCompositionAccepted(t *testing.T) {
	for _, name := range []string{"P-RC-attestation", "P-RC-reanchor"} {
		t.Run(name, func(t *testing.T) {
			f := setup(t, name)
			a, r := admit(t, f)
			if a == nil || r.Code != "accept" || r.FreshnessReceipt == nil {
				t.Fatal(r.Code)
			}
			wantCode(t, f, use(f, a), "accept")
			audit, ok := a.Audit()
			if !ok || audit.RegistryID != f.root || audit.Reanchored != f.reanchor {
				t.Fatal(audit, ok)
			}
			plan := a.Plan()
			if audit.FreshnessID != a.Anchor().FreshnessID || audit.CheckpointID != a.Anchor().CheckpointID {
				t.Fatal("audit origin")
			}
			if f.reanchor && (plan.Reanchor == nil || plan.Reanchor.OldFreshnessID != audit.OldFreshnessID) {
				t.Fatal("recovery audit")
			}
		})
	}
}

// Deliberately optional absence retains the fresh-snapshot legacy control.
func TestCurrentSnapshotLegacyCompositionAccepted(t *testing.T) {
	f := setup(t, "P-RC-current")
	u := use(f, nil)
	u.Context.RequireFreshnessReceipt = false
	wantCode(t, f, u, "accept")
}

// Full F success alone does not change legacy own-age rules on an aged base.
func TestFreshnessCompositionBoundary(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	acceptedAdmission(t, f)
	u := use(f, nil)
	u.Context.RequireFreshnessReceipt = false
	wantCode(t, f, u, "key_binding_unavailable")
	f = setup(t, "P-RC-current")
	u = use(f, nil)
	u.Context.RequireFreshnessReceipt = false
	wantCode(t, f, u, "accept")
}

// Own-age would accept this fresh base, making fallback after a deliberate
// receipt/Evidence mismatch observable as an erroneous accepting identity.
func TestReceiptRefusalNeverRetriesLegacy(t *testing.T) {
	f := setup(t, "P-RC-current")
	a := acceptedAdmission(t, f)
	u := use(f, a)
	u.Context.RequireFreshnessReceipt = false
	u.Context.SignedSnapshots = fact(t, snapshots(t, f.facts))
	wantCode(t, f, u, "freshness_receipt_binding_mismatch")
	legacy := u
	legacy.Admission = nil
	legacy.Persisted = nil
	wantCode(t, f, legacy, "accept")
	wantCode(t, f, use(f, a), "accept")
}

// Missing, wrong and incompletely acknowledged durable state release no
// consumer. Detached plan/audit copies cannot mutate the admission token.
func TestPersistenceAcknowledgementRequired(t *testing.T) {
	for _, name := range []string{"P-RC-attestation", "P-RC-reanchor"} {
		f := setup(t, name)
		a := acceptedAdmission(t, f)
		tests := map[string]func(*c.Use){"absent": func(u *c.Use) { u.Persisted = nil }, "watermark": func(u *c.Use) { u.Persisted.Watermark = "2026-01-01T00:02:59Z" }, "floor": func(u *c.Use) { u.Persisted.DecisionFloor = "0" }, "boot": func(u *c.Use) { u.Persisted.Boot = "boot-b" }, "audit": func(u *c.Use) {
			if u.Persisted.Reanchor == nil {
				u.Persisted.Reanchor = &verifier.FreshnessReanchorPlan{Reanchored: true}
			} else {
				u.Persisted.Reanchor = nil
			}
		}}
		for n, change := range tests {
			t.Run(name+"/"+n, func(t *testing.T) {
				u := use(f, a)
				change(&u)
				want := "freshness_receipt_binding_mismatch"
				if n == "absent" {
					want = "freshness_receipt_unavailable"
				}
				wantCode(t, f, u, want)
				wantCode(t, f, use(f, a), "accept")
			})
		}
		p := a.Plan()
		p.IDs[0] = "changed"
		if p.Reanchor != nil {
			p.Reanchor.OldFreshnessID = "changed"
		}
		wantCode(t, f, use(f, a), "accept")
	}
}

// Refused F returns exactly its typed code and no plan, receipt or Admission;
// the unchanged full proof accepts. Normal admission never retries re-anchor.
func TestProducerRefusalHasNoAuthority(t *testing.T) {
	for _, name := range []string{"P-RC-attestation", "P-RC-reanchor"} {
		f := setup(t, name)
		bad := f
		bad.proof.IDs = []string{"invalid"}
		a, r := admit(t, bad)
		if a != nil || r.Code != "freshness_request_invalid" || r.FreshnessReceipt != nil || !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
			t.Fatalf("refusal: %s", r.Code)
		}
		acceptedAdmission(t, f)
	}
	f := setup(t, "P-RC-reanchor")
	a, r := c.VerifyFreshness(f.proof)
	if a != nil || r.Code != "freshness_gap" || r.FreshnessReceipt != nil || !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
		t.Fatalf("implicit recovery: %s", r.Code)
	}
}

// R1 presence/margin and R2 missing/malformed/binding keep exact ordered codes;
// earlier signed-envelope failure still wins over receipt and clock failures.
func TestReceiptRefusalOrder(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	a := acceptedAdmission(t, f)
	tests := []struct {
		name, want string
		change     func(*c.Use)
	}{
		{"required-absence", "freshness_receipt_unavailable", func(u *c.Use) { u.Admission = nil; u.Persisted = nil; u.Context.FreshnessMarginSeconds = -1 }},
		{"margin", "freshness_receipt_invalid", func(u *c.Use) { u.Context.FreshnessMarginSeconds = 61; u.Context.RegistryOwner = verifier.Evidence{} }},
		{"missing-before-syntax", "freshness_receipt_unavailable", func(u *c.Use) {
			u.Context.RegistryOwner = verifier.Evidence{}
			u.Context.WitnessReadAt = fact(t, "bad")
		}},
		{"malformed-read", "freshness_receipt_invalid", func(u *c.Use) { u.Context.WitnessReadAt = fact(t, "bad") }},
		{"future-read", "freshness_receipt_invalid", func(u *c.Use) { u.Context.WitnessReadAt = fact(t, "2026-01-01T00:03:01Z") }},
		{"invalid-clock", "time_unavailable", func(u *c.Use) { u.Clock.Interval.HighWater = "0" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := use(f, a)
			tt.change(&u)
			wantCode(t, f, u, tt.want)
			wantCode(t, f, use(f, a), "accept")
		})
	}
	u := use(f, a)
	u.Context.FreshnessMarginSeconds = 61
	v := consumer(t, u)
	r := v.VerifyMessage([]byte("not-json"))
	if r.Code != "envelope_malformed" || !reflect.DeepEqual(r.Plan, verifier.Plan{}) {
		t.Fatal(r.Code)
	}
	// Failed signed identity evidence precedes the invalid receipt margin.
	u = use(f, a)
	u.Context.FreshnessMarginSeconds = 61
	rows := map[string]any{}
	for id, raw := range object(f.facts["attestations"]) {
		signed := parsed(t, decode(t, raw))
		object(signed["signatures"].([]any)[0])["sig"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		b, e := record.Canonical(signed)
		if e != nil {
			t.Fatal(e)
		}
		rows[id] = b
	}
	u.Context.Attestations = fact(t, rows)
	wantCode(t, f, u, "key_binding_invalid")

}

// Submission checks use the exact submitted snapshot and their own age, never
// an old registry receipt, for registry/operator/grant roots. No intake is wired.
func TestSubmittedSnapshotUsesOwnAge(t *testing.T) {
	f := setup(t, "P-RC-attestation")
	a, r := admit(t, f)
	if a == nil || r.Code != "accept" {
		t.Fatal(r.Code)
	}
	for root, row := range snapshots(t, f.facts) {
		s := c.Submission{Context: f.proof.Context, Clock: f.proof.Clock, Root: root, Snapshot: row.(map[string]any)["snapshot"].([]byte), Available: true, RollbackSafe: true, MaxAge: 60}
		s.Context.FreshnessReceipt = r.FreshnessReceipt
		s.Context.RequireFreshnessReceipt = true
		state, e := c.CheckSubmittedSnapshot(s, nil, nil)
		wantState := verifier.NotRevoked
		if root == f.root {
			wantState = verifier.Unavailable
		}
		// Legacy age refusal uses Unavailable with nil error; fresh roots accept.
		if state != wantState || e != nil {
			t.Fatalf("submission want state %v, got %v %v", wantState, state, e)
		}

		s.Snapshot = []byte("not-json")
		state, e = c.CheckSubmittedSnapshot(s, nil, nil)
		if state != verifier.Unavailable || code(t, e) != "key_binding_invalid" {
			t.Fatal(state, e)
		}
	}
	current := setup(t, "P-RC-current")
	row := object(snapshots(t, current.facts)[current.root])
	s := c.Submission{Context: current.proof.Context, Clock: current.proof.Clock, Root: current.root, Snapshot: row["snapshot"].([]byte), Available: true, RollbackSafe: true, MaxAge: 60}
	state, e := c.CheckSubmittedSnapshot(s, nil, nil)
	if state != verifier.NotRevoked || e != nil {
		t.Fatal(state, e)
	}
}
