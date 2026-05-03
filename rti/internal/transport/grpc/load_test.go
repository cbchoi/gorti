//go:build soak

// Package grpc / load_test.go — soak harness for the RTI gRPC handlers
// (TASK-078). Build-tag-gated so the default `go test ./...` run skips
// it; CI invokes the soak run separately:
//
//	go test -tags=soak ./rti/internal/transport/grpc/... -run TestSoak
//
// SOAK_DURATION (env, default "30s") sets the wall time the federate
// goroutines exchange RPCs for. The cut-1 default is 30s so the
// orchestrator can smoke-test the harness inside the M5 GREEN window;
// the production-grade 10-minute soak is an opt-in via SOAK_DURATION=10m.
//
// What the harness asserts:
//   - No panics across the run window (any handler panic fails the test
//     because the federate goroutine's recover() promotes it).
//   - No goroutine leaks: runtime.NumGoroutine() before vs. after must
//     be within a small delta (5) — the delta absorbs Go runtime
//     housekeeping goroutines that may legitimately spawn.
//   - All RPC errors carry valid status codes from proto/rti/v1/errors
//     (status.Code(err) != codes.Unknown).

package grpc

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
	"github.com/cbchoi/gorti/rti/internal/federation"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/object"
)

// soakDefaultDuration is the cut-1 smoke duration used when
// SOAK_DURATION isn't set. The orchestrator dispatches the full
// production soak with SOAK_DURATION=10m via CI separately.
const soakDefaultDuration = 30 * time.Second

// soakFederateCount is the worker count fanning RPCs into the server.
// Tuned to push the handler hot path without saturating CI machines.
const soakFederateCount = 5

// soakGoroutineDeltaTolerance is the maximum tolerated NumGoroutine
// delta between the pre-test and post-test snapshots. A delta within
// this band is treated as runtime housekeeping; anything larger trips
// the leak assertion.
const soakGoroutineDeltaTolerance = 5

// TestSoak_GRPCHandlersUnderLoad runs soakFederateCount goroutines, each
// looping randomly over CreateFederation / JoinFederation /
// PublishObjectClass / UpdateAttributes / SendInteraction /
// ResignFederation, against a real *grpc.Server bound to a dynamic
// port. See the file-level package comment for the assertion contract.
func TestSoak_GRPCHandlersUnderLoad(t *testing.T) {
	dur := parseSoakDuration()
	t.Logf("soak: duration=%v federates=%d", dur, soakFederateCount)

	preGoroutines := runtime.NumGoroutine()

	srv, addr, shutdown := startSoakServer(t)
	defer shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()

	var (
		wg          sync.WaitGroup
		callCount   atomic.Int64
		unknownCode atomic.Int64
		panicSeen   atomic.Int64
	)

	for i := 0; i < soakFederateCount; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicSeen.Add(1)
					t.Errorf("soak: federate %d panicked: %v", i, r)
				}
			}()
			runSoakFederate(ctx, t, addr, i, &callCount, &unknownCode)
		}()
	}

	wg.Wait()
	cancel()
	shutdown()
	_ = srv

	// Give the runtime a moment to drain background goroutines started
	// by the gRPC server / connections.
	time.Sleep(200 * time.Millisecond)
	postGoroutines := runtime.NumGoroutine()
	delta := postGoroutines - preGoroutines

	t.Logf("soak: total_calls=%d unknown_code=%d panics=%d goroutines pre=%d post=%d delta=%d",
		callCount.Load(), unknownCode.Load(), panicSeen.Load(),
		preGoroutines, postGoroutines, delta)

	if panicSeen.Load() > 0 {
		t.Fatalf("soak: %d federate panics observed", panicSeen.Load())
	}
	if unknownCode.Load() > 0 {
		t.Errorf("soak: %d RPC errors had codes.Unknown (want all errors carry proto-defined codes)",
			unknownCode.Load())
	}
	if delta > soakGoroutineDeltaTolerance {
		dumpGoroutineProfile(t)
		t.Fatalf("soak: goroutine delta %d > tolerance %d (leak suspected)",
			delta, soakGoroutineDeltaTolerance)
	}
}

// parseSoakDuration honors SOAK_DURATION env var (Go duration syntax).
// Falls back to soakDefaultDuration when unset or unparseable.
func parseSoakDuration() time.Duration {
	raw := os.Getenv("SOAK_DURATION")
	if raw == "" {
		return soakDefaultDuration
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return soakDefaultDuration
	}
	return d
}

