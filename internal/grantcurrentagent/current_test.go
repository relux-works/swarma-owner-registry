package grantcurrentagent

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// All successful controls below are synthetic typed fixtures. They authenticate
// no origin and qualify no protected provider. No exported constructor exists.
func syntheticReader() (*Reader, string) {
	// RFC 8032 public test vector only; no private key or key generation.
	public, _ := hex.DecodeString("d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a")
	key := "ed25519:" + base64.RawURLEncoding.EncodeToString(public)
	a := Agent{Key: key, MXID: "@fixture:example.invalid", UID: 1000, RegistrationNumber: 7,
		Generation: "generation-a", MasterEpoch: "epoch-a", Owner: "fixture-owner", Path: "fixture-agent", Active: true}
	r := &Reader{}
	s := &readState{
		installed: mapping{key: key, mxid: a.MXID, uid: a.UID, registration: a.RegistrationNumber},
		current:   position{lineage: "fixture-lineage", head: "fixture-head", binding: a},
		deadline:  time.Now().Add(time.Hour),
	}
	s.floor = s.current
	s.origin = &originAdmission{reader: r, state: s, current: s.current, floor: s.floor, deadline: s.deadline}
	s.disclosure = &readAdmission{reader: r, state: s, principal: "fixture-reader", key: key, owner: a.Owner}
	r.state = s
	return r, key
}

func denied(t *testing.T, r *Reader, key string) {
	t.Helper()
	got, err := r.CurrentAgent(key)
	if err != ErrUnavailable || got != (Agent{}) {
		t.Fatalf("refusal must disclose no binding: error=%v empty=%v", err, got == (Agent{}))
	}
}

func TestUnconfiguredReaderFailsClosed(t *testing.T) {
	fixture, key := syntheticReader()
	denied(t, nil, key)
	denied(t, &Reader{}, key)
	for _, key := range []string{"", "ed25519:", strings.Repeat("x", 8193), strings.Replace(key, "ed25519:", "rsa2048:", 1), key[:50] + "="} {
		denied(t, fixture, key)
	}
}

func TestSyntheticControlAndCausalRefusals(t *testing.T) {
	cases := []struct {
		name     string
		breakOne func(*readState)
	}{
		{"origin_missing", func(s *readState) { s.origin = nil }},
		{"origin_counterfeit_reader", func(s *readState) { s.origin.reader = &Reader{} }},
		{"origin_refenced_state", func(s *readState) { s.origin.state = &readState{} }},
		{"reader_not_authorized", func(s *readState) { s.disclosure = nil }},
		{"reader_counterfeit", func(s *readState) { s.disclosure.reader = &Reader{} }},
		{"reader_refenced", func(s *readState) { s.disclosure.state = &readState{} }},
		{"reader_unauthenticated", func(s *readState) { s.disclosure.principal = "" }},
		{"wrong_disclosure_domain", func(s *readState) { s.disclosure.owner = "other-owner" }},
		{"wrong_disclosure_subject", func(s *readState) { s.disclosure.key = "other-key" }},
		{"installed_key_changed", func(s *readState) { s.installed.key = "other-key" }},
		{"full_mxid_changed", func(s *readState) { s.installed.mxid += ".other" }},
		{"installed_uid_changed", func(s *readState) { s.installed.uid++ }},
		{"installed_registration_changed", func(s *readState) { s.installed.registration++ }},
		{"no_floor", func(s *readState) { s.floor = position{} }},
		{"stale_head", func(s *readState) { s.current.head = "older-head" }},
		{"omitted_transition", func(s *readState) { s.floor.head = "newer-head" }},
		{"conflicting_lineage", func(s *readState) { s.current.lineage = "other-lineage" }},
		{"same_head_binding_fork", func(s *readState) { s.current.binding.Generation = "generation-b" }},
		{"source_fork_above_floor", func(s *readState) { s.current.binding.Generation = "generation-b"; s.origin.current = s.current }},
		{"restored_active_after_retirement", func(s *readState) { s.floor.binding.Active = false }},
		{"inactive_binding_with_stale_origin", func(s *readState) { s.current.binding.Active = false; s.floor = s.current }},
		{"missing_original_budget", func(s *readState) { s.deadline = time.Time{} }},
		{"expired_budget_with_origin_mismatch", func(s *readState) { s.deadline = time.Now().Add(-time.Second) }},
		{"budget_extension_without_new_origin", func(s *readState) { s.deadline = s.deadline.Add(time.Hour) }},
		{"oversized_head", func(s *readState) { s.current.head = strings.Repeat("x", 256) }},
		{"oversized_reader", func(s *readState) { s.disclosure.principal = strings.Repeat("x", 256) }},
		{"invalid_utf8_owner", func(s *readState) { s.current.binding.Owner = "\xff" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, key := syntheticReader()
			original := r.state.current.binding
			saved, origin, disclosure := *r.state, *r.state.origin, *r.state.disclosure
			if got, err := r.CurrentAgent(key); err != nil || got != original {
				t.Fatal("synthetic unchanged control failed")
			}
			c.breakOne(r.state)
			denied(t, r, key)
			// Restore only the broken fixture input. Reader, key and original
			// operation deadline stay identical; no retry renews that deadline.
			*r.state = saved
			*r.state.origin, *r.state.disclosure = origin, disclosure
			if got, err := r.CurrentAgent(key); err != nil || got != original {
				t.Fatal("synthetic repaired control failed")
			}
		})
	}
}

