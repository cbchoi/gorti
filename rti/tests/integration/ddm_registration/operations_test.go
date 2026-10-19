package ddm_registration_test

import (
	"bytes"
	"slices"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

func (f *federation) provides(who int, tag []byte) []*rtiv1.ProvideAttributeValueUpdate {
	var result []*rtiv1.ProvideAttributeValueUpdate
	for _, event := range f.callbacks[who].snapshot() {
		if update := event.GetProvideUpdate(); update != nil && bytes.Equal(update.UserSuppliedTag, tag) {
			result = append(result, update)
		}
	}
	return result
}

func TestProductionRegionRequestFiltersBeforeCallbacks(t *testing.T) {
	f := startFederation(t)
	near, far := f.region(0, 10, 20), f.region(0, 100, 110)
	query := f.region(3, 15, 18)
	f.reserve("split-regions")
	r, err := f.register("split-regions", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{near}},
		&rtiv1.AttributeRegions{AttributeHandle: f.velocity, RegionHandles: []uint64{far}})
	check(t, err)
	req := &rtiv1.RequestAttributeValueUpdateWithRegionsRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[3], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.position, f.velocity},
		RegionHandles: []uint64{query}, UserSuppliedTag: []byte{0, 0xff, 'r'}}
	_, err = f.ddm.RequestAttributeValueUpdateWithRegions(f.ctx, req)
	check(t, err)
	f.barrier()
	got := f.provides(0, req.UserSuppliedTag)
	if len(got) != 1 || got[0].ObjectHandle != r.ObjectHandle || !slices.Equal(got[0].AttributeHandles, []uint64{f.position}) {
		t.Errorf("region request callbacks = %v, want only overlapping position on %d", got, r.ObjectHandle)
	}
	req.RegionHandles = []uint64{query, ^uint64(0)}
	req.UserSuppliedTag = []byte("invalid-region")
	var trailer metadata.MD
	_, err = f.ddm.RequestAttributeValueUpdateWithRegions(f.ctx, req, grpc.Trailer(&trailer))
	expectRPCError(t, err, trailer, codes.NotFound, "InvalidRegion")
	f.barrier()
	for who := range f.handles {
		if got := f.provides(who, req.UserSuppliedTag); len(got) != 0 {
			t.Errorf("invalid region emitted callbacks to %d: %v", who, got)
		}
	}
}

func TestProductionRegionSendSelectsPerCallRecipients(t *testing.T) {
	f := startFederation(t)
	cls, err := f.support.GetInteractionClassHandle(f.ctx, &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1,
		FederationName: f.name, ClassName: "HLAinteractionRoot.RegionalSignal"})
	check(t, err)
	_, err = f.decl.PublishInteractionClass(f.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle})
	check(t, err)
	near, far := f.region(0, 10, 20), f.region(0, 100, 110)
	for who, region := range map[int]uint64{1: f.region(1, 15, 25), 2: f.region(2, 105, 115)} {
		_, err = f.ddm.SubscribeInteractionClassWithRegions(f.ctx, &rtiv1.SubscribeICWithRegionsRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: f.handles[who], InteractionClassHandle: cls.ClassHandle, RegionHandles: []uint64{region}})
		check(t, err)
	}
	_, err = f.decl.SubscribeInteractionClass(f.ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[3], InteractionClassHandle: cls.ClassHandle})
	check(t, err)
	count := func(who int) int {
		n := 0
		for _, event := range f.callbacks[who].snapshot() {
			if receive := event.GetReceive(); receive != nil && receive.InteractionClassHandle == cls.ClassHandle {
				n++
			}
		}
		return n
	}
	for step, region := range []uint64{near, far, ^uint64(0)} {
		var trailer metadata.MD
		_, err = f.ddm.SendInteractionWithRegions(f.ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle,
			RegionHandles: []uint64{region}}, grpc.Trailer(&trailer))
		if step == 2 {
			expectRPCError(t, err, trailer, codes.NotFound, "InvalidRegion")
		} else {
			check(t, err)
		}
		f.barrier()
		want := [4]int{0, 1, 0, 1}
		if step > 0 {
			want = [4]int{0, 1, 1, 2}
		}
		for who := range f.handles {
			if got := count(who); got != want[who] {
				t.Errorf("step %d subscriber %d receives=%d want=%d", step, who, got, want[who])
			}
		}
	}
}

