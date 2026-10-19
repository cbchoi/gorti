package grpc

import (
	"context"
	"math"
	"reflect"
	"strconv"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	objectpkg "github.com/cbchoi/gorti/rti/internal/hla/object"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func integerObjectTime(value int64) *rtiv1.LogicalTimeValue {
	return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: value}}
}

func TestIntegerObjectCallbackWire(t *testing.T) {
	for _, timestamp := range []int64{0, 1<<53 + 1, math.MaxInt64} {
		for _, kind := range []objectpkg.CallbackKind{objectpkg.CallbackReflect, objectpkg.CallbackReceive, objectpkg.CallbackRemove} {
			event := &objectpkg.CallbackEventOf[int64]{Kind: kind, Object: 8, ObjectClass: 3, InteractionClass: 4, LogicalTime: &timestamp, UserSuppliedTag: []byte{1, 2}, Metadata: &core.ObjectCallbackMetadata{Producer: 9, SentOrder: core.OrderTimestamp, ReceivedOrder: core.OrderTimestamp, Transportation: core.TransportTypeReliable, RetractionHandle: 7}}
			event.Sequence = 17
			got, err := toFederateEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			var legacy *float64
			var exact *rtiv1.LogicalTimeValue
			var metadata *rtiv1.ObjectCallbackMetadata
			switch kind {
			case objectpkg.CallbackReflect:
				legacy, exact, metadata = got.GetReflect().LogicalTime, got.GetReflect().LogicalTimeValue, got.GetReflect().Metadata
			case objectpkg.CallbackReceive:
				legacy, exact, metadata = got.GetReceive().LogicalTime, got.GetReceive().LogicalTimeValue, got.GetReceive().Metadata
			case objectpkg.CallbackRemove:
				legacy, exact, metadata = got.GetRemove().LogicalTime, got.GetRemove().LogicalTimeValue, got.GetRemove().Metadata
			}
			if _, ok := exact.GetValue().(*rtiv1.LogicalTimeValue_Integer64Value); !ok || exact.GetInteger64Value() != timestamp || legacy != nil || got.Seq != 17 || metadata.GetMessageRetractionHandle() != 7 {
				t.Fatalf("callback lost exact time/metadata: %v", got)
			}
		}
	}
}

func TestFloatObjectRejectsIntegerTimestampBeforeRegistry(t *testing.T) {
	stub := &stubObjectRegistry{}
	svc := newObjectService(stub)
	_, err := svc.UpdateAttributeValues(context.Background(), &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, LogicalTimeValue: integerObjectTime(1<<53 + 1)})
	if status.Code(err) != codes.InvalidArgument || len(stub.updCalls) != 0 {
		t.Fatalf("wrong representation reached registry: err=%v calls=%v", err, stub.updCalls)
	}
}

func TestFloatObjectPreservesLegacyFinalTime(t *testing.T) {
	final := math.Inf(1)
	stub := &stubObjectRegistry{}
	svc := newObjectService(stub)
	_, err := svc.UpdateAttributeValues(context.Background(), &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, LogicalTime: &final})
	if err != nil || len(stub.updCalls) != 1 || stub.updCalls[0].TS == nil || !math.IsInf(float64(*stub.updCalls[0].TS), 1) {
		t.Fatalf("legacy final update rejected: %v %+v", err, stub.updCalls)
	}
	_, err = svc.SendInteraction(context.Background(), &rtiv1.SendInteractionRequest{WireVersion: 1, LogicalTime: &final})
	if err != nil || len(stub.sendCalls) != 1 || stub.sendCalls[0].TS == nil || !math.IsInf(float64(*stub.sendCalls[0].TS), 1) {
		t.Fatalf("legacy final interaction rejected: %v %+v", err, stub.sendCalls)
	}
	at := core.LogicalTime(final)
	for _, kind := range []objectpkg.CallbackKind{objectpkg.CallbackReflect, objectpkg.CallbackReceive, objectpkg.CallbackRemove} {
		out, err := toFederateEvent(&objectpkg.CallbackEvent{Kind: kind, LogicalTime: &at})
		if err != nil {
			t.Fatal(err)
		}
		var legacy *float64
		var exact *rtiv1.LogicalTimeValue
		switch kind {
		case objectpkg.CallbackReflect:
			legacy, exact = out.GetReflect().LogicalTime, out.GetReflect().LogicalTimeValue
		case objectpkg.CallbackReceive:
			legacy, exact = out.GetReceive().LogicalTime, out.GetReceive().LogicalTimeValue
		case objectpkg.CallbackRemove:
			legacy, exact = out.GetRemove().LogicalTime, out.GetRemove().LogicalTimeValue
		}
		if legacy == nil || !math.IsInf(*legacy, 1) || exact != nil {
			t.Fatalf("legacy final callback changed: %v", out)
		}
	}
}

