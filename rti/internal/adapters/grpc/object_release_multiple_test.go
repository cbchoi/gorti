package grpc

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type releaseMultipleRegistry struct {
	core.ObjectRegistry
	calls  int
	fed    core.FederationName
	holder core.FederateHandle
	names  []string
	err    error
}

func (r *releaseMultipleRegistry) ReleaseMultipleObjectInstanceNames(_ context.Context, fed core.FederationName, holder core.FederateHandle, names []string) error {
	r.calls++
	r.fed = fed
	r.holder = holder
	r.names = append([]string(nil), names...)
	return r.err
}

func TestReleaseMultipleNamesRPCUsesOneAtomicCall(t *testing.T) {
	r := &releaseMultipleRegistry{}
	s := newObjectService(r)
	req := &rtiv1.ReleaseMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 7, ObjectNames: []string{"a", "b"}}
	if _, err := s.ReleaseMultipleObjectInstanceNames(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if r.calls != 1 || r.fed != "fed" || r.holder != 7 || !reflect.DeepEqual(r.names, req.ObjectNames) {
		t.Fatalf("forwarded=%+v", r)
	}
	req.WireVersion = 99
	if _, err := s.ReleaseMultipleObjectInstanceNames(context.Background(), req); status.Code(err) != codes.FailedPrecondition || r.calls != 1 {
		t.Fatalf("wire validation=%v calls%d", err, r.calls)
	}
	req.WireVersion = 1
	r.err = core.ErrObjectInstanceNameReservedByOther
	if _, err := s.ReleaseMultipleObjectInstanceNames(context.Background(), req); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign reservation error=%v", err)
	}
}