func TestProductionUpdateRequestsUseCurrentAttributeOwners(t *testing.T) {
	f := startFederation(t)
	f.subscribe(1)
	f.subscribe(3)
	region := f.region(0, 10, 20)
	f.reserve("mixed-owners")
	r, err := f.register("mixed-owners", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{region}},
		&rtiv1.AttributeRegions{AttributeHandle: f.velocity, RegionHandles: []uint64{region}})
	check(t, err)
	f.barrier()
	_, err = f.decl.PublishObjectClassAttributes(f.ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[1], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.velocity}})
	check(t, err)
	_, err = f.ownership.UnconditionalAttributeOwnershipDivestiture(f.ctx, &rtiv1.UnconditionalDivestRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, AttributeHandles: []uint64{f.velocity}})
	check(t, err)
	_, err = f.ownership.AttributeOwnershipAcquisition(f.ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[1], ObjectHandle: r.ObjectHandle, AttributeHandles: []uint64{f.velocity}, IfAvailable: true})
	check(t, err)
	newOwnerRegion := f.region(1, 12, 19)
	_, err = f.ddm.AssociateRegionsForUpdates(f.ctx, &rtiv1.AssociateRegionsForUpdatesRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[1], ObjectHandle: r.ObjectHandle, AttributeRegions: []*rtiv1.AttributeRegions{{AttributeHandle: f.velocity, RegionHandles: []uint64{newOwnerRegion}}}})
	check(t, err)
	for _, mode := range []string{"instance", "class", "region"} {
		tag := []byte(mode)
		attrs := []uint64{f.position, f.velocity}
		switch mode {
		case "instance":
			_, err = f.objects.RequestAttributeValueUpdate(f.ctx, &rtiv1.RequestAttributeValueUpdateRequest{WireVersion: 1,
				FederationName: f.name, FederateHandle: f.handles[3], ObjectHandle: r.ObjectHandle, AttributeHandles: attrs, UserSuppliedTag: tag})
		case "class":
			_, err = f.objects.RequestClassAttributeValueUpdate(f.ctx, &rtiv1.RequestClassAttributeValueUpdateRequest{WireVersion: 1,
				FederationName: f.name, FederateHandle: f.handles[3], ObjectClassHandle: f.class, AttributeHandles: attrs, UserSuppliedTag: tag})
		case "region":
			_, err = f.ddm.RequestAttributeValueUpdateWithRegions(f.ctx, &rtiv1.RequestAttributeValueUpdateWithRegionsRequest{WireVersion: 1,
				FederationName: f.name, FederateHandle: f.handles[3], ObjectClassHandle: f.class, AttributeHandles: attrs,
				RegionHandles: []uint64{f.region(3, 15, 18)}, UserSuppliedTag: tag})
		}
		check(t, err)
		f.barrier()
		for who, attr := range map[int]uint64{0: f.position, 1: f.velocity} {
			got := f.provides(who, tag)
			if len(got) != 1 || !slices.Equal(got[0].AttributeHandles, []uint64{attr}) || got[0].ObjectHandle != r.ObjectHandle {
				t.Errorf("%s owner %d callbacks=%v want attr %d", mode, who, got, attr)
			}
		}
	}
}

