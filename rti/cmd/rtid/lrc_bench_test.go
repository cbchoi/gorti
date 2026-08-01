//go:build lrcbench

// W0 — Loopback DEVStone LRC benchmark (measurement gate).
//
// Composes the PRODUCTION rtid stack via newRTID (which builds
// grpcsvc.NewServer with EnableLocalLRC, the unexported multiOutbox at
// its contract-fixed 8192 event capacity / 32 batch / 1ms flush, and
// the exact interceptor chains of main.go:1447-1469), serves it over
// two transports — an in-memory bufconn (precedent:
// rti/internal/transport/grpc/spec_exception_test.go) and a TCP
// loopback listener — and drives a publisher/subscriber federate pair
// through the rti/pkg/federate SDK.
//
// Workload: 40600 AttributeUpdate + 40600 Interaction receive-order
// operations in EXACT ALTERNATION, admitted through the LocalLRC
// Exchange stream (32-operation Batch frames, ack-every 32). The
// subscriber drains the batched callback stream (EventBatches); the
// clock stops at the final validated subscriber callback.
//
// Executable invariants asserted every run:
//
//	(a) the subscriber's COMBINED callback trace digest equals the
//	    publisher's scripted update/interaction interleaving;
//	(b) the batch paths are active — a legacy StreamService/Events
//	    fallback or sub-32 LocalLRC frames fail the run;
//	(c) the LocalLRC Open generation handshake succeeded (peer batch
//	    limit + ack-every negotiated to the contract-fixed 32/32).
//
// Tag-gated (lrcbench) so 'go test ./rti/...' never runs or builds it.
// Run with:
//
//	go test -tags lrcbench -run TestLRCBench -count=1 -v ./rti/cmd/rtid/
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/federate"
)

const (
	// lrcBenchOpsPerKind is the per-kind operation count: 40600
	// attribute updates + 40600 interactions, i.e. 81200 total
	// callbacks, streamed in exact U,I,U,I,... alternation.
	lrcBenchOpsPerKind = 40600
	lrcBenchTotalOps   = 2 * lrcBenchOpsPerKind

	// Contract-fixed LocalLRC frame geometry (do not change): the SDK
	// requests 32-operation batch frames and the server acks every 32
	// committed operations.
	lrcBenchFrameOps = 32

	lrcBenchWatchdog = 5 * time.Minute
)

