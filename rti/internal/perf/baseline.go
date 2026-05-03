package perf

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/federation"
	"github.com/cbchoi/gorti/rti/internal/object"
)

// ErrNotImplemented is retained as a sentinel for transitional callers
// (M5 spec test references it). Once perf.New stops returning it, the
// spec test's RED-state branch becomes dead code; the test still passes
// because the GREEN-state branch runs.
var ErrNotImplemented = errors.New("perf: not implemented (Agent A M5 deliverable)")

// SchemaVersion is the JSON schema version for Result. Bump when adding
// fields; downstream agents (e.g. TASK-084) version-check before reading.
const SchemaVersion = 1

// FederationSize is one of the supported perf-baseline configurations.
// M5 exit demands all four at TASK-080 run time.
type FederationSize int

const (
	Size2   FederationSize = 2
	Size5   FederationSize = 5
	Size25  FederationSize = 25
	Size100 FederationSize = 100
)

// Result is the JSON-serializable output of one Manager.RunBaseline call.
// Field names + types are FROZEN; serialized via encoding/json with
// snake_case via JSON tags below.
type Result struct {
	SchemaVersion       int     `json:"schema_version"`
	FederationSize      int     `json:"federation_size"`
	DurationSeconds     float64 `json:"duration_seconds"`
	InteractionsSent    int64   `json:"interactions_sent"`
	ThroughputPerSecond float64 `json:"throughput_per_second"`
	LatencyP50Ms        float64 `json:"latency_p50_ms"`
	LatencyP99Ms        float64 `json:"latency_p99_ms"`
	Notes               string  `json:"notes,omitempty"`
}

// Options configure one Manager.RunBaseline call.
type Options struct {
	// Size is the number of federates to spawn. Required.
	Size FederationSize

	// Duration is how long to drive the workload. Defaults to 10s when zero.
	Duration time.Duration

	// RtidAddress is the gRPC endpoint the Manager dials. Defaults to
	// ":8442" when empty (matches cmd/rtid's default). Currently unused
	// by the in-process harness; reserved for a future subprocess mode.
	RtidAddress string

	// FederationName scopes the run. Defaults to "perf-baseline-<size>".
	FederationName string
}

// Manager runs a single perf-baseline configuration end-to-end.
//
// Spec test contract (rti/spec/M5/perf_test.go::TestSpec_M5_PerfHarnessRuns):
// Manager.RunBaseline must produce a Result with all numeric fields
// populated and SchemaVersion == 1. The test runs at Size2 with a short
// duration; TASK-080 runs all four sizes for the full 10s each.
type Manager struct {
	opts Options
}

// New constructs a Manager. Validates Options.Size; other fields default.
func New(opts Options) (*Manager, error) {
	if opts.Size <= 0 {
		return nil, fmt.Errorf("perf: Options.Size must be > 0 (got %d)", opts.Size)
	}
	if opts.Duration == 0 {
		opts.Duration = 10 * time.Second
	}
	if opts.FederationName == "" {
		opts.FederationName = fmt.Sprintf("perf-baseline-%d", int(opts.Size))
	}
	return &Manager{opts: opts}, nil
}

// perfInteractionClass is the single interaction class every federate
// publishes + subscribes to. The permissive FOM repository accepts any
// handle; we hard-code 1 to keep the harness self-contained.
const perfInteractionClass = core.InteractionClassHandle(1)

// perfParameterHandle is the parameter slot carrying the embedded send
// timestamp (int64 nanos). One parameter per interaction keeps the
// payload small; we measure end-to-end gRPC + fanout latency, not codec
// overhead (that's TASK-084's territory).
const perfParameterHandle = core.ParameterHandle(1)

// runtime bundles the in-process components used by the perf harness.
// Mirrors rti/cmd/rtid/pingpong.go::pingpongRuntime but with a multi-
// federate outbox sized for the larger federation. The event log is
// intentionally omitted under measurement mode (see buildRuntime).
type runtime struct {
	clock   core.Clock
	fedMgr  *federation.Manager
	declMgr *declaration.Manager
	objReg  *object.Registry
	outbox  *perfOutbox
}

