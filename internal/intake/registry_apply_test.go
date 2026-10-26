package intake_test

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/relux-works/swarma-envelope/verifier"
	"github.com/relux-works/swarma-owner-registry/internal/intake"
)

// Claim: while an admitted effect is in progress, both whole-premise replacement
// and full proof admission wait for it. This is a Go lock/race control, not a
// qualification of the caller's external effect or native durability.
func TestReviewerApplyExcludesUpdates(t *testing.T) {
	for _, kind := range []string{"snapshots", "proof"} {
		t.Run(kind, func(t *testing.T) {
			f := registrySetup(t)
			registryMint(t, &f)
			candidate := registryWant(t, f, "accept")
			entered, release := make(chan struct{}), make(chan struct{})
			applyDone := make(chan error, 1)
			var once sync.Once
			defer once.Do(func() { close(release) })
			go func() {
				applyDone <- f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error { return check(f.current) }, func(*intake.Accepted) error { close(entered); <-release; return nil })
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("effect did not start")
			}
			begun, done := make(chan struct{}), make(chan error, 1)
			go func() {
				close(begun)
				if kind == "snapshots" {
					done <- f.r.ReplaceSnapshots(f.s.facts.Snapshots)
				} else {
					_, result := f.r.VerifyFreshness(f.proof)
					if result.Code != "accept" {
						done <- errors.New(result.Code)
					} else {
						done <- nil
					}
				}
			}()
			<-begun
			select {
			case err := <-done:
				t.Fatalf("update completed during effect: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			once.Do(func() { close(release) })
			select {
			case err := <-applyDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Apply did not finish")
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("update remained blocked")
			}
		})
	}
}

// Claim: one Apply must not dispatch its effect twice, including when a boundary
// wrapper retries the supplied callback. The effect remains an inert counter.
func TestReviewerApplySingleDispatch(t *testing.T) {
	for _, retryError := range []bool{false, true} {
		name := "repeat-success"
		if retryError {
			name = "retry-effect-error"
		}
		t.Run(name, func(t *testing.T) {
			f := registrySetup(t)
			registryMint(t, &f)
			candidate := registryWant(t, f, "accept")
			calls := 0
			sentinel := errors.New("synthetic effect failure")
			err := f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error {
				_ = check(f.current)
				return check(f.current)
			}, func(*intake.Accepted) error {
				calls++
				if retryError {
					return sentinel
				}
				return nil
			})
			if calls != 1 {
				t.Fatalf("one Apply dispatched %d effects; returned %v", calls, err)
			}
			if retryError && err != sentinel {
				t.Fatalf("lost effect failure: %v", err)
			}
			if !retryError {
				requireApplyInvalid(t, err)
			}
		})
	}
}

// Claim: Apply must preserve its own checked outcome even if a boundary wrapper
// drops the callback error; no successful result may replace an effect failure.
func TestReviewerApplyPreservesEffectError(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	calls := 0
	sentinel := errors.New("synthetic effect failure")
	err := f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error { _ = check(f.current); return nil }, func(*intake.Accepted) error { calls++; return sentinel })
	if calls != 1 || err != sentinel {
		t.Fatalf("want one effect and exact failure, got calls=%d error=%v", calls, err)
	}
}

// Claim: a callback retained beyond Apply's boundary must not dispatch an effect
// after that boundary returns. Only a local counter is used as the effect.
func TestReviewerApplyRetiresCallback(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	var saved func(intake.RegistryCurrent) error
	calls := 0
	err := f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error { saved = check; return nil }, func(*intake.Accepted) error { calls++; return nil })
	if saved == nil {
		t.Fatal("callback not supplied")
	}
	later := saved(f.current)
	if calls != 0 || later == nil {
		t.Fatalf("retired callback dispatched: calls=%d Apply=%v callback=%v", calls, err, later)
	}
	requireApplyInvalid(t, err)
}

// Claim: retained whole Evidence is detached from source maps/byte slices;
// a failed later full proof removes old authority instead of falling back.
func TestReviewerRetainedAndFailedProof(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	for key, row := range f.s.facts.Snapshots {
		if len(row.Bytes) > 0 {
			row.Bytes[0] ^= 1
		}
		delete(f.s.facts.Snapshots, key)
	}
	registryWant(t, f, "accept")
	f.proof.Chain = [][]byte{[]byte("invalid")}
	a, result := f.r.VerifyFreshness(f.proof)
	if a != nil || result.Code == "accept" {
		t.Fatal("invalid full proof admitted")
	}
	registryWant(t, f, "freshness_receipt_unavailable")
}

// Claim: an oversized candidate must be refused before obtaining final-boundary
// facts. The probe exceeds the cap by one byte and performs no external work.
func TestReviewerApplyBudgetPreflight(t *testing.T) {
	f := registrySetup(t)
	called := false
	candidate := &intake.Accepted{Type: "attestation/1", Bytes: bytes.Repeat([]byte("x"), intake.MaxIdentityBytes+1)}
	err := f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error { called = true; return check(f.current) }, func(*intake.Accepted) error { t.Fatal("oversized effect"); return nil })
	var typed *intake.Error
	if !errors.As(err, &typed) || typed.Code != intake.RequestTooLarge {
		t.Fatalf("wrong cap refusal: %v", err)
	}
	if called {
		t.Fatal("boundary called before byte cap refusal")
	}
	registryMint(t, &f)
	valid := registryWant(t, f, "accept")
	calls := 0
	err = f.r.Apply(valid, func(check func(intake.RegistryCurrent) error) error { called = true; return check(f.current) }, func(*intake.Accepted) error { calls++; return nil })
	if err != nil || !called || calls != 1 {
		t.Fatalf("valid boundary/effect control refused: %v calls=%d", err, calls)
	}
}

