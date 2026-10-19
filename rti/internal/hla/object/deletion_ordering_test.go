package object

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type deletionOrderingFOM struct{ knownOwnershipFOM }

func (f *deletionOrderingFOM) LookupAttribute(cls core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	if name == "HLAprivilegeToDeleteObject" {
		if cls == 1 {
			return 1, true
		}
		if cls == 9 {
			return 101, true
		}
	}
	return f.knownOwnershipFOM.LookupAttribute(cls, name)
}

func TestGlobalDeletionPublishesProjectedTerminalBeforeRemoval(t *testing.T) {
	ctx := context.Background()
	r, _, owners, obj := knownOwnershipPreparationFixture(t)
	out := &atomicUpdateOutbox{}
	r.opts.Outbox = out
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 1}
	transportationOK(t, owners.EnableOwnershipCallbackReceipts(ctx, scope))
	transportationOK(t, owners.Acquire(ctx, "fed", 2, obj, []core.AttributeHandle{1}, nil))
	transportationOK(t, r.Delete(ctx, "fed", 1, obj, nil, nil))
	if len(out.records) != 2 {
		t.Fatalf("deletion callback count=%d", len(out.records))
	}
	terminal, ok := out.records[0].Event.(*ownership.CallbackEvent)
	if !ok || terminal.Kind != ownership.CallbackUnavailable || !reflect.DeepEqual(terminal.Attributes, []core.AttributeHandle{101}) {
		t.Fatalf("first callback is not projected terminal: %+v", out.records[0].Event)
	}
	_, id, strict := terminal.OwnershipCallbackIdentity()
	if id == 0 || !strict {
		t.Fatal("precommit projection lost identity")
	}
	removal, ok := out.records[1].Event.(*CallbackEvent)
	if !ok || removal.Kind != CallbackRemove || removal.ObjectClass != 9 {
		t.Fatalf("second callback is not known-class removal: %+v", out.records[1].Event)
	}
	state := owners.ExportState("fed")
	if len(state.PendingCallbacks) != 1 || !reflect.DeepEqual(state.PendingCallbacks[0].Attributes, []core.AttributeHandle{1}) || state.PendingCallbacks[0].RetainedProjection == nil {
		t.Fatalf("canonical obligation lost: %+v", state.PendingCallbacks)
	}
	if r.ClassOf("fed", obj) != 0 || !owners.HasAcquisitionObligation("fed", 2, obj, nil) {
		t.Fatal("deletion did not preserve strict terminal lifetime")
	}
}

func TestResignProjectionUsesExistingPinWithoutOwnershipReentry(t *testing.T) {
	r, _, owners, obj := knownOwnershipPreparationFixture(t)
	plan := r.PrepareResignDeletions(context.Background(), "fed", 1, false)
	defer plan.Release()
	ownerPlan, err := owners.PrepareResign("fed", 1, core.ResignActionUnconditionallyDivestAttributes, nil)
	transportationOK(t, err)
	defer ownerPlan.Release()
	type result struct {
		class core.ObjectClassHandle
		attrs []core.AttributeHandle
		err   error
	}
	for _, recipient := range []core.FederateHandle{2, 99} {
		done := make(chan result, 1)
		go func() {
			class, attrs, err := plan.ProjectKnownObjectAttributes(context.Background(), "fed", recipient, obj, []core.AttributeHandle{1})
			done <- result{class, attrs, err}
		}()
		select {
		case got := <-done:
			if recipient == 2 {
				if got.err != nil || got.class != 9 || len(got.attrs) != 1 || got.attrs[0] != 101 {
					t.Fatalf("known projection=%+v", got)
				}
			} else if !errors.Is(got.err, core.ErrObjectHandleInvalid) {
				t.Fatalf("unknown recipient projection=%+v", got)
			}
		case <-time.After(time.Second):
			ownerPlan.Release()
			<-done
			t.Fatal("projection recursively acquired ownership lock")
		}
	}
	ownerPlan.Release()
	plan.Release()
	if _, _, err := plan.ProjectKnownObjectAttributes(context.Background(), "fed", 2, obj, []core.AttributeHandle{1}); !errors.Is(err, core.ErrObjectHandleInvalid) {
		t.Fatal("released plan accessed unpinned state", err)
	}
}

func TestInteger64DeletionPreservesRegistrantAndExactTimestamp(t *testing.T) {
	base, decl, owners, _ := knownOwnershipPreparationFixture(t)
	r, err := NewOf[int64](OptionsOf[int64]{Clock: base.opts.Clock, FOMs: &deletionOrderingFOMs{}, Declarations: decl, Outbox: &atomicUpdateOutbox{}, Ownership: owners, OnRegister: base.opts.OnRegister})
	transportationOK(t, err)
	transportationOK(t, decl.PublishObjectClassAttributes(context.Background(), "integer", 1, 1, []core.AttributeHandle{1, 2}))
	reserveFixtureName(t, r, "integer", 1, "exact-delete")
	obj, _, err := r.Register(context.Background(), "integer", 1, 1, "exact-delete")
	transportationOK(t, err)
	st := r.stateFor("integer")
	st.mu.Lock()
	st.discovered[discoverKey{sub: 2, obj: obj}] = 9
	st.mu.Unlock()
	owners.RegisterInitialOwnership("integer", 2, obj, []core.AttributeHandle{1})
	ts := int64(1<<53 + 17)
	transportationOK(t, r.Delete(context.Background(), "integer", 2, obj, &ts, nil))
	count := 0
	for _, d := range r.opts.Outbox.(*atomicUpdateOutbox).records {
		e := d.Event.(*CallbackEventOf[int64])
		if e.Kind == CallbackRemove {
			count++
		}
		if e.Kind == CallbackRemove && (e.Metadata.Producer != 1 || e.LogicalTime == nil || *e.LogicalTime != ts) {
			t.Fatalf("integer removal=%+v", e)
		}
	}
	if count != 1 {
		t.Fatalf("integer removal count=%d", count)
	}
}

