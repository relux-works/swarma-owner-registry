//go:build linux

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func emptyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	return root
}
func bootstrapDisk(t *testing.T, root string) *disk {
	t.Helper()
	d, e := openDisk(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.close() })
	return d
}
func bootstrapEvents(t *testing.T) []boundary {
	t.Helper()
	d := bootstrapDisk(t, emptyRoot(t))
	var events []boundary
	guard := newDurability(t)
	d.hooks = hooks{before: guard.before, after: func(op, p string) error { guard.after(op, p); events = append(events, boundary{op, p}); return nil }}
	s, e := createDisk(d, DefaultMaxBytes)
	if e != nil {
		t.Fatal(e)
	}
	guard.resolved()
	s.Close()
	return events
}

// Every bootstrap mkdir/parent barrier, anchor and CURRENT boundary is failed
// before the operation and crashed after it with process/content+namespace
// models. Open never bootstraps a missing anchor or treats debris as genesis.
// Healthy Create at the same boundaries is the positive durability control.
func TestBootstrapFaultAndCrashMatrix(t *testing.T) {
	events := bootstrapEvents(t)
	t.Logf("bootstrap boundaries=%d faults=%d crashes=%d", len(events), len(events), 2*len(events))
	for i, event := range events {
		t.Run(fmt.Sprintf("fault/%02d-%s", i, event.Operation), func(t *testing.T) {
			root := emptyRoot(t)
			d := bootstrapDisk(t, root)
			seen := 0
			applied := false
			d.hooks.before = func(op, p string) error {
				if seen == i {
					applied = true
					return errors.New("bootstrap failure")
				}
				seen++
				return nil
			}
			s, e := createDisk(d, DefaultMaxBytes)
			if s != nil {
				s.Close()
				t.Fatal("failed Create returned store")
			}
			code(t, e, StateUnavailable)
			if !applied {
				t.Fatal("fault absent")
			}
			before := tree(t, root)
			opened, e := Open(root, digest(genesis()))
			if e != nil {
				code(t, e, StateUnavailable)
				unchanged(t, root, before)
			} else {
				v, e := opened.View()
				if e != nil || v.Sequence != 0 {
					t.Fatal("fault exposed invented state")
				}
				opened.Close()
			}
		})
		for _, power := range []bool{false, true} {
			t.Run(fmt.Sprintf("crash-%t/%02d-%s", power, i, event.Operation), func(t *testing.T) {
				root := emptyRoot(t)
				model := newPowerModel(t, root)
				d := bootstrapDisk(t, root)
				seen := 0
				guard := newDurability(t)
				d.hooks = hooks{before: guard.before, after: func(op, p string) error {
					guard.after(op, p)
					if op == "file.sync" {
						model.fileSync(p)
					}
					if op == "dir.sync" {
						model.sync(p)
					}
					if seen == i {
						panic(crashSignal{})
					}
					seen++
					return nil
				}}
				crashCall(t, func() { createDisk(d, DefaultMaxBytes) })
				d.hooks = hooks{}
				d.close()
				if power {
					model.cut()
				}
				before := tree(t, root)
				s, e := Open(root, digest(genesis()))
				if e != nil {
					code(t, e, StateUnavailable)
					unchanged(t, root, before)
					return
				}
				defer s.Close()
				v, e := s.View()
				if e != nil || v.Sequence != 0 || v.StateID != digest(genesis()) {
					t.Fatal("bootstrap recovered invented state")
				}
			})
		}
	}
}

func firstScenario(t *testing.T) (*Store, string, Request, View) {
	t.Helper()
	s, root := setup(t)
	r := proposal()
	v, e := s.Preview(r)
	if e != nil {
		t.Fatal(e)
	}
	return s, root, r, v
}
func newDirScenario(t *testing.T, kind string) (*Store, string, Request, View) {
	t.Helper()
	s, root := setup(t)
	r := nextProposal(mustCommit(t, s, proposal()))
	r.Records[0].Type = kind
	v, e := s.Preview(r)
	if e != nil {
		t.Fatal(e)
	}
	return s, root, r, v
}
func scenarioEvents(t *testing.T, prepare func(*testing.T) (*Store, string, Request, View)) []boundary {
	t.Helper()
	s, _, r, _ := prepare(t)
	var events []boundary
	guard := newDurability(t)
	s.disk.hooks = hooks{before: guard.before, after: func(op, p string) error { guard.after(op, p); events = append(events, boundary{op, p}); return nil }}
	mustCommit(t, s, r)
	guard.resolved()
	s.disk.hooks = hooks{}
	return events
}
func exerciseCommitBoundary(t *testing.T, prepare func(*testing.T) (*Store, string, Request, View), index int, power *bool) {
	t.Helper()
	s, root, r, expected := prepare(t)
	model := newPowerModel(t, root)
	seen := 0
	applied := false
	guard := newDurability(t)
	if power == nil {
		s.disk.hooks.before = func(op, p string) error {
			if seen == index {
				applied = true
				return errors.New("commit boundary failure")
			}
			seen++
			return nil
		}
		_, e := s.Commit(r)
		if !applied {
			t.Fatal("fault absent")
		}
		code(t, e, StateUnavailable)
		s.disk.hooks = hooks{}
		_, e = s.View()
		code(t, e, StateUnavailable)
		_, e = s.Commit(r)
		code(t, e, StateUnavailable)
		if e = s.Reconcile(expected.StateID); e != nil {
			code(t, e, StateUnavailable)
			return
		}
		assertExact(t, s, r, expected)
		return
	}
	s.disk.hooks = hooks{before: guard.before, after: func(op, p string) error {
		guard.after(op, p)
		if op == "file.sync" {
			model.fileSync(p)
		}
		if op == "dir.sync" {
			model.sync(p)
		}
		if seen == index {
			panic(crashSignal{})
		}
		seen++
		return nil
	}}
	crashCall(t, func() { s.Commit(r) })
	s.disk.hooks = hooks{}
	s.Close()
	if *power {
		model.cut()
	}
	before := tree(t, root)
	opened, e := Open(root, expected.StateID)
	if e != nil {
		code(t, e, StateUnavailable)
		unchanged(t, root, before)
		return
	}
	defer opened.Close()
	assertExact(t, opened, r, expected)
}

