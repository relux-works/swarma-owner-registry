package enrollment_test

import (
	"bytes"
	"reflect"
	"testing"

	admission "github.com/relux-works/swarma-owner-registry/internal/enrollment"
	"github.com/relux-works/swarma-registrar/registry"
)

func reverseEntries(p *admission.Projection) {
	for i, j := 0, len(p.Entries)-1; i < j; i, j = i+1, j-1 {
		p.Entries[i], p.Entries[j] = p.Entries[j], p.Entries[i]
	}
}

// R's path order makes enumeration irrelevant for same-revision reloads, even
// when the initial input or independently reconciled retained export is reversed.
// Exact bytes stay unchanged, the transaction resolves and history gains no copy.
func TestSameRevisionPermutedEntriesAccepted(t *testing.T) {
	for _, kind := range []string{"input", "initial-input", "retained"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, "E74")
			p := projection(t, f.Fixture.Registry.PreviousRevision, f.Fixture.Registry.Previous)
			if len(p.Entries) != 2 {
				t.Fatal("two-entry control required")
			}
			original := p.Clone()
			if kind == "initial-input" {
				reverseEntries(&p)
			}
			inputBefore := p.Clone()
			x := &transaction{}
			a, _ := adapter(t, f, p, admission.State{}, x)
			accepted(t, a, p)
			if !reflect.DeepEqual(inputBefore, p) {
				t.Fatal("load sorted caller-owned input")
			}
			if kind == "retained" {
				state := a.LastResolved()
				reverseEntries(&state.Exports[0])
				retainedBefore := state.Clone()
				x = &transaction{}
				a, _ = adapter(t, f, original, state, x)
				if !reflect.DeepEqual(state, retainedBefore) {
					t.Fatal("constructor sorted caller-owned retained state")
				}
			}
			accepted(t, a, original)
			reordered := original.Clone()
			reverseEntries(&reordered)
			if reflect.DeepEqual(original.Entries, reordered.Entries) {
				t.Fatal("permutation control unchanged")
			}
			before := len(x.calls)
			accepted(t, a, reordered)
			if len(x.calls) != before+4 {
				t.Fatal("permuted reload did not resolve")
			}
			state := a.LastResolved()
			if len(state.Exports) != 1 || !reflect.DeepEqual(state.Exports[0], original) {
				t.Fatal("same inventory duplicated or changed retained export")
			}
			if !reflect.DeepEqual(reordered.Records, original.Records) {
				t.Fatal("reload mutated mirror bytes")
			}
		})
	}
}

// Ignoring enumeration must not ignore changed envelope bytes or paths in the
// same revision. A duplicate path remains refused by the Loader prerequisites.
func TestSameRevisionInventoryChangeRefused(t *testing.T) {
	for _, kind := range []string{"bytes", "path", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, "E74")
			p := projection(t, f.Fixture.Registry.PreviousRevision, f.Fixture.Registry.Previous)
			a, _ := adapter(t, f, p, admission.State{}, &transaction{})
			accepted(t, a, p)
			state := a.LastResolved()
			unchanged, _ := adapter(t, f, p, state, &transaction{})
			accepted(t, unchanged, p)
			changed := p.Clone()
			switch kind {
			case "bytes":
				old := bytes.Clone(changed.Entries[0].Bytes)
				changed.Entries[0].Bytes = append(changed.Entries[0].Bytes, ' ')
				for id, b := range changed.Records {
					if bytes.Equal(b, old) {
						changed.Records[id] = bytes.Clone(changed.Entries[0].Bytes)
					}
				}
			case "path":
				changed.Entries[0].Path = "enrollments/z.json"
			case "duplicate":
				changed.Entries = append(changed.Entries, changed.Entries[0])
			}
			x := &transaction{}
			reloaded, _ := adapter(t, f, changed, state, x)
			refused(t, reloaded, changed, registry.RegistryUnavailable, x)
			if len(x.calls) != 0 {
				t.Fatal("changed same-revision inventory reached transaction")
			}
		})
	}
}
