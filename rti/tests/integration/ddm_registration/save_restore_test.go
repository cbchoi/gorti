package ddm_registration_test

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (f *federation) save(label string) {
	f.t.Helper()
	_, err := f.savepoint.RequestFederationSave(f.ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], Label: label})
	check(f.t, err)
	for who, handle := range f.handles {
		f.callbacks[who].wait(f.t, "initiate save", func(e *rtiv1.FederateEvent) bool { return e.GetSaveInitiate().GetLabel() == label })
		request := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: f.name, FederateHandle: handle}
		_, err = f.savepoint.FederateSaveBegun(f.ctx, request)
		check(f.t, err)
		_, err = f.savepoint.FederateSaveComplete(f.ctx, request)
		check(f.t, err)
	}
	for _, log := range f.callbacks {
		e := log.wait(f.t, "save outcome", func(e *rtiv1.FederateEvent) bool {
			return e.GetSaveCompleted().GetLabel() == label || e.GetSaveFailed().GetLabel() == label
		})
		if e.GetSaveCompleted() == nil {
			f.t.Fatalf("save failed: %v", e)
		}
	}
	state, err := f.savepoint.QuerySaveState(f.ctx, &rtiv1.QuerySaveStateRequest{WireVersion: 1, FederationName: f.name, Label: label})
	check(f.t, err)
	if state.State != rtiv1.SaveState_SAVE_STATE_SAVED {
		f.t.Fatalf("save state: %v", state)
	}
}

func (f *federation) restore(label string) {
	f.t.Helper()
	_, err := f.savepoint.RequestFederationRestore(f.ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], Label: label})
	check(f.t, err)
	f.callbacks[0].wait(f.t, "restore request acknowledgement", func(e *rtiv1.FederateEvent) bool {
		return e.GetRestoreRequestSucceeded().GetLabel() == label
	})
	for who, handle := range f.handles {
		f.callbacks[who].wait(f.t, "restore begun", func(e *rtiv1.FederateEvent) bool { return e.GetRestoreBegun() != nil })
		f.callbacks[who].wait(f.t, "initiate restore", func(e *rtiv1.FederateEvent) bool { return e.GetRestoreInitiate().GetLabel() == label })
		_, err = f.savepoint.FederateRestoreComplete(f.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: handle})
		check(f.t, err)
	}
	for _, log := range f.callbacks {
		e := log.wait(f.t, "restore outcome", func(e *rtiv1.FederateEvent) bool {
			return e.GetRestoreCompleted().GetLabel() == label || e.GetRestoreFailed().GetLabel() == label
		})
		if e.GetRestoreCompleted() == nil {
			f.t.Fatalf("restore failed: %v", e)
		}
	}
	state, err := f.savepoint.QueryRestoreState(f.ctx, &rtiv1.QueryRestoreStateRequest{WireVersion: 1, FederationName: f.name, Label: label})
	check(f.t, err)
	if state.State != rtiv1.RestoreState_RESTORE_STATE_COMPLETED {
		f.t.Fatalf("restore state: %v", state)
	}
}

// Same-process, same-roster engineering regression; not a restart-recovery claim.
func TestProductionRegisterObjectInstanceWithRegionsSaveRestoreAssociation(t *testing.T) {
	f := startFederationWithSave(t, true)
	publisher := f.region(0, 10, 20)
	f.subscribe(1, f.region(1, 15, 25))
	f.subscribe(2, f.region(2, 100, 200))
	f.subscribe(3)
	f.barrier()
	before := f.objectCount()
	f.reserve("saved-regional-object")
	r, err := f.register("saved-regional-object",
		&rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{publisher}},
		&rtiv1.AttributeRegions{AttributeHandle: f.velocity, RegionHandles: []uint64{publisher}})
	check(t, err)
	handle := f.identity(r, "saved-regional-object", before)
	f.barrier()
	f.expectCallbacks(1, handle, 1, 0, nil)
	f.expectCallbacks(2, handle, 0, 0, nil)
	f.expectCallbacks(3, handle, 1, 0, nil)
	f.save("regional-association")
	_, err = f.ddm.UnassociateRegionsForUpdates(f.ctx, &rtiv1.UnassociateRegionsForUpdatesRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: handle})
	check(t, err)
	f.barrier()
	f.expectCallbacks(2, handle, 1, 0, nil)
	// Removing the last producer region exposes the default region. Restore
	// returns server knowledge to the save point, but cannot erase delivered logs.
	knownRequest := &rtiv1.GetKnownObjectClassHandleRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[2], ObjectHandle: handle}
	_, err = f.support.GetKnownObjectClassHandle(f.ctx, knownRequest)
	check(t, err)
	deleteRegion := &rtiv1.DeleteRegionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], RegionHandle: publisher}
	_, err = f.ddm.DeleteRegion(f.ctx, deleteRegion)
	check(t, err)
	var deletedTrailer metadata.MD
	deleted, err := f.ddm.QueryBounds(f.regionQueryContext(publisher), &rtiv1.QueryBoundsRequest{WireVersion: 1,
		FederationName: f.name, RegionHandle: publisher, DimensionHandle: f.dimension}, grpc.Trailer(&deletedTrailer))
	expectRPCError(t, err, deletedTrailer, codes.NotFound, "InvalidRegion")
	if deleted != nil {
		t.Fatalf("mutation did not delete producer region: %v", deleted)
	}
	f.restore("regional-association")
	if _, err := f.support.GetKnownObjectClassHandle(f.ctx, knownRequest); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("restore retained post-save discovery: %v", err)
	}
	f.identity(r, "saved-regional-object", before)
	f.bounds(publisher, 10, 20)
	var trailer metadata.MD
	_, err = f.ddm.DeleteRegion(f.ctx, deleteRegion, grpc.Trailer(&trailer))
	expectRPCError(t, err, trailer, codes.FailedPrecondition, "RegionInUseForUpdateOrSubscription")
	value := []byte{0x40, 0x34, 0, 0, 0, 0, 0, 0}
	_, err = f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], ObjectHandle: handle, Attributes: map[uint64][]byte{f.position: value}})
	check(t, err)
	f.barrier()
	f.expectCallbacks(0, handle, 0, 0, value)
	f.expectCallbacks(1, handle, 1, 1, value)
	f.expectCallbacks(2, handle, 1, 0, value)
	f.expectCallbacks(3, handle, 1, 1, value)
	f.deleteObjectAndRegion(handle, publisher, "saved-regional-object", before)
}
