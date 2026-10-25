package pins_test

import (
	"testing"

	"github.com/relux-works/swarma-owner-registry/internal/pins"
)

// The protected format is required and closed; neither omission, coercion nor
// another algorithm may supply authority. Both supported explicit pins parse.
func TestObjectFormatRequired(t *testing.T) {
	for _, value := range []any{nil, "", "sha512", "SHA1", " sha1", "sha256 ", true, int64(40), []any{"sha1"}, map[string]any{"name": "sha1"}} {
		d := validDocument()
		d["object_format"] = value
		refused(t, wire(t, d), pins.ConfigMalformed)
	}
	d := validDocument()
	delete(d, "object_format")
	refused(t, wire(t, d), pins.ConfigMalformed)
	for _, format := range []string{"sha1", "sha256"} {
		d := validDocument()
		d["object_format"] = format
		c := accepted(t, d)
		if c.ObjectFormat() != format {
			t.Fatal("protected format changed")
		}
	}
}

// An unavailable Config cannot supply a default Git profile.
func TestObjectFormatUnavailable(t *testing.T) {
	var absent *pins.Config
	for _, c := range []*pins.Config{absent, {}} {
		if c.ObjectFormat() != "" {
			t.Fatal("unavailable config supplied format")
		}
	}
	d := validDocument()
	d["object_format"] = "sha256"
	if accepted(t, d).ObjectFormat() != "sha256" {
		t.Fatal("valid control lost format")
	}
}
