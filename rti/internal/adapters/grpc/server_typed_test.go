package grpc

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type typedAssemblyObjectsOf[T core.LogicalTimeScalar] struct {
	*ddmExactRecorderOf[T]
	updates chan *T
}

func (o *typedAssemblyObjectsOf[T]) UpdateAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, at *T) error {
	if at != nil {
		cloned := *at
		at = &cloned
	}
	o.updates <- at
	return nil
}

type typedAssemblyTimeOf[T core.LogicalTimeScalar] struct {
	core.TimeManagerOf[T]
	value    T
	requests chan T
}

func (m *typedAssemblyTimeOf[T]) TimeAdvanceRequest(_ context.Context, _ core.FederationName, _ core.FederateHandle, at T) error {
	m.requests <- at
	return nil
}

func (m *typedAssemblyTimeOf[T]) Snapshot(core.FederationName) core.TimeSnapshotOf[T] {
	return core.TimeSnapshotOf[T]{Federates: []core.TimeFederateStateOf[T]{{Handle: 1, CurrentTime: m.value}}}
}

func typedAssemblyFixture[T core.LogicalTimeScalar](value T) (OptionsOf[T], *typedAssemblyObjectsOf[T], *typedAssemblyTimeOf[T], *savepointExactRecorderOf[T]) {
	objects := &typedAssemblyObjectsOf[T]{ddmExactRecorderOf: &ddmExactRecorderOf[T]{calls: make(chan ddmExactCallOf[T], 1)}, updates: make(chan *T, 1)}
	manager := &typedAssemblyTimeOf[T]{value: value, requests: make(chan T, 1)}
	saves := &savepointExactRecorderOf[T]{calls: make(chan *T, 1)}
	options := OptionsOf[T]{Federations: stubFedStoreForServerTest{}, Declarations: declaration.New(), Objects: objects, Time: manager, Outbox: stubOutboxForServerTest{}, Savepoint: saves, DDM: struct {
		core.DataDistributionManagement
	}{}, EnableInteractionStream: true, EnableConfirmedObjectStream: true, EnableLocalLRC: true}
	if _, canonicalFloat := any(value).(core.LogicalTime); !canonicalFloat {
		options.EnableInteractionStream = false
		options.EnableConfirmedObjectStream = false
		options.EnableLocalLRC = false
	}
	return options, objects, manager, saves
}

func runTypedServerExactUnary[T core.LogicalTimeScalar](t *testing.T, value T) {
	t.Helper()
	options, objects, manager, saves := typedAssemblyFixture(value)
	server, err := NewServerOf(options)
	if err != nil {
		t.Fatal(err)
	}
	conn := saveDDMExactRPCConn(t, func(grpcServer *grpcgo.Server) {
		if err := server.Register(grpcServer); err != nil {
			t.Fatal(err)
		}
		info := grpcServer.GetServiceInfo()
		for _, name := range []string{"rti.v1.ObjectService", "rti.v1.TimeService", "rti.v1.DDMService", "rti.v1.SavepointService", "rti.v1.StreamService"} {
			if _, exists := info[name]; !exists {
				t.Fatalf("missing typed service %s", name)
			}
		}
		_, isCanonicalFloat := any(value).(core.LogicalTime)
		for _, name := range []string{"rti.v1.ConfirmedObjectService", "rti.v1.LocalLRCService", "rti.v1.InteractionStreamService"} {
			_, exists := info[name]
			if exists != isCanonicalFloat {
				t.Fatalf("incorrect optimization availability %s: %v", name, exists)
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	typed, err := core.ScalarTimeValue(value)
	if err != nil {
		t.Fatal(err)
	}
	legacy, exact, err := wire.EncodeOptionalLogicalTimePair(core.ScalarRepresentation[T](), &typed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rtiv1.NewObjectServiceClient(conn).UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: 9, LogicalTime: legacy, LogicalTimeValue: exact})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-objects.updates; got == nil || *got != value {
		t.Fatal("typed object timestamp changed")
	}
	requiredLegacy, requiredExact, err := wire.EncodeLogicalTimePair(core.ScalarRepresentation[T](), typed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rtiv1.NewTimeServiceClient(conn).TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, LogicalTime: requiredLegacy, LogicalTimeValue: requiredExact})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-manager.requests; got != value {
		t.Fatalf("typed advance changed: %v", got)
	}
	response, err := rtiv1.NewTimeServiceClient(conn).QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := wire.DecodeLogicalTimePair(core.ScalarRepresentation[T](), response.LogicalTime, response.LogicalTimeValue)
	if err != nil || decoded != typed {
		t.Fatalf("typed query changed: %v %v", decoded, err)
	}
	_, err = rtiv1.NewDDMServiceClient(conn).SendInteractionWithRegions(ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, InteractionClassHandle: 3, LogicalTime: legacy, LogicalTimeValue: exact, RegionHandles: []uint64{5}})
	if err != nil {
		t.Fatal(err)
	}
	if got := (<-objects.calls).at; got == nil || *got != value {
		t.Fatal("typed regional timestamp changed")
	}
	_, err = rtiv1.NewSavepointServiceClient(conn).RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, Label: "save", SaveTime: legacy, SaveTimeValue: exact})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-saves.calls; got == nil || *got != value {
		t.Fatal("typed save timestamp changed")
	}
	if _, isInteger := any(value).(int64); isInteger {
		zero := 0.0
		_, err = rtiv1.NewObjectServiceClient(conn).UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: 9, LogicalTime: &zero, LogicalTimeValue: exact})
		if status.Code(err) != codes.InvalidArgument || len(objects.updates) != 0 {
			t.Fatalf("assembled integer service accepted legacy zero: %v", err)
		}
	}
}

