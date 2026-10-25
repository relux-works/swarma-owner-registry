package journal

import (
	"fmt"
	"testing"
)

// Probe the current journal failure boundary with direct and wrapped typed-nil
// signer errors. A real signer is the valid control; a panic is a test failure.
func TestPublicationTypedNilSignerError(t *testing.T) {
	c, p, _ := publicationFixture(t)
	_, valid := BuildPublication(c, fixtureSigner(t), p)
	accepted(t, valid)
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrapped=%t", wrapped), func(t *testing.T) {
			calls := 0
			s := signerFunc(func([]byte) ([]byte, error) {
				calls++
				var typed *Error
				var err error = typed
				if wrapped {
					err = fmt.Errorf("signing failed: %w", err)
				}
				return nil, err
			})
			defer func() {
				if v := recover(); v != nil {
					t.Errorf("BuildPublication panicked: %v", v)
				}
				if calls != 1 {
					t.Errorf("signer calls=%d, want 1", calls)
				}
			}()
			prepared, r := BuildPublication(c, s, p)
			assertPreparedEmpty(t, prepared)
			refusedResult(t, r, "signer_unavailable")
		})
	}
}
