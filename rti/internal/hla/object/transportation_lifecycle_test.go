package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type transportationFOM struct{ roFOM }

func (f *transportationFOM) TransportationForAttribute(cls core.ObjectClassHandle, attr core.AttributeHandle) (core.TransportType, bool) {
	_, ok := f.AttributeName(cls, attr)
	return core.TransportTypeBestEffort, ok
}

func (f *transportationFOM) TransportationForInteraction(cls core.InteractionClassHandle) (core.TransportType, bool) {
	_, ok := f.InteractionClassName(cls)
	return core.TransportTypeBestEffort, ok
}

type transportationFOMRepository struct{ stubFOMRepo }

func (*transportationFOMRepository) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return &transportationFOM{}, nil
}

type transportationFixture struct {
	r      *Registry
	decl   *declaration.Manager
	owners *ownership.Manager
	out    *recordingOutbox
	obj    core.ObjectHandle
}

func newTransportationFixture(t *testing.T) transportationFixture {
	t.Helper()
	ctx := context.Background()
	r, decl, out := newTestRegistry(t, nil)
	r.opts.FOMs = &transportationFOMRepository{}
	owners, err := ownership.New(ownership.Options{
		Outbox: out, PrepareAcquisition: r.PrepareOwnershipAcquisition,
		PrepareDivestiture: r.PrepareOwnershipReleaseDenied, PrepareReleaseDenied: r.PrepareOwnershipReleaseDenied,
	})
	transportationOK(t, err)
	r.opts.Ownership = owners
	r.opts.OnRegister = func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
		owners.RegisterInitialOwnership(fed, h, obj, attrs)
	}
	for _, h := range []core.FederateHandle{1, 2, 3} {
		transportationOK(t, decl.PublishObjectClassAttributes(ctx, "fed", h, 1, []core.AttributeHandle{1, 2}))
		transportationOK(t, decl.SubscribeObjectClassAttributes(ctx, "fed", h, 1, []core.AttributeHandle{1, 2}))
		transportationOK(t, decl.PublishInteractionClass(ctx, "fed", h, 7))
		transportationOK(t, decl.SubscribeInteractionClass(ctx, "fed", h, 7))
	}
	reserveFixtureName(t, r, "fed", 1, "transport-item")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "transport-item")
	transportationOK(t, err)
	transportationOK(t, r.RequestAttributeTransportationTypeChange(ctx, "fed", 1, obj, []core.AttributeHandle{1, 2}, core.TransportTypeReliable))
	transportationOK(t, r.RequestInteractionTransportationTypeChange(ctx, "fed", 1, 7, core.TransportTypeReliable))
	return transportationFixture{r, decl, owners, out, obj}
}

func transportationOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (f transportationFixture) checkAttribute(t *testing.T, attr core.AttributeHandle, want core.TransportType) {
	t.Helper()
	got, err := f.r.QueryAttributeTransportationType(context.Background(), "fed", 3, f.obj, attr)
	transportationOK(t, err)
	if got != want {
		t.Errorf("attribute %d transportation=%v, want %v", attr, got, want)
	}
}

func TestTransportationUnownedAttributeUsesFDDDefault(t *testing.T) {
	f := newTransportationFixture(t)
	transportationOK(t, f.owners.UnconditionalDivest(context.Background(), "fed", 1, f.obj, []core.AttributeHandle{1}))
	f.checkAttribute(t, 1, core.TransportTypeBestEffort)
	f.checkAttribute(t, 2, core.TransportTypeReliable)
	if got := f.r.AttributeTransportType("fed", f.obj, 1); got != core.TransportTypeUnspecified {
		t.Errorf("divested attribute retains transport override %v", got)
	}
}

func TestTransportationUnownedQueryIgnoresStaleSnapshotOverride(t *testing.T) {
	f := newTransportationFixture(t)
	transportationOK(t, f.owners.UnconditionalDivest(context.Background(), "fed", 1, f.obj, []core.AttributeHandle{1}))
	// A legacy snapshot may contain a transport override for an unowned attribute.
	f.r.transports.mu.Lock()
	f.r.transports.attr[attrTransportKey{"fed", f.obj, 1}] = core.TransportTypeReliable
	f.r.transports.mu.Unlock()
	f.checkAttribute(t, 1, core.TransportTypeBestEffort)
}

func TestTransportationReacquisitionResetsWithoutInterveningQuery(t *testing.T) {
	f := newTransportationFixture(t)
	ctx := context.Background()
	transportationOK(t, f.owners.UnconditionalDivest(ctx, "fed", 1, f.obj, []core.AttributeHandle{1}))
	transportationOK(t, f.owners.AcquireIfAvailable(ctx, "fed", 1, f.obj, []core.AttributeHandle{1}, nil))
	f.checkAttribute(t, 1, core.TransportTypeBestEffort)
	f.checkAttribute(t, 2, core.TransportTypeReliable)
}

