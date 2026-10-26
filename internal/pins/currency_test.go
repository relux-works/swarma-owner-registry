package pins_test

import (
	"github.com/relux-works/swarma-owner-registry/internal/contracts"
	"github.com/relux-works/swarma-owner-registry/internal/pins"
	"testing"
)

// Only an explicit protected 1 or 2 selects a profile. Omission retains legacy
// zero, malformed/future versions refuse, and no selection opens activation.
func TestCurrencyVersionProtectedSelection(t *testing.T) {
	for _, version := range []any{nil, int64(1), int64(2), int64(0), int64(3), "2", true} {
		d := validDocument()
		if version != nil {
			d["registry_currency_version"] = version
		}
		if version == int64(2) {
			rows := make([]any, 0)
			for _, kind := range contracts.RecordTypesForCurrency(2) {
				rows = append(rows, kind)
			}
			d["record_types"] = rows
		}
		c, e := pins.Parse(wire(t, d))
		valid := version == nil || version == int64(1) || version == int64(2)
		if !valid {
			if c != nil || e == nil || e.Error() != "profile_unsupported" {
				t.Fatal(version, c, e)
			}
			continue
		}
		if e != nil {
			t.Fatal(version, e)
		}
		ctx, e := c.VerifierContext()
		want := 0
		if version != nil {
			want = int(version.(int64))
		}
		if e != nil || ctx.RegistryCurrencyVersion != want {
			t.Fatal(version, ctx.RegistryCurrencyVersion, e)
		}
		if want == 2 && (c.CheckProfile("checkpoint/2", "agent") != nil || c.CheckProfile("checkpoint/1", "agent") == nil) {
			t.Fatal("profile downgrade")
		}
		for _, gate := range pins.Gates() {
			if c.IsOpen(gate) {
				t.Fatal("profile selection activated", gate)
			}
		}
	}
}
