package ddm_registration_test

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProductionReleaseMultipleNamesIsAtomic(t *testing.T) {
	f := startFederation(t)
	_, err := f.objects.ReserveMultipleObjectInstanceNames(f.ctx, &rtiv1.ReserveMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectNames: []string{"batch-a", "batch-b"}})
	check(t, err)
	_, err = f.objects.ReserveObjectInstanceName(f.ctx, &rtiv1.ReserveObjectInstanceNameRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], ObjectName: "foreign"})
	check(t, err)
	_, err = f.objects.ReleaseMultipleObjectInstanceNames(f.ctx, &rtiv1.ReleaseMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectNames: []string{"batch-a", "foreign", "batch-b"}})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign member release=%v", err)
	}
	// A failed batch must leave both valid names available to their original
	// holder's subsequent single release, proving no prefix was committed.
	for _, name := range []string{"batch-a", "batch-b"} {
		_, err = f.objects.ReleaseObjectInstanceName(f.ctx, &rtiv1.ReleaseObjectInstanceNameRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectName: name})
		check(t, err)
	}
	_, err = f.objects.ReserveMultipleObjectInstanceNames(f.ctx, &rtiv1.ReserveMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectNames: []string{"batch-a", "batch-b"}})
	check(t, err)
	_, err = f.objects.ReleaseMultipleObjectInstanceNames(f.ctx, &rtiv1.ReleaseMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectNames: []string{"batch-b", "batch-a", "batch-a"}})
	check(t, err)
	_, err = f.objects.ReserveMultipleObjectInstanceNames(f.ctx, &rtiv1.ReserveMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], ObjectNames: []string{"batch-a", "batch-b"}})
	check(t, err)
	_, err = f.objects.ReleaseMultipleObjectInstanceNames(f.ctx, &rtiv1.ReleaseMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], ObjectNames: []string{"batch-a", "batch-b", "foreign"}})
	check(t, err)
}