// First commit covers every blob/delta/file/publication boundary and its new
// grant directory. Each other compiled type exercises its mkdir, parent fsync
// and close independently; repeated file-write boundaries are covered once.
func TestFirstCommitAndNewDirectoryMatrix(t *testing.T) {
	first := scenarioEvents(t, firstScenario)
	t.Logf("first commit boundaries=%d faults=%d crashes=%d", len(first), len(first), 2*len(first))
	for i, event := range first {
		t.Run(fmt.Sprintf("first/fault/%02d-%s", i, event.Operation), func(t *testing.T) { exerciseCommitBoundary(t, firstScenario, i, nil) })
		for _, power := range []bool{false, true} {
			t.Run(fmt.Sprintf("first/crash-%t/%02d-%s", power, i, event.Operation), func(t *testing.T) { exerciseCommitBoundary(t, firstScenario, i, &power) })
		}
	}
	count := 0
	for _, kind := range []string{"attestation/1", "revocation/1", "enrollment/1", "journal-entry/1", "checkpoint/1"} {
		prepare := func(t *testing.T) (*Store, string, Request, View) { return newDirScenario(t, kind) }
		events := scenarioEvents(t, prepare)
		dir, _ := typeDir(kind)
		selected := 0
		for i, event := range events {
			if !(event.Operation == "dir.mkdir" && event.Path == "records/"+dir || (event.Operation == "dir.sync" || event.Operation == "dir.close") && event.Path == "records") {
				continue
			}
			selected++
			count++
			t.Run(fmt.Sprintf("%s/fault/%02d-%s", dir, i, event.Operation), func(t *testing.T) { exerciseCommitBoundary(t, prepare, i, nil) })
			for _, power := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/crash-%t/%02d-%s", dir, power, i, event.Operation), func(t *testing.T) { exerciseCommitBoundary(t, prepare, i, &power) })
			}
		}
		if selected != 3 {
			t.Fatalf("new directory seams missing: %s count=%d", kind, selected)
		}
	}
	t.Logf("other type-directory boundaries=%d faults=%d crashes=%d", count, count, 2*count)
}

// A durable name alone does not flush file contents, a file sync alone does
// not persist its name, and a child sync cannot persist an unsynced parent.
// These controls establish that the model actually distinguishes both axes.
func TestPowerModelIndependentContentAndNames(t *testing.T) {
	for _, mode := range []string{"name-only", "content-only", "both", "lost-parent"} {
		t.Run(mode, func(t *testing.T) {
			root := emptyRoot(t)
			model := newPowerModel(t, root)
			dir := "."
			if mode == "lost-parent" {
				dir = "child"
				if e := os.Mkdir(filepath.Join(root, dir), 0700); e != nil {
					t.Fatal(e)
				}
			}
			p := filepath.Join(dir, "item")
			if e := os.WriteFile(filepath.Join(root, p), []byte("exact"), 0600); e != nil {
				t.Fatal(e)
			}
			if mode != "name-only" {
				model.fileSync(p)
			}
			if mode != "content-only" {
				model.sync(dir)
			}
			model.cut()
			raw, e := os.ReadFile(filepath.Join(root, p))
			switch mode {
			case "name-only":
				if e != nil || len(raw) != 0 {
					t.Fatal("namespace sync invented content")
				}
			case "both":
				if e != nil || string(raw) != "exact" {
					t.Fatal("synced content/name lost")
				}
			default:
				if !os.IsNotExist(e) {
					t.Fatal("unsynced namespace survived")
				}
			}
		})
	}
}
