package grpc

import (
	"context"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type checkpointProbe struct {
	rw    sync.RWMutex
	mu    sync.Mutex
	steps []string
	deny  error
}

func (p *checkpointProbe) record(step string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = append(p.steps, step)
}

func (p *checkpointProbe) AcquireService(fed core.FederationName) (func(), error) {
	p.rw.RLock()
	p.mu.Lock()
	p.steps = append(p.steps, "service:"+string(fed))
	err := p.deny
	p.mu.Unlock()
	if err != nil {
		p.rw.RUnlock()
		return nil, err
	}
	return func() { p.record("service-release"); p.rw.RUnlock() }, nil
}

func (p *checkpointProbe) AcquireCheckpoint(fed core.FederationName) func() {
	p.rw.Lock()
	p.record("checkpoint:" + string(fed))
	return func() { p.record("checkpoint-release"); p.rw.Unlock() }
}

func (p *checkpointProbe) assertSteps(t *testing.T, want ...string) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !reflect.DeepEqual(p.steps, want) {
		t.Fatalf("steps = %v, want %v", p.steps, want)
	}
}

type checkpointMembership struct {
	probe *checkpointProbe
	err   error
}

func (m checkpointMembership) GenerationFor(core.FederationName) (uint64, bool) { return 1, true }
func (m checkpointMembership) ValidateMember(core.FederationName, core.FederateHandle) error {
	m.probe.record("validate")
	return m.err
}
func (m checkpointMembership) AcquireMember(core.FederationName, core.FederateHandle) (func(), error) {
	m.probe.record("member")
	if m.err != nil {
		return nil, m.err
	}
	return func() { m.probe.record("member-release") }, nil
}

