package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

func TestSynchronizationRetryCannotRecreateReceiptDuringResign(t *testing.T) {
	ctx := context.Background()
	var secondJoined atomic.Bool
	secondJoined.Store(true)
	var secondReceipts atomic.Int32
	validate := func(_ context.Context, _ core.FederationName, h core.FederateHandle) error {
		if h == 2 && !secondJoined.Load() {
			return core.ErrFederateNotJoined
		}
		return nil
	}
	receipts, err := grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{
		Authenticate:              validate,
		ValidateRequiredRecipient: validate,
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return nil
		},
		Pending: func(_ core.FederationName, h core.FederateHandle, delta int) {
			if h == 2 {
				secondReceipts.Add(int32(delta))
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := &syncIntegrationOutbox{}
	out := grpcadapter.NewMandatoryCallbackOutbox(raw, func() *grpcadapter.CallbackReceipts { return receipts })
	manager, err := syncpkg.New(syncpkg.Options{RequireInvocations: true, Outbox: out, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2} }})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterBy(ctx, "f", 1, "p", nil, nil); err != nil {
		t.Fatal(err)
	}
	// Rebuild uninvoked obligations as pending, just like a strict restore.
	commit, err := manager.PrepareImport("f", manager.ExportState("f"))
	if err != nil {
		t.Fatal(err)
	}
	commit()
	receipts.ClearFederate("f", 1)
	receipts.ClearFederate("f", 2)
	if secondReceipts.Load() != 0 {
		t.Fatal("clear fixture retained a receipt")
	}
	lifecycle := newSynchronizationLifecycle(manager, nil)
	var generationGate sync.RWMutex
	generationGate.Lock()
	writerHeld := true
	defer func() {
		if writerHeld {
			generationGate.Unlock()
		}
	}()
	attempted := make(chan struct{})
	var releases atomic.Int32
	lifecycle.retryAdmission = func(context.Context, core.FederationName) (func(), error) {
		close(attempted)
		generationGate.RLock()
		return func() { releases.Add(1); generationGate.RUnlock() }, nil
	}
	done := make(chan struct{})
	go func() { defer close(done); lifecycle.Retry(ctx) }()
	select {
	case <-attempted:
	case <-done:
		t.Fatalf("retry bypassed generation admission and recreated %d receipt(s) after ClearFederate", secondReceipts.Load())
	case <-time.After(2 * time.Second):
		t.Fatal("retry never reached admission")
	}
	// The FM writer still owns its lease. Its member hook must not wait on the
	// retry operation mutex while that retry is waiting for this same writer.
	hookDone := make(chan error, 1)
	go func() { hookDone <- lifecycle.Resigned(ctx, "f", 2) }()
	select {
	case err := <-hookDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		generationGate.Unlock()
		writerHeld = false
		<-done
		<-hookDone
		t.Fatal("retry held lifecycle operation lock before acquiring generation admission")
	}
	secondJoined.Store(false)
	generationGate.Unlock()
	writerHeld = false
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("admitted retry did not finish")
	}
	if secondReceipts.Load() != 0 || releases.Load() != 1 {
		t.Fatalf("orphan receipt=%d, admission releases=%d", secondReceipts.Load(), releases.Load())
	}
}

type synchronizationAdmissionRecorder struct {
	joined, resigned, retries int
	pinned                    *bool
}

func (m *synchronizationAdmissionRecorder) FederateJoined(context.Context, core.FederationName, core.FederateHandle) error {
	m.joined++
	return nil
}
func (m *synchronizationAdmissionRecorder) FederateResigned(context.Context, core.FederationName, core.FederateHandle) error {
	m.resigned++
	return nil
}
func (m *synchronizationAdmissionRecorder) RetryPending(context.Context, core.FederationName) error {
	if m.pinned != nil && !*m.pinned {
		return errors.New("retry is outside generation admission")
	}
	m.retries++
	return nil
}
func (*synchronizationAdmissionRecorder) PendingFederations() []core.FederationName {
	return []core.FederationName{"f"}
}

