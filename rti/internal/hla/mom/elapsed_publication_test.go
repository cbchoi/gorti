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
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type elapsedPublicationOutbox struct {
	mu     sync.Mutex
	fail   bool
	events []*object.CallbackEvent
}

func (o *elapsedPublicationOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if e, ok := event.(*object.CallbackEvent); ok {
		if o.fail && e.Kind == object.CallbackReflect {
			return core.ErrOutboxUnavailable
		}
		o.events = append(o.events, e)
	}
	return nil
}

func (o *elapsedPublicationOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	o.mu.Lock()
	var events []*object.CallbackEvent
	for _, delivery := range deliveries {
		if event, ok := delivery.Event.(*object.CallbackEvent); ok {
			if o.fail && event.Kind == object.CallbackReflect {
				o.mu.Unlock()
				return nil, core.ErrOutboxUnavailable
			}
			events = append(events, event)
		}
	}
	// Keep scope and reflection invisible until the entire publication commits.
	return &elapsedPublicationReservation{out: o, events: events}, nil
}

type elapsedPublicationReservation struct {
	once   sync.Once
	out    *elapsedPublicationOutbox
	events []*object.CallbackEvent
}

func (r *elapsedPublicationReservation) Commit() error {
	r.once.Do(func() {
		r.out.events = append(r.out.events, r.events...)
		r.events = nil
		r.out.mu.Unlock()
	})
	return nil
}

func (r *elapsedPublicationReservation) Release() {
	r.once.Do(func() {
		r.events = nil
		r.out.mu.Unlock()
	})
}

type elapsedPublicationHarness struct {
	m                  *mom.Manager
	tm                 *timepkg.Manager
	clock              *core.ManualClock
	out                *elapsedPublicationOutbox
	schedule           *catalogSchedule
	granted, advancing core.AttributeHandle
	source             func(context.Context, core.FederationName, core.FederateHandle) (mom.ElapsedTimeMetrics, error)
}

