package save_restore_test

import (
	"bytes"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (f *federation) declare(publisher, subscriber int) {
	f.t.Helper()
	_, err := f.decl.PublishObjectClassAttributes(f.ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[publisher], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.attr}})
	check(f.t, err)
	_, err = f.decl.SubscribeObjectClassAttributes(f.ctx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[subscriber], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.attr}})
	check(f.t, err)
	_, err = f.decl.PublishInteractionClass(f.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[publisher], InteractionClassHandle: f.inter})
	check(f.t, err)
	_, err = f.decl.SubscribeInteractionClass(f.ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[subscriber], InteractionClassHandle: f.inter})
	check(f.t, err)
}

func (f *federation) undeclare(publisher, subscriber int) {
	f.t.Helper()
	_, err := f.decl.UnpublishObjectClassAttributes(f.ctx, &rtiv1.UnpubObjAttrsRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[publisher], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.attr}})
	check(f.t, err)
	_, err = f.decl.UnsubscribeObjectClassAttributes(f.ctx, &rtiv1.UnsubObjAttrsRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[subscriber], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.attr}})
	check(f.t, err)
	_, err = f.decl.UnpublishInteractionClass(f.ctx, &rtiv1.UnpubInterRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[publisher], InteractionClassHandle: f.inter})
	check(f.t, err)
	_, err = f.decl.UnsubscribeInteractionClass(f.ctx, &rtiv1.UnsubInterRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[subscriber], InteractionClassHandle: f.inter})
	check(f.t, err)
}

func (f *federation) register(owner int, name string) uint64 {
	f.t.Helper()
	f.reserve(owner, name, true)
	r, err := f.objects.RegisterObjectInstance(f.ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[owner], ObjectClassHandle: f.class, ObjectName: name})
	check(f.t, err)
	if r.ObjectHandle == 0 || r.ObjectName != name {
		f.t.Fatalf("registration returned invalid identity: %v", r)
	}
	return r.ObjectHandle
}

func (f *federation) lookup(name string, want uint64) {
	f.t.Helper()
	r, err := f.support.GetObjectInstanceHandle(f.ctx, &rtiv1.GetObjectInstanceHandleRequest{WireVersion: 1, FederationName: f.name, ObjectName: name})
	if want == 0 {
		if status.Code(err) != codes.NotFound {
			f.t.Errorf("object %q must be absent; response=%v error=%v", name, r, err)
		}
		return
	}
	if err != nil || r.GetObjectHandle() != want {
		f.t.Errorf("object %q: handle=%d error=%v, want %d", name, r.GetObjectHandle(), err, want)
		return
	}
	reverse, err := f.support.GetObjectInstanceName(f.ctx, &rtiv1.GetObjectInstanceNameRequest{WireVersion: 1, FederationName: f.name, ObjectHandle: want})
	if err != nil || reverse.GetObjectName() != name {
		f.t.Errorf("object handle %d: name=%q error=%v, want %q", want, reverse.GetObjectName(), err, name)
	}
}

func (f *federation) reserve(owner int, name string, success bool) {
	f.t.Helper()
	mark := f.callbacks[owner].mark()
	_, err := f.objects.ReserveObjectInstanceName(f.ctx, &rtiv1.ReserveObjectInstanceNameRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[owner], ObjectName: name})
	check(f.t, err)
	e, _ := f.callbacks[owner].wait(f.t, mark, "name reservation result", func(e *rtiv1.FederateEvent) bool {
		return e.GetReservationSucceeded().GetObjectName() == name || e.GetReservationFailed().GetObjectName() == name
	})
	if (e.GetReservationSucceeded() != nil) != success {
		f.t.Fatalf("reservation %q by participant %d: got %v, success=%v", name, owner, e, success)
	}
}

func (f *federation) release(owner int, name string) {
	f.t.Helper()
	_, err := f.objects.ReleaseObjectInstanceName(f.ctx, &rtiv1.ReleaseObjectInstanceNameRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[owner], ObjectName: name})
	check(f.t, err)
}

func (f *federation) enableTime() {
	f.t.Helper()
	for _, h := range f.handles {
		_, err := f.time.EnableTimeRegulation(f.ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h, Lookahead: 0.5})
		check(f.t, err)
		_, err = f.time.EnableTimeConstrained(f.ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h})
		check(f.t, err)
	}
}

func (f *federation) advance(value float64) [2]int {
	f.t.Helper()
	var marks, grants [2]int
	for i, h := range f.handles {
		marks[i] = f.callbacks[i].mark()
		_, err := f.time.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h, LogicalTime: value})
		check(f.t, err)
	}
	for i, log := range f.callbacks {
		_, grants[i] = log.wait(f.t, marks[i], fmt.Sprintf("time grant %g", value), func(e *rtiv1.FederateEvent) bool {
			return e.GetGrant() != nil && e.GetGrant().LogicalTime == value
		})
	}
	f.logicalTime(value)
	return grants
}

func (f *federation) logicalTime(want float64) {
	f.t.Helper()
	for _, h := range f.handles {
		r, err := f.time.QueryLogicalTime(f.ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h})
		check(f.t, err)
		if r.LogicalTime != want {
			f.t.Errorf("participant %d logical time = %g, want %g", h, r.LogicalTime, want)
		}
	}
}

