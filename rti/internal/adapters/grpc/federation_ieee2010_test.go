package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIEEE2010CreateReservedMIMRejectedBeforeStore(t *testing.T) {
	store := &fakeFedStore{}
	service := newFedSvc(store)
	_, err := service.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V2, FederationName: "reserved",
		MimModule: &rtiv1.FOMModule{Path: "HLAstandardMIM", Xml: []byte("source")},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("reserved MIM status = %v; want InvalidArgument", err)
	}
	if len(store.createCalls) != 0 {
		t.Fatal("invalid selection reached federation store")
	}
}