// RunBaseline executes the configured measurement and returns a Result.
//
// Harness shape:
//  1. Build an in-process runtime (federation + declaration + object
//     registry + a buffered per-federate outbox; event log is discarded).
//  2. Create one federation, join `Size` federates, every federate
//     publishes + subscribes to perfInteractionClass.
//  3. Each federate runs two goroutines: a sender (tight loop calling
//     SendInteraction with an embedded send-time nanos parameter) and
//     a receiver (drains its outbox channel, samples per-event latency
//     against the embedded send time).
//  4. Run for opts.Duration. Aggregate throughput (sent / elapsed) and
//     p50 / p99 latency over the receiver samples.
//
// In-process is deliberate: it gives a measurement signal of the core
// fanout/serialization path without involving the gRPC dial/handler
// overhead. The full gRPC end-to-end is exercised separately by the
// soak harness (see rti/internal/transport/grpc/load_test.go).
func (m *Manager) RunBaseline(ctx context.Context) (Result, error) {
	rt, cleanup, err := buildRuntime(m.opts)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	handles, err := joinAll(ctx, rt, m.opts)
	if err != nil {
		return Result{}, err
	}
	if err := declareAll(ctx, rt, m.opts.FederationName, handles); err != nil {
		return Result{}, err
	}

	// Per-federate inbox subscriptions. cancels are deferred so the
	// outbox tears down cleanly on return.
	subs := make([]<-chan core.OutboundEvent, len(handles))
	cancels := make([]func() error, len(handles))
	for i, h := range handles {
		ch, cancel, err := rt.outbox.Subscribe(ctx, core.FederationName(m.opts.FederationName), h)
		if err != nil {
			for _, c := range cancels[:i] {
				_ = c()
			}
			return Result{}, fmt.Errorf("perf: subscribe federate %d: %w", h, err)
		}
		subs[i] = ch
		cancels[i] = cancel
	}
	defer func() {
		for _, c := range cancels {
			_ = c()
		}
	}()

	runCtx, cancelRun := context.WithTimeout(ctx, m.opts.Duration)
	defer cancelRun()

	var (
		sentCount    atomic.Int64
		latNanos     []int64
		latNanosMu   sync.Mutex
		wg           sync.WaitGroup
	)

	// Receivers: drain each federate's channel, compute latency vs the
	// embedded send time. A receiver for federate i samples whatever
	// events the registry's fanout delivered to it (events from peers,
	// excluding its own sends).
	for i := range handles {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]int64, 0, 4096)
			ch := subs[i]
			for {
				select {
				case <-runCtx.Done():
					latNanosMu.Lock()
					latNanos = append(latNanos, local...)
					latNanosMu.Unlock()
					// Drain remaining buffered events without blocking
					// so the outbox cancel can close the channel.
					for {
						select {
						case <-ch:
						default:
							return
						}
					}
				case evt, alive := <-ch:
					if !alive {
						latNanosMu.Lock()
						latNanos = append(latNanos, local...)
						latNanosMu.Unlock()
						return
					}
					if sendNs, ok := extractSendNanos(evt); ok {
						lat := rt.clock.Now().UnixNano() - sendNs
						if lat >= 0 {
							local = append(local, lat)
						}
					}
				}
			}
		}()
	}

	// Senders: tight loop, one goroutine per federate, each calling
	// SendInteraction with an 8-byte parameter payload encoding the
	// current wall-time nanos. SendInteraction itself fans out to all
	// peer subscribers synchronously (the outbox is non-blocking on
	// full channels — drops are the bounded-overflow contract; we treat
	// them as part of the steady-state envelope).
	start := time.Now()
	for i, h := range handles {
		_ = i
		h := h
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-runCtx.Done():
					return
				default:
				}
				params := makeSendParams(rt.clock.Now().UnixNano())
				if err := rt.objReg.SendInteraction(
					runCtx,
					core.FederationName(m.opts.FederationName),
					h,
					perfInteractionClass,
					params,
					nil, // RO delivery — we measure raw fanout latency
				); err != nil {
					// Context cancellation surfaces here at the boundary;
					// any other error breaks the loop (and the count
					// stops accumulating).
					return
				}
				sentCount.Add(1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	sent := sentCount.Load()

	// Resign cleanly so any future replay has a balanced log.
	_ = resignAll(context.Background(), rt, m.opts.FederationName, handles)

	res := Result{
		SchemaVersion:       SchemaVersion,
		FederationSize:      int(m.opts.Size),
		DurationSeconds:     elapsed.Seconds(),
		InteractionsSent:    sent,
		ThroughputPerSecond: float64(sent) / elapsed.Seconds(),
	}
	res.LatencyP50Ms, res.LatencyP99Ms = percentilesMs(latNanos)
	if sent == 0 {
		res.Notes = "zero interactions sent — workload aborted before any send; check ctx cancellation"
	} else if len(latNanos) == 0 {
		// Spec asserts LatencyP99Ms >= LatencyP50Ms, both default to 0
		// here, which satisfies the contract. Note the missing samples
		// so the report can flag it.
		res.Notes = fmt.Sprintf("size=%d sent=%d but zero latency samples (subscribers may have been overflowed)", m.opts.Size, sent)
	}
	return res, nil
}