// startSoakServer composes a real Server (with the actual federation /
// declaration / object / outbox manager wiring) and serves it on a
// dynamic TCP port. Returns the server, its address, and a shutdown
// function that's safe to call multiple times.
func startSoakServer(t *testing.T) (*Server, string, func()) {
	t.Helper()
	clock := core.NewRealClock()
	mw, err := eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
		Clock:   clock,
		Mode:    core.ModeVerbose,
		Factory: discardFactory(clock),
	})
	if err != nil {
		t.Fatalf("soak: NewMultiplexWriter: %v", err)
	}
	foms := newSoakFOMRepo()
	fedMgr, err := federation.New(federation.Options{
		Clock: clock, EventLog: mw, FOMs: foms,
	})
	if err != nil {
		t.Fatalf("soak: federation.New: %v", err)
	}
	declMgr := declaration.New()
	outbox := newSoakOutbox(8192)
	objReg, err := object.New(object.Options{
		EventLog: mw, Declarations: declMgr, Outbox: outbox,
		FOMs: foms, Clock: clock,
	})
	if err != nil {
		t.Fatalf("soak: object.New: %v", err)
	}
	srv, err := NewServer(Options{
		Federations: fedMgr, Declarations: declMgr,
		Objects: objReg, Outbox: outbox,
	})
	if err != nil {
		t.Fatalf("soak: NewServer: %v", err)
	}

	gs := grpc.NewServer()
	if err := srv.Register(gs); err != nil {
		t.Fatalf("soak: Register: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("soak: net.Listen: %v", err)
	}
	go func() { _ = gs.Serve(ln) }()

	var shutOnce sync.Once
	shutdown := func() {
		shutOnce.Do(func() {
			gs.GracefulStop()
			_ = ln.Close()
			_ = mw.Close()
		})
	}
	return srv, ln.Addr().String(), shutdown
}

// runSoakFederate is the per-worker loop. Picks a random RPC each
// iteration; counts every call; flags any error with codes.Unknown.
//
// The design deliberately doesn't synchronize state across federates —
// CreateFederation collisions are expected (and produce AlreadyExists,
// which is a known proto code, NOT Unknown).
func runSoakFederate(
	ctx context.Context, t *testing.T, addr string, idx int,
	calls *atomic.Int64, unknown *atomic.Int64,
) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Errorf("soak: federate %d Dial: %v", idx, err)
		return
	}
	defer func() { _ = conn.Close() }()

	fedClient := rtiv1.NewFederationServiceClient(conn)
	declClient := rtiv1.NewDeclarationServiceClient(conn)
	objClient := rtiv1.NewObjectServiceClient(conn)

	// Each federate owns its own federation+name space so the random
	// RPC mix stays independent across workers; the federation manager
	// still sees concurrent traffic across federations.
	fedName := fmt.Sprintf("soak-fed-%d", idx)
	federateName := fmt.Sprintf("soak-federate-%d", idx)
	var federateHandle uint64
	var joined bool

	rng := rand.New(rand.NewSource(int64(idx) ^ time.Now().UnixNano()))

	// Bootstrap: create + join. Failures here are recorded but the
	// loop still runs (subsequent ops will produce Unknown-free
	// NotFound / FailedPrecondition that we count).
	if _, err := fedClient.CreateFederation(ctx, &rtiv1.CreateFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: fedName,
		Mode:           rtiv1.Mode_MODE_VERBOSE,
		Seed:           uint64(idx + 1),
	}); err != nil {
		recordCall(calls, unknown, err)
	} else {
		calls.Add(1)
	}
	if resp, err := fedClient.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: fedName,
		FederateName:   federateName,
	}); err != nil {
		recordCall(calls, unknown, err)
	} else {
		federateHandle = resp.GetFederateHandle()
		joined = true
		calls.Add(1)
	}

	for {
		select {
		case <-ctx.Done():
			// Final resign so the goroutine exits cleanly.
			if joined {
				_, _ = fedClient.ResignFederation(context.Background(), &rtiv1.ResignFederationRequest{
					WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
					FederationName: fedName,
					FederateHandle: federateHandle,
					Action:         rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
				})
			}
			return
		default:
		}

		// Pick one of the 6 listed RPCs. Weights are deliberately
		// even — the assertion is "no Unknowns / panics / leaks", not
		// "production-realistic mix".
		op := rng.Intn(6)
		var err error
		switch op {
		case 0: // CreateFederation (will collide → AlreadyExists)
			_, err = fedClient.CreateFederation(ctx, &rtiv1.CreateFederationRequest{
				WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName: fedName,
				Mode:           rtiv1.Mode_MODE_VERBOSE,
			})
		case 1: // JoinFederation (will collide → AlreadyExists)
			_, err = fedClient.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
				WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName: fedName,
				FederateName:   federateName,
			})
		case 2: // PublishObjectClassAttributes
			_, err = declClient.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{
				WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName:    fedName,
				FederateHandle:    federateHandle,
				ObjectClassHandle: 1,
				AttributeHandles:  []uint64{1, 2, 3},
			})
		case 3: // UpdateAttributeValues (no registered object → expect FailedPrecondition / NotFound — both proto-mapped)
			_, err = objClient.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{
				WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName: fedName,
				FederateHandle: federateHandle,
				ObjectHandle:   1,
				Attributes:     map[uint64][]byte{1: {0x42}},
			})
		case 4: // SendInteraction
			_, err = objClient.SendInteraction(ctx, &rtiv1.SendInteractionRequest{
				WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName:         fedName,
				FederateHandle:         federateHandle,
				InteractionClassHandle: 1,
				Parameters:             map[uint64][]byte{1: {0x55}},
			})
		case 5: // ResignFederation + immediate re-join (keeps the federation populated)
			if joined {
				_, err = fedClient.ResignFederation(ctx, &rtiv1.ResignFederationRequest{
					WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
					FederationName: fedName,
					FederateHandle: federateHandle,
					Action:         rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
				})
				joined = false
				if err == nil {
					if resp, jerr := fedClient.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
						WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
						FederationName: fedName,
						FederateName:   federateName,
					}); jerr == nil {
						federateHandle = resp.GetFederateHandle()
						joined = true
						calls.Add(1)
					} else {
						recordCall(calls, unknown, jerr)
					}
				}
			}
		}
		recordCall(calls, unknown, err)
	}
}

