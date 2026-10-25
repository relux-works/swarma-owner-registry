//go:build linux

package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Hash-valid unrelated blobs fence both recovery entry points before writes,
// preserving all evidence. A referenced bundle and normalized nil result reopen
// and retry successfully first, so rejecting every blob cannot pass.
func TestOrphanBlobFences(t *testing.T) {
	for _, mode := range []string{"open", "reconcile"} {
		t.Run(mode, func(t *testing.T) {
			s, root := setup(t)
			r := proposal()
			r.Proof.Bundle = []byte("referenced exact bundle")
			r.Result = nil
			out := mustCommit(t, s, r)
			expected := s.stateID
			s.Close()
			good, e := Open(root, expected)
			if e != nil {
				t.Fatal(e)
			}
			defer good.Close()
			if e = good.Reconcile(expected); e != nil {
				t.Fatal(e)
			}
			if retry := mustCommit(t, good, r); !reflect.DeepEqual(retry, out) {
				t.Fatal("referenced control lost exact outcome")
			}
			raw := []byte("unrelated hash-valid blob")
			if e = os.WriteFile(filepath.Join(root, "blobs", byteID(raw)+".blob"), raw, 0600); e != nil {
				t.Fatal(e)
			}
			before := tree(t, root)
			if mode == "open" {
				good.Close()
				bad, err := Open(root, expected)
				if bad != nil {
					bad.Close()
					t.Error("orphan blob admitted by Open")
				}
				code(t, err, StateUnavailable)
			} else {
				writes := 0
				good.disk.hooks.before = func(op, path string) error { writes++; return nil }
				code(t, good.Reconcile(expected), StateUnavailable)
				if writes != 0 {
					t.Fatal("orphan recovery crossed durability boundary")
				}
				_, e = good.View()
				code(t, e, StateUnavailable)
			}
			unchanged(t, root, before)
		})
	}
}

// An empty former complete-image root has neither a format marker nor blobs/;
// it must refuse unchanged, while an explicitly created current root reopens.
func TestLegacyEmptyRootFences(t *testing.T) {
	type oldRetained struct {
		Request Request
		Outcome Outcome
	}
	type oldImage struct {
		Sequence        uint64
		Head, Watermark string
		Files           map[string][]byte
		Requests        map[string]oldRetained
	}
	s, root := setup(t)
	expected := s.stateID
	s.Close()
	good, e := Open(root, expected)
	if e != nil {
		t.Fatal(e)
	}
	good.Close()
	old := oldImage{Files: map[string][]byte{}, Requests: map[string]oldRetained{}}
	if e = os.Remove(filepath.Join(root, "blobs")); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "anchor.json"), canonical(old), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "CURRENT"), []byte(digest(old)+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	before := tree(t, root)
	bad, e := Open(root, digest(old))
	if bad != nil {
		bad.Close()
		t.Error("empty legacy root misread as current format")
	}
	code(t, e, StateUnavailable)
	unchanged(t, root, before)
}

// Missing or unknown anchor markers fence even when CURRENT and the protected
// ID match those altered bytes. The current marker is checked on a valid root.
func TestAnchorFormatFences(t *testing.T) {
	for _, mode := range []string{"missing", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, root := setup(t)
			raw, e := os.ReadFile(filepath.Join(root, "anchor.json"))
			if e != nil {
				t.Fatal(e)
			}
			var anchor map[string]any
			if e = json.Unmarshal(raw, &anchor); e != nil {
				t.Fatal(e)
			}
			if anchor["Format"] != "delta-1" {
				t.Error("current anchor lacks explicit delta-1 marker")
			}
			s.Close()
			if mode == "missing" {
				delete(anchor, "Format")
			} else {
				anchor["Format"] = "unknown"
			}
			if e = os.WriteFile(filepath.Join(root, "anchor.json"), canonical(anchor), 0600); e != nil {
				t.Fatal(e)
			}
			expected := digest(anchor)
			if e = os.WriteFile(filepath.Join(root, "CURRENT"), []byte(expected+"\n"), 0600); e != nil {
				t.Fatal(e)
			}
			before := tree(t, root)
			bad, e := Open(root, expected)
			if bad != nil {
				bad.Close()
				t.Error("unsupported anchor format admitted")
			}
			code(t, e, StateUnavailable)
			unchanged(t, root, before)
		})
	}
}
