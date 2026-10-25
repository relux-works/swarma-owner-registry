package enrollment_test

import (
	"testing"

	admission "github.com/relux-works/swarma-owner-registry/internal/enrollment"
	"github.com/relux-works/swarma-registrar/registry"
)

// The published cross-format vectors refuse without any transaction, paired
// with each format's published positive. The pin comes from protected config,
// not the supplied revision's width. Successful loads also exercise reloads.
func TestEnrollmentObjectFormat(t *testing.T) {
	for _, tc := range []struct {
		name, fixture, format string
		accept                bool
	}{
		{"sha1-cross-format", "E115", "sha1", false},
		{"sha256-cross-format", "E121", "sha256", false},
		{"sha1-exact-pin", "PE16", "sha1", true},
		{"sha256-exact-pin", "PE17", "sha256", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture(t, tc.fixture)
			if f.Fixture.Registry.ObjectFormat != tc.format {
				t.Fatal("fixture must explicitly name its protected format")
			}
			p := current(t, f)
			x := &transaction{}
			a, _ := adapter(t, f, p, admission.State{}, x)
			if !tc.accept {
				refused(t, a, p, registry.RegistryUnavailable, x)
				if len(x.calls) != 0 {
					t.Fatal("cross-format revision reached transaction")
				}
				return
			}
			accepted(t, a, p)
			accepted(t, a, p)
			if len(x.calls) != 8 || len(a.LastResolved().Exports) != 1 {
				t.Fatal("exact-format loads did not resolve once each")
			}
		})
	}
}