type objectIntegerCall struct {
	kind    string
	time    *int64
	options core.ObjectMessageOptions
}

type objectIntegerSpy struct {
	core.ObjectRegistryOf[int64]
	calls []objectIntegerCall
}

func (s *objectIntegerSpy) UpdateAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, at *int64) error {
	s.calls = append(s.calls, objectIntegerCall{kind: "update", time: at})
	return nil
}
func (s *objectIntegerSpy) UpdateAttributesRetractable(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, at *int64, handle uint64) error {
	s.calls = append(s.calls, objectIntegerCall{kind: "update-retractable", time: at, options: core.ObjectMessageOptions{RetractionHandle: handle}})
	return nil
}
func (s *objectIntegerSpy) SendInteraction(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[core.ParameterHandle][]byte, at *int64) error {
	s.calls = append(s.calls, objectIntegerCall{kind: "interaction", time: at})
	return nil
}
func (s *objectIntegerSpy) SendInteractionRetractable(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[core.ParameterHandle][]byte, at *int64, handle uint64) error {
	s.calls = append(s.calls, objectIntegerCall{kind: "interaction-retractable", time: at, options: core.ObjectMessageOptions{RetractionHandle: handle}})
	return nil
}
func (s *objectIntegerSpy) Delete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, at *int64, tag []byte) error {
	s.calls = append(s.calls, objectIntegerCall{kind: "delete", time: at, options: core.ObjectMessageOptions{Tag: tag}})
	return nil
}

type objectIntegerMetadataSpy struct{ *objectIntegerSpy }

func (s objectIntegerMetadataSpy) UpdateAttributesWithMetadata(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, at *int64, options core.ObjectMessageOptions) error {
	s.calls = append(s.calls, objectIntegerCall{kind: "update", time: at, options: options})
	return nil
}
func (s objectIntegerMetadataSpy) SendInteractionWithMetadata(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[core.ParameterHandle][]byte, at *int64, options core.ObjectMessageOptions) error {
	s.calls = append(s.calls, objectIntegerCall{kind: "interaction", time: at, options: options})
	return nil
}
func (s objectIntegerMetadataSpy) DeleteWithMetadata(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, at *int64, options core.ObjectMessageOptions) error {
	s.calls = append(s.calls, objectIntegerCall{kind: "delete", time: at, options: options})
	return nil
}

func callIntegerObjectService(s *objectServiceOf[int64], operation string, legacy *float64, exact *rtiv1.LogicalTimeValue, options core.ObjectMessageOptions) error {
	ctx := context.Background()
	var err error
	switch operation {
	case "update":
		_, err = s.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, LogicalTime: legacy, LogicalTimeValue: exact, UserSuppliedTag: options.Tag, MessageRetractionHandle: options.RetractionHandle})
	case "interaction":
		_, err = s.SendInteraction(ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, LogicalTime: legacy, LogicalTimeValue: exact, UserSuppliedTag: options.Tag, MessageRetractionHandle: options.RetractionHandle})
	case "delete":
		_, err = s.DeleteObjectInstance(ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, LogicalTime: legacy, LogicalTimeValue: exact, UserSuppliedTag: options.Tag, MessageRetractionHandle: options.RetractionHandle})
	}
	return err
}

func TestIntegerObjectIngressValidationBeforeEffects(t *testing.T) {
	zero := float64(0)
	for _, metadata := range []bool{false, true} {
		for _, operation := range []string{"update", "interaction", "delete"} {
			for _, pair := range []struct {
				name   string
				legacy *float64
				exact  *rtiv1.LogicalTimeValue
			}{
				{"legacy-zero", &zero, nil},
				{"legacy-and-exact", &zero, integerObjectTime(0)},
				{"wrong-selector", nil, &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: 1}}},
				{"negative", nil, integerObjectTime(-1)},
				{"missing-selector", nil, &rtiv1.LogicalTimeValue{}},
			} {
				t.Run(operation+"/"+pair.name+"/metadata="+strconv.FormatBool(metadata), func(t *testing.T) {
					spy := &objectIntegerSpy{}
					var registry core.ObjectRegistryOf[int64] = spy
					if metadata {
						registry = objectIntegerMetadataSpy{spy}
					}
					err := callIntegerObjectService(newObjectServiceOf[int64](registry), operation, pair.legacy, pair.exact, core.ObjectMessageOptions{})
					if status.Code(err) != codes.InvalidArgument || len(spy.calls) != 0 {
						t.Fatalf("invalid timestamp caused effects: %v %+v", err, spy.calls)
					}
				})
			}
		}
	}
}

