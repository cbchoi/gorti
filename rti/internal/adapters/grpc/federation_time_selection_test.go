package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnsupportedTimeSelectionHasStandardExceptionAndNoCreate(t *testing.T) {
	store := &fakeFedStore{}
	client := dialBufconnFederation(t, store)
	for _, name := range []string{integer64TimeFactory, "custom", " HLAfloat64Time"} {
		var trailer metadata.MD
		_, err := client.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{WireVersion: wireV1(), FederationName: "not-created", LogicalTimeImplementationName: name}, stdgrpc.Trailer(&trailer))
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("unsupported representation %q: %v", name, err)
		}
		if got := trailer.Get(specExceptionTrailerKey); len(got) != 1 || got[0] != "CouldNotCreateLogicalTimeFactory" {
			t.Fatalf("exception=%v", got)
		}
	}
	if len(store.createCalls) != 0 {
		t.Fatal("unsupported time reached federation store")
	}
}

func TestTimeSelectionForwardedAndReported(t *testing.T) {
	store := &fakeFedStore{listResp: []core.FederationSummary{{Name: "selected", LogicalTimeImplementationName: float64TimeFactory}}}
	client := dialBufconnFederation(t, store)
	for _, name := range []string{"", float64TimeFactory} {
		created, err := client.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{WireVersion: wireV1(), FederationName: "selected", LogicalTimeImplementationName: name})
		if err != nil || created.GetLogicalTimeImplementationName() != float64TimeFactory {
			t.Fatalf("create=%v,%v", created, err)
		}
	}
	for _, request := range store.createCalls {
		if request.LogicalTimeImplementationName != float64TimeFactory {
			t.Fatalf("unresolved core request: %+v", request)
		}
	}
	listed, err := client.ListFederations(context.Background(), &rtiv1.ListFederationsRequest{WireVersion: wireV1()})
	if err != nil || len(listed.Federations) != 1 || listed.Federations[0].LogicalTimeImplementationName != float64TimeFactory {
		t.Fatalf("list=%v,%v", listed, err)
	}
}
