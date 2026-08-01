package federate

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

const objectSurfaceFOM = `<?xml version="1.0" encoding="UTF-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>object-surface</name><type>FOM</type></modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name>
        <sharing>PublishSubscribe</sharing>
        <attribute>
          <name>Position</name>
          <dataType>HLAfloat64BE</dataType>
          <updateType>Periodic</updateType>
          <ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation>
          <order>TimeStamp</order>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass><name>HLAinteractionRoot</name></interactionClass>
  </interactions>
</objectModel>`

type captureDeclarationClient struct {
	rtiv1.DeclarationServiceClient
	publish   *rtiv1.PubObjAttrsRequest
	subscribe *rtiv1.SubObjAttrsRequest
}

func (c *captureDeclarationClient) PublishObjectClassAttributes(
	_ context.Context, in *rtiv1.PubObjAttrsRequest, _ ...grpc.CallOption,
) (*rtiv1.Empty, error) {
	c.publish = in
	return &rtiv1.Empty{}, nil
}

func (c *captureDeclarationClient) SubscribeObjectClassAttributes(
	_ context.Context, in *rtiv1.SubObjAttrsRequest, _ ...grpc.CallOption,
) (*rtiv1.Empty, error) {
	c.subscribe = in
	return &rtiv1.Empty{}, nil
}

type captureObjectClient struct {
	rtiv1.ObjectServiceClient
	reserve  *rtiv1.ReserveObjectInstanceNameRequest
	register *rtiv1.RegisterObjectRequest
	update   *rtiv1.UpdateAttributeValuesRequest
}

func (c *captureObjectClient) ReserveObjectInstanceName(
	_ context.Context, in *rtiv1.ReserveObjectInstanceNameRequest, _ ...grpc.CallOption,
) (*rtiv1.Empty, error) {
	c.reserve = in
	return &rtiv1.Empty{}, nil
}

func (c *captureObjectClient) RegisterObjectInstance(
	_ context.Context, in *rtiv1.RegisterObjectRequest, _ ...grpc.CallOption,
) (*rtiv1.RegisterObjectResponse, error) {
	c.register = in
	return &rtiv1.RegisterObjectResponse{ObjectHandle: 42, ObjectName: in.GetObjectName()}, nil
}

func (c *captureObjectClient) UpdateAttributeValues(
	_ context.Context, in *rtiv1.UpdateAttributeValuesRequest, _ ...grpc.CallOption,
) (*rtiv1.Empty, error) {
	c.update = in
	return &rtiv1.Empty{}, nil
}

func objectSurfaceTables() *fomTables {
	return &fomTables{
		objectByName:   map[string]uint64{"Vehicle": 7},
		attrByName:     map[string]map[string]uint64{"Vehicle": {"Position": 2, "Speed": 3}},
		objectByHandle: map[uint64]string{7: "Vehicle"},
		attrByHandle:   map[uint64]map[uint64]string{7: {2: "Position", 3: "Speed"}},
	}
}

func TestBuildFOMTablesResolvesObjectAttributes(t *testing.T) {
	tables, err := buildFOMTables([]fomParseModule{{Path: "objects.xml", XML: []byte(objectSurfaceFOM)}})
	if err != nil {
		t.Fatalf("buildFOMTables: %v", err)
	}
	classHandle, found := tables.objectClassHandle("Vehicle")
	if !found {
		t.Fatal("Vehicle object class was not indexed")
	}
	attributeHandle, found := tables.attributeHandle("Vehicle", "Position")
	if !found {
		t.Fatal("Vehicle.Position attribute was not indexed")
	}
	if got, ok := tables.objectClassName(classHandle); !ok || got != "Vehicle" {
		t.Fatalf("objectClassName(%d) = (%q, %v), want (Vehicle, true)", classHandle, got, ok)
	}
	if got, ok := tables.attributeName(classHandle, attributeHandle); !ok || got != "Position" {
		t.Fatalf("attributeName(%d, %d) = (%q, %v), want (Position, true)", classHandle, attributeHandle, got, ok)
	}
}

func TestObjectHandleLookups(t *testing.T) {
	fed := &Federate{handles: objectSurfaceTables()}
	classHandle, found := fed.ObjectClassHandle("Vehicle")
	if !found || classHandle != 7 {
		t.Fatalf("ObjectClassHandle(Vehicle) = (%d, %v), want (7, true)", classHandle, found)
	}
	attributeHandle, found := fed.AttributeHandle(classHandle, "Position")
	if !found || attributeHandle != 2 {
		t.Fatalf("AttributeHandle(7, Position) = (%d, %v), want (2, true)", attributeHandle, found)
	}
	if handle, ok := fed.AttributeHandle(999, "Position"); ok || handle != 0 {
		t.Fatalf("AttributeHandle(999, Position) = (%d, %v), want (0, false)", handle, ok)
	}
}