func TestCheckpointUnaryAdmissionPrecedesMembership(t *testing.T) {
	probe := new(checkpointProbe)
	opts := validOpts()
	opts.CheckpointAdmission = probe
	opts.Membership = checkpointMembership{probe: probe}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = srv.UnaryMembershipInterceptor()(context.Background(),
		&rtiv1.SendInteractionRequest{FederationName: "fed", FederateHandle: 1},
		&stdgrpc.UnaryServerInfo{FullMethod: "/rti.v1.ObjectService/SendInteraction"},
		func(context.Context, any) (any, error) { probe.record("handler"); return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	probe.assertSteps(t, "service:fed", "member", "handler", "member-release", "service-release")
}

func TestCheckpointUnaryDeniesLifecycleAndOrdinaryCalls(t *testing.T) {
	cases := []struct {
		name    string
		request any
	}{
		{"FederationService/CreateFederation", &rtiv1.CreateFederationRequest{FederationName: "fed"}},
		{"FederationService/JoinFederation", &rtiv1.JoinFederationRequest{FederationName: "fed"}},
		{"FederationService/ResignFederation", &rtiv1.ResignFederationRequest{FederationName: "fed", FederateHandle: 1}},
		{"FederationService/DestroyFederation", &rtiv1.DestroyFederationRequest{FederationName: "fed"}},
		{"ObjectService/SendInteraction", &rtiv1.SendInteractionRequest{FederationName: "fed", FederateHandle: 1}},
		{"MomService/QueryFederateAttributes", &rtiv1.QueryFederateAttributesRequest{FederationName: "fed", FederateHandle: 2}},
	}
	for _, denied := range []error{core.ErrSaveAlreadyInProgress, core.ErrRestoreAlreadyInProgress} {
		for _, tc := range cases {
			t.Run(denied.Error()+"/"+tc.name, func(t *testing.T) {
				probe := &checkpointProbe{deny: denied}
				srv := &Server{checkpointAdmission: probe, membership: checkpointMembership{probe: probe}}
				_, err := srv.UnaryMembershipInterceptor()(context.Background(), tc.request,
					&stdgrpc.UnaryServerInfo{FullMethod: "/rti.v1." + tc.name},
					func(context.Context, any) (any, error) { t.Error("denied handler ran"); return nil, nil })
				if status.Code(err) != codes.FailedPrecondition {
					t.Fatalf("error = %v", err)
				}
				probe.assertSteps(t, "service:fed")
			})
		}
	}
}

func TestCheckpointEverySavepointRPCUsesControlLease(t *testing.T) {
	for _, method := range rtiv1.SavepointService_ServiceDesc.Methods {
		t.Run(method.MethodName, func(t *testing.T) {
			probe := &checkpointProbe{deny: core.ErrSaveAlreadyInProgress}
			srv := &Server{checkpointAdmission: probe, membership: checkpointMembership{probe: probe}}
			_, err := srv.UnaryMembershipInterceptor()(context.Background(),
				&rtiv1.FederateSaveResponseRequest{FederationName: "fed", FederateHandle: 1},
				&stdgrpc.UnaryServerInfo{FullMethod: "/rti.v1.SavepointService/" + method.MethodName},
				func(context.Context, any) (any, error) { probe.record("handler"); return nil, nil })
			if err != nil {
				t.Fatal(err)
			}
			probe.assertSteps(t, "checkpoint:fed", "member", "handler", "member-release", "checkpoint-release")
		})
	}
}

type checkpointScriptStream struct {
	stdgrpc.ServerStream
	requests []proto.Message
	send     func(any) error
}

func (*checkpointScriptStream) Context() context.Context { return context.Background() }
func (s *checkpointScriptStream) RecvMsg(message any) error {
	if len(s.requests) == 0 {
		return io.EOF
	}
	proto.Reset(message.(proto.Message))
	proto.Merge(message.(proto.Message), s.requests[0])
	s.requests = s.requests[1:]
	return nil
}
func (s *checkpointScriptStream) SendMsg(message any) error {
	if s.send != nil {
		return s.send(message)
	}
	return nil
}

func TestCheckpointMutationStreamsReleaseBeforeSlowACK(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "interaction", true: "confirmed"}[confirmed], func(t *testing.T) {
			probe := new(checkpointProbe)
			request := proto.Message(&rtiv1.SendInteractionRequest{FederationName: "fed", FederateHandle: 1})
			if confirmed {
				request = &rtiv1.ConfirmedObjectRequest{Operation: &rtiv1.ConfirmedObjectRequest_Interaction{Interaction: request.(*rtiv1.SendInteractionRequest)}}
			}
			entered, unblock := make(chan struct{}), make(chan struct{})
			base := &checkpointScriptStream{requests: []proto.Message{request}, send: func(any) error { close(entered); <-unblock; return nil }}
			stream := &membershipServerStream{ServerStream: base,
				server:      &Server{checkpointAdmission: probe, membership: checkpointMembership{probe: probe}},
				interaction: !confirmed, confirmedObject: confirmed, handshakePassed: true}
			defer stream.releaseCurrent()
			if err := stream.RecvMsg(proto.Clone(request)); err != nil {
				t.Fatal(err)
			}
			probe.assertSteps(t, "service:fed", "member")
			sent := make(chan error, 1)
			go func() { sent <- stream.SendMsg(&rtiv1.Empty{}) }()
			<-entered
			acquired := make(chan struct{})
			go func() { release := probe.AcquireCheckpoint("fed"); release(); close(acquired) }()
			select {
			case <-acquired:
			case <-time.After(time.Second):
				t.Error("checkpoint blocked by ACK network write")
			}
			close(unblock)
			if err := <-sent; err != nil {
				t.Fatal(err)
			}
			<-acquired
			probe.assertSteps(t, "service:fed", "member", "member-release", "service-release", "checkpoint:fed", "checkpoint-release")
		})
	}
}

