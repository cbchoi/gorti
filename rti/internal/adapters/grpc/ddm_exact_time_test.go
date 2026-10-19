package grpc

import (
	"context"
	"reflect"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ddmExactCallOf[T core.LogicalTimeScalar] struct {
	at      *T
	params  map[core.ParameterHandle][]byte
	regions []core.DDMRegionHandleCore
	options core.ObjectMessageOptions
}

type ddmExactRecorderOf[T core.LogicalTimeScalar] struct {
	core.ObjectRegistryOf[T]
	calls chan ddmExactCallOf[T]
}

func (r *ddmExactRecorderOf[T]) SendInteractionWithRegions(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, params map[core.ParameterHandle][]byte, at *T, regions []core.DDMRegionHandleCore) error {
	return r.record(params, at, regions, core.ObjectMessageOptions{})
}

func (r *ddmExactRecorderOf[T]) record(params map[core.ParameterHandle][]byte, at *T, regions []core.DDMRegionHandleCore, options core.ObjectMessageOptions) error {
	if at != nil {
		cloned := *at
		at = &cloned
	}
	r.calls <- ddmExactCallOf[T]{at: at, params: params, regions: regions, options: options}
	return nil
}

type ddmExactMetadataRecorderOf[T core.LogicalTimeScalar] struct{ *ddmExactRecorderOf[T] }

func (r *ddmExactMetadataRecorderOf[T]) SendInteractionWithRegionsMetadata(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, params map[core.ParameterHandle][]byte, at *T, regions []core.DDMRegionHandleCore, options core.ObjectMessageOptions) error {
	return r.record(params, at, regions, options)
}

func runDDMExactTimeRPC[T core.LogicalTimeScalar](t *testing.T, cases []optionalExactTimeCase[T]) {
	t.Helper()
	for _, metadata := range []bool{false, true} {
		name := "fallback"
		if metadata {
			name = "metadata"
		}
		t.Run(name, func(t *testing.T) {
			recorder := &ddmExactRecorderOf[T]{calls: make(chan ddmExactCallOf[T], 1)}
			var objects core.ObjectRegistryOf[T] = recorder
			if metadata {
				objects = &ddmExactMetadataRecorderOf[T]{recorder}
			}
			conn := saveDDMExactRPCConn(t, func(server *grpcgo.Server) { rtiv1.RegisterDDMServiceServer(server, newDDMServiceOf[T](nil, objects)) })
			client := rtiv1.NewDDMServiceClient(conn)
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					options := core.ObjectMessageOptions{}
					if metadata {
						options = core.ObjectMessageOptions{Tag: []byte("tag"), RetractionHandle: 17}
					}
					_, err := client.SendInteractionWithRegions(ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, InteractionClassHandle: 3, Parameters: map[uint64][]byte{4: []byte("value")}, LogicalTime: test.legacy, LogicalTimeValue: test.exact, RegionHandles: []uint64{5, 6}, UserSuppliedTag: options.Tag, MessageRetractionHandle: options.RetractionHandle})
					if test.invalid {
						if status.Code(err) != codes.InvalidArgument {
							t.Fatalf("invalid timestamp returned %v", err)
						}
						if len(recorder.calls) != 0 {
							t.Fatal("invalid timestamp reached regional object mutation")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					call := <-recorder.calls
					if (call.at == nil) != (test.want == nil) || call.at != nil && *call.at != *test.want {
						t.Fatalf("timestamp changed: got %v want %v", call.at, test.want)
					}
					if !reflect.DeepEqual(call.params, map[core.ParameterHandle][]byte{4: []byte("value")}) || !reflect.DeepEqual(call.regions, []core.DDMRegionHandleCore{5, 6}) || !reflect.DeepEqual(call.options, options) {
						t.Fatalf("regional metadata changed: %+v", call)
					}
				})
			}
		})
	}
}

func TestDDMInteger64ExactTimeRPC(t *testing.T)     { runDDMExactTimeRPC(t, integerOptionalTimeCases()) }
func TestDDMFloat64LegacyExactTimeRPC(t *testing.T) { runDDMExactTimeRPC(t, floatOptionalTimeCases()) }

type ddmFloatOnlyMetadataRecorder struct {
	*ddmExactRecorderOf[int64]
}

func (*ddmFloatOnlyMetadataRecorder) SendInteractionWithRegionsMetadata(context.Context, core.FederationName, core.FederateHandle, core.InteractionClassHandle, map[core.ParameterHandle][]byte, *core.LogicalTime, []core.DDMRegionHandleCore, core.ObjectMessageOptions) error {
	panic("Integer64 request reached Float64 metadata method")
}

func TestDDMInteger64MetadataCapabilityFailsClosed(t *testing.T) {
	recorder := &ddmExactRecorderOf[int64]{calls: make(chan ddmExactCallOf[int64], 1)}
	conn := saveDDMExactRPCConn(t, func(server *grpcgo.Server) {
		rtiv1.RegisterDDMServiceServer(server, newDDMServiceOf[int64](nil, &ddmFloatOnlyMetadataRecorder{recorder}))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := rtiv1.NewDDMServiceClient(conn).SendInteractionWithRegions(ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, InteractionClassHandle: 3, LogicalTimeValue: integerTimeSelector(1<<53 + 1), RegionHandles: []uint64{5}, UserSuppliedTag: []byte("tag"), MessageRetractionHandle: 17})
	if status.Code(err) != codes.Unimplemented || len(recorder.calls) != 0 {
		t.Fatalf("metadata fell through to untyped or metadata-free path: %v", err)
	}
}