func TestReserveObjectInstanceNameAndCallbacks(t *testing.T) {
	client := &captureObjectClient{}
	fed := &Federate{
		conn:           &Connection{obj: client},
		federationName: "fair-comparison",
		federateHandle: 9,
	}
	if err := fed.ReserveObjectInstanceName(context.Background(), "VerifierEntity-1"); err != nil {
		t.Fatal(err)
	}
	if client.reserve.GetFederationName() != "fair-comparison" ||
		client.reserve.GetFederateHandle() != 9 ||
		client.reserve.GetObjectName() != "VerifierEntity-1" {
		t.Fatalf("reserve request = %#v", client.reserve)
	}

	succeeded, ok := fed.translate(&rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_ReservationSucceeded{
			ReservationSucceeded: &rtiv1.ObjectInstanceNameReservationSucceeded{ObjectName: "VerifierEntity-1"},
		},
	}).(ObjectInstanceNameReservationSucceeded)
	if !ok || succeeded.ObjectName != "VerifierEntity-1" {
		t.Fatalf("succeeded callback = %#v", succeeded)
	}
	failed, ok := fed.translate(&rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_ReservationFailed{
			ReservationFailed: &rtiv1.ObjectInstanceNameReservationFailed{ObjectName: "VerifierEntity-2"},
		},
	}).(ObjectInstanceNameReservationFailed)
	if !ok || failed.ObjectName != "VerifierEntity-2" {
		t.Fatalf("failed callback = %#v", failed)
	}
}

func TestObjectClassDeclarationsResolveNames(t *testing.T) {
	decl := &captureDeclarationClient{}
	fed := &Federate{
		conn:           &Connection{decl: decl},
		federationName: "bench",
		federateHandle: 11,
		handles:        objectSurfaceTables(),
	}

	attributes := []string{"Speed", "Position"}
	if err := fed.PublishObjectClassAttributes(context.Background(), "Vehicle", attributes); err != nil {
		t.Fatalf("PublishObjectClassAttributes: %v", err)
	}
	attributes[0] = "mutated"
	if got := decl.publish.GetObjectClassHandle(); got != 7 {
		t.Fatalf("published class handle = %d, want 7", got)
	}
	if got := decl.publish.GetAttributeHandles(); len(got) != 2 || got[0] != 3 || got[1] != 2 {
		t.Fatalf("published attribute handles = %v, want [3 2]", got)
	}

	if err := fed.SubscribeObjectClassAttributes(context.Background(), "Vehicle", []string{"Position"}); err != nil {
		t.Fatalf("SubscribeObjectClassAttributes: %v", err)
	}
	if got := decl.subscribe.GetAttributeHandles(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("subscribed attribute handles = %v, want [2]", got)
	}

	err := fed.PublishObjectClassAttributes(context.Background(), "Vehicle", []string{"Missing"})
	if err == nil || !strings.Contains(err.Error(), `attribute "Missing"`) {
		t.Fatalf("unknown attribute error = %v", err)
	}
}

func TestRegisterAndUpdateAttributeValuesResolveNamesAndCopyInputs(t *testing.T) {
	objects := &captureObjectClient{}
	fed := &Federate{
		conn:           &Connection{obj: objects},
		federationName: "bench",
		federateHandle: 11,
		handles:        objectSurfaceTables(),
	}

	objectHandle, err := fed.RegisterObjectInstance(context.Background(), "Vehicle", "vehicle-1")
	if err != nil {
		t.Fatalf("RegisterObjectInstance: %v", err)
	}
	if objectHandle != 42 {
		t.Fatalf("object handle = %d, want 42", objectHandle)
	}
	if got := objects.register.GetObjectClassHandle(); got != 7 {
		t.Fatalf("registered class handle = %d, want 7", got)
	}

	payload := []byte{1, 2, 3}
	timestamp := 0.0
	if err := fed.UpdateAttributeValues(
		context.Background(), objectHandle, map[string][]byte{"Position": payload}, &timestamp,
	); err != nil {
		t.Fatalf("UpdateAttributeValues: %v", err)
	}
	payload[0] = 99
	timestamp = 17
	if got := objects.update.GetAttributes()[2]; len(got) != 3 || got[0] != 1 {
		t.Fatalf("wire payload = %v, want defensive copy [1 2 3]", got)
	}
	if objects.update.LogicalTime == nil || objects.update.GetLogicalTime() != 0 {
		t.Fatalf("wire timestamp = %v, want present zero", objects.update.LogicalTime)
	}

	if err := fed.UpdateAttributes(context.Background(), objectHandle, map[string][]byte{"Speed": {4}}, nil); err != nil {
		t.Fatalf("UpdateAttributes alias: %v", err)
	}
	if objects.update.LogicalTime != nil {
		t.Fatalf("receive-order update timestamp = %v, want nil", objects.update.LogicalTime)
	}
}