func TestCheckpointMutationStreamsDenyEachOperation(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "interaction", true: "confirmed"}[confirmed], func(t *testing.T) {
			probe := new(checkpointProbe)
			request := proto.Message(&rtiv1.SendInteractionRequest{FederationName: "fed", FederateHandle: 1})
			if confirmed {
				request = &rtiv1.ConfirmedObjectRequest{Operation: &rtiv1.ConfirmedObjectRequest_Interaction{Interaction: request.(*rtiv1.SendInteractionRequest)}}
			}
			stream := &membershipServerStream{ServerStream: &checkpointScriptStream{requests: []proto.Message{request, request}},
				server:      &Server{checkpointAdmission: probe, membership: checkpointMembership{probe: probe}},
				interaction: !confirmed, confirmedObject: confirmed, handshakePassed: true}
			defer stream.releaseCurrent()
			if err := stream.RecvMsg(proto.Clone(request)); err != nil {
				t.Fatal(err)
			}
			if err := stream.SendMsg(&rtiv1.Empty{}); err != nil {
				t.Fatal(err)
			}
			probe.deny = core.ErrRestoreAlreadyInProgress
			if err := stream.RecvMsg(proto.Clone(request)); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("error = %v", err)
			}
			probe.assertSteps(t, "service:fed", "member", "member-release", "service-release", "service:fed")
		})
	}
}

func TestCheckpointCallbackStreamRemainsAvailable(t *testing.T) {
	probe := &checkpointProbe{deny: core.ErrRestoreAlreadyInProgress}
	stream := &membershipServerStream{ServerStream: &checkpointScriptStream{requests: []proto.Message{&rtiv1.EventsRequest{FederationName: "fed", FederateHandle: 1}}},
		server: &Server{checkpointAdmission: probe, membership: checkpointMembership{probe: probe}}}
	if err := stream.RecvMsg(new(rtiv1.EventsRequest)); err != nil {
		t.Fatal(err)
	}
	probe.assertSteps(t, "validate")
}

func TestCheckpointMembershipFailureReleasesAdmission(t *testing.T) {
	probe := new(checkpointProbe)
	srv := &Server{checkpointAdmission: probe, membership: checkpointMembership{probe: probe, err: core.ErrFederateNotJoined}}
	_, err := srv.UnaryMembershipInterceptor()(context.Background(), &rtiv1.SendInteractionRequest{FederationName: "fed", FederateHandle: 1}, &stdgrpc.UnaryServerInfo{},
		func(context.Context, any) (any, error) { t.Error("handler ran"); return nil, nil })
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("error = %v", err)
	}
	probe.assertSteps(t, "service:fed", "member", "service-release")
}

type checkpointLocalStream struct {
	*scriptedLocalLRCServerStream
	probe *checkpointProbe
}

func (s *checkpointLocalStream) Send(ack *rtiv1.LocalLRCAck) error {
	if len(s.responses) == 0 {
		s.probe.deny = core.ErrSaveAlreadyInProgress
	}
	return s.scriptedLocalLRCServerStream.Send(ack)
}

func TestCheckpointLocalLRCDeniesEveryFrameShape(t *testing.T) {
	frames := []*rtiv1.LocalLRCRequest{
		{Body: &rtiv1.LocalLRCRequest_AttributeUpdate{AttributeUpdate: localLRCUpdateOperation(1, 3).GetAttributeUpdate()}},
		{Body: &rtiv1.LocalLRCRequest_Interaction{Interaction: localLRCInteractionOperation(1).GetInteraction()}},
		localLRCBatchRequest(localLRCUpdateOperation(1, 3), localLRCInteractionOperation(2)),
	}
	for i, frame := range frames {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			probe := new(checkpointProbe)
			membership := &fixedLocalLRCMembership{generation: 1, federation: "fed", handle: 7}
			registry := &stubObjectRegistry{}
			srv := &Server{checkpointAdmission: probe, membership: membership, objService: newObjectService(registry)}
			stream := &checkpointLocalStream{probe: probe, scriptedLocalLRCServerStream: &scriptedLocalLRCServerStream{ctx: context.Background(), requests: []*rtiv1.LocalLRCRequest{localLRCOpenRequest(1, 1), frame}}}
			if err := newLocalLRCService(srv).Exchange(stream); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("error=%v", err)
			}
			if membership.acquired != 0 || len(registry.updCalls) != 0 || len(registry.sendCalls) != 0 {
				t.Fatal("denied frame reached membership or mutation")
			}
		})
	}
}

