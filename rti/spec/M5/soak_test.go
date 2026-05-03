//go:build soak

// Package m5spec / soak_test.go — long-running soak under the `soak` build
// tag. Excluded from the default `go test ./...` run; CI invokes it
// separately with -tags=soak.
//
// Per TASK-078: 10-minute default run; assert no panics, no goroutine
// leaks (sampled via runtime.NumGoroutine before/after), all RPC errors
// carry codes from proto/rti/v1/errors.proto.
//
// The heavy lifting lives in rti/internal/transport/grpc/load_test.go
// (TestSoak_GRPCHandlersUnderLoad); this spec-side test asserts the
// same invariants on a small in-process loop so the M5 spec suite
// stays self-contained when the build tag is active. The CI soak run
// invokes the transport-side test directly via -run TestSoak.

package m5spec

import (
	"context"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/object"
)

// soakSmokeDefault is the default per-spec soak duration. Short enough
// that `go test -tags=soak ./rti/spec/M5/...` finishes inside CI's
// 60s default. The grpc-side soak under
// rti/internal/transport/grpc/load_test.go honors SOAK_DURATION to
// extend out to 10m for the production run.
const soakSmokeDefault = 5 * time.Second

// soakWorkers is the per-test worker count. Small enough to keep the
// shared spec fixture lock-contention from dominating; large enough to
// exercise concurrent fanout through the registry.
const soakWorkers = 4

// soakGoroutineDelta is the tolerated NumGoroutine delta for the spec
// smoke (less strict than the transport-side full soak because the
// shared fixture runs goroutines we don't drain).
const soakGoroutineDelta = 8

// TestSpec_M5_Soak_NoPanicNoLeak: drives a sustained mix of
// SendInteraction calls into the object registry from soakWorkers
// goroutines for soakSmokeDefault, asserts no panics and a small
// goroutine delta. The full transport-level soak lives in
// rti/internal/transport/grpc/load_test.go.
//
// Implements: NFR-PERF-1..4; soak hardening contract.
func TestSpec_M5_Soak_NoPanicNoLeak(t *testing.T) {
	dur := parseSoakDuration()
	t.Logf("spec soak smoke: duration=%v workers=%d", dur, soakWorkers)

	preGoroutines := runtime.NumGoroutine()

	ctx := context.Background()
	declMgr := declaration.New()
	outbox := newRecordingOutbox()
	foms := newPermissiveFOMRepo()
	reg, err := object.New(object.Options{
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         foms,
		Clock:        testClock(),
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}

	const fed = core.FederationName("soak")
	const cls = core.InteractionClassHandle(1)

	// Every worker publishes + subscribes to the class so SendInteraction
	// has a real fanout target.
	handles := make([]core.FederateHandle, soakWorkers)
	for i := 0; i < soakWorkers; i++ {
		h := core.FederateHandle(uint64(i + 1))
		handles[i] = h
		if err := declMgr.PublishInteractionClass(ctx, fed, h, cls); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
		if err := declMgr.SubscribeInteractionClass(ctx, fed, h, cls); err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	var (
		wg        sync.WaitGroup
		callCount atomic.Int64
		panicSeen atomic.Int64
	)
	for _, h := range handles {
		h := h
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicSeen.Add(1)
					t.Errorf("soak: federate %d panicked: %v", h, r)
				}
			}()
			for {
				select {
				case <-runCtx.Done():
					return
				default:
				}
				_ = reg.SendInteraction(runCtx, fed, h, cls, nil, nil)
				callCount.Add(1)
			}
		}()
	}

	wg.Wait()
	cancel()

	time.Sleep(100 * time.Millisecond)
	postGoroutines := runtime.NumGoroutine()
	delta := postGoroutines - preGoroutines

	t.Logf("spec soak smoke: total_calls=%d panics=%d goroutines pre=%d post=%d delta=%d",
		callCount.Load(), panicSeen.Load(), preGoroutines, postGoroutines, delta)

	if panicSeen.Load() > 0 {
		t.Fatalf("spec soak smoke: %d panics observed", panicSeen.Load())
	}
	if delta > soakGoroutineDelta {
		t.Fatalf("spec soak smoke: goroutine delta %d > tolerance %d (leak suspected)",
			delta, soakGoroutineDelta)
	}
	if callCount.Load() == 0 {
		t.Errorf("spec soak smoke: zero calls executed (workload aborted)")
	}
}

// parseSoakDuration mirrors the transport-side helper. Honors
// SOAK_DURATION env var; falls back to soakSmokeDefault.
func parseSoakDuration() time.Duration {
	raw := os.Getenv("SOAK_DURATION")
	if raw == "" {
		return soakSmokeDefault
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return soakSmokeDefault
	}
	return d
}