// lrcBenchFOMXML is a minimal FOM with one publishable object class
// (Vehicle.position) and one interaction class (Honk.payload), both
// receive-order — the LocalLRC transport under test is the
// receive-order fast path.
func lrcBenchFOMXML() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>lrcbench</name>
    <type>FOM</type>
    <version>1.0</version>
    <modificationDate>2026-07-26</modificationDate>
    <securityClassification>Unclassified</securityClassification>
    <description>W0 loopback DEVStone LRC benchmark FOM.</description>
    <useHistory>None</useHistory>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name>
        <sharing>PublishSubscribe</sharing>
        <semantics>Benchmark vehicle.</semantics>
        <attribute>
          <name>position</name>
          <dataType>HLAfloat64BE</dataType>
          <updateType>Periodic</updateType>
          <updateCondition>NA</updateCondition>
          <ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation>
          <order>Receive</order>
          <semantics>Send-time nanos payload.</semantics>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <interactionClass>
        <name>Honk</name>
        <sharing>PublishSubscribe</sharing>
        <transportation>HLAreliable</transportation>
        <order>Receive</order>
        <semantics>Benchmark interaction.</semantics>
        <parameter>
          <name>payload</name>
          <dataType>HLAinteger64BE</dataType>
          <semantics>Send-time nanos payload.</semantics>
        </parameter>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>`)
}

// streamMethodRecorder is a client-side stream interceptor that counts
// every opened stream by full method name. It runs entirely on the
// benchmark's own grpc.ClientConn — the server-side production
// interceptor chains are untouched — and exists to make the legacy
// Events fallback an executable failure instead of a silent slow path.
//
// It additionally counts every message received on EventBatches
// streams: each successful RecvMsg is exactly one server egress wire
// frame (including the initial Ready frame), so the benchmark can
// report egress frame counts (W7) without touching the server stack.
// The counter is a single atomic add per frame.
type streamMethodRecorder struct {
	mu           sync.Mutex
	methods      map[string]int
	egressFrames atomic.Int64
}

func newStreamMethodRecorder() *streamMethodRecorder {
	return &streamMethodRecorder{methods: map[string]int{}}
}

func (r *streamMethodRecorder) interceptor() stdgrpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *stdgrpc.StreamDesc,
		cc *stdgrpc.ClientConn,
		method string,
		streamer stdgrpc.Streamer,
		opts ...stdgrpc.CallOption,
	) (stdgrpc.ClientStream, error) {
		r.mu.Lock()
		r.methods[method]++
		r.mu.Unlock()
		cs, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			return nil, err
		}
		if method == rtiv1.StreamService_EventBatches_FullMethodName {
			cs = &frameCountingStream{ClientStream: cs, frames: &r.egressFrames}
		}
		return cs, nil
	}
}

func (r *streamMethodRecorder) count(method string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.methods[method]
}

// frameCountingStream counts successful RecvMsg calls on an
// EventBatches client stream. One RecvMsg == one server egress wire
// frame, so the total across the benchmark's two federate connections
// is the server's EventBatches egress frame count.
type frameCountingStream struct {
	stdgrpc.ClientStream
	frames *atomic.Int64
}

func (s *frameCountingStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	if err == nil {
		s.frames.Add(1)
	}
	return err
}

// dialLRCBench dials target with the benchmark's method recorder and
// wraps the conn via federate.WrapGRPCClientConn. The wrap keeps the
// SDK's LocalLRC + batched-callback fast paths enabled without passing
// any ExtraDialOptions through ConnectWithOptions (the streamEligible
// trap at rti/pkg/federate/federate.go).
func dialLRCBench(
	t *testing.T,
	target string,
	rec *streamMethodRecorder,
	extra ...stdgrpc.DialOption,
) *federate.Connection {
	t.Helper()
	opts := append([]stdgrpc.DialOption{
		stdgrpc.WithTransportCredentials(insecure.NewCredentials()),
		stdgrpc.WithStreamInterceptor(rec.interceptor()),
	}, extra...)
	cc, err := stdgrpc.NewClient(target, opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(%s): %v", target, err)
	}
	conn := federate.WrapGRPCClientConn(cc)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

type lrcBenchResult struct {
	transport    string
	totalNs      int64
	p50Ns        int64
	p99Ns        int64
	egressFrames int64
}

// runLRCBench composes one production rtid stack, serves it over the
// requested transport, runs the alternating 81200-op workload, checks
// the three executable invariants, and returns the measurement.
func runLRCBench(t *testing.T, transport string) lrcBenchResult {
	t.Helper()

	srv, err := newRTID(rtidConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	t.Cleanup(func() { _ = srv.plugins.Close() })
	t.Cleanup(srv.grpcS.Stop)

	// Sanity-pin the contract-fixed outbox geometry: 8192 events /
	// 32 batch / 1ms flush. The bench measures the production values;
	// a drift here invalidates every W2-W5 delta.
	if srv.outbox.batchSize != 32 || srv.outbox.bufferSize != 8192/32 ||
		srv.outbox.flushInterval != time.Millisecond {
		t.Fatalf("production outbox geometry drifted: batch=%d buffers=%d flush=%s",
			srv.outbox.batchSize, srv.outbox.bufferSize, srv.outbox.flushInterval)
	}

	rec := newStreamMethodRecorder()
	var pubConn, subConn *federate.Connection
	switch transport {
	case "bufconn":
		lis := bufconn.Listen(1 << 20)
		go func() { _ = srv.grpcS.Serve(lis) }()
		dialer := stdgrpc.WithContextDialer(
			func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			})
		pubConn = dialLRCBench(t, "passthrough:///bufnet", rec, dialer)
		subConn = dialLRCBench(t, "passthrough:///bufnet", rec, dialer)
	case "tcp":
		gln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("net.Listen: %v", err)
		}
		go func() { _ = srv.grpcS.Serve(gln) }()
		pubConn = dialLRCBench(t, gln.Addr().String(), rec)
		subConn = dialLRCBench(t, gln.Addr().String(), rec)
	default:
		t.Fatalf("unknown transport %q", transport)
	}

	ctx, cancel := context.WithTimeout(context.Background(), lrcBenchWatchdog)
	defer cancel()

	spec := federate.FederationSpec{
		Name: "lrcbench-" + transport,
		FOMModules: []federate.FOMModule{
			{Path: "lrcbench.xml", XML: lrcBenchFOMXML()},
		},
	}
	pub, err := pubConn.JoinFederation(ctx, spec, "publisher")
	if err != nil {
		t.Fatalf("publisher join: %v", err)
	}
	t.Cleanup(func() { _ = pub.Resign(context.Background()) })
	sub, err := subConn.JoinFederation(ctx, spec, "subscriber")
	if err != nil {
		t.Fatalf("subscriber join: %v", err)
	}
	t.Cleanup(func() { _ = sub.Resign(context.Background()) })

	// Publisher's own callback channel stays drained so nothing can
	// wedge its EventBatches stream (advisories etc.); the benchmark
	// trace is the subscriber's.
	go func() {
		for range pub.Events() {
		}
	}()

	if err := sub.SubscribeObjectClassAttributes(ctx, "Vehicle", []string{"position"}); err != nil {
		t.Fatalf("subscribe object class: %v", err)
	}
	if err := sub.SubscribeInteractionClass(ctx, "Honk"); err != nil {
		t.Fatalf("subscribe interaction class: %v", err)
	}
	if err := pub.PublishObjectClassAttributes(ctx, "Vehicle", []string{"position"}); err != nil {
		t.Fatalf("publish object class: %v", err)
	}
	if err := pub.PublishInteractionClass(ctx, "Honk"); err != nil {
		t.Fatalf("publish interaction class: %v", err)
	}
	objectHandle, err := pub.RegisterObjectInstance(ctx, "Vehicle", "bench-vehicle-1")
	if err != nil {
		t.Fatalf("RegisterObjectInstance: %v", err)
	}

	// Resolve the wire handles the LocalLRC queue calls need.
	classHandle, ok := pub.ObjectClassHandle("Vehicle")
	if !ok {
		t.Fatal("Vehicle class handle not resolved")
	}
	attrHandle, ok := pub.AttributeHandle(classHandle, "position")
	if !ok {
		t.Fatal("position attribute handle not resolved")
	}
	interactionHandle, ok := pub.InteractionClassHandle("Honk")
	if !ok {
		t.Fatal("Honk interaction handle not resolved")
	}
	paramHandle, ok := pub.ParameterHandle(interactionHandle, "payload")
	if !ok {
		t.Fatal("payload parameter handle not resolved")
	}

	// The subscriber must observe the instance before the workload so
	// the first reflect cannot race the discover.
	select {
	case evt, ok := <-sub.Events():
		if !ok {
			t.Fatal("subscriber event stream closed before discover")
		}
		if _, isDiscover := evt.(federate.DiscoverObjectInstance); !isDiscover {
			t.Fatalf("expected DiscoverObjectInstance first, got %T", evt)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for DiscoverObjectInstance")
	}

	// ------------------------------------------------------------------
	// Timed region: publisher script (exact U,I alternation) → LocalLRC
	// Exchange batch frames → production outbox → EventBatches → the
	// subscriber's final validated callback.
	// ------------------------------------------------------------------
	pubErr := make(chan error, 1)
	start := time.Now()
	go func() {
		payload := make([]byte, 8)
		for i := 0; i < lrcBenchOpsPerKind; i++ {
			binary.BigEndian.PutUint64(payload, uint64(time.Now().UnixNano()))
			if _, err := pub.QueueAttributeValuesByHandle(ctx, objectHandle,
				map[uint64][]byte{attrHandle: payload}); err != nil {
				pubErr <- err
				return
			}
			binary.BigEndian.PutUint64(payload, uint64(time.Now().UnixNano()))
			if _, err := pub.QueueInteractionByHandle(ctx, interactionHandle,
				map[uint64][]byte{paramHandle: payload}); err != nil {
				pubErr <- err
				return
			}
		}
		pubErr <- pub.FlushLocalLRC(ctx)
	}()

	trace := sha256.New()
	latencies := make([]int64, 0, lrcBenchTotalOps)
	reflects, receives := 0, 0
	for count := 0; count < lrcBenchTotalOps; count++ {
		var evt federate.Event
		var alive bool
		select {
		case evt, alive = <-sub.Events():
		case <-ctx.Done():
			t.Fatalf("watchdog: %d/%d callbacks after %s", count, lrcBenchTotalOps, lrcBenchWatchdog)
		}
		if !alive {
			t.Fatalf("subscriber event stream closed after %d/%d callbacks", count, lrcBenchTotalOps)
		}
		now := time.Now().UnixNano()
		switch e := evt.(type) {
		case federate.ReflectAttributeValues:
			trace.Write([]byte{'U'})
			reflects++
			p := e.Attributes["position"]
			if len(p) != 8 {
				t.Fatalf("reflect #%d: bad position payload %v", reflects, p)
			}
			latencies = append(latencies, now-int64(binary.BigEndian.Uint64(p)))
		case federate.ReceiveInteraction:
			trace.Write([]byte{'I'})
			receives++
			p := e.Parameters["payload"]
			if len(p) != 8 {
				t.Fatalf("interaction #%d: bad payload %v", receives, p)
			}
			latencies = append(latencies, now-int64(binary.BigEndian.Uint64(p)))
		default:
			t.Fatalf("unexpected event %T in benchmark callback trace", evt)
		}
	}
	totalNs := time.Since(start).Nanoseconds()
	// -------------------------- end timed region ----------------------

	if err := <-pubErr; err != nil {
		t.Fatalf("publisher workload: %v", err)
	}
	if reflects != lrcBenchOpsPerKind || receives != lrcBenchOpsPerKind {
		t.Fatalf("callback counts: %d reflects / %d receives, want %d each",
			reflects, receives, lrcBenchOpsPerKind)
	}

	// (a) Combined-callback-trace digest == publisher script digest.
	// This is the hard invariant as an executable check: the subscriber
	// observed the publisher's exact update/interaction interleaving.
	script := sha256.New()
	for i := 0; i < lrcBenchOpsPerKind; i++ {
		script.Write([]byte{'U', 'I'})
	}
	got := hex.EncodeToString(trace.Sum(nil))
	want := hex.EncodeToString(script.Sum(nil))
	if got != want {
		t.Fatalf("callback trace digest %s != publisher script digest %s "+
			"(update/interaction interleaving NOT preserved)", got, want)
	}

	// (b) Batch paths active. Legacy fallback detection: any
	// StreamService/Events stream fails the run; both federates must be
	// on EventBatches, and the LocalLRC frames must have reached the
	// full contract-fixed 32 operations.
	if n := rec.count(rtiv1.StreamService_Events_FullMethodName); n != 0 {
		t.Fatalf("legacy StreamService/Events fallback used %d time(s) — batch path inactive", n)
	}
	if n := rec.count(rtiv1.StreamService_EventBatches_FullMethodName); n != 2 {
		t.Fatalf("StreamService/EventBatches streams = %d, want 2 (publisher + subscriber)", n)
	}
	if n := rec.count(rtiv1.LocalLRCService_Exchange_FullMethodName); n != 1 {
		t.Fatalf("LocalLRCService/Exchange streams = %d, want 1 (publisher)", n)
	}

	// (c) LocalLRC Open generation handshake + frame geometry. The
	// negotiated values only exist after a successful Open handshake
	// (federation generation fenced server-side), so asserting them
	// proves the handshake succeeded AND pins the contract-fixed
	// 32-op frames / ack-every-32.
	stats := pub.LocalLRCStats()
	if stats.PeerBatchLimit != lrcBenchFrameOps || stats.AckEvery != lrcBenchFrameOps ||
		stats.BatchSize != lrcBenchFrameOps {
		t.Fatalf("LocalLRC handshake geometry: peer=%d ackEvery=%d batch=%d, want %d/%d/%d",
			stats.PeerBatchLimit, stats.AckEvery, stats.BatchSize,
			lrcBenchFrameOps, lrcBenchFrameOps, lrcBenchFrameOps)
	}
	if stats.MaxFrameOperations != lrcBenchFrameOps {
		t.Fatalf("max LocalLRC frame = %d operations, want full %d-op batch frames",
			stats.MaxFrameOperations, lrcBenchFrameOps)
	}
	if stats.Enqueued != lrcBenchTotalOps || stats.Acked != lrcBenchTotalOps || stats.Failures != 0 {
		t.Fatalf("LocalLRC counters: enqueued=%d acked=%d failures=%d, want %d/%d/0",
			stats.Enqueued, stats.Acked, stats.Failures, lrcBenchTotalOps, lrcBenchTotalOps)
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	return lrcBenchResult{
		transport:    transport,
		totalNs:      totalNs,
		p50Ns:        latencies[len(latencies)/2],
		p99Ns:        latencies[len(latencies)*99/100],
		egressFrames: rec.egressFrames.Load(),
	}
}

func reportLRCBench(t *testing.T, r lrcBenchResult) {
	t.Helper()
	// gc_percent is read back from the runtime (goruntime.go) AFTER the
	// timed region so each bench line carries executable evidence that
	// the round-2 GC product default engaged (400 when GOGC is unset)
	// rather than trusting the newRTID composition path.
	t.Logf("LRCBENCH transport=%s ops=%d total_ns=%d ns_per_op=%d p50_ns=%d p99_ns=%d egress_frames=%d gc_percent=%d",
		r.transport, lrcBenchTotalOps, r.totalNs, r.totalNs/lrcBenchTotalOps, r.p50Ns, r.p99Ns, r.egressFrames,
		currentGCPercent())
}

func TestLRCBenchBufconn(t *testing.T) {
	reportLRCBench(t, runLRCBench(t, "bufconn"))
}

func TestLRCBenchTCP(t *testing.T) {
	reportLRCBench(t, runLRCBench(t, "tcp"))
}
