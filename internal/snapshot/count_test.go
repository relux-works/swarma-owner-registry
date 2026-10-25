package snapshot

import (
	"strings"
	"testing"

	"github.com/relux-works/waggle/record"
)

// Capacity classification is confined to the root body's entries array,
// including escaped member names. Unrelated arrays retain malformed refusal;
// quoted delimiters cannot fake entries, and depth/syntax stay bounded.
func TestEntryCountPreflightBounds(t *testing.T) {
	array := func(n int) string { return "[" + strings.Repeat("0,", n-1) + "0]" }
	for _, tc := range []struct {
		name, raw string
		code      Code
	}{
		{"64", `{"body":{"entries":` + array(64) + `}}`, ""},
		{"65", `{"body":{"entries":` + array(65) + `}}`, CapacityExhausted},
		{"escaped", `{"b\u006fdy":{"entr\u0069es":` + array(65) + `}}`, CapacityExhausted},
		{"quoted", `{"body":{"entries":["comma, and brackets [] and quote \\\" are data"]}}`, ""},
		{"unrelated-array", `{"body":{"signatures":` + array(65) + `}}`, Malformed},
		{"nested-decoy", `{"other":{"body":{"entries":` + array(65) + `}}}`, Malformed},
		{"depth", strings.Repeat("[", 17) + strings.Repeat("]", 17), Malformed},
		{"syntax", `{"body":{"entries":[0,]}}`, Malformed},
		{"trailing", `{"body":{"entries":[]}} {}`, Malformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := checkEntryCount([]byte(tc.raw))
			if tc.code == "" {
				if e != nil {
					t.Fatal(e)
				}
			} else {
				errorCode(t, e, tc.code)
			}
		})
	}
	// Classification does not relax the shared parser's independent array cap.
	if _, e := record.Parse([]byte(`{"body":{"entries":`+array(65)+`}}`), MaxBytes, 16); e == nil {
		t.Fatal("shared parser array bound relaxed")
	}
}
