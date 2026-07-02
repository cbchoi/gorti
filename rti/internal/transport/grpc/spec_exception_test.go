// M39 Agent HB — tests for the structured spec-exception channel.
//
// Two layers:
//
//  1. specExceptionName table unit tests (bare + wrapped sentinels,
//     unmapped errors).
//  2. Wire-level bufconn tests: a real gRPC server + client so the
//     `rti-spec-exception` TRAILING metadata is asserted exactly as an
//     SDK would observe it (grpc.Trailer call option).
package grpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// ===========================================================================
// specExceptionName — table lookup
// ===========================================================================

func TestSpecExceptionName_TableLookup(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
		ok   bool
	}{
		{"bare sentinel", core.ErrObjectClassNotPublished, "ObjectClassNotPublished", true},
		{"wrapped sentinel", fmt.Errorf("SendInteraction: %w", core.ErrInteractionClassNotPublished), "InteractionClassNotPublished", true},
		{"double wrapped", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", core.ErrTimeInvalidLogicalTime)), "InvalidLogicalTime", true},
		{"ownership divest", core.ErrOwnershipDivestPending, "AttributeAlreadyBeingDivested", true},
		{"attribute not owned", core.ErrAttributeNotOwned, "AttributeNotOwned", true},
		{"sync achieve", core.ErrSyncPointNotRegistered, "SynchronizationPointLabelNotAnnounced", true},
		{"unmapped ambiguous sentinel", core.ErrOwnershipNotInTransfer, "", false},
		{"unmapped random error", errors.New("boom"), "", false},
		{"nil-adjacent: unknown wrap", fmt.Errorf("ctx: %w", errors.New("boom")), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := specExceptionName(tc.err)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("specExceptionName(%v) = (%q, %v), want (%q, %v)",
					tc.err, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestSpecExceptionTable_NamesAreAnnexC guards against typos drifting
// from the Annex C vocabulary: every value must be non-empty
// UpperCamelCase ASCII (the full class list lives in
// cppsdk/include/RTI/Exception.h; this is a cheap structural check).
func TestSpecExceptionTable_NamesAreAnnexC(t *testing.T) {
	for _, row := range specExceptionTable {
		if row.sentinel == nil {
			t.Fatalf("nil sentinel mapped to %q", row.spec)
		}
		if row.spec == "" {
			t.Fatalf("empty spec name for sentinel %v", row.sentinel)
		}
		if c := row.spec[0]; c < 'A' || c > 'Z' {
			t.Fatalf("spec name %q must be UpperCamelCase", row.spec)
		}
		for i := 0; i < len(row.spec); i++ {
			c := row.spec[i]
			if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
				t.Fatalf("spec name %q contains non-letter at %d", row.spec, i)
			}
		}
	}
}

// ===========================================================================
// Wire-level: trailer visible to a real gRPC client (bufconn)
// ===========================================================================

// dialBufconnFederation spins up a real gRPC server hosting only the
// FederationService (backed by store) on an in-memory bufconn listener
// and returns a connected client. Cleanup is registered on t.
func dialBufconnFederation(t *testing.T, store core.FederationStore) rtiv1.FederationServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := stdgrpc.NewServer()
	rtiv1.RegisterFederationServiceServer(gs, newFederationService(store))
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := stdgrpc.NewClient("passthrough:///bufnet",
		stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		stdgrpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return rtiv1.NewFederationServiceClient(conn)
}

func TestSpecExceptionTrailer_CreateFederation_AlreadyExists(t *testing.T) {
	client := dialBufconnFederation(t, &fakeFedStore{
		createErr: core.ErrFederationAlreadyExists,
	})

	var trailer metadata.MD
	_, err := client.CreateFederation(context.Background(),
		&rtiv1.CreateFederationRequest{
			WireVersion:    wireV1(),
			FederationName: fedAlphaName,
		}, stdgrpc.Trailer(&trailer))
	if err == nil {
		t.Fatal("CreateFederation: want error, got nil")
	}
	got := trailer.Get(specExceptionTrailerKey)
	if len(got) != 1 || got[0] != "FederationExecutionAlreadyExists" {
		t.Fatalf("trailer %q = %v, want [FederationExecutionAlreadyExists]",
			specExceptionTrailerKey, got)
	}
}

func TestSpecExceptionTrailer_JoinFederation_WrappedNotFound(t *testing.T) {
	// Handlers wrap sentinels with context; the trailer must still
	// resolve through errors.Is.
	client := dialBufconnFederation(t, &fakeFedStore{
		joinErr: fmt.Errorf("join %q: %w", fedAlphaName, core.ErrFederationNotFound),
	})

	var trailer metadata.MD
	_, err := client.JoinFederation(context.Background(),
		&rtiv1.JoinFederationRequest{
			WireVersion:    wireV1(),
			FederationName: fedAlphaName,
			FederateName:   "fed1",
		}, stdgrpc.Trailer(&trailer))
	if err == nil {
		t.Fatal("JoinFederation: want error, got nil")
	}
	got := trailer.Get(specExceptionTrailerKey)
	if len(got) != 1 || got[0] != "FederationExecutionDoesNotExist" {
		t.Fatalf("trailer %q = %v, want [FederationExecutionDoesNotExist]",
			specExceptionTrailerKey, got)
	}
}

func TestSpecExceptionTrailer_AbsentForNonSentinelError(t *testing.T) {
	// Legacy contract: an error with no Annex C identity must NOT set
	// the trailer — clients fall back to their sniff tables.
	client := dialBufconnFederation(t, &fakeFedStore{
		createErr: errors.New("disk on fire"),
	})

	var trailer metadata.MD
	_, err := client.CreateFederation(context.Background(),
		&rtiv1.CreateFederationRequest{
			WireVersion:    wireV1(),
			FederationName: fedAlphaName,
		}, stdgrpc.Trailer(&trailer))
	if err == nil {
		t.Fatal("CreateFederation: want error, got nil")
	}
	if got := trailer.Get(specExceptionTrailerKey); len(got) != 0 {
		t.Fatalf("trailer %q = %v, want absent", specExceptionTrailerKey, got)
	}
}

func TestSpecExceptionTrailer_AbsentOnSuccess(t *testing.T) {
	client := dialBufconnFederation(t, &fakeFedStore{})

	var trailer metadata.MD
	_, err := client.CreateFederation(context.Background(),
		&rtiv1.CreateFederationRequest{
			WireVersion:    wireV1(),
			FederationName: fedAlphaName,
		}, stdgrpc.Trailer(&trailer))
	if err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	if got := trailer.Get(specExceptionTrailerKey); len(got) != 0 {
		t.Fatalf("trailer %q = %v, want absent on success", specExceptionTrailerKey, got)
	}
}
