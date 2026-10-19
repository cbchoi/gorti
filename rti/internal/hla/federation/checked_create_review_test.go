package federation

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func checkedReviewManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Options{Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: snapFakeFOMs{}, GenerationEpoch: 20, GenerationSpan: 8})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func checkedReviewResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("operation deadlocked during checked creation")
		return nil
	}
}

func TestCheckedCreateReviewJoinWaiterCannotJoinFailedExecution(t *testing.T) {
	for iteration := 0; iteration < 20; iteration++ {
		m := checkedReviewManager(t)
		ctx := context.Background()
		entered, fail := make(chan uint64, 1), make(chan struct{})
		createDone := make(chan error, 1)
		failure := errors.New("durable initialization failed")
		go func() {
			createDone <- m.CreateFederationWithCheckedSetup(ctx, core.CreateFederationRequest{Name: "f"}, func(g uint64) error {
				// These are the read paths used by InitializeSaveHistory under setup.
				if current, ok := m.GenerationFor("f"); !ok || current != g {
					return errors.New("setup cannot resolve its pinned generation")
				}
				entered <- g
				<-fail
				return failure
			})
		}()
		var failedGeneration uint64
		select {
		case failedGeneration = <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("setup did not start")
		}
		m.mu.RLock()
		failedState := m.federations["f"]
		m.mu.RUnlock()
		var setupCalls atomic.Int32
		joinDone := make(chan error, 1)
		go func() {
			h, err := m.JoinFederationWithSetup(ctx, core.JoinFederationRequest{Federation: "f", FederateName: "waiter", ExpectedGeneration: &failedGeneration}, func(core.MemberIdentity) { setupCalls.Add(1) })
			if h != 0 {
				err = errors.New("join leaked a handle from failed execution")
			}
			joinDone <- err
		}()
		// The waiter owns this old state's lifecycle lock before waiting on the
		// create-held operation gate. Do not rely on a scheduling sleep.
		deadline := time.Now().Add(3 * time.Second) //nolint:forbidigo // Wall-clock deadline detects a stalled concurrent lock acquisition.
		for failedState.lifecycle.TryLock() {
			failedState.lifecycle.Unlock()
			if time.Now().After(deadline) { //nolint:forbidigo // Must use the same real clock as the concurrency deadline above.
				close(fail)
				t.Fatal("join never reached the create-held gate")
			}
			runtime.Gosched()
		}
		select {
		case err := <-joinDone:
			close(fail)
			t.Fatalf("join escaped pending setup: %v", err)
		default:
		}
		close(fail)
		if err := checkedReviewResult(t, createDone); !errors.Is(err, failure) {
			t.Fatalf("create: %v", err)
		}
		// Race a replacement execution against the already parked old waiter.
		if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "f"}); err != nil {
			t.Fatal(err)
		}
		if err := checkedReviewResult(t, joinDone); !errors.Is(err, core.ErrFederationNotFound) {
			t.Fatalf("old waiter joined replacement: %v", err)
		}
		if setupCalls.Load() != 0 {
			t.Fatal("failed join published session state")
		}
		if members := m.MembersOf("f"); len(members) != 0 {
			t.Fatalf("waiter mutated new execution: %v", members)
		}
		if next, ok := m.GenerationFor("f"); !ok || next != failedGeneration+1 {
			t.Fatalf("generation recycled: %d -> %d", failedGeneration, next)
		}
		failedState.mu.RLock()
		mutated := len(failedState.handleToName) != 0 || failedState.nextFederateHandle != 0
		failedState.mu.RUnlock()
		if mutated {
			t.Fatal("failed execution's orphan state was mutated")
		}
	}
}

func TestCheckedCreateReviewGenerationWaiterAndFailedPreparationIsolation(t *testing.T) {
	m := checkedReviewManager(t)
	ctx := context.Background()
	entered, proceed := make(chan uint64, 1), make(chan struct{})
	created := make(chan error, 1)
	go func() {
		created <- m.CreateFederationWithCheckedSetup(ctx, core.CreateFederationRequest{Name: "f"}, func(g uint64) error { entered <- g; <-proceed; return errors.New("store failure") })
	}()
	var generation uint64
	select {
	case generation = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("setup deadlocked")
	}
	leased := make(chan error, 1)
	go func() {
		release, err := m.AcquireGeneration("f", generation)
		if release != nil {
			release()
			leased <- errors.New("failed creation obtained a valid generation lease")
			return
		}
		leased <- err
	}()
	select {
	case err := <-leased:
		close(proceed)
		t.Fatalf("generation lease passed pending setup: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	// Unrelated creation must not be held behind another federation's I/O.
	if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "other"}); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	if err := checkedReviewResult(t, created); err == nil {
		t.Fatal("setup error hidden")
	}
	if err := checkedReviewResult(t, leased); !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatalf("lease: %v", err)
	}
	if _, ok := m.GenerationFor("f"); ok {
		t.Fatal("failed execution retained")
	}
	if _, ok := m.GenerationFor("other"); !ok {
		t.Fatal("failed setup damaged another execution")
	}
	if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "f"}); err != nil {
		t.Fatal(err)
	}
	if release, err := m.AcquireGeneration("f", generation); !errors.Is(err, core.ErrFederationGenerationMismatch) {
		if release != nil {
			release()
		}
		t.Fatalf("stale generation authorized: %v", err)
	}
}

type checkedReviewFOMRepository struct {
	snapFakeFOMs
	loads int
	model *snapFakeFOM
}

func (r *checkedReviewFOMRepository) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	r.loads++
	return r.model, nil
}

func TestCheckedCreateReviewFOMAccessorUsesValidatedHandleWithoutLeaseUpgrade(t *testing.T) {
	repository := &checkedReviewFOMRepository{model: &snapFakeFOM{}}
	m, err := New(Options{Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: repository})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- m.CreateFederationWithCheckedSetup(context.Background(), core.CreateFederationRequest{Name: "f"}, func(uint64) error {
			model, err := m.FOMFor("f")
			if err != nil {
				return err
			}
			if model != repository.model {
				return errors.New("FOM accessor replaced validated model")
			}
			return nil
		})
	}()
	if err := checkedReviewResult(t, result); err != nil {
		t.Fatal(err)
	}
	if repository.loads != 1 {
		t.Fatalf("validated model reparsed %d times", repository.loads)
	}
}