func TestIntegerObjectIngressOptionalAndFallbackBranches(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		for _, operation := range []string{"update", "interaction", "delete"} {
			for _, timestamp := range []*rtiv1.LogicalTimeValue{nil, integerObjectTime(0), integerObjectTime(1<<53 + 1), integerObjectTime(math.MaxInt64)} {
				spy := &objectIntegerSpy{}
				var registry core.ObjectRegistryOf[int64] = spy
				if metadata {
					registry = objectIntegerMetadataSpy{spy}
				}
				options := core.ObjectMessageOptions{}
				if metadata {
					options.Tag, options.RetractionHandle = []byte{1}, 7
				}
				err := callIntegerObjectService(newObjectServiceOf[int64](registry), operation, nil, timestamp, options)
				if err != nil || len(spy.calls) != 1 {
					t.Fatalf("%s: %v %+v", operation, err, spy.calls)
				}
				got := spy.calls[0]
				if (got.time == nil) != (timestamp == nil) || timestamp != nil && *got.time != timestamp.GetInteger64Value() || !reflect.DeepEqual(got.options, options) {
					t.Fatalf("%s lost timestamp/options: %+v", operation, got)
				}
			}
		}
	}
	for _, operation := range []string{"update", "interaction"} {
		spy := &objectIntegerSpy{}
		err := callIntegerObjectService(newObjectServiceOf[int64](spy), operation, nil, integerObjectTime(1<<53+1), core.ObjectMessageOptions{RetractionHandle: 8})
		if err != nil || len(spy.calls) != 1 || spy.calls[0].kind != operation+"-retractable" || spy.calls[0].options.RetractionHandle != 8 || *spy.calls[0].time != 1<<53+1 {
			t.Fatalf("retractable fallback lost exact time: %v %+v", err, spy.calls)
		}
		spy.calls = nil
		err = callIntegerObjectService(newObjectServiceOf[int64](spy), operation, nil, integerObjectTime(1), core.ObjectMessageOptions{Tag: []byte{1}})
		if status.Code(err) != codes.Unimplemented || len(spy.calls) != 0 {
			t.Fatalf("fallback accepted unsupported tag: %v", err)
		}
	}
	spy := &objectIntegerSpy{}
	err := callIntegerObjectService(newObjectServiceOf[int64](spy), "delete", nil, integerObjectTime(1), core.ObjectMessageOptions{RetractionHandle: 1})
	if status.Code(err) != codes.Unimplemented || len(spy.calls) != 0 {
		t.Fatalf("fallback accepted unsupported delete retraction: %v", err)
	}
}

func TestObjectCallbackKindsBothRepresentations(t *testing.T) {
	objectCallbackKinds[core.LogicalTime](t)
	objectCallbackKinds[int64](t)
}

func objectCallbackKinds[T core.LogicalTimeScalar](t *testing.T) {
	t.Helper()
	for kind := objectpkg.CallbackDiscover; kind <= objectpkg.CallbackMultipleReservationFailed; kind++ {
		out, err := toFederateEvent(&objectpkg.CallbackEventOf[T]{Sequence: 13, Kind: kind, Object: 7, ObjectClass: 3, InteractionClass: 9, ObjectName: "test", ObjectNames: []string{"first", "second"}, CollidingNames: []string{"second"}, AttributeHandles: []core.AttributeHandle{2, 4}, Metadata: &core.ObjectCallbackMetadata{Producer: 1}})
		if err != nil || out == nil || out.Event == nil || out.Seq != 13 {
			t.Fatalf("callback kind %d missing: %v %v", kind, out, err)
		}
		if kind == objectpkg.CallbackDiscover && out.GetDiscover().GetProducingFederate() != 1 {
			t.Fatal("discover lost producing federate")
		}
	}
	if _, err := toFederateEvent((*objectpkg.CallbackEventOf[T])(nil)); err == nil {
		t.Fatal("nil callback accepted")
	}
	if _, err := toFederateEvent(&objectpkg.CallbackEventOf[T]{Kind: 255}); err == nil {
		t.Fatal("unknown callback kind accepted")
	}
}