func TestExpiredOriginalBudgetCausalRefusal(t *testing.T) {
	// Each fixture admits one original budget in both places. The expired
	// neighbor differs only in that budget; there is no handle/deadline mismatch
	// and no clock substitution or claim of qualified time.
	for _, expired := range []bool{false, true} {
		name, remaining := "valid_budget", time.Hour
		if expired {
			name, remaining = "expired_budget", -time.Hour
		}
		t.Run(name, func(t *testing.T) {
			r, key := syntheticReader()
			s := r.state
			originalBudget := time.Now().Add(remaining)
			s.deadline, s.origin.deadline = originalBudget, originalBudget
			before, origin, disclosure := *s, *s.origin, *s.disclosure
			if expired {
				denied(t, r, key)
			} else if got, err := r.CurrentAgent(key); err != nil || got != s.current.binding {
				t.Fatal("valid original budget control failed")
			}
			if r.state != s || *s != before || *s.origin != origin || *s.disclosure != disclosure {
				t.Fatal("read changed original budget or admitted premises")
			}
		})
	}
}

func TestInactiveBindingCausalRefusal(t *testing.T) {
	// Both immutable origin positions and both current/floor positions agree.
	// Only Active differs between these synthetic neighboring fixtures; the
	// installed mapping, disclosure and unexpired original budget remain valid.
	// This does not qualify authoritative retirement or history handling.
	originalBudget := time.Now().Add(time.Hour)
	for _, active := range []bool{true, false} {
		name := "inactive_binding"
		if active {
			name = "active_binding"
		}
		t.Run(name, func(t *testing.T) {
			r, key := syntheticReader()
			s := r.state
			s.deadline, s.origin.deadline = originalBudget, originalBudget
			s.current.binding.Active = active
			s.floor = s.current
			s.origin.current, s.origin.floor = s.current, s.floor
			before, origin, disclosure := *s, *s.origin, *s.disclosure
			if !active {
				denied(t, r, key)
			} else if got, err := r.CurrentAgent(key); err != nil || got != s.current.binding {
				t.Fatal("active binding control failed")
			}
			if r.state != s || *s != before || *s.origin != origin || *s.disclosure != disclosure {
				t.Fatal("read changed binding or admitted premises")
			}
		})
	}
}

func TestAllSixBindingMembersAndSenderMustMatchFloor(t *testing.T) {
	for _, field := range []string{"uid", "registration", "generation", "epoch", "owner", "path", "mxid", "key"} {
		t.Run(field, func(t *testing.T) {
			r, key := syntheticReader()
			a := &r.state.current.binding
			switch field {
			case "uid":
				a.UID++
			case "registration":
				a.RegistrationNumber++
			case "generation":
				a.Generation += "b"
			case "epoch":
				a.MasterEpoch += "b"
			case "owner":
				a.Owner += "b"
			case "path":
				a.Path += "b"
			case "mxid":
				a.MXID += ".other"
			case "key":
				other, _ := hex.DecodeString("3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c")
				a.Key = "ed25519:" + base64.RawURLEncoding.EncodeToString(other)
			}
			denied(t, r, key)
		})
	}
}