// buildRuntime constructs the in-process components.
//
// The event log is intentionally omitted (both federation manager AND
// object registry permit a nil EventLog under their cut-1 relaxation):
// the perf harness measures the steady-state fanout pipeline, not the
// per-event log-append serialization cost. The lone Writer underlying
// MultiplexWriter has no internal mutex on `nextSeq` (see
// rti/internal/eventlog/writer.go) so concurrent senders trip the race
// detector; it is correctly serialized in production where one
// federation == one Append-caller goroutine, but the tight-loop perf
// harness violates that assumption. The event-log path remains
// covered by the standard /rti/spec/M2,M3 tests and the soak harness.
func buildRuntime(opts Options) (*runtime, func(), error) {
	clock := core.NewRealClock()
	foms := newPermissiveFOMRepo()
	fedMgr, err := federation.New(federation.Options{
		Clock:    clock,
		EventLog: nil, // see comment above — measurement-mode contract
		FOMs:     foms,
	})
	if err != nil {
		return nil, func() {}, fmt.Errorf("perf: build federation manager: %w", err)
	}
	declMgr := declaration.New()
	// Per-federate outbox buffer sized to absorb bursts at large N.
	// Size 100 * tight loop = many in-flight events; 8192 leaves
	// enough headroom that a slow receiver doesn't immediately overflow.
	outbox := newPerfOutbox(8192)
	objReg, err := object.New(object.Options{
		EventLog:     nil, // see comment above
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         foms,
		Clock:        clock,
	})
	if err != nil {
		return nil, func() {}, fmt.Errorf("perf: build object registry: %w", err)
	}
	return &runtime{
		clock:   clock,
		fedMgr:  fedMgr,
		declMgr: declMgr,
		objReg:  objReg,
		outbox:  outbox,
	}, func() {}, nil
}

// joinAll creates the federation and joins `Size` federates.
func joinAll(ctx context.Context, rt *runtime, opts Options) ([]core.FederateHandle, error) {
	if err := rt.fedMgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name: core.FederationName(opts.FederationName),
		Mode: core.ModeVerbose,
		Seed: 1,
	}); err != nil {
		return nil, fmt.Errorf("perf: CreateFederation: %w", err)
	}
	handles := make([]core.FederateHandle, 0, int(opts.Size))
	for i := 0; i < int(opts.Size); i++ {
		h, err := rt.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{
			Federation:   core.FederationName(opts.FederationName),
			FederateName: fmt.Sprintf("perf-fed-%d", i),
		})
		if err != nil {
			return nil, fmt.Errorf("perf: JoinFederation %d: %w", i, err)
		}
		handles = append(handles, h)
	}
	return handles, nil
}

