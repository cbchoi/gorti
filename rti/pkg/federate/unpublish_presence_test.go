package federate

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type explicitUnpublication interface {
	UnpublishObjectClassByHandle(context.Context, uint64) error
	UnpublishObjectClassAttributeSetByHandle(context.Context, uint64, []uint64) error
}

func TestGoUnpublishPresenceAndLegacyEmpty(t *testing.T) {
	var requests []*rtiv1.UnpubObjAttrsRequest
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		requests = append(requests, req.(*rtiv1.UnpubObjAttrsRequest))
		return &rtiv1.Empty{}, nil
	})
	api, ok := any(f).(explicitUnpublication)
	if !ok {
		t.Fatal("Go SDK needs distinct whole-class and explicit attribute-set unpublication")
	}
	cases := []struct {
		name      string
		call      func() error
		present   bool
		attrs     []uint64
		remaining []uint64
	}{
		{"whole", func() error { return api.UnpublishObjectClassByHandle(context.Background(), 21) }, false, nil, nil},
		{"explicit-empty", func() error {
			return api.UnpublishObjectClassAttributeSetByHandle(context.Background(), 21, []uint64{})
		}, true, nil, []uint64{51, 52}},
		{"explicit-nil", func() error { return api.UnpublishObjectClassAttributeSetByHandle(context.Background(), 21, nil) }, true, nil, []uint64{51, 52}},
		{"explicit-one", func() error {
			return api.UnpublishObjectClassAttributeSetByHandle(context.Background(), 21, []uint64{51})
		}, true, []uint64{51}, []uint64{52}},
		{"legacy-empty", func() error { return f.UnpublishObjectClassAttributesByHandle(context.Background(), 21, nil) }, false, nil, nil},
		{"legacy-one", func() error { return f.UnpublishObjectClassAttributesByHandle(context.Background(), 21, []uint64{51}) }, false, []uint64{51}, []uint64{52}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.publishedObjectAttributes = map[uint64]map[uint64]struct{}{21: {51: {}, 52: {}}}
			if err := tc.call(); err != nil {
				t.Fatal(err)
			}
			request := requests[len(requests)-1]
			field := request.ProtoReflect().Descriptor().Fields().ByName("attribute_set_present")
			if field == nil || request.ProtoReflect().Has(field) != tc.present || tc.present && !request.ProtoReflect().Get(field).Bool() {
				t.Fatalf("wrong attribute-set presence: %v", request)
			}
			if !reflect.DeepEqual(request.GetAttributeHandles(), tc.attrs) || request.GetFederateHandle() != 7 || request.GetFederationName() != testFederationName || request.GetObjectClassHandle() != 21 {
				t.Fatalf("wrong envelope/list: %v", request)
			}
			for _, attr := range []uint64{51, 52} {
				_, actual := f.publishedObjectAttributes[21][attr]
				want := false
				for _, remaining := range tc.remaining {
					want = want || remaining == attr
				}
				if actual != want {
					t.Fatalf("attribute %d publication=%t, want %t", attr, actual, want)
				}
			}
		})
	}
}

func TestGoUnpublishFailureKeepsPublicationState(t *testing.T) {
	f := remainingServicesFederate(t, func(_ context.Context, _ any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		return nil, status.Error(codes.FailedPrecondition, "ownership acquisition pending")
	})
	api, ok := any(f).(explicitUnpublication)
	if !ok {
		t.Fatal("missing explicit unpublication API")
	}
	f.publishedObjectAttributes = map[uint64]map[uint64]struct{}{21: {51: {}}}
	if err := api.UnpublishObjectClassByHandle(context.Background(), 21); err == nil {
		t.Fatal("expected server error")
	}
	if _, ok := f.publishedObjectAttributes[21][51]; !ok {
		t.Fatal("failed removal changed SDK state")
	}
}

func TestGoNamedUnpublishDistinguishesWholeClassAndEmptySet(t *testing.T) {
	var requests []*rtiv1.UnpubObjAttrsRequest
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		requests = append(requests, req.(*rtiv1.UnpubObjAttrsRequest))
		return &rtiv1.Empty{}, nil
	})
	f.handles, _ = tablesFromFOMView(authoritativeView(1))
	ctx := context.Background()
	for _, call := range []func() error{
		func() error { return f.UnpublishObjectClass(ctx, "Z") },
		func() error { return f.UnpublishObjectClassAttributeSet(ctx, "Z", []string{}) },
		func() error { return f.UnpublishObjectClassAttributeSet(ctx, "Z", []string{"Own"}) },
		func() error { return f.UnpublishObjectClassAttributes(ctx, "Z", nil) },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	for i, request := range requests {
		if request.GetObjectClassHandle() != 19 {
			t.Fatalf("request %d class: %v", i, request)
		}
		wantPresence := i == 1 || i == 2
		if (request.AttributeSetPresent != nil) != wantPresence || wantPresence && !request.GetAttributeSetPresent() {
			t.Fatalf("request %d presence: %v", i, request)
		}
	}
	if !reflect.DeepEqual(requests[2].AttributeHandles, []uint64{2}) {
		t.Fatalf("named attributes were not resolved: %v", requests[2])
	}
	if err := f.UnpublishObjectClassAttributeSet(ctx, "missing", nil); err == nil {
		t.Fatal("missing class accepted")
	}
	if err := f.UnpublishObjectClassAttributeSet(ctx, "Z", []string{"missing"}); err == nil {
		t.Fatal("missing attribute accepted")
	}
	if len(requests) != 4 {
		t.Fatal("invalid names reached the wire")
	}
}
