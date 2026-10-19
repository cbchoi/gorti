package ddm_registration_test

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProductionLocalDeleteAndCallerKnowledge(t *testing.T) {
	f := startFederation(t)
	f.subscribe(1)
	f.subscribe(2)
	f.reserve("local-shared")
	r, err := f.register("local-shared")
	check(t, err)
	f.barrier()
	obj := r.ObjectHandle
	known := func(who int, want bool) {
		t.Helper()
		h := f.handles[who]
		byName, errName := f.support.GetObjectInstanceHandle(f.ctx, &rtiv1.GetObjectInstanceHandleRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h, ObjectName: "local-shared"})
		byHandle, errHandle := f.support.GetObjectInstanceName(f.ctx, &rtiv1.GetObjectInstanceNameRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h, ObjectHandle: obj})
		class, errClass := f.support.GetKnownObjectClassHandle(f.ctx, &rtiv1.GetKnownObjectClassHandleRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h, ObjectHandle: obj})
		if want {
			check(t, errName)
			check(t, errHandle)
			check(t, errClass)
			if byName.ObjectHandle != obj || byHandle.ObjectName != "local-shared" || class.ClassHandle != f.class {
				t.Fatal("wrong caller-scoped identity")
			}
		} else if status.Code(errName) != codes.NotFound || status.Code(errHandle) != codes.NotFound || status.Code(errClass) != codes.InvalidArgument {
			t.Fatalf("unknown object exposed to %d: %v / %v / %v", who, errName, errHandle, errClass)
		}
	}
	known(0, true)
	known(1, true)
	known(2, true)
	known(3, false)
	_, err = f.objects.LocalDeleteObjectInstance(f.ctx, &rtiv1.LocalDeleteObjectInstanceRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: obj})
	if err == nil {
		t.Fatal("owner local deletion accepted")
	}
	_, err = f.decl.UnsubscribeObjectClassAttributes(f.ctx, &rtiv1.UnsubObjAttrsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.position, f.velocity}})
	check(t, err)
	_, err = f.objects.LocalDeleteObjectInstance(f.ctx, &rtiv1.LocalDeleteObjectInstanceRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], ObjectHandle: obj})
	check(t, err)
	f.barrier()
	known(0, true)
	known(1, false)
	known(2, true)
	// The pre-existing caller-free administrative lookup remains compatible.
	f.lookup("local-shared", obj)
	f.subscribe(1)
	f.barrier()
	known(1, true)
	_, err = f.support.GetKnownObjectClassHandle(f.ctx, &rtiv1.GetKnownObjectClassHandleRequest{WireVersion: 1, FederationName: f.name, FederateHandle: 999999, ObjectHandle: obj})
	if err == nil {
		t.Fatal("forged caller accepted")
	}
}