func TestTransportationEveryCompletedOwnershipTransferResetsOnlyChangedAttributes(t *testing.T) {
	for _, method := range []string{"unconditional-then-acquire", "unconditional-with-waiter", "negotiated-then-acquire", "acquire-then-negotiated", "two-phase", "if-wanted"} {
		t.Run(method, func(t *testing.T) {
			f := newTransportationFixture(t)
			ctx := context.Background()
			attrs := []core.AttributeHandle{1}
			switch method {
			case "unconditional-then-acquire":
				transportationOK(t, f.owners.UnconditionalDivest(ctx, "fed", 1, f.obj, attrs))
				transportationOK(t, f.owners.AcquireIfAvailable(ctx, "fed", 2, f.obj, attrs, nil))
			case "unconditional-with-waiter":
				transportationOK(t, f.owners.Acquire(ctx, "fed", 2, f.obj, attrs, nil))
				transportationOK(t, f.owners.UnconditionalDivest(ctx, "fed", 1, f.obj, attrs))
			case "negotiated-then-acquire":
				transportationOK(t, f.owners.NegotiatedDivest(ctx, "fed", 1, f.obj, attrs, nil))
				transportationOK(t, f.owners.Acquire(ctx, "fed", 2, f.obj, attrs, nil))
			case "acquire-then-negotiated":
				transportationOK(t, f.owners.Acquire(ctx, "fed", 2, f.obj, attrs, nil))
				transportationOK(t, f.owners.NegotiatedDivest(ctx, "fed", 1, f.obj, attrs, nil))
			case "two-phase":
				transportationOK(t, f.owners.NegotiatedDivestTwoPhase(ctx, "fed", 1, f.obj, attrs, nil))
				transportationOK(t, f.owners.Acquire(ctx, "fed", 2, f.obj, attrs, nil))
				f.checkAttribute(t, 1, core.TransportTypeReliable)
				transportationOK(t, f.owners.ConfirmDivestiture(ctx, "fed", 1, f.obj, attrs))
			case "if-wanted":
				transportationOK(t, f.owners.Acquire(ctx, "fed", 2, f.obj, attrs, nil))
				_, err := f.owners.DivestIfWantedWithResult(ctx, "fed", 1, f.obj, attrs)
				transportationOK(t, err)
			}
			if owner, owned := f.owners.QueryOwnership("fed", f.obj, 1); !owned || owner != 2 {
				t.Fatalf("transfer did not complete: owner=%d owned=%v", owner, owned)
			}
			f.checkAttribute(t, 1, core.TransportTypeBestEffort)
			f.checkAttribute(t, 2, core.TransportTypeReliable)
			state := f.r.ExportState("fed")
			if len(state.AttributeTransports) != 1 || state.AttributeTransports[0].Attribute != 2 {
				t.Errorf("snapshot contains stale transportation override: %+v", state.AttributeTransports)
			}
			transportationOK(t, f.r.UpdateAttributesWithMetadata(ctx, "fed", 2, f.obj, map[core.AttributeHandle][]byte{1: {42}}, nil, core.ObjectMessageOptions{}))
			reflections := 0
			for _, record := range f.out.Records() {
				if event, ok := record.Event.(*CallbackEvent); ok && event.Kind == CallbackReflect {
					reflections++
					if event.Metadata.Transportation != core.TransportTypeBestEffort {
						t.Errorf("new-owner reflection transportation=%v", event.Metadata.Transportation)
					}
				}
			}
			if reflections == 0 {
				t.Fatal("missing actual new-owner reflection")
			}
		})
	}
}

func TestTransportationPendingCancelledAndFailedOwnershipPreservesOverrides(t *testing.T) {
	f := newTransportationFixture(t)
	ctx := context.Background()
	attrs := []core.AttributeHandle{1}
	transportationOK(t, f.owners.NegotiatedDivestTwoPhase(ctx, "fed", 1, f.obj, attrs, nil))
	transportationOK(t, f.owners.Acquire(ctx, "fed", 2, f.obj, attrs, nil))
	f.checkAttribute(t, 1, core.TransportTypeReliable)
	transportationOK(t, f.owners.CancelAcquire(ctx, "fed", 2, f.obj, attrs))
	transportationOK(t, f.owners.CancelDivest(ctx, "fed", 1, f.obj, attrs))
	transportationOK(t, f.owners.AcquireIfAvailable(ctx, "fed", 2, f.obj, attrs, nil))
	if err := f.owners.UnconditionalDivest(ctx, "fed", 2, f.obj, attrs); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("nonowner divest error=%v", err)
	}
	if err := f.owners.UnconditionalDivest(ctx, "fed", 1, f.obj, []core.AttributeHandle{1, 99}); !errors.Is(err, core.ErrAttributeNotFound) {
		t.Fatalf("invalid mixed divest error=%v", err)
	}
	f.checkAttribute(t, 1, core.TransportTypeReliable)
	f.checkAttribute(t, 2, core.TransportTypeReliable)
}