func TestNumericAndStringBoundaries(t *testing.T) {
	for _, uid := range []int64{-1, 0, 4294967294, 4294967295} {
		for _, reg := range []int64{0, 1, 9007199254740991, 9007199254740992} {
			r, key := syntheticReader()
			r.state.current.binding.UID, r.state.current.binding.RegistrationNumber = uid, reg
			r.state.floor = r.state.current
			r.state.origin.current, r.state.origin.floor = r.state.current, r.state.floor
			r.state.installed.uid, r.state.installed.registration = uid, reg
			got, err := r.CurrentAgent(key)
			valid := uid >= 0 && uid <= 4294967294 && reg >= 1 && reg <= 9007199254740991
			if valid && (err != nil || got != r.state.current.binding) {
				t.Fatal("inclusive numeric boundary refused")
			}
			if !valid && (err != ErrUnavailable || got != (Agent{})) {
				t.Fatal("out-of-range numeric boundary disclosed")
			}
		}
	}
	for _, length := range []int{0, 255, 256} {
		for _, field := range []string{"generation", "epoch", "owner", "path", "mxid"} {
			r, key := syntheticReader()
			value := strings.Repeat("x", length)
			a := &r.state.current.binding
			switch field {
			case "generation":
				a.Generation = value
			case "epoch":
				a.MasterEpoch = value
			case "owner":
				a.Owner = value
				r.state.disclosure.owner = value
			case "path":
				a.Path = value
			case "mxid":
				a.MXID = value
				r.state.installed.mxid = value
			}
			r.state.floor = r.state.current
			r.state.origin.current, r.state.origin.floor = r.state.current, r.state.floor
			got, err := r.CurrentAgent(key)
			if length == 255 && (err != nil || got != *a) {
				t.Fatal("255-byte component ceiling refused")
			}
			if length != 255 && (err != ErrUnavailable || got != (Agent{})) {
				t.Fatal("invalid component string boundary disclosed")
			}
		}
	}
	// The ceiling is UTF-8 bytes, not Unicode character count.
	r, key := syntheticReader()
	r.state.current.binding.Path = strings.Repeat("é", 128)
	r.state.floor = r.state.current
	r.state.origin.current, r.state.origin.floor = r.state.current, r.state.floor
	denied(t, r, key)
}

func TestRefenceRestartAndNoBudgetRenewal(t *testing.T) {
	r, key := syntheticReader()
	deadline := r.state.deadline
	for range 3 {
		if _, err := r.CurrentAgent(key); err != nil {
			t.Fatal(err)
		}
		if r.state.deadline != deadline {
			t.Fatal("read renewed original budget")
		}
	}
	copyOfState := *r.state
	r.state = &copyOfState
	denied(t, r, key) // identical data with old process-local handles is refenced
	restarted := &Reader{state: r.state}
	denied(t, restarted, key)
}

func TestTransitionRequiresExactReconciliation(t *testing.T) {
	r, key := syntheticReader()
	old := r.state.current.binding
	if got, err := r.CurrentAgent(key); err != nil || got != old {
		t.Fatal("control")
	}
	r.state.current.head = "replacement-head"
	r.state.current.binding.Generation = "generation-b"
	denied(t, r, key)
	// Synthetic test reconciliation only. Production cannot advance a floor.
	r.state.floor = r.state.current
	denied(t, r, key) // changing both rows cannot reuse the prior origin handle
	r.state.origin = &originAdmission{reader: r, state: r.state, current: r.state.current, floor: r.state.floor, deadline: r.state.deadline}
	got, err := r.CurrentAgent(key)
	if err != nil || got.Generation != "generation-b" || got == old {
		t.Fatal("replacement fixture")
	}
	r.state.current.binding = old // replaying the retired binding stays denied
	denied(t, r, key)
}

func TestContentionRefusesWithoutWaiting(t *testing.T) {
	r, key := syntheticReader()
	r.mu.Lock()
	defer r.mu.Unlock()
	denied(t, r, key)
}
