package grpc

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type routingFactories map[core.FederationName]string

func (f routingFactories) LogicalTimeImplementationFor(name core.FederationName) (string, bool) {
	value, ok := f[name]
	return value, ok
}

func routingFixture(t *testing.T, value int64) (*Server, *typedAssemblyObjectsOf[core.LogicalTime], *typedAssemblyObjectsOf[int64], *typedAssemblyTimeOf[core.LogicalTime], *typedAssemblyTimeOf[int64], *savepointExactRecorderOf[int64]) {
	t.Helper()
	options, floats, floatTime, _ := typedAssemblyFixture(core.LogicalTime(3.5))
	integers, ints, intTime, saves := typedAssemblyFixture(value)
	options.Integer64 = &TypedBackends[int64]{Objects: integers.Objects, Time: integers.Time, Savepoint: integers.Savepoint}
	options.TimeImplementations = routingFactories{"float": float64TimeFactory, "int": integer64TimeFactory, "invalid": "unsupported"}
	server, err := NewServer(options)
	if err != nil {
		t.Fatal(err)
	}
	return server, floats, ints, floatTime, intTime, saves
}

func TestFederationUnaryRoutingExactBoundaries(t *testing.T) {
	for _, value := range []int64{0, 1<<53 + 1, math.MaxInt64} {
		t.Run(strconv.FormatInt(value, 10), func(t *testing.T) {
			server, floats, ints, floatTime, intTime, saves := routingFixture(t, value)
			conn := saveDDMExactRPCConn(t, func(gs *grpcgo.Server) {
				if err := server.Register(gs); err != nil {
					t.Fatal(err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			typed, _ := core.ScalarTimeValue(value)
			_, exact, err := wire.EncodeLogicalTimePair(core.ScalarRepresentation[int64](), typed)
			if err != nil {
				t.Fatal(err)
			}
			objects := rtiv1.NewObjectServiceClient(conn)
			_, err = objects.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, ObjectHandle: 9, LogicalTimeValue: exact})
			if err != nil {
				t.Fatal(err)
			}
			if got := <-ints.updates; got == nil || *got != value {
				t.Fatal("integer update lost precision")
			}
			floatValue := 3.5
			_, err = objects.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: "float", FederateHandle: 1, ObjectHandle: 9, LogicalTime: &floatValue})
			if err != nil {
				t.Fatal(err)
			}
			if got := <-floats.updates; got == nil || *got != 3.5 {
				t.Fatal("Float64 legacy update changed")
			}
			times := rtiv1.NewTimeServiceClient(conn)
			_, err = times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, LogicalTimeValue: exact})
			if err != nil {
				t.Fatal(err)
			}
			if got := <-intTime.requests; got != value {
				t.Fatal("integer TAR changed")
			}
			for _, fed := range []string{"int", "float"} {
				response, err := times.QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: fed, FederateHandle: 1})
				if err != nil {
					t.Fatal(err)
				}
				if fed == "int" {
					decoded, err := wire.DecodeLogicalTimePair(core.ScalarRepresentation[int64](), response.LogicalTime, response.LogicalTimeValue)
					if err != nil || decoded != typed {
						t.Fatalf("exact integer query: %v %v", decoded, err)
					}
				} else if response.LogicalTime != 3.5 {
					t.Fatal("wrong Float64 query backend")
				}
			}
			_, err = rtiv1.NewDDMServiceClient(conn).SendInteractionWithRegions(ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, InteractionClassHandle: 3, LogicalTimeValue: exact, RegionHandles: []uint64{5}})
			if err != nil {
				t.Fatal(err)
			}
			if got := (<-ints.calls).at; got == nil || *got != value {
				t.Fatal("wrong DDM timestamp")
			}
			_, err = rtiv1.NewSavepointServiceClient(conn).RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, Label: "save", SaveTimeValue: exact})
			if err != nil {
				t.Fatal(err)
			}
			if got := <-saves.calls; got == nil || *got != value {
				t.Fatal("wrong save time")
			}
			for _, test := range []struct {
				fed    string
				legacy *float64
				typed  *rtiv1.LogicalTimeValue
				code   codes.Code
			}{
				{"missing", nil, exact, codes.NotFound},
				{"invalid", nil, exact, codes.FailedPrecondition},
				{"int", &floatValue, exact, codes.InvalidArgument},
				{"int", &floatValue, nil, codes.InvalidArgument},
				{"float", nil, exact, codes.InvalidArgument},
			} {
				_, err = objects.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: test.fed, FederateHandle: 1, ObjectHandle: 9, LogicalTime: test.legacy, LogicalTimeValue: test.typed})
				if status.Code(err) != test.code {
					t.Fatalf("%s: expected %s, got %v", test.fed, test.code, err)
				}
			}
			if len(floats.updates)+len(ints.updates)+len(floatTime.requests)+len(intTime.requests)+len(saves.calls) != 0 {
				t.Fatal("rejected request changed a backend")
			}
		})
	}
}