type deletionOrderingFOMs struct{ knownOwnershipFOMs }

func (*deletionOrderingFOMs) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return &deletionOrderingFOM{}, nil
}

func TestDeletionProducingFederateRemainsRegistrant(t *testing.T) {
	for _, resign := range []bool{false, true} {
		name := testDelete
		if resign {
			name = "resign"
		}
		t.Run(name, func(t *testing.T) {
			r, decl, owners, obj := knownOwnershipPreparationFixture(t)
			r.opts.FOMs = &deletionOrderingFOMs{}
			ctx := context.Background()
			transportationOK(t, decl.SubscribeObjectClassAttributes(ctx, "fed", 3, 9, []core.AttributeHandle{101}))
			transportationOK(t, owners.UnconditionalDivest(ctx, "fed", 1, obj, []core.AttributeHandle{1}))
			transportationOK(t, owners.AcquireIfAvailable(ctx, "fed", 2, obj, []core.AttributeHandle{1}, nil))
			if resign {
				plan := r.PrepareResignDeletions(ctx, "fed", 2, true)
				defer plan.Release()
				found := false
				for _, d := range plan.Deliveries() {
					if d.Recipient != 3 {
						continue
					}
					found = true
					e := d.Event.(*CallbackEvent)
					if e.Metadata.Producer != 1 || e.ObjectClass != 9 {
						t.Fatalf("removal producer/class=%d/%d", e.Metadata.Producer, e.ObjectClass)
					}
				}
				if !found {
					t.Fatal("known recipient missing removal")
				}
			} else {
				out := r.opts.Outbox.(*recordingOutbox)
				transportationOK(t, r.Delete(ctx, "fed", 2, obj, nil, []byte("deleted")))
				found := false
				for _, d := range out.Records() {
					e, ok := d.Event.(*CallbackEvent)
					if !ok || e.Kind != CallbackRemove || d.Federate != 3 {
						continue
					}
					found = true
					if e.Metadata.Producer != 1 || e.ObjectClass != 9 {
						t.Fatalf("removal producer/class=%d/%d", e.Metadata.Producer, e.ObjectClass)
					}
				}
				if !found {
					t.Fatal("known recipient missing removal")
				}
			}
		})
	}
}

func TestResignPlanReturnsDetachedSelections(t *testing.T) {
	r, _, _, obj := knownOwnershipPreparationFixture(t)
	plan := r.PrepareResignDeletions(context.Background(), "fed", 1, true)
	defer plan.Release()
	selected := plan.Objects()
	selected[0] = core.ObjectHandle(999)
	if plan.Objects()[0] != obj {
		t.Fatal("caller altered committed deletion selection")
	}
	deliveries := plan.Deliveries()
	deliveries[0].Recipient = 99
	if plan.Deliveries()[0].Recipient == 99 {
		t.Fatal("caller altered committed removal selection")
	}
}

func TestResignDeletionCleansResourcesWithoutErasingTerminalOutcome(t *testing.T) {
	for _, commit := range []bool{false, true} {
		name := "release"
		if commit {
			name = testCommit
		}
		t.Run(name, func(t *testing.T) {
			f := newAssociationValidationFixture(t)
			ctx := context.Background()
			scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 4}
			transportationOK(t, f.owners.EnableOwnershipCallbackReceipts(ctx, scope))
			transportationOK(t, f.owners.AcquireIfAvailable(ctx, "fed", 2, f.object, []core.AttributeHandle{1}, nil))
			projection, err := f.registry.CaptureKnownObjectAttributes(ctx, "fed", 2, f.object, []core.AttributeHandle{1})
			transportationOK(t, err)
			_, err = f.registry.PrepareAttributeTransportationChange(ctx, transportationScope(), f.object, []core.AttributeHandle{1}, core.TransportTypeBestEffort)
			transportationOK(t, err)
			beforeObjects, beforeRegions := f.registry.ExportState("fed"), f.regions.ExportState("fed")
			objects := f.registry.PrepareResignDeletions(ctx, "fed", 1, true)
			owners, err := f.owners.PrepareResign("fed", 1, core.ResignActionDeleteObjects, objects.Objects())
			if err != nil {
				objects.Release()
				t.Fatal(err)
			}
			if !commit {
				owners.Release()
				objects.Release()
				transportationOK(t, projection.Validate())
				if !reflect.DeepEqual(beforeObjects, f.registry.ExportState("fed")) || !reflect.DeepEqual(beforeRegions, f.regions.ExportState("fed")) {
					t.Fatal("released deletion changed resource state")
				}
				return
			}
			owners.Commit()
			objects.CommitState()
			objects.Finish()
			owners.Finish(ctx, nil)
			if err := projection.Validate(); !errors.Is(err, ErrStaleObjectKnowledge) {
				t.Fatal("deleted knowledge epoch remained usable", err)
			}
			if _, _, known := f.registry.LookupKnownObjectInstance("fed", 2, f.object); known {
				t.Fatal("deleted object remains discoverable")
			}
			if f.regions.HasObjectAssociations("fed", f.object) {
				t.Fatal("deleted object retains DDM associations")
			}
			if len(f.registry.transports.tickets) != 0 {
				t.Fatal("deleted object retains transport activation tickets")
			}
			if !f.owners.HasAcquisitionObligation("fed", 2, f.object, nil) {
				t.Fatal("resource cleanup erased uninvoked terminal outcome")
			}
		})
	}
}
