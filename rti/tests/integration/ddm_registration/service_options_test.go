package ddm_registration_test

import (
	"encoding/binary"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func TestProductionScopeSwitchPreservesReflectionAndScopeState(t *testing.T) {
	f := startFederation(t)
	pub, sub := f.region(0, 10, 20), f.region(1, 15, 25)
	f.subscribe(1, sub)
	f.reserve("scope-switch")
	r, err := f.register("scope-switch", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{pub}})
	check(t, err)
	update := func() {
		t.Helper()
		_, err := f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, Attributes: map[uint64][]byte{f.position: {1}}})
		check(t, err)
		f.barrier()
	}
	counts := func() (in, out, reflected int) {
		for _, e := range f.callbacks[1].snapshot() {
			if x := e.GetAttributesInScope(); x != nil && x.ObjectHandle == r.ObjectHandle {
				in++
			}
			if x := e.GetAttributesOutOfScope(); x != nil && x.ObjectHandle == r.ObjectHandle {
				out++
			}
			if x := e.GetReflect(); x != nil && x.ObjectHandle == r.ObjectHandle {
				reflected++
			}
		}
		return
	}
	toggle := func(enabled bool) {
		t.Helper()
		_, err := f.decl.SetAdvisorySwitch(f.ctx, &rtiv1.SetAdvisorySwitchRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], SwitchKind: rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_ATTRIBUTE_SCOPE, Enabled: enabled})
		check(t, err)
	}
	move := func(lo, hi uint64) { t.Helper(); check(t, f.setRange(1, sub, lo, hi)); f.commit(1, sub); update() }
	update()
	if in, out, reflected := counts(); in != 1 || out != 0 || reflected != 1 {
		t.Fatalf("initial %d/%d/%d", in, out, reflected)
	}
	toggle(false)
	move(100, 110)
	move(15, 25)
	if in, out, reflected := counts(); in != 1 || out != 0 || reflected != 2 {
		t.Fatalf("disabled %d/%d/%d", in, out, reflected)
	}
	toggle(true)
	update()
	if in, out, reflected := counts(); in != 1 || out != 0 || reflected != 3 {
		t.Fatalf("cached state %d/%d/%d", in, out, reflected)
	}
	move(100, 110)
	move(15, 25)
	if in, out, reflected := counts(); in != 2 || out != 1 || reflected != 4 {
		t.Fatalf("resumed %d/%d/%d", in, out, reflected)
	}
}

func TestProductionMOMReportsActualClassTraffic(t *testing.T) {
	f := startFederation(t)
	f.subscribe(1)
	f.reserve("counted")
	r, err := f.register("counted")
	check(t, err)
	for i := 0; i < 3; i++ {
		_, err := f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, Attributes: map[uint64][]byte{f.position: {byte(i)}}})
		check(t, err)
	}
	f.barrier()
	for _, tc := range []struct {
		request, report, counts string
		target                  int
	}{
		{"HLArequestUpdatesSent", "HLAreportUpdatesSent", "HLAupdateCounts", 0},
		{"HLArequestReflectionsReceived", "HLAreportReflectionsReceived", "HLAreflectCounts", 1},
	} {
		lookup := func(name string) uint64 {
			t.Helper()
			r, err := f.support.GetInteractionClassHandle(f.ctx, &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: name})
			check(t, err)
			return r.ClassHandle
		}
		reqClass := lookup("HLAinteractionRoot.HLAmanager.HLAfederate.HLArequest." + tc.request)
		reportClass := lookup("HLAinteractionRoot.HLAmanager.HLAfederate.HLAreport." + tc.report)
		param := func(cls uint64, name string) uint64 {
			t.Helper()
			r, err := f.support.GetParameterHandle(f.ctx, &rtiv1.GetParameterHandleRequest{WireVersion: 1, FederationName: f.name, ClassHandle: cls, ParameterName: name})
			check(t, err)
			return r.ParameterHandle
		}
		target := make([]byte, 12)
		binary.BigEndian.PutUint32(target, 8)
		binary.BigEndian.PutUint64(target[4:], f.handles[tc.target])
		_, err := f.objects.SendInteraction(f.ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[3], InteractionClassHandle: reqClass, Parameters: map[uint64][]byte{param(reqClass, "HLAfederate"): target}})
		check(t, err)
		f.barrier()
		for _, e := range f.callbacks[3].snapshot() {
			if receive := e.GetReceive(); receive != nil && receive.InteractionClassHandle == reportClass {
				t.Fatal("unsubscribed requester received a private MOM report")
			}
		}
		_, err = f.decl.SubscribeInteractionClass(f.ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[3], InteractionClassHandle: reportClass})
		check(t, err)
		_, err = f.objects.SendInteraction(f.ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[3], InteractionClassHandle: reqClass, Parameters: map[uint64][]byte{param(reqClass, "HLAfederate"): target}})
		check(t, err)
		f.barrier()
		for _, index := range []int{0, 1, 2} {
			for _, e := range f.callbacks[index].snapshot() {
				if receive := e.GetReceive(); receive != nil && receive.InteractionClassHandle == reportClass {
					t.Fatalf("unsubscribed participant %d received MOM report", index)
				}
			}
		}
		var report *rtiv1.ReceiveInteraction
		for _, e := range f.callbacks[3].snapshot() {
			if receive := e.GetReceive(); receive != nil && receive.InteractionClassHandle == reportClass {
				report = receive
			}
		}
		if report == nil {
			t.Fatalf("missing report %s", tc.report)
		}
		data := report.Parameters[param(reportClass, tc.counts)]
		if len(report.Parameters) != 3 || len(data) != 20 || binary.BigEndian.Uint32(data) != 1 || binary.BigEndian.Uint32(data[4:]) != 8 || binary.BigEndian.Uint64(data[8:]) != f.class || binary.BigEndian.Uint32(data[16:]) != 3 {
			t.Fatalf("%s class counts: %x", tc.report, data)
		}
	}
}