func (f *federation) save(label string) {
	f.t.Helper()
	marks := [2]int{f.callbacks[0].mark(), f.callbacks[1].mark()}
	_, err := f.savepoint.RequestFederationSave(f.ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], Label: label})
	check(f.t, err)
	for i, h := range f.handles {
		f.callbacks[i].wait(f.t, marks[i], "initiate save", func(e *rtiv1.FederateEvent) bool { return e.GetSaveInitiate().GetLabel() == label })
		_, err = f.savepoint.FederateSaveBegun(f.ctx, &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h})
		check(f.t, err)
		_, err = f.savepoint.FederateSaveComplete(f.ctx, &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h})
		check(f.t, err)
	}
	for i, log := range f.callbacks {
		e, _ := log.wait(f.t, marks[i], "federation save outcome", func(e *rtiv1.FederateEvent) bool {
			return e.GetSaveCompleted().GetLabel() == label || e.GetSaveFailed().GetLabel() == label
		})
		if e.GetSaveCompleted() == nil {
			f.t.Fatalf("production save reported failure: %v", e.GetSaveFailed())
		}
	}
	state, err := f.savepoint.QuerySaveState(f.ctx, &rtiv1.QuerySaveStateRequest{WireVersion: 1, FederationName: f.name, Label: label})
	check(f.t, err)
	if state.State != rtiv1.SaveState_SAVE_STATE_SAVED {
		f.t.Fatalf("save state: %v", state)
	}
}

func (f *federation) beginRestore(label string) [2]int {
	f.t.Helper()
	marks := [2]int{f.callbacks[0].mark(), f.callbacks[1].mark()}
	_, err := f.savepoint.RequestFederationRestore(f.ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], Label: label})
	check(f.t, err)
	_, request := f.callbacks[0].wait(f.t, marks[0], "restore request acknowledgement", func(e *rtiv1.FederateEvent) bool {
		return e.GetRestoreRequestSucceeded().GetLabel() == label
	})
	for i, log := range f.callbacks {
		_, begun := log.wait(f.t, marks[i], "restore begun", func(e *rtiv1.FederateEvent) bool { return e.GetRestoreBegun() != nil })
		e, initiated := log.wait(f.t, marks[i], "initiate restore", func(e *rtiv1.FederateEvent) bool { return e.GetRestoreInitiate().GetLabel() == label })
		if begun >= initiated || (i == 0 && request >= begun) {
			f.t.Fatalf("participant %d restore callback order: request=%d begun=%d initiate=%d", i, request, begun, initiated)
		}
		if e.GetRestoreInitiate().FederateHandle != f.handles[i] || e.GetRestoreInitiate().FederateName != fmt.Sprintf("participant-%d", i) {
			f.t.Fatalf("restore identity changed with the same roster: %v", e)
		}
	}
	return marks
}

func (f *federation) completeRestore(participant int) {
	f.t.Helper()
	_, err := f.savepoint.FederateRestoreComplete(f.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[participant]})
	check(f.t, err)
}

func (f *federation) restoreOutcome(label string, marks [2]int, success bool) {
	f.t.Helper()
	for i, log := range f.callbacks {
		e, _ := log.wait(f.t, marks[i], "federation restore outcome", func(e *rtiv1.FederateEvent) bool {
			return e.GetRestoreCompleted().GetLabel() == label || e.GetRestoreFailed().GetLabel() == label
		})
		if (e.GetRestoreCompleted() != nil) != success {
			f.t.Fatalf("restore success=%v, callback=%v", success, e)
		}
	}
	state, err := f.savepoint.QueryRestoreState(f.ctx, &rtiv1.QueryRestoreStateRequest{WireVersion: 1, FederationName: f.name, Label: label})
	check(f.t, err)
	want := rtiv1.RestoreState_RESTORE_STATE_FAILED
	if success {
		want = rtiv1.RestoreState_RESTORE_STATE_COMPLETED
	}
	if state.State != want {
		f.t.Fatalf("restore state = %v, want %v", state.State, want)
	}
}

func (f *federation) restore(label string) {
	f.t.Helper()
	marks := f.beginRestore(label)
	f.completeRestore(0)
	state, err := f.savepoint.QueryRestoreState(f.ctx, &rtiv1.QueryRestoreStateRequest{WireVersion: 1, FederationName: f.name, Label: label})
	check(f.t, err)
	if state.State != rtiv1.RestoreState_RESTORE_STATE_INITIATED {
		f.t.Fatalf("restore completed before both participants responded: %v", state)
	}
	f.completeRestore(1)
	f.restoreOutcome(label, marks, true)
}

func (f *federation) send(sender int, payload []byte, timestamp *float64) error {
	_, err := f.objects.SendInteraction(f.ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[sender], InteractionClassHandle: f.inter, Parameters: map[uint64][]byte{f.param: payload}, LogicalTime: timestamp})
	return err
}

func (f *federation) expectInteraction(receiver, from int, payload []byte) int {
	f.t.Helper()
	_, at := f.callbacks[receiver].wait(f.t, from, "interaction payload", func(e *rtiv1.FederateEvent) bool {
		return e.GetReceive().GetInteractionClassHandle() == f.inter && bytes.Equal(e.GetReceive().GetParameters()[f.param], payload)
	})
	return at
}

func (f *federation) update(owner int, object uint64, payload []byte) error {
	_, err := f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[owner], ObjectHandle: object, Attributes: map[uint64][]byte{f.attr: payload}})
	return err
}

func (f *federation) expectReflection(receiver, from int, object uint64, payload []byte) {
	f.t.Helper()
	f.callbacks[receiver].wait(f.t, from, "attribute reflection", func(e *rtiv1.FederateEvent) bool {
		return e.GetReflect().GetObjectHandle() == object && bytes.Equal(e.GetReflect().GetAttributes()[f.attr], payload)
	})
}