func TestSynchronizationRetryAdmissionFailureAndMemberHookIsolation(t *testing.T) {
	ctx := context.Background()
	pinned := false
	manager := &synchronizationAdmissionRecorder{pinned: &pinned}
	lifecycle := newSynchronizationLifecycle(manager, nil)
	var calls, releases int
	deny := true
	lifecycle.retryAdmission = func(context.Context, core.FederationName) (func(), error) {
		calls++
		if deny {
			return nil, core.ErrSaveAlreadyInProgress
		}
		pinned = true
		return func() { releases++; pinned = false }, nil
	}
	if err := lifecycle.Joined(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Resigned(ctx, "f", 2); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("member hooks recursively acquired generation admission")
	}
	lifecycle.Retry(ctx)
	if err := lifecycle.Restored(ctx, "f"); !errors.Is(err, core.ErrSaveAlreadyInProgress) {
		t.Fatal("restore retry did not surface admission failure", err)
	}
	if calls != 2 || manager.retries != 0 || len(lifecycle.pending) != 1 {
		t.Fatal("failed admission called manager or lost pending work")
	}
	deny = false
	lifecycle.Retry(ctx)
	if err := lifecycle.Restored(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	if calls != 4 || releases != 2 || manager.retries != 2 || pinned || len(lifecycle.pending) != 0 {
		t.Fatal("retry lease lifetime or recovery is incorrect")
	}
}

type synchronizationObservationRecorder func(core.FederationName) func()

func (f synchronizationObservationRecorder) AcquireObservation(fed core.FederationName) func() {
	return f(fed)
}

func TestSynchronizationRetryAdmissionCheckpointGenerationOrder(t *testing.T) {
	for _, test := range []string{"success", "unknown-generation", "generation-denied", "canceled-after-acquire"} {
		t.Run(test, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var order []string
			checkpoint := synchronizationObservationRecorder(func(core.FederationName) func() {
				order = append(order, testCheckpointLabel)
				return func() { order = append(order, "release-checkpoint") }
			})
			members := retryMembershipStub{
				generation: func(core.FederationName) (uint64, bool) {
					order = append(order, testGenerationLabel)
					return 7, test != "unknown-generation"
				},
				acquire: func(_ core.FederationName, generation uint64) (func(), error) {
					if generation != 7 {
						t.Fatal("generation not pinned")
					}
					order = append(order, "acquire-generation")
					if test == "generation-denied" {
						return nil, core.ErrFederationNotFound
					}
					if test == "canceled-after-acquire" {
						cancel()
					}
					return func() { order = append(order, "release-generation") }, nil
				},
			}
			release, err := newSynchronizationRetryAdmission(members, checkpoint)(ctx, "f")
			if test == "success" {
				if err != nil || release == nil {
					t.Fatal("admission failed", err)
				}
				release()
			} else if err == nil || release != nil {
				t.Fatal("failed admission leaked a lease", err)
			}
			want := []string{testCheckpointLabel, testGenerationLabel}
			if test != "unknown-generation" {
				want = append(want, "acquire-generation")
			}
			if test == "success" || test == "canceled-after-acquire" {
				want = append(want, "release-generation")
			}
			want = append(want, "release-checkpoint")
			if len(order) != len(want) {
				t.Fatalf("admission order=%v want=%v", order, want)
			}
			for i := range want {
				if order[i] != want[i] {
					t.Fatalf("admission order=%v want=%v", order, want)
				}
			}
		})
	}
}

func TestSynchronizationProductionRetryAdmissionInstalled(t *testing.T) {
	runtime, _, _ := integerProductionServer(t, t.TempDir(), 223)
	if runtime.synchronizationLifecycle == nil || runtime.synchronizationLifecycle.retryAdmission == nil {
		t.Fatal("production synchronization background retry lacks checkpoint/generation admission")
	}
}