func TestTransportationUnpublishedQueryUsesFDDWithoutChangingOtherPublishers(t *testing.T) {
	f := newTransportationFixture(t)
	ctx := context.Background()
	transportationOK(t, f.r.RequestInteractionTransportationTypeChange(ctx, "fed", 2, 7, core.TransportTypeReliable))
	transportationOK(t, f.decl.UnpublishInteractionClass(ctx, "fed", 1, 7))
	for _, target := range []core.FederateHandle{1, 2, 3, 999} {
		got, err := f.r.QueryInteractionTransportationType(ctx, "fed", 3, target, 7)
		transportationOK(t, err)
		want := core.TransportTypeBestEffort
		if target == 2 {
			want = core.TransportTypeReliable
		}
		if got != want {
			t.Errorf("producer %d transport=%v, want %v", target, got, want)
		}
	}
	// Unpublish has no transportation-reset postcondition. A query must not mutate the stored choice.
	transportationOK(t, f.decl.PublishInteractionClass(ctx, "fed", 1, 7))
	got, err := f.r.QueryInteractionTransportationType(ctx, "fed", 3, 1, 7)
	transportationOK(t, err)
	if got != core.TransportTypeReliable {
		t.Errorf("read-only query changed the producer's recorded choice: %v", got)
	}
}

func TestTransportationResignRetiresDivestedOverridesOnlyOnCommit(t *testing.T) {
	for _, commit := range []bool{false, true} {
		name := "rollback"
		if commit {
			name = testCommit
		}
		t.Run(name, func(t *testing.T) {
			f := newTransportationFixture(t)
			ctx := context.Background()
			// Attribute 2 belongs to a different federate and keeps that owner's override.
			transportationOK(t, f.owners.UnconditionalDivest(ctx, "fed", 1, f.obj, []core.AttributeHandle{2}))
			transportationOK(t, f.owners.AcquireIfAvailable(ctx, "fed", 3, f.obj, []core.AttributeHandle{2}, nil))
			transportationOK(t, f.r.RequestAttributeTransportationTypeChange(ctx, "fed", 3, f.obj, []core.AttributeHandle{2}, core.TransportTypeReliable))
			transportationOK(t, f.owners.Acquire(ctx, "fed", 2, f.obj, []core.AttributeHandle{1}, nil))
			objects := f.r.PrepareResignDeletions(ctx, "fed", 1, false)
			owners, err := f.owners.PrepareResign("fed", 1, core.ResignActionUnconditionallyDivestAttributes, objects.Objects())
			if err != nil {
				objects.Release()
				t.Fatal(err)
			}
			if commit {
				owners.Commit()
				objects.Commit()
				f.checkAttribute(t, 1, core.TransportTypeBestEffort)
				if got := f.r.AttributeTransportType("fed", f.obj, 1); got != core.TransportTypeUnspecified {
					t.Errorf("resign transfer retains previous owner's override: %v", got)
				}
			} else {
				owners.Release()
				objects.Release()
				f.checkAttribute(t, 1, core.TransportTypeReliable)
			}
			f.checkAttribute(t, 2, core.TransportTypeReliable)
		})
	}
}

func TestTransportationOwnershipReleaseCannotEraseNewOwnersChoice(t *testing.T) {
	f := newTransportationFixture(t)
	ctx := context.Background()
	release, err := f.r.PrepareOwnershipAcquisition(ctx, "fed", 2, f.obj, []core.AttributeHandle{1})
	transportationOK(t, err)
	// Commit an owner change under the prepared object pin, then release as the manager does.
	f.owners.RegisterInitialOwnership("fed", 2, f.obj, []core.AttributeHandle{1})
	release()
	f.checkAttribute(t, 1, core.TransportTypeBestEffort)
	transportationOK(t, f.r.RequestAttributeTransportationTypeChange(ctx, "fed", 2, f.obj, []core.AttributeHandle{1}, core.TransportTypeReliable))
	release()
	f.checkAttribute(t, 1, core.TransportTypeReliable)
}
