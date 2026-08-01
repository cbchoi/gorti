// W2: frame-scoped membership lease tests.
//
// The LocalLRC Exchange stream acquires ONE membership lease per
// received frame (single op or whole Batch) via the value-type
// core.FederationMembershipLeaser tier, and resign/destroy take the
// exclusive side of the same operation gate. These tests pin the
// resulting teardown contract against the REAL federation.Manager:
//
//   - a resign or destroy issued mid-batch does NOT interleave with
//     the in-flight frame — every op of that frame applies;
//   - the stream dies at the frame boundary — no op of any later
//     frame applies after teardown.
package grpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/federation"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// leaseHookedRegistry counts SendInteraction applications and runs an
// optional hook with the 1-based call number before delegating.
type leaseHookedRegistry struct {
	*stubObjectRegistry
	calls  int
	onSend func(call int)
}

func (r *leaseHookedRegistry) SendInteraction(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	class core.InteractionClassHandle,
	parameters map[core.ParameterHandle][]byte,
	ts *core.LogicalTime,
) error {
	r.calls++
	if r.onSend != nil {
		r.onSend(r.calls)
	}
	return r.stubObjectRegistry.SendInteraction(ctx, fed, producer, class, parameters, ts)
}

// leaseHookedServerStream runs beforeRecv[i] (when present) before
// handing out the i-th scripted request. Used to hold the NEXT frame
// back until a concurrent teardown has fully completed, making the
// frame-boundary ordering deterministic.
type leaseHookedServerStream struct {
	scriptedLocalLRCServerStream
	beforeRecv map[int]func()
}

func (s *leaseHookedServerStream) Recv() (*rtiv1.LocalLRCRequest, error) {
	if hook := s.beforeRecv[s.next]; hook != nil {
		hook()
	}
	return s.scriptedLocalLRCServerStream.Recv()
}

// newLeaseTestManager builds a real federation.Manager (the production
// core.FederationMembershipLeaser implementation) with one federation
// and one joined federate.
func newLeaseTestManager(
	t *testing.T,
	fed core.FederationName,
) (*federation.Manager, core.FederateHandle, uint64) {
	t.Helper()
	ctx := context.Background()
	mgr, err := federation.New(federation.Options{
		Clock: core.NewFakeClock(time.Unix(0, 0)),
		FOMs:  adminFOMs{},
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	handle, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{
		Federation: fed, FederateName: "lrc",
	})
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	generation, ok := mgr.GenerationFor(fed)
	if !ok {
		t.Fatalf("GenerationFor(%q) not found", fed)
	}
	var leaser core.FederationMembershipLeaser = mgr // compile-time: Manager is a leaser
	_ = leaser
	return mgr, handle, generation
}

func leaseOpenRequest(
	fed core.FederationName,
	handle core.FederateHandle,
	generation uint64,
) *rtiv1.LocalLRCRequest {
	return &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Open{
		Open: &rtiv1.LocalLRCOpen{
			WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName:               string(fed),
			FederateHandle:               uint64(handle),
			ExpectedFederationGeneration: generation,
			AckEvery:                     32,
		},
	}}
}

func leaseInteractionBatch(firstSequence, count uint64) *rtiv1.LocalLRCRequest {
	operations := make([]*rtiv1.LocalLRCOperation, 0, count)
	for sequence := firstSequence; sequence < firstSequence+count; sequence++ {
		operations = append(operations, localLRCInteractionOperation(sequence))
	}
	return localLRCBatchRequest(operations...)
}

func TestLocalLRCExchangeResignDuringBatchDiesAtFrameBoundary(t *testing.T) {
	const fed = core.FederationName("lease-resign-fed")
	ctx := context.Background()
	mgr, handle, generation := newLeaseTestManager(t, fed)

	resignStarted := make(chan struct{})
	resignDone := make(chan struct{})
	var resignErr error

	registry := &leaseHookedRegistry{stubObjectRegistry: &stubObjectRegistry{}}
	registry.onSend = func(call int) {
		if call != 2 {
			return
		}
		// Fire the resign in the middle of the 4-op frame. It must
		// block on the exclusive side of the operation gate until the
		// frame lease is released.
		go func() {
			close(resignStarted)
			resignErr = mgr.ResignFederation(ctx, fed, handle, core.ResignActionNoAction)
			close(resignDone)
		}()
		<-resignStarted
		select {
		case <-resignDone:
			t.Error("resign completed mid-frame; frame lease did not fence it")
		case <-time.After(50 * time.Millisecond):
			// Expected: resign is parked behind the in-flight frame.
		}
	}

	stream := &leaseHookedServerStream{
		scriptedLocalLRCServerStream: scriptedLocalLRCServerStream{
			ctx: ctx,
			requests: []*rtiv1.LocalLRCRequest{
				leaseOpenRequest(fed, handle, generation),
				leaseInteractionBatch(1, 4),
				leaseInteractionBatch(5, 2),
			},
		},
		// Hold the second frame back until the resign has fully
		// completed, so its lease acquisition deterministically
		// observes the post-teardown roster.
		beforeRecv: map[int]func(){2: func() { <-resignDone }},
	}
	server := &Server{objService: newObjectService(registry), membership: mgr}

	err := newLocalLRCService(server).Exchange(stream)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Exchange status = %v (%v), want FailedPrecondition (federate not joined)",
			status.Code(err), err)
	}
	<-resignDone
	if resignErr != nil {
		t.Fatalf("concurrent resign: %v", resignErr)
	}
	// Every op of the in-flight frame applied; nothing after teardown.
	if registry.calls != 4 {
		t.Fatalf("registry applications = %d, want 4 (full first frame, none of the second)",
			registry.calls)
	}
	// The first frame's 4 committed ops were acked before the error.
	last := stream.responses[len(stream.responses)-1]
	if last.GetCommittedThrough() != 4 {
		t.Fatalf("final ACK committed_through = %d, want 4", last.GetCommittedThrough())
	}
}

