package federate

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
)

type explicitObjectUnsubscription interface {
	UnsubscribeObjectClassByHandle(context.Context, uint64) error
	UnsubscribeObjectClassAttributeSetByHandle(context.Context, uint64, []uint64) error
}

func TestGoUnsubscribePresenceAndLegacyEmpty(t *testing.T) {
	var request *rtiv1.UnsubObjAttrsRequest
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		request = req.(*rtiv1.UnsubObjAttrsRequest)
		return &rtiv1.Empty{}, nil
	})
	api, ok := any(f).(explicitObjectUnsubscription)
	if !ok {
		t.Fatal("missing whole-class/explicit-set unsubscribe API")
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		call    func() error
		present bool
		attrs   []uint64
	}{
		{"whole", func() error { return api.UnsubscribeObjectClassByHandle(ctx, 21) }, false, nil},
		{"explicit-empty", func() error { return api.UnsubscribeObjectClassAttributeSetByHandle(ctx, 21, []uint64{}) }, true, nil},
		{"explicit-nil", func() error { return api.UnsubscribeObjectClassAttributeSetByHandle(ctx, 21, nil) }, true, nil},
		{"explicit-scoped", func() error { return api.UnsubscribeObjectClassAttributeSetByHandle(ctx, 21, []uint64{51}) }, true, []uint64{51}},
		{"legacy-empty", func() error { return f.UnsubscribeObjectClassAttributesByHandle(ctx, 21, nil) }, false, nil},
		{"legacy-scoped", func() error { return f.UnsubscribeObjectClassAttributesByHandle(ctx, 21, []uint64{51}) }, false, []uint64{51}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err != nil {
				t.Fatal(err)
			}
			field := request.ProtoReflect().Descriptor().Fields().ByName("attribute_set_present")
			if field == nil || request.ProtoReflect().Has(field) != tc.present || tc.present && !request.ProtoReflect().Get(field).Bool() {
				t.Fatalf("wrong presence: %v", request)
			}
			if request.FederateHandle != 7 || request.FederationName != testFederationName || request.ObjectClassHandle != 21 || !reflect.DeepEqual(request.AttributeHandles, tc.attrs) {
				t.Fatalf("wrong request: %v", request)
			}
		})
	}
}

func TestGoNamedUnsubscribeResolvesClassAndAttributesBeforeWire(t *testing.T) {
	var requests []*rtiv1.UnsubObjAttrsRequest
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		requests = append(requests, req.(*rtiv1.UnsubObjAttrsRequest))
		return &rtiv1.Empty{}, nil
	})
	f.handles, _ = tablesFromFOMView(authoritativeView(1))
	ctx := context.Background()
	for _, call := range []func() error{
		func() error { return f.UnsubscribeObjectClass(ctx, "Z") },
		func() error { return f.UnsubscribeObjectClassAttributeSet(ctx, "Z", nil) },
		func() error { return f.UnsubscribeObjectClassAttributeSet(ctx, "Z", []string{"Own"}) },
		func() error { return f.UnsubscribeObjectClassAttributes(ctx, "Z", nil) },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	for i, request := range requests {
		want := i == 1 || i == 2
		if request.ObjectClassHandle != 19 || (request.AttributeSetPresent != nil) != want || want && !request.GetAttributeSetPresent() {
			t.Fatalf("request %d=%v", i, request)
		}
	}
	if !reflect.DeepEqual(requests[2].AttributeHandles, []uint64{2}) {
		t.Fatalf("named set not resolved: %v", requests[2])
	}
	if err := f.UnsubscribeObjectClass(ctx, "missing"); err == nil {
		t.Fatal("undefined whole class accepted")
	}
	if err := f.UnsubscribeObjectClassAttributeSet(ctx, "missing", nil); err == nil {
		t.Fatal("undefined empty-set class accepted")
	}
	if err := f.UnsubscribeObjectClassAttributeSet(ctx, "Z", []string{"missing"}); err == nil {
		t.Fatal("undefined attribute accepted")
	}
	if len(requests) != 4 {
		t.Fatal("invalid names reached RPC")
	}
}
