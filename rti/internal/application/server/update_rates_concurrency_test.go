package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type rateConcurrencyFixture struct {
	bridge   *updateRateBridge
	out      *delivery.MultiOutbox
	cls      core.ObjectClassHandle
	attr     core.AttributeHandle
	pub, sub ddm.RegionHandle
	dim      ddm.DimensionHandle
	queue    <-chan []core.OutboundEvent
}

func newRateConcurrencyFixture(t *testing.T) rateConcurrencyFixture {
	t.Helper()
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(ctx, []core.FOMModule{{Path: "concurrency.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name><dimensions><dimension>X</dimension></dimensions></attribute></objectClass></objectClass></objects><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions><updateRates><updateRate><name>Slow</name><rate>2</rate></updateRate><updateRate><name>Fast</name><rate>10</rate></updateRate></updateRates></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("f", model)
	cls, _ := model.LookupObjectClass("HLAobjectRoot.V")
	attr, _ := model.LookupAttribute(cls, "x")
	out := delivery.NewWithBatch(1, 1, 0)
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	decl.SetAdvisoryOutbox(out)
	owners, err := ownership.New(ownership.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	var objects *object.Registry
	regions, err := ddm.New(ddm.Options{Outbox: out, FOMs: repo, ValidateSubscriptionOptions: decl.ValidateSubscriptionOptions,
		Objects: regionAssociationValidatorFunc(func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, func(), error) {
			return objects.PrepareRegionAssociation(ctx, fed, caller, obj, attrs)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	objects, err = object.New(object.Options{Declarations: decl, Outbox: out, FOMs: repo, Clock: core.NewRealClock(), Ownership: owners, RegistrationRegions: regions})
	if err != nil {
		t.Fatal(err)
	}
	restore, err := objects.PrepareImportState("f", &object.RegistryState{LastObjectHandle: 100, Objects: []object.InstanceState{{Handle: 100, Name: "v", Class: cls, Registrant: 1}}, Discoveries: []object.DiscoveryState{{Object: 100, Federate: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	restore()
	owners.RegisterInitialOwnership("f", 1, 100, []core.AttributeHandle{attr})
	bridge := newUpdateRateBridge(repo, decl, objects, regions, owners)
	bridge.ObjectChanged(ctx, "f", 100)
	queue, stop, err := out.Subscribe(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stop() })
	if err := decl.SubscribeObjectClassAttributesWithOptions(ctx, "f", 2, cls, []core.AttributeHandle{attr}, core.SubscriptionOptions{Active: true, UpdateRateDesignator: "Slow"}); err != nil {
		t.Fatal(err)
	}
	<-queue
	space, _ := regions.LookupRoutingSpace("f", "default")
	dim, _ := regions.LookupDimension("f", space, "X")
	pub, err := regions.CreateRegion(ctx, "f", 1, space, []ddm.DimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := regions.CreateRegion(ctx, "f", 3, space, []ddm.DimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		owner  core.FederateHandle
		region ddm.RegionHandle
		lo, hi uint64
	}{{1, pub, 0, 10}, {3, sub, 20, 30}} {
		if err := regions.SetRangeBounds("f", r.owner, r.region, dim, core.DDMRange{Lower: r.lo, Upper: r.hi}); err != nil {
			t.Fatal(err)
		}
		if err := regions.CommitRegionModifications(ctx, "f", r.owner, []ddm.RegionHandle{r.region}); err != nil {
			t.Fatal(err)
		}
	}
	if err := regions.AssociateRegionsForUpdates(ctx, "f", 1, 100, map[core.AttributeHandle][]ddm.RegionHandle{attr: {pub}}); err != nil {
		t.Fatal(err)
	}
	if err := regions.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, "f", 3, cls, []core.AttributeHandle{attr}, []ddm.RegionHandle{sub}, core.SubscriptionOptions{Active: true, UpdateRateDesignator: "Fast"}); err != nil {
		t.Fatal(err)
	}
	return rateConcurrencyFixture{bridge, out, cls, attr, pub, sub, dim, queue}
}

func waitRateSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("deadlock waiting for %s", what)
	}
}

func TestUpdateRateRejectedRegionCannotLeakThroughOwnerNotification(t *testing.T) {
	f := newRateConcurrencyFixture(t)
	ctx := context.Background()
	newQueue, stop, err := f.out.Subscribe(ctx, "f", 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stop() }()
	for _, owner := range []core.FederateHandle{1, 4} {
		if err := f.out.Send(ctx, "f", owner, &fakeOutboundEvent{seq: 99}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.bridge.regions.SetRangeBounds("f", 3, f.sub, f.dim, core.DDMRange{Lower: 5, Upper: 15}); err != nil {
		t.Fatal(err)
	}
	atGuard, continueDDM := make(chan struct{}), make(chan struct{})
	resume := sync.OnceFunc(func() { close(continueDDM) })
	defer resume()
	f.bridge.regions.SetUpdateRateHooks(f.bridge.refreshLocked, f.bridge.objectInterest, func() func() {
		close(atGuard)
		<-continueDDM
		return f.bridge.guard()
	})
	finished := make(chan error, 1)
	go func() { finished <- f.bridge.regions.CommitRegionModifications(ctx, "f", 3, []ddm.RegionHandle{f.sub}) }()
	waitRateSignal(t, atGuard, "DDM guard")
	// A committed owner change must project only admitted regional demand.
	f.bridge.owners.RegisterInitialOwnership("f", 4, 100, []core.AttributeHandle{f.attr})
	f.bridge.owners.NotifyOwnershipChanged(ctx, "f", 100)
	resume()
	select {
	case err := <-finished:
		if !errors.Is(err, core.ErrFederateOverflow) {
			t.Fatalf("rejection=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DDM commit did not complete")
	}
	if bounds, _ := f.bridge.regions.QueryBounds("f", f.sub, f.dim); bounds.Lower != 20 {
		t.Fatalf("rejected bounds=%+v", bounds)
	}
	if demand, _ := f.bridge.declarations.EffectiveAttributeUpdateRate("f", 100, f.attr); demand.Rate != 2 {
		t.Fatalf("rejected regional demand leaked through owner callback: %+v", demand)
	}
	<-f.queue
	<-newQueue
	if err := f.bridge.declarations.RetryAdvisories(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-newQueue:
		event := batch[0].(*core.UpdateRateAdvisory)
		if event.UpdateRateDesignator == nil || *event.UpdateRateDesignator != "Slow" {
			t.Fatalf("retry published rejected regional demand: %+v", event)
		}
	default:
		t.Fatal("missing new owner's current-demand advisory")
	}
}

type gatedRateRepository struct {
	core.FOMRepository
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *gatedRateRepository) Get(ctx context.Context, fed core.FederationName) (core.FOMHandle, error) {
	r.once.Do(func() { close(r.entered); <-r.release })
	return r.FOMRepository.Get(ctx, fed)
}

func TestUpdateRateAssociationGuardPrecedesObjectLease(t *testing.T) {
	for _, unassociate := range []bool{false, true} {
		t.Run(map[bool]string{false: "associate", true: "unassociate"}[unassociate], func(t *testing.T) {
			f := newRateConcurrencyFixture(t)
			ctx := context.Background()
			gate := &gatedRateRepository{FOMRepository: f.bridge.foms, entered: make(chan struct{}), release: make(chan struct{})}
			f.bridge.foms = gate
			ownerDone := make(chan struct{})
			go func() { f.bridge.owners.NotifyOwnershipChanged(ctx, "f", 100); close(ownerDone) }()
			waitRateSignal(t, gate.entered, "owner notification holding bridge guard")
			atGuard := make(chan struct{})
			f.bridge.regions.SetUpdateRateHooks(f.bridge.refreshLocked, f.bridge.objectInterest, func() func() {
				close(atGuard)
				return f.bridge.guard()
			})
			finished := make(chan error, 1)
			go func() {
				bindings := map[core.AttributeHandle][]ddm.RegionHandle{f.attr: {f.pub}}
				if unassociate {
					finished <- f.bridge.regions.UnassociateRegionsForUpdates(ctx, "f", 1, 100, bindings)
				} else {
					finished <- f.bridge.regions.AssociateRegionsForUpdates(ctx, "f", 1, 100, bindings)
				}
			}()
			waitRateSignal(t, atGuard, "association attempting bridge guard")
			// The owner now needs the object lock. A lease acquired before the
			// waiting bridge guard would make these two operations deadlock.
			close(gate.release)
			waitRateSignal(t, ownerDone, "owner object snapshot")
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("association deadlocked with owner notification")
			}
		})
	}
}

func TestUpdateRateRegionalRegistrationDoesNotAcquireBridgeUnderObjectLock(t *testing.T) {
	f := newRateConcurrencyFixture(t)
	ctx := context.Background()
	if err := f.bridge.declarations.PublishObjectClassAttributes(ctx, "f", 1, f.cls, []core.AttributeHandle{f.attr}); err != nil {
		t.Fatal(err)
	}
	f.bridge.mu.Lock()
	defer f.bridge.mu.Unlock()
	finished := make(chan error, 1)
	go func() {
		_, _, err := f.bridge.objects.RegisterWithRegions(ctx, "f", 1, f.cls, "", map[core.AttributeHandle][]ddm.RegionHandle{f.attr: {f.pub}})
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("regional registration acquired the bridge guard under its object transaction")
	}
}