// declareAll has every federate publish + subscribe to the single
// perfInteractionClass so SendInteraction fans out to all peers.
func declareAll(ctx context.Context, rt *runtime, fedName string, handles []core.FederateHandle) error {
	fed := core.FederationName(fedName)
	for _, h := range handles {
		if err := rt.declMgr.PublishInteractionClass(ctx, fed, h, perfInteractionClass); err != nil {
			return fmt.Errorf("perf: publish federate %d: %w", h, err)
		}
		if err := rt.declMgr.SubscribeInteractionClass(ctx, fed, h, perfInteractionClass); err != nil {
			return fmt.Errorf("perf: subscribe federate %d: %w", h, err)
		}
	}
	return nil
}

// resignAll cleans up; errors are ignored at the call site (best-effort
// teardown after a measurement run).
func resignAll(ctx context.Context, rt *runtime, fedName string, handles []core.FederateHandle) error {
	fed := core.FederationName(fedName)
	for _, h := range handles {
		if err := rt.fedMgr.ResignFederation(ctx, fed, h, core.ResignActionUnconditionallyDivestAttributes); err != nil {
			return err
		}
	}
	return nil
}

// makeSendParams encodes the send-time nanos into a single 8-byte
// parameter slot. The receiver decodes the same byte layout.
func makeSendParams(sendNs int64) map[core.ParameterHandle][]byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(sendNs))
	return map[core.ParameterHandle][]byte{perfParameterHandle: buf}
}

// extractSendNanos pulls the embedded send-time nanos back out of a
// received event. Returns (0, false) when the event is not a
// ReceiveInteraction or doesn't carry the perfParameterHandle slot.
//
// Uses the federateEventCarrier shim that matches the one in
// rti/internal/transport/grpc/stream.go, but redeclared here to avoid
// importing the grpc package from the perf package (one-way dep:
// transport/grpc may import object, not the other way).
func extractSendNanos(evt core.OutboundEvent) (int64, bool) {
	c, ok := evt.(perfFederateEventCarrier)
	if !ok {
		return 0, false
	}
	pb := c.Inner()
	if pb == nil {
		return 0, false
	}
	recv := pb.GetReceive()
	if recv == nil {
		return 0, false
	}
	raw, ok := recv.GetParameters()[uint64(perfParameterHandle)]
	if !ok || len(raw) < 8 {
		return 0, false
	}
	return int64(binary.BigEndian.Uint64(raw[:8])), true
}

// perfFederateEventCarrier mirrors the duck-type used by the gRPC
// stream service to extract the inner *rtiv1.FederateEvent from an
// outbound event. We redeclare it here (rather than importing) so the
// perf package has no dependency on transport/grpc.
type perfFederateEventCarrier interface {
	Inner() *innerFederateEvent
}

// innerFederateEvent is a structural alias for the generated
// *rtiv1.FederateEvent. We import it via the object package's outbound
// event type; the import path is fixed and the alias keeps the surface
// small.
//
// Implementation: the object registry's outboundEvent.Inner() returns
// a *rtiv1.FederateEvent. We re-export the same type via the import
// below so the type assertion in extractSendNanos resolves.
type innerFederateEvent = rtiv1FederateEvent

// rtiv1FederateEvent is a type alias importing the generated proto
// type. Declared in the helper file events.go to keep this main file
// focused on the harness logic.

// percentilesMs returns the p50 and p99 latencies in milliseconds.
// Returns (0, 0) when the sample slice is empty (the result satisfies
// the spec's LatencyP99Ms >= LatencyP50Ms invariant).
func percentilesMs(samples []int64) (float64, float64) {
	if len(samples) == 0 {
		return 0, 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p50ns := samples[percentileIndex(len(samples), 0.50)]
	p99ns := samples[percentileIndex(len(samples), 0.99)]
	return nsToMs(p50ns), nsToMs(p99ns)
}

func percentileIndex(n int, p float64) int {
	if n <= 1 {
		return 0
	}
	idx := int(float64(n-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx
}

func nsToMs(ns int64) float64 {
	return float64(ns) / 1_000_000.0
}