func TestNewServerOfInteger64ExactUnary(t *testing.T) {
	for _, value := range []int64{0, 1<<53 + 1, math.MaxInt64} {
		t.Run(strconv.FormatInt(value, 10), func(t *testing.T) { runTypedServerExactUnary(t, value) })
	}
}

func TestNewServerOfFloat64ExactUnary(t *testing.T) {
	runTypedServerExactUnary(t, core.LogicalTime(3.5))
}

func TestNewServerOfPreservesOwnershipReceiptPlumbing(t *testing.T) {
	options, objects, _, _ := typedAssemblyFixture[int64](1<<53 + 1)
	options.Sessions = &session.Manager{}
	options.CallbackReceipts = &CallbackReceipts{}
	options.OwnershipReceipts = &OwnershipCallbackReceipts{}
	options.Ownership = struct{ core.OwnershipCoordinator }{}
	server, err := NewServerOf(options)
	if err != nil {
		t.Fatal(err)
	}
	if server.fedService.sessions != options.Sessions || server.streamService.sessions != options.Sessions || server.streamService.receipts != options.CallbackReceipts || server.streamService.ownershipReceipts != options.OwnershipReceipts || server.ownershipCallbackService == nil {
		t.Fatal("receipt/session plumbing lost")
	}
	if server.ownershipService == nil || any(server.ownershipService.objects) != any(objects) {
		t.Fatal("typed ownership knowledge binding lost")
	}
	grpcServer := grpcgo.NewServer()
	defer grpcServer.Stop()
	if err := server.Register(grpcServer); err != nil {
		t.Fatal(err)
	}
	if _, ok := grpcServer.GetServiceInfo()["rti.v1.OwnershipCallbackService"]; !ok {
		t.Fatal("mandatory receipt service missing")
	}
}

func TestNewServerOfRequiredOptions(t *testing.T) {
	for _, test := range []struct {
		name   string
		remove func(*OptionsOf[int64])
		want   error
	}{
		{"federations", func(o *OptionsOf[int64]) { o.Federations = nil }, ErrFederationsRequired},
		{"declarations", func(o *OptionsOf[int64]) { o.Declarations = nil }, ErrDeclarationsRequired},
		{"objects", func(o *OptionsOf[int64]) { o.Objects = nil }, ErrObjectsRequired},
		{"outbox", func(o *OptionsOf[int64]) { o.Outbox = nil }, ErrOutboxRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, _, _, _ := typedAssemblyFixture[int64](0)
			test.remove(&options)
			server, err := NewServerOf(options)
			if server != nil || !errors.Is(err, test.want) {
				t.Fatalf("required option not rejected: %v %v", server, err)
			}
		})
	}
}

func TestNewServerOfRejectsInteger64Optimizations(t *testing.T) {
	for _, test := range []struct {
		name   string
		enable func(*OptionsOf[int64])
	}{
		{"interaction", func(o *OptionsOf[int64]) { o.EnableInteractionStream = true }},
		{"confirmed", func(o *OptionsOf[int64]) { o.EnableConfirmedObjectStream = true }},
		{"local-lrc", func(o *OptionsOf[int64]) { o.EnableLocalLRC = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, _, _, _ := typedAssemblyFixture[int64](1<<53 + 1)
			test.enable(&options)
			server, err := NewServerOf(options)
			if server != nil || !errors.Is(err, ErrUnsupportedTimeOptimization) {
				t.Fatalf("unsupported optimization not rejected: %v %v", server, err)
			}
		})
	}
}

func TestNewServerLegacyDelegatesTypedFloatAssembly(t *testing.T) {
	options, _, _, _ := typedAssemblyFixture(core.LogicalTime(3.5))
	// Options is an alias, not an embedded-options migration for legacy callers.
	var legacyOptions Options = options
	server, err := NewServer(legacyOptions)
	if err != nil {
		t.Fatal(err)
	}
	if server.objService == nil || server.objectBackend != server.objService || server.localLRCService == nil || server.confirmedObjectService == nil {
		t.Fatal("legacy Float64 assembly/fast paths lost")
	}
}