func TestCheckpointAdminDeniesLifecycleAndPromotion(t *testing.T) {
	for _, method := range []string{"resign", "destroy", "promote"} {
		t.Run(method, func(t *testing.T) {
			probe := &checkpointProbe{deny: core.ErrSaveAlreadyInProgress}
			store := &recordingFedStore{}
			svc := newMutatingService(MutatingOptions{Federations: store, CheckpointAdmission: probe, Cluster: checkpointPromoter{t: t}})
			var err error
			switch method {
			case "resign":
				_, err = svc.ForceResign(context.Background(), &rtiv1.ForceResignRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", FederateHandle: 1})
			case "destroy":
				_, err = svc.DestroyFederation(context.Background(), &rtiv1.AdminDestroyFederationRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", EvictJoinedFederates: true})
			case "promote":
				_, err = svc.PromoteFederation(context.Background(), &rtiv1.PromoteFederationRequest{FederationName: "fed", TargetNodeId: "other"})
			}
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("error=%v", err)
			}
			if len(store.resignCalls) != 0 || len(store.destroyCalls) != 0 {
				t.Fatal("denied admin mutation ran")
			}
			probe.assertSteps(t, "service:fed")
		})
	}
}

type checkpointPromoter struct{ t *testing.T }

func (p checkpointPromoter) PromoteFederation(core.FederationName, string) (string, error) {
	p.t.Error("denied promotion ran")
	return "", nil
}
func (checkpointPromoter) SelfID() string      { return "self" }
func (checkpointPromoter) SelfAddress() string { return "local" }

func TestCheckpointLocalLRCFenceOrderingAndFailureCleanup(t *testing.T) {
	for _, memberErr := range []error{nil, core.ErrFederateNotJoined} {
		name := "success"
		if memberErr != nil {
			name = "membership-failure"
		}
		t.Run(name, func(t *testing.T) {
			probe := new(checkpointProbe)
			fencer := newLocalLRCFrameFencer(checkpointMembership{probe: probe, err: memberErr}, "fed", 1)
			fencer.admission = probe
			lease, err := fencer.acquire(context.Background())
			if memberErr != nil {
				if status.Code(err) != codes.FailedPrecondition {
					t.Fatalf("error = %v", err)
				}
				probe.assertSteps(t, "service:fed", "member", "service-release")
			} else {
				if err != nil {
					t.Fatal(err)
				}
				probe.assertSteps(t, "service:fed", "member")
				lease.end()
				probe.assertSteps(t, "service:fed", "member", "member-release", "service-release")
			}
			if !probe.rw.TryLock() {
				t.Fatal("service admission leaked")
			}
			probe.rw.Unlock()
		})
	}
}

func TestCheckpointDisabledLocalLRCValueLeaseHasNoAllocations(t *testing.T) {
	membership := &leaseCountingMembership{fixedLocalLRCMembership: fixedLocalLRCMembership{generation: 1, federation: "fed", handle: 7}}
	fencer := newLocalLRCFrameFencer(membership, "fed", 7)
	ctx := context.Background()
	allocations := testing.AllocsPerRun(100, func() {
		lease, err := fencer.acquire(ctx)
		if err != nil {
			panic(err)
		}
		lease.end()
	})
	if allocations != 0 {
		t.Fatalf("allocations per disabled frame = %g, want 0", allocations)
	}
	if membership.acquired != 0 {
		t.Fatal("value lease fell back to closure tier")
	}
}

type checkpointWireRegistry struct {
	*stubObjectRegistry
	probe *checkpointProbe
	t     *testing.T
	err   error
}

func (r *checkpointWireRegistry) apply(name string) error {
	if r.probe.rw.TryLock() {
		r.probe.rw.Unlock()
		r.t.Error("wire fast path ran without admission")
	}
	r.probe.record(name)
	return r.err
}
func (r *checkpointWireRegistry) UpdateAttributesWire(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, map[uint64][]byte, *core.LogicalTime) error {
	return r.apply("wire-update")
}
func (r *checkpointWireRegistry) SendInteractionWire(context.Context, core.FederationName, core.FederateHandle, core.InteractionClassHandle, map[uint64][]byte, *core.LogicalTime) error {
	return r.apply("wire-interaction")
}

func TestCheckpointLocalLRCWireBatchReleasesBeforeSlowACK(t *testing.T) {
	probe := new(checkpointProbe)
	membership := &leaseCountingMembership{fixedLocalLRCMembership: fixedLocalLRCMembership{generation: 1, federation: "fed", handle: 7}}
	registry := &checkpointWireRegistry{stubObjectRegistry: &stubObjectRegistry{}, probe: probe, t: t}
	srv := &Server{checkpointAdmission: probe, membership: membership, objService: newObjectService(registry)}
	stream := &stalledAckLocalLRCServerStream{
		scriptedLocalLRCServerStream: scriptedLocalLRCServerStream{ctx: context.Background(), requests: []*rtiv1.LocalLRCRequest{
			localLRCOpenRequest(1, 2), localLRCBatchRequest(localLRCUpdateOperation(1, 3), localLRCInteractionOperation(2)),
		}}, stallOn: 2, stalled: make(chan struct{}), release: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() { done <- newLocalLRCService(srv).Exchange(stream) }()
	select {
	case <-stream.stalled:
	case err := <-done:
		t.Fatalf("stream ended before ACK: %v", err)
	case <-time.After(time.Second):
		close(stream.release)
		t.Fatal("stream did not reach ACK")
	}
	if !probe.rw.TryLock() {
		t.Error("checkpoint blocked by LocalLRC ACK")
	} else {
		probe.rw.Unlock()
	}
	if !membership.gate.TryLock() {
		t.Error("membership held across LocalLRC ACK")
	} else {
		membership.gate.Unlock()
	}
	close(stream.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	probe.assertSteps(t, "service:fed", "service-release", "service:fed", "wire-update", "wire-interaction", "service-release")
	if membership.leases != 1 {
		t.Fatalf("frame value leases = %d, want 1", membership.leases)
	}
	if len(registry.updCalls) != 0 || len(registry.sendCalls) != 0 {
		t.Fatal("wire fast path fell back to unary")
	}
}

func TestCheckpointLocalLRCWireFailureReleasesAdmission(t *testing.T) {
	probe := new(checkpointProbe)
	membership := &leaseCountingMembership{fixedLocalLRCMembership: fixedLocalLRCMembership{generation: 1, federation: "fed", handle: 7}}
	registry := &checkpointWireRegistry{stubObjectRegistry: &stubObjectRegistry{}, probe: probe, t: t, err: core.ErrObjectNotFound}
	srv := &Server{checkpointAdmission: probe, membership: membership, objService: newObjectService(registry)}
	stream := &scriptedLocalLRCServerStream{ctx: context.Background(), requests: []*rtiv1.LocalLRCRequest{
		localLRCOpenRequest(1, 2), localLRCBatchRequest(localLRCUpdateOperation(1, 3), localLRCInteractionOperation(2)),
	}}
	if err := newLocalLRCService(srv).Exchange(stream); err == nil {
		t.Fatal("wire failure was lost")
	}
	probe.assertSteps(t, "service:fed", "service-release", "service:fed", "wire-update", "service-release")
	if !probe.rw.TryLock() {
		t.Fatal("service admission leaked after wire failure")
	}
	probe.rw.Unlock()
	if !membership.gate.TryLock() {
		t.Fatal("membership leaked after wire failure")
	}
	membership.gate.Unlock()
	if len(stream.responses) != 1 {
		t.Fatal("failed operation was ACKed")
	}
}

func TestCheckpointStreamHandlerErrorReleasesBothLeases(t *testing.T) {
	probe := new(checkpointProbe)
	srv := &Server{checkpointAdmission: probe, membership: checkpointMembership{probe: probe}}
	base := &checkpointScriptStream{requests: []proto.Message{&rtiv1.SendInteractionRequest{FederationName: "fed", FederateHandle: 1}}}
	err := srv.StreamMembershipInterceptor()(nil, base, &stdgrpc.StreamServerInfo{FullMethod: interactionStreamMethod},
		func(_ any, stream stdgrpc.ServerStream) error {
			if err := stream.RecvMsg(new(rtiv1.SendInteractionRequest)); err != nil {
				return err
			}
			return io.ErrUnexpectedEOF
		})
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("error = %v", err)
	}
	probe.assertSteps(t, "service:fed", "member", "member-release", "service-release")
}
