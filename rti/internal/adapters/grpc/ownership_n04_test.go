package grpc

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestN04OwnershipWireAdditions(t *testing.T) {
	confirm := (&rtiv1.ConfirmDivestitureRequest{}).ProtoReflect().Descriptor().Fields().ByName("tag")
	if confirm == nil || confirm.Number() != 6 {
		t.Error("missing confirmation tag field 6")
	}
	svc := rtiv1.File_rti_v1_ownership_proto.Services().ByName("OwnershipService")
	legacy := svc.Methods().ByName("AttributeOwnershipDivestitureIfWanted")
	if legacy == nil || legacy.Output().FullName() != "rti.v1.Empty" {
		t.Error("legacy divest RPC changed")
	}
	result := svc.Methods().ByName("AttributeOwnershipDivestitureIfWantedWithResult")
	if result == nil {
		t.Error("missing result RPC")
	}
}

type n04HistoricalCoordinator struct {
	core.OwnershipCoordinator
}

func (n04HistoricalCoordinator) DivestIfWantedWithResult(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, error) {
	return []core.AttributeHandle{9}, nil
}

func (n04HistoricalCoordinator) QueryOwnership(core.FederationName, core.ObjectHandle, core.AttributeHandle) (core.FederateHandle, bool) {
	panic("historical result must not query current ownership")
}

func TestN04OwnershipRPCReturnsExactOperationResult(t *testing.T) {
	s := newOwnershipService(n04HistoricalCoordinator{})
	resp, err := s.AttributeOwnershipDivestitureIfWantedWithResult(context.Background(), &rtiv1.DivestIfWantedRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", FederateHandle: 1,
		ObjectHandle: 100, AttributeHandles: []uint64{9, 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp.DivestedAttributeHandles, []uint64{9}) {
		t.Fatalf("result = %v", resp)
	}
}

func TestN04OwnershipCallbackWireBinaryTag(t *testing.T) {
	tag := []byte{0, 0x80, 0xff}
	wire, err := toFederateEvent(&ownership.CallbackEvent{Kind: ownership.CallbackAcquisitionNotification, Object: 100, Tag: tag, Federate: 2})
	if err != nil {
		t.Fatal(err)
	}
	msg := wire.GetOwnershipAcquired().ProtoReflect()
	field := msg.Descriptor().Fields().ByName("tag")
	if field == nil {
		t.Fatal("missing acquisition callback tag")
	}
	if !bytes.Equal(msg.Get(field).Bytes(), tag) {
		t.Fatalf("tag = %x", msg.Get(field).Bytes())
	}
	tag[0] = 42
	if msg.Get(field).Bytes()[0] != 0 {
		t.Fatal("wire aliases domain tag")
	}
}