func TestLocalLRCExchangeDestroyDuringBatchDiesAtFrameBoundary(t *testing.T) {
	const fed = core.FederationName("lease-destroy-fed")
	ctx := context.Background()
	mgr, handle, generation := newLeaseTestManager(t, fed)

	teardownStarted := make(chan struct{})
	teardownDone := make(chan struct{})
	var teardownResignErr, teardownDestroyErr error

	registry := &leaseHookedRegistry{stubObjectRegistry: &stubObjectRegistry{}}
	registry.onSend = func(call int) {
		if call != 2 {
			return
		}
		// Full teardown (resign, then destroy the now-empty
		// federation) fired mid-frame. Both take the exclusive
		// operation gate, so neither may complete before the frame
		// lease is released.
		go func() {
			close(teardownStarted)
			teardownResignErr = mgr.ResignFederation(ctx, fed, handle, core.ResignActionNoAction)
			teardownDestroyErr = mgr.DestroyFederation(ctx, fed)
			close(teardownDone)
		}()
		<-teardownStarted
		select {
		case <-teardownDone:
			t.Error("destroy completed mid-frame; frame lease did not fence it")
		case <-time.After(50 * time.Millisecond):
			// Expected: teardown is parked behind the in-flight frame.
		}
	}

	stream := &leaseHookedServerStream{
		scriptedLocalLRCServerStream: scriptedLocalLRCServerStream{
			ctx: ctx,
			requests: []*rtiv1.LocalLRCRequest{
				leaseOpenRequest(fed, handle, generation),
				leaseInteractionBatch(1, 4),
				leaseInteractionBatch(5, 2),
			},
		},
		beforeRecv: map[int]func(){2: func() { <-teardownDone }},
	}
	server := &Server{objService: newObjectService(registry), membership: mgr}

	err := newLocalLRCService(server).Exchange(stream)
	if status.Code(err) != codes.NotFound {
		t.Fatalf("Exchange status = %v (%v), want NotFound (federation destroyed)",
			status.Code(err), err)
	}
	<-teardownDone
	if teardownResignErr != nil || teardownDestroyErr != nil {
		t.Fatalf("concurrent teardown: resign=%v destroy=%v",
			teardownResignErr, teardownDestroyErr)
	}
	if registry.calls != 4 {
		t.Fatalf("registry applications = %d, want 4 (full first frame, none of the second)",
			registry.calls)
	}
	last := stream.responses[len(stream.responses)-1]
	if last.GetCommittedThrough() != 4 {
		t.Fatalf("final ACK committed_through = %d, want 4", last.GetCommittedThrough())
	}
}

// TestLocalLRCExchangeUsesValueLeasePerFrame pins the leaser tier
// selection: when the membership implements
// core.FederationMembershipLeaser, Exchange must take exactly one
// value lease per frame and never fall back to the closure-returning
// AcquireMember.
func TestLocalLRCExchangeUsesValueLeasePerFrame(t *testing.T) {
	membership := &leaseCountingMembership{
		fixedLocalLRCMembership: fixedLocalLRCMembership{
			generation: 9, federation: "fed", handle: 7,
		},
	}
	registry := &orderedLocalLRCRegistry{stubObjectRegistry: &stubObjectRegistry{}}
	server := &Server{objService: newObjectService(registry), membership: membership}
	stream := &scriptedLocalLRCServerStream{
		ctx: context.Background(),
		requests: []*rtiv1.LocalLRCRequest{
			localLRCOpenRequest(9, 32),
			localLRCBatchRequest(
				localLRCUpdateOperation(1, 3),
				localLRCInteractionOperation(2),
				localLRCUpdateOperation(3, 6),
			),
			{Body: &rtiv1.LocalLRCRequest_Interaction{Interaction: &rtiv1.LocalLRCInteraction{
				Sequence: 4, InteractionClassHandle: 12,
			}}},
		},
	}

	if err := newLocalLRCService(server).Exchange(stream); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	// Two frames (one 3-op batch + one single op) → two value leases.
	if membership.leases != 2 {
		t.Fatalf("value leases = %d, want 2 (one per frame)", membership.leases)
	}
	if membership.acquired != 0 {
		t.Fatalf("closure AcquireMember calls = %d, want 0 (leaser tier must win)",
			membership.acquired)
	}
	// Every lease was released: the gate's exclusive side is takeable.
	if !membership.gate.TryLock() {
		t.Fatal("operation gate still read-held after Exchange — a frame lease leaked")
	}
	membership.gate.Unlock()
}