// recordCall increments the call counter and, when err != nil, checks
// that its status code is one of the proto-defined codes (anything but
// codes.Unknown). Context cancellation isn't an Unknown — it surfaces
// as codes.Canceled / codes.DeadlineExceeded — but we still tolerate
// it explicitly because it's the soak-loop exit path.
func recordCall(calls *atomic.Int64, unknown *atomic.Int64, err error) {
	calls.Add(1)
	if err == nil {
		return
	}
	c := status.Code(err)
	if c == codes.Canceled || c == codes.DeadlineExceeded {
		return
	}
	if c == codes.Unknown {
		unknown.Add(1)
	}
}

// dumpGoroutineProfile writes the live goroutine profile to t.Logf at
// debug level when a leak is detected, so the failure log itself
// carries enough info to diagnose without a separate pprof step.
func dumpGoroutineProfile(t *testing.T) {
	t.Helper()
	prof := pprof.Lookup("goroutine")
	if prof == nil {
		t.Log("dumpGoroutineProfile: pprof.Lookup(\"goroutine\") returned nil")
		return
	}
	tmp, err := os.CreateTemp("", "soak-leak-*.prof")
	if err != nil {
		t.Logf("dumpGoroutineProfile: CreateTemp: %v", err)
		return
	}
	defer func() { _ = tmp.Close() }()
	if err := prof.WriteTo(tmp, 1); err != nil {
		t.Logf("dumpGoroutineProfile: WriteTo: %v", err)
		return
	}
	t.Logf("soak: goroutine profile dumped to %s", tmp.Name())
}

// soakOutbox is a multi-federate, drop-on-overflow outbox sized for the
// soak workload. It satisfies SubscribableOutbox (the gRPC stream
// service requires it) plus core.Outbox.
type soakOutbox struct {
	mu          sync.RWMutex
	subscribers map[soakKey]chan core.OutboundEvent
	bufferSize  int
}

type soakKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

func newSoakOutbox(bufferSize int) *soakOutbox {
	if bufferSize < 1 {
		bufferSize = 1
	}
	return &soakOutbox{
		subscribers: map[soakKey]chan core.OutboundEvent{},
		bufferSize:  bufferSize,
	}
}

func (o *soakOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.RLock()
	ch, ok := o.subscribers[soakKey{fed, h}]
	o.mu.RUnlock()
	if !ok {
		return nil
	}
	select {
	case ch <- evt:
	default:
	}
	return nil
}

func (o *soakOutbox) Subscribe(_ context.Context, fed core.FederationName, h core.FederateHandle) (<-chan core.OutboundEvent, func() error, error) {
	key := soakKey{fed, h}
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, dup := o.subscribers[key]; dup {
		return nil, nil, fmt.Errorf("soak: subscriber already registered for %v", key)
	}
	ch := make(chan core.OutboundEvent, o.bufferSize)
	o.subscribers[key] = ch
	var once sync.Once
	cancel := func() error {
		once.Do(func() {
			o.mu.Lock()
			defer o.mu.Unlock()
			if existing, ok := o.subscribers[key]; ok && existing == ch {
				delete(o.subscribers, key)
				close(ch)
			}
		})
		return nil
	}
	return ch, cancel, nil
}

var _ core.Outbox = (*soakOutbox)(nil)

// soakFOMRepo is the same permissive shim used by the perf and pingpong
// harnesses, redeclared locally so this file has no cross-package
// dependency on cmd/rtid.
type soakFOMRepo struct{}

func newSoakFOMRepo() *soakFOMRepo { return &soakFOMRepo{} }

func (*soakFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return soakFOMHandle{}, nil
}
func (*soakFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return soakFOMHandle{}, nil
}

type soakFOMHandle struct{}

func (soakFOMHandle) IsValid() bool { return true }
func (soakFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (soakFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (soakFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (soakFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

// discardFactory mirrors cmd/rtid's discardWriterFactory but is local
// to the soak file (tag-gated) so the production binary stays
// independent of the soak path.
func discardFactory(clock core.Clock) eventlog.WriterFactory {
	return func(opts eventlog.WriterOptions) (*eventlog.Writer, error) {
		opts.Sink = soakDiscardSink{}
		opts.Clock = clock
		return eventlog.NewWriter(opts)
	}
}

type soakDiscardSink struct{}

func (soakDiscardSink) Write(p []byte) (int, error) { return len(p), nil }