func requireApplyInvalid(t *testing.T, err error) {
	t.Helper()
	var typed *intake.Error
	if !errors.As(err, &typed) || typed.Code != intake.RequestInvalid {
		t.Fatalf("want request_invalid, got %v", err)
	}
}

// Claim: a wrapper that never invokes the check cannot report successful
// admission. One completed synchronous invocation is the nearby valid control.
func TestReviewerApplyRequiresInvocation(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	calls := 0
	effect := func(*intake.Accepted) error { calls++; return nil }
	err := f.r.Apply(candidate, func(func(intake.RegistryCurrent) error) error { return nil }, effect)
	requireApplyInvalid(t, err)
	if calls != 0 {
		t.Fatal("effect without invocation")
	}
	err = f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error { return check(f.current) }, effect)
	if err != nil || calls != 1 {
		t.Fatal("valid invocation refused", err, calls)
	}
}

// Claim: dropping an admission error cannot turn refusal into success or call
// an effect. The same candidate with current witness is the valid neighbor.
func TestReviewerApplyPreservesAdmissionError(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	current := f.current
	current.JournalWitness = verifier.Evidence{}
	calls := 0
	effect := func(*intake.Accepted) error { calls++; return nil }
	err := f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error { _ = check(current); return nil }, effect)
	var typed *intake.Error
	if !errors.As(err, &typed) || string(typed.Code) != "freshness_receipt_unavailable" || calls != 0 {
		t.Fatalf("lost admission refusal: %v calls=%d", err, calls)
	}
	err = f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error { return check(f.current) }, effect)
	if err != nil || calls != 1 {
		t.Fatal("valid admission refused", err, calls)
	}
}

// Claim: even a callback that completed successfully is retired on Apply return;
// a saved callback cannot dispatch a second inert effect after that boundary.
func TestReviewerApplyRetiresCompletedCallback(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	var saved func(intake.RegistryCurrent) error
	calls := 0
	err := f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error { saved = check; return check(f.current) }, func(*intake.Accepted) error { calls++; return nil })
	if err != nil || calls != 1 {
		t.Fatal("valid synchronous control refused", err, calls)
	}
	requireApplyInvalid(t, saved(f.current))
	if calls != 1 {
		t.Fatal("effect dispatched after Apply returned")
	}
}

// Claim: a concurrent second invocation is refused without a second effect,
// even when the boundary suppresses its error. Only inert counters/channels are
// used; this does not qualify the caller's external exclusion.
func TestReviewerApplyConcurrentInvocationRefused(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	var calls atomic.Int32
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	first, second := make(chan error, 1), make(chan error, 1)
	var secondErr error
	duplicate := false
	err := f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error {
		go func() { first <- check(f.current) }()
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("first effect did not enter")
		}
		go func() { second <- check(f.current) }()
		select {
		case secondErr = <-second:
		case <-entered:
			duplicate = true
			once.Do(func() { close(release) })
			secondErr = <-second
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent invocation did not return")
		}
		once.Do(func() { close(release) })
		if firstErr := <-first; firstErr != nil {
			t.Fatal("first invocation refused", firstErr)
		}
		return nil // Suppress the second refusal deliberately.
	}, func(*intake.Accepted) error { calls.Add(1); entered <- struct{}{}; <-release; return nil })
	if duplicate || calls.Load() != 1 {
		t.Fatalf("concurrent callback dispatched %d effects", calls.Load())
	}
	requireApplyInvalid(t, secondErr)
	requireApplyInvalid(t, err)
}

// Claim: Apply retires the callback and waits for an already-running callback
// before returning a lifecycle refusal. It cannot cancel an effect already
// dispatched inside the boundary, but leaves no running callback after return.
func TestReviewerApplyWaitsForInFlightCallback(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	entered, release, boundaryReturned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	applyDone, callbackDone := make(chan error, 1), make(chan error, 1)
	go func() {
		applyDone <- f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error {
			go func() { callbackDone <- check(f.current) }()
			<-entered
			close(boundaryReturned)
			return nil
		}, func(*intake.Accepted) error { close(entered); <-release; return nil })
	}()
	select {
	case <-boundaryReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("boundary did not return")
	}
	select {
	case err := <-applyDone:
		t.Fatalf("Apply returned with a live callback: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	select {
	case err := <-applyDone:
		requireApplyInvalid(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Apply did not retire")
	}
	if err := <-callbackDone; err != nil {
		t.Fatal("already-dispatched effect result changed", err)
	}
}

// Claim: a wrapper recovering an effect panic cannot report success or leave
// the callback reusable. Panic recovery itself remains the wrapper's policy.
func TestReviewerApplyRecoveredCallbackPanicRefused(t *testing.T) {
	f := registrySetup(t)
	registryMint(t, &f)
	candidate := registryWant(t, f, "accept")
	var saved func(intake.RegistryCurrent) error
	calls := 0
	err := f.r.Apply(candidate, func(check func(intake.RegistryCurrent) error) error {
		saved = check
		defer func() { _ = recover() }()
		return check(f.current)
	}, func(*intake.Accepted) error { calls++; panic("synthetic effect panic") })
	requireApplyInvalid(t, err)
	requireApplyInvalid(t, saved(f.current))
	if calls != 1 {
		t.Fatal("recovered panic allowed retry", calls)
	}
}
