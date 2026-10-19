package grpc

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestJoinFederationNameConflictExceptionIsDistinct(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"name-conflict", core.ErrFederateNameAlreadyInUse, "FederateNameAlreadyInUse"},
		{"wrapped-name-conflict", fmt.Errorf("join: %w", core.ErrFederateNameAlreadyInUse), "FederateNameAlreadyInUse"},
		{"already-member", core.ErrFederateAlreadyJoined, "FederateAlreadyExecutionMember"},
		{"wrapped-already-member", fmt.Errorf("join: %w", core.ErrFederateAlreadyJoined), "FederateAlreadyExecutionMember"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := specExceptionName(tc.err); !ok || got != tc.want {
				t.Errorf("spec exception=(%q,%v), want %q", got, ok, tc.want)
			}
			client := dialBufconnFederation(t, &fakeFedStore{joinErr: tc.err})
			var trailer metadata.MD
			response, err := client.JoinFederation(context.Background(), &rtiv1.JoinFederationRequest{
				WireVersion: wireV1(), FederationName: "name-conflict", FederateName: "taken",
			}, stdgrpc.Trailer(&trailer))
			if response != nil || status.Code(err) != codes.AlreadyExists {
				t.Errorf("response=%v status=%v, want nil/AlreadyExists", response, err)
			}
			if got := trailer.Get(specExceptionTrailerKey); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("exception trailer=%v, want [%s]", got, tc.want)
			}
		})
	}
}

// S1516.1 4.9.3(d), 4.9.5(b): reject the entrant without changing the owner.
func TestJoinFederationDuplicateNameWirePreservesOwnerAndAllowsRetry(t *testing.T) {
	ctx := context.Background()
	manager, err := federation.New(federation.Options{
		Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: adminFOMs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := dialBufconnFederation(t, manager)
	const fed = "name-conflict"
	if _, err := client.CreateFederation(ctx, &rtiv1.CreateFederationRequest{
		WireVersion: wireV1(), FederationName: fed,
		FomModules: []*rtiv1.FOMModule{{Path: "name-conflict.xml", Xml: []byte(
			`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion: wireV1(), FederationName: fed, FederateName: "taken", FederateType: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := manager.MemberIdentity(fed, core.FederateHandle(owner.GetFederateHandle()))
	if err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	var trailer metadata.MD
	rejected, err := client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion: wireV1(), FederationName: fed, FederateName: "taken", FederateType: "entrant",
	}, stdgrpc.Trailer(&trailer))
	if rejected != nil || status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate name response=%v error=%v", rejected, err)
	}
	if got := trailer.Get(specExceptionTrailerKey); len(got) != 1 || got[0] != "FederateNameAlreadyInUse" {
		t.Errorf("duplicate name exception=%v, want [FederateNameAlreadyInUse]", got)
	}
	if after := manager.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("duplicate name changed owner state: before=%+v after=%+v", before, after)
	}
	entrant, err := client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion: wireV1(), FederationName: fed, FederateName: "unique-retry", FederateType: "entrant",
	})
	if err != nil {
		t.Fatalf("unique-name retry: %v", err)
	}
	if entrant.GetFederateHandle() != owner.GetFederateHandle()+1 {
		t.Fatalf("retry handle=%d owner=%d; rejected join consumed a handle", entrant.GetFederateHandle(), owner.GetFederateHandle())
	}
	if got, err := manager.MemberIdentity(fed, identity.Handle); err != nil || got != identity {
		t.Fatalf("owner identity changed: got=%+v err=%v want=%+v", got, err, identity)
	}
	listed, err := client.ListFederationMembers(ctx, &rtiv1.ListFederationMembersRequest{
		WireVersion: wireV1(), FederationName: fed,
	})
	if err != nil || len(listed.GetMembers()) != 2 {
		t.Fatalf("retry roster=%v err=%v, want two members", listed, err)
	}
	if _, err := client.ResignFederation(ctx, &rtiv1.ResignFederationRequest{
		WireVersion: wireV1(), FederationName: fed, FederateHandle: owner.GetFederateHandle(),
		Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION,
	}); err != nil {
		t.Fatalf("original owner remains usable: %v", err)
	}
	if err := manager.ValidateMember(fed, core.FederateHandle(entrant.GetFederateHandle())); err != nil {
		t.Fatalf("owner resign affected entrant: %v", err)
	}
}