func TestUpdateAttributeValuesByHandle(t *testing.T) {
	objects := &captureObjectClient{}
	fed := &Federate{
		conn:           &Connection{obj: objects},
		federationName: "bench",
		federateHandle: 11,
	}
	timestamp := 3.0
	attributes := map[uint64][]byte{2: {1, 2, 3}}
	if err := fed.UpdateAttributeValuesByHandle(
		context.Background(), 42, attributes, &timestamp,
	); err != nil {
		t.Fatalf("UpdateAttributeValuesByHandle: %v", err)
	}
	if objects.update.GetObjectHandle() != 42 || objects.update.GetFederateHandle() != 11 {
		t.Fatalf("update identity = %#v", objects.update)
	}
	if got := objects.update.GetAttributes()[2]; len(got) != 3 || got[0] != 1 {
		t.Fatalf("attributes[2] = %v, want [1 2 3]", got)
	}
	if objects.update.LogicalTime == nil || objects.update.GetLogicalTime() != 3 {
		t.Fatalf("logical time = %v, want 3", objects.update.LogicalTime)
	}
}

func TestTranslateDiscoverAndReflectAttributeValues(t *testing.T) {
	fed := &Federate{handles: objectSurfaceTables()}
	discovered := fed.translate(&rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_Discover{Discover: &rtiv1.DiscoverObjectInstance{
			ObjectHandle: 42, ObjectClassHandle: 7, ObjectName: "vehicle-1",
		}},
	})
	discover, ok := discovered.(DiscoverObjectInstance)
	if !ok {
		t.Fatalf("discover translation type = %T", discovered)
	}
	if discover.ObjectHandle != 42 || discover.ClassName != "Vehicle" || discover.ObjectName != "vehicle-1" {
		t.Fatalf("discover translation = %+v", discover)
	}
	if class, found := fed.objectClassFor(42); !found || class != 7 {
		t.Fatalf("tracked object class = (%d, %v), want (7, true)", class, found)
	}

	payload := []byte{8, 9}
	zero := 0.0
	reflected := fed.translate(&rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{
			ObjectHandle: 42, ObjectClassHandle: 7,
			Attributes:  map[uint64][]byte{2: payload, 999: {1}},
			LogicalTime: &zero,
		}},
	})
	reflect, ok := reflected.(ReflectAttributeValues)
	if !ok {
		t.Fatalf("reflect translation type = %T", reflected)
	}
	payload[0] = 77
	if reflect.ObjectHandle != 42 || reflect.ClassName != "Vehicle" {
		t.Fatalf("reflect identity = %+v", reflect)
	}
	if got := reflect.Attributes["Position"]; len(got) != 2 || got[0] != 8 {
		t.Fatalf("reflected Position = %v, want defensive copy [8 9]", got)
	}
	if _, found := reflect.Attributes["999"]; found || len(reflect.Attributes) != 1 {
		t.Fatalf("reflected attributes = %v, want only known names", reflect.Attributes)
	}
	if reflect.Timestamp == nil || *reflect.Timestamp != 0 {
		t.Fatalf("reflected timestamp = %v, want present zero", reflect.Timestamp)
	}
}

func TestTranslateReflectAttributeValuesByHandle(t *testing.T) {
	attributes := map[uint64][]byte{2: {8, 9}}
	fed := &Federate{
		conn: &Connection{callbackRepresentation: CallbackRepresentationHandles},
	}
	translated := fed.translate(&rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{
			ObjectHandle: 42, ObjectClassHandle: 7, Attributes: attributes,
		}},
	})
	reflected, ok := translated.(ReflectAttributeValuesByHandle)
	if !ok {
		t.Fatalf("handle reflection type = %T", translated)
	}
	if reflected.ObjectHandle != 42 || reflected.ObjectClassHandle != 7 ||
		reflected.Timestamp != nil || !reflect.DeepEqual(reflected.Attributes, attributes) {
		t.Fatalf("handle reflection = %+v", reflected)
	}
	if class, found := fed.objectClassFor(42); !found || class != 7 {
		t.Fatalf("tracked object class = (%d, %v), want (7, true)", class, found)
	}
}