func TestProductionDDMSubscriptionValidationAndPartialRemoval(t *testing.T) {
	f := startFederation(t)
	near, far := f.region(1, 10, 20), f.region(1, 100, 110)
	foreign := f.region(2, 10, 20)
	f.subscribe(1, near, far)
	var trailer metadata.MD
	_, err := f.ddm.SubscribeObjectClassAttributesWithRegions(f.ctx, &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[1], ObjectClassHandle: f.class,
		AttributeHandles: []uint64{f.position, f.velocity}, RegionHandles: []uint64{near, foreign}}, grpc.Trailer(&trailer))
	expectRPCError(t, err, trailer, codes.PermissionDenied, "RegionNotCreatedByThisFederate")
	_, err = f.ddm.UnsubscribeObjectClassAttributesWithRegions(f.ctx, &rtiv1.UnsubscribeOCAWithRegionsRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[1], ObjectClassHandle: f.class,
		AttributeHandles: []uint64{f.position, f.velocity}, RegionHandles: []uint64{near}})
	check(t, err)
	publisher := f.region(0, 102, 108)
	f.reserve("remaining-far-region")
	r, err := f.register("remaining-far-region", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{publisher}},
		&rtiv1.AttributeRegions{AttributeHandle: f.velocity, RegionHandles: []uint64{publisher}})
	check(t, err)
	f.barrier()
	f.expectCallbacks(1, r.ObjectHandle, 1, 0, nil)
}

func TestProductionDDMAssociationValidationHasNoEffects(t *testing.T) {
	f := startFederation(t)
	f.subscribe(1)
	publisher := f.region(0, 10, 20)
	f.reserve("validation-object")
	r, err := f.register("validation-object", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{publisher}})
	check(t, err)
	f.barrier()
	inactive := f.region(1, 100, 110)
	nearQuery, farQuery := f.region(3, 12, 18), f.region(3, 102, 108)
	for _, action := range []string{"associate", "unassociate"} {
		var trailer metadata.MD
		pairs := []*rtiv1.AttributeRegions{{AttributeHandle: f.position, RegionHandles: []uint64{inactive}}}
		if action == "associate" {
			_, err = f.ddm.AssociateRegionsForUpdates(f.ctx, &rtiv1.AssociateRegionsForUpdatesRequest{WireVersion: 1,
				FederationName: f.name, FederateHandle: f.handles[1], ObjectHandle: r.ObjectHandle, AttributeRegions: pairs}, grpc.Trailer(&trailer))
		} else {
			_, err = f.ddm.UnassociateRegionsForUpdates(f.ctx, &rtiv1.UnassociateRegionsForUpdatesRequest{WireVersion: 1,
				FederationName: f.name, FederateHandle: f.handles[1], ObjectHandle: r.ObjectHandle}, grpc.Trailer(&trailer))
		}
		check(t, err)

		// A nonowner may maintain its inactive associations, never the owner's active regions.
		if action == "associate" {
			_, err = f.ddm.AssociateRegionsForUpdates(f.ctx, &rtiv1.AssociateRegionsForUpdatesRequest{WireVersion: 1,
				FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, AttributeRegions: pairs}, grpc.Trailer(&trailer))
		} else {
			_, err = f.ddm.UnassociateRegionsForUpdates(f.ctx, &rtiv1.UnassociateRegionsForUpdatesRequest{WireVersion: 1,
				FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, AttributeRegions: pairs}, grpc.Trailer(&trailer))
		}
		expectRPCError(t, err, trailer, codes.PermissionDenied, "RegionNotCreatedByThisFederate")

		for _, query := range []struct {
			name   string
			region uint64
			want   int
		}{{"near", nearQuery, 1}, {"far", farQuery, 0}} {
			tag := []byte(action + "-" + query.name)
			_, err = f.ddm.RequestAttributeValueUpdateWithRegions(f.ctx, &rtiv1.RequestAttributeValueUpdateWithRegionsRequest{WireVersion: 1,
				FederationName: f.name, FederateHandle: f.handles[3], ObjectClassHandle: f.class,
				AttributeHandles: []uint64{f.position}, RegionHandles: []uint64{query.region}, UserSuppliedTag: tag})
			check(t, err)
			f.barrier()
			got := f.provides(0, tag)
			if len(got) != query.want || (len(got) == 1 && (got[0].ObjectHandle != r.ObjectHandle || !slices.Equal(got[0].AttributeHandles, []uint64{f.position}))) {
				t.Errorf("%s changed current-owner routing for %s: callbacks=%v, want=%d", action, query.name, got, query.want)
			}
			for who := 1; who < len(f.handles); who++ {
				if got := f.provides(who, tag); len(got) != 0 {
					t.Errorf("%s routed owner request to nonowner %d: %v", action, who, got)
				}
			}
		}
	}
	var trailer metadata.MD
	_, err = f.ddm.DeleteRegion(f.ctx, &rtiv1.DeleteRegionRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], RegionHandle: publisher}, grpc.Trailer(&trailer))
	expectRPCError(t, err, trailer, codes.FailedPrecondition, "RegionInUseForUpdateOrSubscription")
}