func newElapsedPublication(t *testing.T) *elapsedPublicationHarness {
	t.Helper()
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	fom, err := repo.Load(ctx, []core.FOMModule{{Path: "elapsed.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("elapsed", fom)
	out := &elapsedPublicationOutbox{}
	c := core.NewManualClock(time.Unix(0, 0))
	tm, err := timepkg.New(timepkg.Options{Clock: c, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	schedule := &catalogSchedule{}
	m, err := mom.New(mom.Options{Outbox: out, SchedulePeriodic: schedule.schedule})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetRuntimeMetricsEnabled(true); err != nil {
		t.Fatal(err)
	}
	source := func(_ context.Context, f core.FederationName, h core.FederateHandle) (mom.ElapsedTimeMetrics, error) {
		x, err := tm.MeasureElapsed(f, h)
		if err != nil {
			return mom.ElapsedTimeMetrics{}, err
		}
		return mom.ElapsedTimeMetrics{Granted: x.Granted, Advancing: x.Advancing, Acknowledge: func(a mom.ElapsedTimeAttributes) error {
			return tm.AcknowledgeElapsed(f, h, x, timepkg.ElapsedAttributes{Granted: a.Granted, Advancing: a.Advancing})
		}}, nil
	}
	m.SetMetricSources(mom.MetricSources{ElapsedTime: source})
	decl := declaration.New()
	registry, err := object.New(object.Options{Clock: c, FOMs: repo, Declarations: decl, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	m.EnableInstanceFanout(registry, decl, repo)
	if err := m.FederationCreated(ctx, "elapsed", nil); err != nil {
		t.Fatal(err)
	}
	if err := tm.OnFederateJoined("elapsed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(ctx, "elapsed", 1, "target", "test"); err != nil {
		t.Fatal(err)
	}
	class, ok := fom.LookupObjectClass(mom.ClassHLAfederate)
	if !ok {
		t.Fatal("missing federate class")
	}
	g, ok := fom.LookupAttribute(class, "HLAtimeGrantedTime")
	if !ok {
		t.Fatal("missing granted attribute")
	}
	a, ok := fom.LookupAttribute(class, "HLAtimeAdvancingTime")
	if !ok {
		t.Fatal("missing advancing attribute")
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, "elapsed", 99, class, []core.AttributeHandle{g, a}); err != nil {
		t.Fatal(err)
	}
	d := mom.NewDispatcher(m)
	params := map[core.ParameterHandle][]byte{}
	ic, ok := fom.LookupInteractionClass(mom.ClassSetTiming)
	if !ok {
		t.Fatal("missing timing class")
	}
	for name, b := range map[string][]byte{"HLAfederate": catalogHandle(1), "HLAreportPeriod": catalogInt(1)} {
		p, ok := fom.LookupParameter(ic, name)
		if !ok {
			t.Fatal(name)
		}
		params[p] = b
	}
	if err := d.Dispatch(ctx, "elapsed", mom.ClassSetTiming, 1, params, fom, fom.(core.FOMHandleNameLookup)); err != nil {
		t.Fatal(err)
	}
	return &elapsedPublicationHarness{m: m, tm: tm, clock: c, out: out, schedule: schedule, granted: g, advancing: a, source: source}
}

func (h *elapsedPublicationHarness) query(t *testing.T, g, a uint32) {
	t.Helper()
	s, err := h.m.FederateMetricValues(context.Background(), "elapsed", 1)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]uint32{"HLAtimeGrantedTime": g, "HLAtimeAdvancingTime": a} {
		b := s.Values[name]
		if len(b) != 4 || binary.BigEndian.Uint32(b) != want {
			t.Fatalf("%s=%v want %d; unavailable=%v", name, b, want, s.Unavailable)
		}
	}
}

func TestElapsedPublicationFailureRetryAndPeriodicConservation(t *testing.T) {
	h := newElapsedPublication(t)
	h.clock.Advance(1500 * time.Microsecond)
	h.query(t, 1, 0)
	h.query(t, 1, 0)
	h.out.fail = true
	if err := h.m.RefreshMetrics(context.Background(), "elapsed", 1); !errors.Is(err, core.ErrOutboxUnavailable) {
		t.Fatal(err)
	}
	if len(h.out.events) != 0 {
		t.Fatalf("failed publication leaked callbacks: %+v", h.out.events)
	}
	h.query(t, 1, 0)
	h.clock.Advance(time.Millisecond)
	h.schedule.call()
	if _, err := h.m.PeriodicStatus("elapsed", 1); !errors.Is(err, core.ErrOutboxUnavailable) {
		t.Fatal(err)
	}
	if len(h.out.events) != 0 {
		t.Fatalf("failed periodic publication leaked callbacks: %+v", h.out.events)
	}
	h.query(t, 2, 0)
	h.out.fail = false
	h.clock.Advance(time.Millisecond)
	h.schedule.call()
	if _, err := h.m.PeriodicStatus("elapsed", 1); err != nil {
		t.Fatal(err)
	}
	if len(h.out.events) != 2 || h.out.events[0].Kind != object.CallbackAttributesInScope || h.out.events[1].Kind != object.CallbackReflect {
		t.Fatalf("retry callbacks=%+v, want scope-in then reflection", h.out.events)
	}
	h.query(t, 0, 0)
	h.clock.Advance(500 * time.Microsecond)
	h.query(t, 1, 0)
	if err := h.m.RefreshMetrics(context.Background(), "elapsed", 1); err != nil {
		t.Fatal(err)
	}
	h.query(t, 0, 0)
	var total uint32
	for _, e := range h.out.events {
		if b := e.Attributes[uint64(h.granted)]; len(b) == 4 {
			total += binary.BigEndian.Uint32(b)
		}
	}
	if total != 4 {
		t.Fatalf("published total=%d want 4ms", total)
	}
}

func TestElapsedPublicationOnlyAcknowledgesRepresentableAttributes(t *testing.T) {
	h := newElapsedPublication(t)
	var ack mom.ElapsedTimeAttributes
	h.m.SetMetricSources(mom.MetricSources{ElapsedTime: func(context.Context, core.FederationName, core.FederateHandle) (mom.ElapsedTimeMetrics, error) {
		return mom.ElapsedTimeMetrics{Granted: (time.Duration(math.MaxInt32) + 1) * time.Millisecond, Advancing: 3250 * time.Microsecond, Acknowledge: func(a mom.ElapsedTimeAttributes) error { ack = a; return nil }}, nil
	}})
	if err := h.m.RefreshMetrics(context.Background(), "elapsed", 1); err != nil {
		t.Fatal(err)
	}
	if ack.Granted || !ack.Advancing {
		t.Fatalf("wrong ack: %+v", ack)
	}
	s, err := h.m.FederateMetricValues(context.Background(), "elapsed", 1)
	if err != nil || s.Unavailable["HLAtimeGrantedTime"] != "overflow" || len(s.Values["HLAtimeGrantedTime"]) != 0 {
		t.Fatal(s, err)
	}
	failure := errors.New("elapsed coverage lost")
	h.m.SetMetricSources(mom.MetricSources{ElapsedTime: func(context.Context, core.FederationName, core.FederateHandle) (mom.ElapsedTimeMetrics, error) {
		return mom.ElapsedTimeMetrics{}, failure
	}})
	s, err = h.m.FederateMetricValues(context.Background(), "elapsed", 1)
	if err != nil || s.Unavailable["HLAtimeGrantedTime"] != failure.Error() || s.Unavailable["HLAtimeAdvancingTime"] != failure.Error() {
		t.Fatal(s, err)
	}
}

func TestElapsedPublicationAckFailureSurfaces(t *testing.T) {
	h := newElapsedPublication(t)
	failure := errors.New("publication epoch replaced")
	h.m.SetMetricSources(mom.MetricSources{ElapsedTime: func(context.Context, core.FederationName, core.FederateHandle) (mom.ElapsedTimeMetrics, error) {
		return mom.ElapsedTimeMetrics{Granted: time.Millisecond, Acknowledge: func(mom.ElapsedTimeAttributes) error { return failure }}, nil
	}})
	if err := h.m.RefreshMetrics(context.Background(), "elapsed", 1); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	h.schedule.call()
	if _, err := h.m.PeriodicStatus("elapsed", 1); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestElapsedPublicationConcurrentQueriesRefreshAndPeriodic(t *testing.T) {
	h := newElapsedPublication(t)
	tick := h.schedule.call
	var wg sync.WaitGroup
	for worker := 0; worker < 5; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				h.clock.Advance(750 * time.Microsecond)
				if _, err := h.m.FederateMetricValues(context.Background(), "elapsed", 1); err != nil {
					t.Error(err)
					return
				}
				if err := h.m.RefreshMetrics(context.Background(), "elapsed", 1); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			tick()
		}
	}()
	wg.Wait()
	if err := h.m.RefreshMetrics(context.Background(), "elapsed", 1); err != nil {
		t.Fatal(err)
	}
	var total uint32
	for _, e := range h.out.events {
		if b := e.Attributes[uint64(h.granted)]; len(b) == 4 {
			total += binary.BigEndian.Uint32(b)
		}
	}
	if total != 75 {
		t.Fatalf("overlapping intervals counted twice/lost: published %d want 75ms", total)
	}
	h.query(t, 0, 0)
}
