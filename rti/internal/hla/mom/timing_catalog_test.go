package mom_test

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

type catalogSchedule struct {
	call     func()
	duration time.Duration
	stops    int
}

func (o *catalogOutbox) Reserve(_ context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	staged := make([]catalogDelivery, len(deliveries))
	for i, delivery := range deliveries {
		staged[i] = catalogDelivery{fed, delivery.Recipient, delivery.Event}
	}
	return &catalogPublicationReservation{out: o, deliveries: staged}, nil
}

type catalogPublicationReservation struct {
	once       sync.Once
	out        *catalogOutbox
	deliveries []catalogDelivery
}

func (r *catalogPublicationReservation) Commit() error {
	r.once.Do(func() {
		r.out.sent = append(r.out.sent, r.deliveries...)
		r.deliveries = nil
	})
	return nil
}

func (r *catalogPublicationReservation) Release() {
	r.once.Do(func() { r.deliveries = nil })
}

func (s *catalogSchedule) schedule(d time.Duration, f func()) func() {
	s.call = f
	s.duration = d
	return func() { s.stops++ }
}

func newTimedCatalog(t *testing.T, traffic ...*mom.TrafficAccumulator) (*catalogHarness, *catalogSchedule) {
	t.Helper()
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	fom, err := repo.Load(ctx, []core.FOMModule{{Path: "m.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(catalogFederation, fom)
	out := &catalogOutbox{}
	schedule := &catalogSchedule{}
	m, err := mom.New(mom.Options{Outbox: out, SchedulePeriodic: schedule.schedule})
	if err != nil {
		t.Fatal(err)
	}
	if len(traffic) > 0 {
		if err := m.SetTrafficAccumulator(traffic[0]); err != nil {
			t.Fatal(err)
		}
	}
	decl := declaration.New()
	reg, err := object.New(object.Options{Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: repo, Declarations: decl, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	m.EnableInstanceFanout(reg, decl, repo)
	if err := m.FederationCreated(ctx, catalogFederation, nil); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{7, catalogTarget} {
		if err := m.FederateJoined(ctx, catalogFederation, h, "member", "test"); err != nil {
			t.Fatal(err)
		}
	}
	class, _ := fom.LookupObjectClass(mom.ClassHLAfederate)
	var attrs []core.AttributeHandle
	for _, name := range []string{"HLAupdatesSent", "HLAinteractionsSent", "HLAreflectionsReceived", "HLAinteractionsReceived", "HLAFOMmoduleDesignatorList"} {
		a, ok := fom.LookupAttribute(class, name)
		if !ok {
			t.Fatal(name)
		}
		attrs = append(attrs, a)
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, catalogFederation, 7, class, attrs); err != nil {
		t.Fatal(err)
	}
	d := mom.NewDispatcher(m)
	d.SetEmitter(mom.NewProductionEmitter(out))
	return &catalogHarness{fom, d, m, decl, out}, schedule
}

func TestMOMTimingPeriodicUpdatesAndCancellation(t *testing.T) {
	h, s := newTimedCatalog(t)
	p := map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget)), "HLAreportPeriod": catalogInt(2)}
	if s.call != nil {
		t.Fatal("periodic updates enabled by default")
	}
	if err := h.request(t, mom.ClassSetTiming, p); err != nil {
		t.Fatal(err)
	}
	if s.call == nil || s.duration != 2*time.Second {
		t.Fatal("timing request did not schedule")
	}
	h.m.IncrementUpdatesSent(catalogFederation, catalogTarget)
	h.out.sent = nil
	s.call()
	if len(h.out.sent) != 2 {
		t.Fatalf("first periodic publication callbacks=%d, want scope-in and reflection", len(h.out.sent))
	}
	if event, ok := h.out.sent[0].event.(*object.CallbackEvent); !ok || event.Kind != object.CallbackAttributesInScope {
		t.Fatalf("first periodic callback=%+v, want scope-in", h.out.sent[0].event)
	}
	class, _ := h.fom.LookupObjectClass(mom.ClassHLAfederate)
	updates, _ := h.fom.LookupAttribute(class, "HLAupdatesSent")
	found := false
	for _, e := range h.out.sent {
		if reflect, ok := e.event.(*object.CallbackEvent); ok && reflect.Kind == object.CallbackReflect {
			if b := reflect.Attributes[uint64(updates)]; len(b) == 4 && binary.BigEndian.Uint32(b) == 1 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("periodic update did not reach real registry reflection")
	}
	old := s.call
	p["HLAreportPeriod"] = catalogInt(0)
	if err := h.request(t, mom.ClassSetTiming, p); err != nil {
		t.Fatal(err)
	}
	h.out.sent = nil
	old()
	if len(h.out.sent) != 0 || s.stops == 0 {
		t.Fatal("disabled timing still publishes")
	}
	p["HLAreportPeriod"] = catalogInt(0xffffffff)
	if err := h.request(t, mom.ClassSetTiming, p); err == nil {
		t.Fatal("negative report period accepted")
	}
	p["HLAreportPeriod"] = catalogInt(1)
	if err := h.request(t, mom.ClassSetTiming, p); err != nil {
		t.Fatal(err)
	}
	state := h.m.ExportState(catalogFederation)
	commit, err := h.m.PrepareImportState(catalogFederation, state)
	if err != nil {
		t.Fatal(err)
	}
	stops := s.stops
	commit()
	if s.stops <= stops || s.duration != time.Second {
		t.Fatal("restore did not replace scheduler")
	}
	old = s.call
	if err := h.m.FederateResigned(context.Background(), catalogFederation, catalogTarget); err != nil {
		t.Fatal(err)
	}
	h.out.sent = nil
	old()
	if len(h.out.sent) != 0 {
		t.Fatal("resigned periodic state survived")
	}
}

func TestMOMModuleDesignatorAttributeUsesActualSource(t *testing.T) {
	h, _ := newTimedCatalog(t)
	sources := remainingSources(t)
	h.d.SetCatalogSources(sources)
	if err := h.m.RefreshFOMModules(context.Background(), catalogFederation, catalogTarget); err != nil {
		t.Fatal(err)
	}
	class, _ := h.fom.LookupObjectClass(mom.ClassHLAfederate)
	attr, _ := h.fom.LookupAttribute(class, "HLAFOMmoduleDesignatorList")
	found := false
	for _, e := range h.out.sent {
		if reflect, ok := e.event.(*object.CallbackEvent); ok && reflect.Kind == object.CallbackReflect {
			if b := reflect.Attributes[uint64(attr)]; len(b) > 4 && binary.BigEndian.Uint32(b) == 1 && readCatalogString(t, b[4:]) == "actual.xml" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("module designator list not published")
	}
}

func TestMOMTimingDoesNotPublishWrappedAggregate(t *testing.T) {
	traffic := mom.NewTrafficAccumulator()
	h, s := newTimedCatalog(t, traffic)
	state := traffic.ExportState(catalogFederation)
	for i := range state.Federates {
		if state.Federates[i].Handle == catalogTarget {
			state.Federates[i].Counts = []mom.TrafficCountState{{Kind: mom.UpdatesSent, Class: 90, Count: math.MaxInt32}}
		}
	}
	if err := traffic.ImportState(catalogFederation, state); err != nil {
		t.Fatal(err)
	}
	if err := traffic.Record(catalogFederation, catalogTarget, mom.UpdatesSent, 90, core.TransportTypeReliable); !errors.Is(err, mom.ErrCounterOverflow) {
		t.Fatal(err)
	}
	if err := h.request(t, mom.ClassSetTiming, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget)), "HLAreportPeriod": catalogInt(1)}); err != nil {
		t.Fatal(err)
	}
	h.out.sent = nil
	s.call()
	_, err := h.m.PeriodicStatus(catalogFederation, catalogTarget)
	if !errors.Is(err, mom.ErrCounterOverflow) || len(h.out.sent) != 0 {
		t.Fatalf("fabricated wrapped periodic aggregate: events=%d error=%v", len(h.out.sent), err)
	}
}