// stalledAckLocalLRCServerStream parks the Send of the ack whose
// CommittedThrough equals stallOn until release is closed, signalling
// stalled first. The scripted in-memory harness has no real HTTP/2
// flow-control window, so a Send parked on a channel is the closest
// achievable stand-in for a client whose ack window is exhausted (a
// tiny-window bufconn setup cannot pin WHERE the server blocks, this
// can).
type stalledAckLocalLRCServerStream struct {
	scriptedLocalLRCServerStream
	stallOn uint64
	stalled chan struct{}
	release chan struct{}
}

func (s *stalledAckLocalLRCServerStream) Send(ack *rtiv1.LocalLRCAck) error {
	if ack.GetCommittedThrough() == s.stallOn {
		close(s.stalled)
		<-s.release
	}
	return s.scriptedLocalLRCServerStream.Send(ack)
}

// TestLocalLRCExchangeReleasesFrameLeaseBeforeAckSend pins the
// lease-across-ack fix: the frame lease (shared side of the operation
// gate) must be released after the frame's last op applies and BEFORE
// any ack stream.Send. While the boundary ack of a batch frame is
// parked on a stalled client, AcquireExclusive-style teardown
// (resign/destroy take gate.Lock) must succeed immediately — TryLock
// is deterministic here because the stall strictly follows either the
// release (fixed) or the still-held lease (regression).
func TestLocalLRCExchangeReleasesFrameLeaseBeforeAckSend(t *testing.T) {
	membership := &leaseCountingMembership{
		fixedLocalLRCMembership: fixedLocalLRCMembership{
			generation: 9, federation: "fed", handle: 7,
		},
	}
	registry := &orderedLocalLRCRegistry{stubObjectRegistry: &stubObjectRegistry{}}
	server := &Server{objService: newObjectService(registry), membership: membership}
	stream := &stalledAckLocalLRCServerStream{
		scriptedLocalLRCServerStream: scriptedLocalLRCServerStream{
			ctx: context.Background(),
			requests: []*rtiv1.LocalLRCRequest{
				localLRCOpenRequest(9, 2),
				localLRCBatchRequest(
					localLRCUpdateOperation(1, 3),
					localLRCInteractionOperation(2),
				),
			},
		},
		stallOn: 2, // the frame's ack-every boundary ack
		stalled: make(chan struct{}),
		release: make(chan struct{}),
	}

	done := make(chan error, 1)
	go func() { done <- newLocalLRCService(server).Exchange(stream) }()

	<-stream.stalled
	// The boundary ack Send is parked. Teardown must not be blocked.
	if !membership.gate.TryLock() {
		close(stream.release)
		<-done
		t.Fatal("frame lease held across blocked ack Send — teardown would stall behind a slow client")
	}
	membership.gate.Unlock()
	close(stream.release)
	if err := <-done; err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	// The frame fully applied and the cadence ack was still delivered.
	if len(registry.order) != 2 {
		t.Fatalf("registry operations = %d, want 2", len(registry.order))
	}
	if len(stream.responses) != 2 || stream.responses[1].GetCommittedThrough() != 2 {
		t.Fatalf("responses = %+v, want opening ACK then CommittedThrough 2", stream.responses)
	}
}

// leaseCountingMembership implements core.FederationMembershipLeaser
// on top of the guard stub so the tier-selection test can verify the
// value-lease path wins over the closure path.
type leaseCountingMembership struct {
	fixedLocalLRCMembership
	gate   sync.RWMutex
	leases int
}

func (m *leaseCountingMembership) AcquireMemberLease(
	fed core.FederationName,
	handle core.FederateHandle,
) (core.MemberLease, error) {
	if err := m.ValidateMember(fed, handle); err != nil {
		return core.MemberLease{}, err
	}
	m.leases++
	m.gate.RLock()
	return core.NewMemberLease(&m.gate), nil
}
