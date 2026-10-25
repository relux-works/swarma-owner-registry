package pins_test

import (
	"reflect"
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/pins"
)

const s5Revision = "5f4e7435d75697385ff6ce15a7501ac6f31c8ea5"

// The old S4 policy is refused with no config; the complete S5 control is accepted.
func TestS4PinRefusedAfterS5(t *testing.T) {
	d := validDocument()
	d["revision"] = "f76a400ebeb599b95a9bf9b2c59b522bef0392d8"
	refused(t, wire(t, d), pins.RevisionUnsupported)
	d["revision"] = s5Revision
	accepted(t, d)
}

// S5 retains closed defaults and static-only Context; explicit booleans affect
// exactly one declared gate. Unknown receipt gates and non-booleans cannot open it.
func TestPinnedS5ProfileAccepted(t *testing.T) {
	d := validDocument()
	d["revision"] = s5Revision
	c := accepted(t, d)
	for _, gate := range pins.Gates() {
		if c.IsOpen(gate) {
			t.Fatalf("default open: %s", gate)
		}
		codeIs(t, c.Require(gate), pins.GateClosed)
	}
	ctx, err := c.VerifierContext()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"FreshnessReceipt", "RequireFreshnessReceipt", "FreshnessMarginSeconds"} {
		field := reflect.ValueOf(ctx).FieldByName(name)
		if !field.IsValid() || !field.IsZero() {
			t.Fatalf("static Context supplies receipt input %s", name)
		}
	}
	for _, selected := range pins.Gates() {
		for _, enabled := range []bool{false, true} {
			d["activation"] = map[string]any{string(selected): enabled}
			c = accepted(t, d)
			for _, gate := range pins.Gates() {
				want := gate == selected && enabled
				if c.IsOpen(gate) != want {
					t.Fatalf("gate %s widened by %s", gate, selected)
				}
				if want {
					if err := c.Require(gate); err != nil {
						t.Fatal(err)
					}
				} else {
					codeIs(t, c.Require(gate), pins.GateClosed)
				}
			}
		}
		for _, invalid := range []any{"true", int64(1), nil, []any{}, map[string]any{}} {
			d["activation"] = map[string]any{string(selected): invalid}
			refused(t, wire(t, d), pins.ConfigMalformed)
		}
	}
	d["activation"] = map[string]any{"s5_receipt_composition": true}
	refused(t, wire(t, d), pins.ConfigMalformed)
}
