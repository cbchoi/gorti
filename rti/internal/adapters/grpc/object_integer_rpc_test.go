package grpc

import (
	"context"
	"math"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	objectpkg "github.com/cbchoi/gorti/rti/internal/hla/object"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type objectRPCJournal struct {
	mu      sync.Mutex
	records []core.EventRecord
}

func (j *objectRPCJournal) Append(_ context.Context, _ core.FederationName, record core.EventRecord) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.records = append(j.records, record)
	return nil
}
func (*objectRPCJournal) Sync(context.Context, core.FederationName) error { return nil }
func (*objectRPCJournal) OpenReader(context.Context, string) (core.EventLogReader, error) {
	return nil, nil
}
func (j *objectRPCJournal) last() core.EventRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.records[len(j.records)-1]
}

func (j *objectRPCJournal) count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.records)
}

func TestIntegerObjectRPCRoundTrip(t *testing.T) {
	objectRPCRoundTrip[int64](t, []int64{0, 1<<53 + 1, 1<<53 + 2, math.MaxInt64})
}

func TestFloatObjectRPCRoundTrip(t *testing.T) {
	objectRPCRoundTrip[core.LogicalTime](t, []core.LogicalTime{0, 3.25})
}

// Register the concrete production handlers directly, without activating the
// parent-owned federation selector. Both registry and stream are real.
func objectRPCRoundTrip[T core.LogicalTimeScalar](t *testing.T, timestamps []T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const fed core.FederationName = "object-exact-rpc"
	repo := adminFOMs{}
	fom, err := repo.Get(ctx, fed)
	check(err)
	cls, _ := fom.LookupObjectClass("Thing")
	attr, _ := fom.LookupAttribute(cls, "value")
	interaction, _ := fom.LookupInteractionClass("Signal")
	parameter, _ := fom.LookupParameter(interaction, "value")
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	check(decl.PublishObjectClassAttributes(ctx, fed, 1, cls, []core.AttributeHandle{attr}))
	check(decl.SubscribeObjectClassAttributes(ctx, fed, 2, cls, []core.AttributeHandle{attr}))
	check(decl.PublishInteractionClass(ctx, fed, 1, interaction))
	check(decl.SubscribeInteractionClass(ctx, fed, 2, interaction))
	out := delivery.NewWithBatch(128, 1, 0)
	journal := &objectRPCJournal{}
	registry, err := objectpkg.NewOf[T](objectpkg.OptionsOf[T]{Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: repo, Declarations: decl, Outbox: out, EventLog: journal})
	check(err)
	server := grpcgo.NewServer()
	rtiv1.RegisterObjectServiceServer(server, newObjectServiceOf[T](registry))
	rtiv1.RegisterStreamServiceServer(server, newStreamService(out))
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///object-exact", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	check(err)
	t.Cleanup(func() { _ = conn.Close() })
	client := rtiv1.NewObjectServiceClient(conn)
	stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(ctx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: 2})
	check(err)
	ready, err := stream.Recv()
	check(err)
	if !ready.Ready {
		t.Fatal("callback stream did not become ready")
	}
	var pending []*rtiv1.FederateEvent
	receive := func() *rtiv1.FederateEvent {
		t.Helper()
		for {
			if len(pending) == 0 {
				batch, err := stream.Recv()
				check(err)
				pending = batch.Events
			}
			if len(pending) == 0 {
				t.Fatal("empty callback batch")
			}
			event := pending[0]
			pending = pending[1:]
			if scope := event.GetAttributesInScope(); scope != nil {
				if event.Seq == 0 || !reflect.DeepEqual(scope.AttributeHandles, []uint64{uint64(attr)}) {
					t.Fatalf("bad scope advisory: %v", event)
				}
				continue
			}
			return event
		}
	}
	tag := []byte{0, 255, 42}
	for _, timestamp := range timestamps {
		registered, err := client.RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: 1, ObjectClassHandle: uint64(cls)})
		check(err)
		if receive().GetDiscover().GetObjectHandle() != registered.ObjectHandle {
			t.Fatal("missing real discovery")
		}
		// Malformed wire time must not reach the real registry/journal.
		wrong := integerObjectTime(1)
		if core.ScalarRepresentation[T]() == core.ScalarRepresentation[int64]() {
			wrong = &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: 1}}
		}
		before := journal.count()
		_, err = client.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: 1, ObjectHandle: registered.ObjectHandle, Attributes: map[uint64][]byte{uint64(attr): {9}}, LogicalTimeValue: wrong})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("update accepted wrong representation: %v", err)
		}
		_, err = client.SendInteraction(ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: 1, InteractionClassHandle: uint64(interaction), Parameters: map[uint64][]byte{uint64(parameter): {9}}, LogicalTimeValue: wrong})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("interaction accepted wrong representation: %v", err)
		}
		_, err = client.DeleteObjectInstance(ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: 1, ObjectHandle: registered.ObjectHandle, LogicalTimeValue: wrong})
		if status.Code(err) != codes.InvalidArgument || journal.count() != before {
			t.Fatalf("invalid RPC changed registry/journal: %v", err)
		}
		// Absence must remain receive order even in an Integer64 execution.
		_, err = client.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: 1, ObjectHandle: registered.ObjectHandle, Attributes: map[uint64][]byte{uint64(attr): {5}}})
		check(err)
		ro := receive().GetReflect()
		if ro == nil || ro.LogicalTime != nil || ro.LogicalTimeValue != nil {
			t.Fatalf("RO acquired a timestamp: %v", ro)
		}
		legacy, exact, err := encodeOptionalObjectTime(&timestamp)
		check(err)
		for _, operation := range []string{"update", "interaction", "delete"} {
			var callback *rtiv1.FederateEvent
			var actualLegacy *float64
			var actualExact *rtiv1.LogicalTimeValue
			var actualTag []byte
			var metadata *rtiv1.ObjectCallbackMetadata
			switch operation {
			case "update":
				_, err = client.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: 1, ObjectHandle: registered.ObjectHandle, Attributes: map[uint64][]byte{uint64(attr): {7}}, LogicalTime: legacy, LogicalTimeValue: exact, UserSuppliedTag: tag})
				check(err)
				callback = receive()
				value := callback.GetReflect()
				if value == nil || value.ObjectHandle != registered.ObjectHandle || !reflect.DeepEqual(value.Attributes[uint64(attr)], []byte{7}) {
					t.Fatalf("bad reflect: %v", callback)
				}
				actualLegacy, actualExact, actualTag, metadata = value.LogicalTime, value.LogicalTimeValue, value.UserSuppliedTag, value.Metadata
			case "interaction":
				_, err = client.SendInteraction(ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: 1, InteractionClassHandle: uint64(interaction), Parameters: map[uint64][]byte{uint64(parameter): {8}}, LogicalTime: legacy, LogicalTimeValue: exact, UserSuppliedTag: tag})
				check(err)
				callback = receive()
				value := callback.GetReceive()
				if value == nil || value.InteractionClassHandle != uint64(interaction) || !reflect.DeepEqual(value.Parameters[uint64(parameter)], []byte{8}) {
					t.Fatalf("bad interaction: %v", callback)
				}
				actualLegacy, actualExact, actualTag, metadata = value.LogicalTime, value.LogicalTimeValue, value.UserSuppliedTag, value.Metadata
			case "delete":
				_, err = client.DeleteObjectInstance(ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: 1, ObjectHandle: registered.ObjectHandle, LogicalTime: legacy, LogicalTimeValue: exact, UserSuppliedTag: tag})
				check(err)
				callback = receive()
				value := callback.GetRemove()
				if value == nil || value.ObjectHandle != registered.ObjectHandle {
					t.Fatalf("bad removal: %v", callback)
				}
				actualLegacy, actualExact, actualTag, metadata = value.LogicalTime, value.LogicalTimeValue, value.UserSuppliedTag, value.Metadata
			}
			got, err := wire.DecodeOptionalLogicalTimePair(core.ScalarRepresentation[T](), actualLegacy, actualExact)
			check(err)
			want, err := core.ScalarTimeValue(timestamp)
			check(err)
			if got == nil || *got != want || !reflect.DeepEqual(actualTag, tag) || metadata.GetProducingFederate() != 1 || metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP || callback.Seq == 0 {
				t.Fatalf("%s callback lost exact time/metadata: %v", operation, callback)
			}
			if core.ScalarRepresentation[T]() == core.ScalarRepresentation[int64]() && actualLegacy != nil {
				t.Fatal("Integer64 callback populated a legacy double")
			}
			record, ok := journal.last().(*core.ObjectJournalRecordOf[T])
			if !ok || record.LogicalTime == nil || *record.LogicalTime != timestamp {
				t.Fatalf("ingress journal lost exact time: %+v", journal.last())
			}
		}
	}
}
