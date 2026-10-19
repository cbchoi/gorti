package ddm_test

import (
	"os"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// S1516.1 9.1.4: identical committed bounds must be immediately observable
// at different receiver logical times, without a time advance for the commit.
func TestApplicableR1DDMCommitIndependentOfConstrainedLogicalTime(t *testing.T) {
	f := completionFederation(t)
	conn, err := completionDial(f.ctx[1], os.Getenv("GORTI_COMPLETION_DDM_ENDPOINT"))
	completionCheck(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	times := rtiv1.NewTimeServiceClient(conn)
	_, err = times.EnableTimeConstrained(f.ctx[1], &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[1]})
	completionCheck(t, err)
	query := func(want float64) {
		t.Helper()
		got, err := times.QueryLogicalTime(f.ctx[1], &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[1]})
		completionCheck(t, err)
		if got.LogicalTime != want {
			t.Fatalf("receiver logical time=%v want=%v", got.LogicalTime, want)
		}
	}
	r := f.region(1, f.x)
	for _, logical := range []float64{0, 100} {
		if logical != 0 {
			_, err = times.TimeAdvanceRequest(f.ctx[1], &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[1], LogicalTime: logical})
			completionCheck(t, err)
		}
		query(logical)
		f.set(1, r, f.x, 10, 20)
		f.commit(1, r)
		f.bounds(1, r, f.x, 10, 20)
		query(logical)
		f.set(1, r, f.x, 30, 40)
		f.bounds(1, r, f.x, 10, 20)
		f.commit(1, r)
		f.bounds(1, r, f.x, 30, 40)
		query(logical)
	}
}

func TestApplicableR1LiveObjectNameAdmissionBoundaries(t *testing.T) {
	for _, operation := range []string{"single-HLA", "multiple-mixed-HLA", "multiple-empty", "unreserved-registration"} {
		t.Run(operation, func(t *testing.T) {
			f := completionFederation(t)
			conn, err := completionDial(f.ctx[0], os.Getenv("GORTI_COMPLETION_DDM_ENDPOINT"))
			completionCheck(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			objects := rtiv1.NewObjectServiceClient(conn)
			// Valid reservation/release controls establish joined/authenticated
			// service readiness before checking source-derived negative admission.
			_, err = objects.ReserveObjectInstanceName(f.ctx[0], &rtiv1.ReserveObjectInstanceNameRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], ObjectName: "positiveControl"})
			completionCheck(t, err)
			_, err = objects.ReleaseObjectInstanceName(f.ctx[0], &rtiv1.ReleaseObjectInstanceNameRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], ObjectName: "positiveControl"})
			completionCheck(t, err)
			var trailer metadata.MD
			switch operation {
			case "single-HLA":
				_, err = objects.ReserveObjectInstanceName(f.ctx[0], &rtiv1.ReserveObjectInstanceNameRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], ObjectName: "HLAreserved"}, grpc.Trailer(&trailer))
			case "multiple-mixed-HLA":
				_, err = objects.ReserveMultipleObjectInstanceNames(f.ctx[0], &rtiv1.ReserveMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], ObjectNames: []string{"validCandidate", "HLAreserved"}}, grpc.Trailer(&trailer))
			case "multiple-empty":
				_, err = objects.ReserveMultipleObjectInstanceNames(f.ctx[0], &rtiv1.ReserveMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0]}, grpc.Trailer(&trailer))
			case "unreserved-registration":
				cls, lookupErr := f.support.GetObjectClassHandle(f.ctx[0], &rtiv1.GetObjectClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAobjectRoot.Vehicle"})
				completionCheck(t, lookupErr)
				attr, lookupErr := f.support.GetAttributeHandle(f.ctx[0], &rtiv1.GetAttributeHandleRequest{WireVersion: 1, FederationName: f.name, ClassHandle: cls.ClassHandle, AttributeName: "Position"})
				completionCheck(t, lookupErr)
				_, publishErr := rtiv1.NewDeclarationServiceClient(conn).PublishObjectClassAttributes(f.ctx[0], &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], ObjectClassHandle: cls.ClassHandle, AttributeHandles: []uint64{attr.AttributeHandle}})
				completionCheck(t, publishErr)
				var registered *rtiv1.RegisterObjectResponse
				registered, err = objects.RegisterObjectInstance(f.ctx[0], &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.owner[0], ObjectClassHandle: cls.ClassHandle, ObjectName: "neverReserved"}, grpc.Trailer(&trailer))
				if registered != nil && registered.ObjectHandle != 0 {
					t.Errorf("unreserved name created visible handle %d", registered.ObjectHandle)
				}
			}
			if err == nil {
				t.Errorf("raw live %s accepted invalid input; exception trailer=%v", operation, trailer.Get("rti-spec-exception"))
			}
			want := "IllegalName"
			if operation == "multiple-empty" {
				want = "NameSetWasEmpty"
			} else if operation == "unreserved-registration" {
				want = "ObjectInstanceNameNotReserved"
			}
			if got := trailer.Get("rti-spec-exception"); len(got) != 1 || got[0] != want {
				t.Errorf("raw live %s exception=%v, want %s", operation, got, want)
			}
		})
	}
}