func TestFederationRoutingRejectsIntegerOptimizedStreams(t *testing.T) {
	server, floats, ints, _, _, _ := routingFixture(t, 1<<53+1)
	conn := saveDDMExactRPCConn(t, func(gs *grpcgo.Server) {
		if err := server.Register(gs); err != nil {
			t.Fatal(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	confirmed, err := rtiv1.NewConfirmedObjectServiceClient(conn).Exchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := confirmed.Send(&rtiv1.ConfirmedObjectRequest{Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: &rtiv1.ConfirmedObjectOpen{WireVersion: 1, FederationName: "int", FederateHandle: 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := confirmed.Recv(); status.Code(err) != codes.Unimplemented {
		t.Fatalf("integer confirmed stream: %v", err)
	}
	lrc, err := rtiv1.NewLocalLRCServiceClient(conn).Exchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := lrc.Send(&rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Open{Open: &rtiv1.LocalLRCOpen{WireVersion: 1, FederationName: "int", FederateHandle: 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := lrc.Recv(); status.Code(err) != codes.Unimplemented {
		t.Fatalf("integer LocalLRC stream: %v", err)
	}
	interaction, err := conn.NewStream(ctx, &grpcgo.StreamDesc{ClientStreams: true, ServerStreams: true}, interactionStreamMethod)
	if err != nil {
		t.Fatal(err)
	}
	if err := interaction.SendMsg(&rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, InteractionClassHandle: 3}); err != nil {
		t.Fatal(err)
	}
	if err := interaction.RecvMsg(&rtiv1.Empty{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("integer interaction stream: %v", err)
	}
	if len(floats.updates)+len(ints.updates)+len(floats.calls)+len(ints.calls) != 0 {
		t.Fatal("unsupported stream reached object backend")
	}
}

func TestFederationRoutingRequiredBackends(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Options)
		want   error
	}{
		{"objects", func(o *Options) { o.Integer64.Objects = nil }, ErrObjectsRequired},
		{"time", func(o *Options) { o.Integer64.Time = nil }, ErrInteger64TimeRequired},
		{"factory-source", func(o *Options) { o.TimeImplementations = nil }, ErrTimeImplementationsRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, _, _, _ := typedAssemblyFixture(core.LogicalTime(0))
			integer, _, _, _ := typedAssemblyFixture[int64](0)
			options.Integer64 = &TypedBackends[int64]{Objects: integer.Objects, Time: integer.Time}
			options.TimeImplementations = routingFactories{"int": integer64TimeFactory}
			test.change(&options)
			server, err := NewServer(options)
			if server != nil || !errors.Is(err, test.want) {
				t.Fatalf("incoherent backend admitted: %v %v", server, err)
			}
		})
	}
}

func TestFederationRoutingOptionalSaveFailsClosed(t *testing.T) {
	options, _, _, floatSaves := typedAssemblyFixture(core.LogicalTime(0))
	integer, _, _, _ := typedAssemblyFixture[int64](0)
	options.Integer64 = &TypedBackends[int64]{Objects: integer.Objects, Time: integer.Time}
	options.TimeImplementations = routingFactories{"float": float64TimeFactory, "int": integer64TimeFactory}
	server, err := NewServer(options)
	if err != nil {
		t.Fatal(err)
	}
	conn := saveDDMExactRPCConn(t, func(gs *grpcgo.Server) {
		if err := server.Register(gs); err != nil {
			t.Fatal(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := rtiv1.NewSavepointServiceClient(conn)
	request := &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, Label: "save"}
	if _, err := client.RequestFederationSave(ctx, request); status.Code(err) != codes.Unimplemented {
		t.Fatalf("missing integer save fell back: %v", err)
	}
	if len(floatSaves.calls) != 0 {
		t.Fatal("integer request changed Float64 save")
	}
	request.FederationName = "float"
	if _, err := client.RequestFederationSave(ctx, request); err != nil {
		t.Fatal(err)
	}
	if got := <-floatSaves.calls; got != nil {
		t.Fatal("untimed Float64 save changed")
	}
}

func TestFederationRoutingRetainsFloatOptimizedTransport(t *testing.T) {
	server, floats, ints, _, _, _ := routingFixture(t, 0)
	server.membership = &fixedLocalLRCMembership{federation: "float", handle: 1}
	conn := saveDDMExactRPCConn(t, func(gs *grpcgo.Server) {
		if err := server.Register(gs); err != nil {
			t.Fatal(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := rtiv1.NewConfirmedObjectServiceClient(conn).Exchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&rtiv1.ConfirmedObjectRequest{Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: &rtiv1.ConfirmedObjectOpen{WireVersion: 1, FederationName: "float", FederateHandle: 1}}}); err != nil {
		t.Fatal(err)
	}
	if opened, err := stream.Recv(); err != nil || opened.GetSequence() != 0 {
		t.Fatalf("Float64 open changed: %v %v", opened, err)
	}
	if err := stream.Send(&rtiv1.ConfirmedObjectRequest{Sequence: 1, Operation: &rtiv1.ConfirmedObjectRequest_AttributeUpdate{AttributeUpdate: &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: "float", FederateHandle: 1, ObjectHandle: 9}}}); err != nil {
		t.Fatal(err)
	}
	if ack, err := stream.Recv(); err != nil || ack.GetSequence() != 1 || len(ack.GetErrorStatus()) != 0 {
		t.Fatalf("Float64 mutation changed: %v %v", ack, err)
	}
	if got := <-floats.updates; got != nil || len(ints.updates) != 0 {
		t.Fatal("Float64 RO update misrouted")
	}
	lrc, err := rtiv1.NewLocalLRCServiceClient(conn).Exchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := lrc.Send(&rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Open{Open: &rtiv1.LocalLRCOpen{WireVersion: 1, FederationName: "float", FederateHandle: 1}}}); err != nil {
		t.Fatal(err)
	}
	if ack, err := lrc.Recv(); err != nil || ack.GetMaxBatchOperations() == 0 {
		t.Fatalf("Float64 LocalLRC capability changed: %v %v", ack, err)
	}
}

type routingCreationRecorder struct {
	stubFedStoreForServerTest
	calls chan core.CreateFederationRequest
}

func (f *routingCreationRecorder) CreateFederation(_ context.Context, req core.CreateFederationRequest) error {
	f.calls <- req
	return nil
}

func TestFederationRoutingFactoryCapabilityIsExplicit(t *testing.T) {
	options, _, _, _ := typedAssemblyFixture(core.LogicalTime(0))
	recorder := &routingCreationRecorder{calls: make(chan core.CreateFederationRequest, 1)}
	options.Federations = recorder
	integer, _, _, _ := typedAssemblyFixture[int64](0)
	options.Integer64 = &TypedBackends[int64]{Objects: integer.Objects, Time: integer.Time}
	options.TimeImplementations = routingFactories{"int": integer64TimeFactory}
	request := &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "int", LogicalTimeImplementationName: integer64TimeFactory}
	server, err := NewServer(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.fedService.CreateFederation(context.Background(), request); err == nil || len(recorder.calls) != 0 {
		t.Fatal("default creation enabled Integer64")
	}
	options.ResolveTimeImplementation = func(name string) (string, error) {
		if name == integer64TimeFactory {
			return name, nil
		}
		return core.ResolveLogicalTimeImplementationName(name)
	}
	server, err = NewServer(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.fedService.CreateFederation(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got := <-recorder.calls; got.LogicalTimeImplementationName != integer64TimeFactory {
		t.Fatal("explicit factory capability lost")
	}
}