func TestProductionRegionRequestsIncludeInheritedClassAttributes(t *testing.T) {
	f := startFederation(t)
	child, err := f.support.GetObjectClassHandle(f.ctx, &rtiv1.GetObjectClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAobjectRoot.Vehicle.SpecialVehicle"})
	check(t, err)
	position, err := f.support.GetAttributeHandle(f.ctx, &rtiv1.GetAttributeHandleRequest{WireVersion: 1, FederationName: f.name, ClassHandle: child.ClassHandle, AttributeName: "Position"})
	check(t, err)
	if position.AttributeHandle == f.position {
		t.Fatal("fixture must exercise class-relative handle remapping")
	}
	_, err = f.decl.PublishObjectClassAttributes(f.ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectClassHandle: child.ClassHandle, AttributeHandles: []uint64{position.AttributeHandle}})
	check(t, err)
	r, err := f.ddm.RegisterObjectInstanceWithRegions(f.ctx, &rtiv1.RegisterObjectWithRegionsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectClassHandle: child.ClassHandle,
		AttributeRegions: []*rtiv1.AttributeRegions{{AttributeHandle: position.AttributeHandle, RegionHandles: []uint64{f.region(0, 10, 20)}}}})
	check(t, err)
	for _, scoped := range []bool{false, true} {
		tag := []byte{0xff, byte(0)}
		if scoped {
			tag[1] = 1
		}
		if scoped {
			_, err = f.ddm.RequestAttributeValueUpdateWithRegions(f.ctx, &rtiv1.RequestAttributeValueUpdateWithRegionsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[3], ObjectClassHandle: f.class,
				AttributeHandles: []uint64{f.position}, RegionHandles: []uint64{f.region(3, 12, 18)}, UserSuppliedTag: tag})
		} else {
			_, err = f.objects.RequestClassAttributeValueUpdate(f.ctx, &rtiv1.RequestClassAttributeValueUpdateRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[3], ObjectClassHandle: f.class,
				AttributeHandles: []uint64{f.position}, UserSuppliedTag: tag})
		}
		check(t, err)
		f.barrier()
		got := f.provides(0, tag)
		if len(got) != 1 || got[0].ObjectHandle != r.ObjectHandle || !slices.Equal(got[0].AttributeHandles, []uint64{position.AttributeHandle}) {
			t.Errorf("scoped=%v inherited request=%v", scoped, got)
		}
	}
}

func TestProductionDDMClearOnlyAssociatedOwnedAttributes(t *testing.T) {
	f := startFederation(t)
	_, err := f.decl.UnpublishObjectClassAttributes(f.ctx, &rtiv1.UnpubObjAttrsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.velocity}})
	check(t, err)
	region := f.region(0, 10, 20)
	f.reserve("partial-publication")
	r, err := f.register("partial-publication", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{region}})
	check(t, err)
	_, err = f.ddm.UnassociateRegionsForUpdates(f.ctx, &rtiv1.UnassociateRegionsForUpdatesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle})
	check(t, err)
	_, err = f.ddm.DeleteRegion(f.ctx, &rtiv1.DeleteRegionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], RegionHandle: region})
	check(t, err)
}
