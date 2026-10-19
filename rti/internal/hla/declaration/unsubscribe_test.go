package declaration_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

type explicitUnsubscription interface {
	UnsubscribeObjectClass(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle) error
	UnsubscribeObjectClassAttributeSet(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) error
}

func unsubscribeFixture(t *testing.T) (*declaration.Manager, core.ObjectClassHandle, []core.AttributeHandle) {
	t.Helper()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(context.Background(), []core.FOMModule{{Path: "unsubscribe.xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Base</name><attribute><name>inherited</name><dataType>HLAinteger32BE</dataType></attribute><objectClass><name>Item</name><attribute><name>x</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("f", model)
	class, _ := model.LookupObjectClass("HLAobjectRoot.Base.Item")
	x, _ := model.LookupAttribute(class, "x")
	inherited, _ := model.LookupAttribute(class, "inherited")
	m := declaration.New()
	m.SetFOMRepository(repo)
	return m, class, []core.AttributeHandle{x, inherited}
}

func TestUnsubscriptionExplicitEmptyValidatesWithoutChangingStateOrAdvice(t *testing.T) {
	m, class, attrs := unsubscribeFixture(t)
	api, ok := any(m).(explicitUnsubscription)
	if !ok {
		t.Fatal("missing distinct whole-class/explicit-set unsubscription")
	}
	ctx := context.Background()
	if err := m.SubscribeObjectClassAttributes(ctx, "f", 2, class, attrs); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("f")
	out := delivery.NewWithBatch(1, 1, 0)
	q, stop, err := out.Subscribe(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stop() }()
	m.SetAdvisoryOutbox(out)
	for _, supplied := range [][]core.AttributeHandle{nil, {}} {
		if err := api.UnsubscribeObjectClassAttributeSet(ctx, "f", 2, class, supplied); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, m.ExportState("f")) {
			t.Fatal("empty set changed subscription state")
		}
	}
	if err := api.UnsubscribeObjectClassAttributeSet(ctx, "f", 2, 999999, nil); !errors.Is(err, core.ErrObjectClassNotFound) {
		t.Fatalf("wrong class error: %v", err)
	}
	if err := api.UnsubscribeObjectClassAttributeSet(ctx, "f", 2, class, []core.AttributeHandle{attrs[0], 999999}); !errors.Is(err, core.ErrAttributeNotFound) {
		t.Fatalf("wrong attribute error: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("invalid mixed set partially removed subscriptions")
	}
	select {
	case <-q:
		t.Fatal("empty/invalid set emitted advice")
	default:
	}
}

func TestUnsubscriptionLegacyRejectsInvalidMixedSetWithoutEffects(t *testing.T) {
	m, class, attrs := unsubscribeFixture(t)
	ctx := context.Background()
	if err := m.SubscribeObjectClassAttributes(ctx, "f", 2, class, attrs); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("f")
	if err := m.UnsubscribeObjectClassAttributes(ctx, "f", 2, class, []core.AttributeHandle{attrs[0], 999999}); !errors.Is(err, core.ErrAttributeNotFound) {
		t.Fatalf("wrong attribute accepted: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("invalid request changed state")
	}
	if err := m.UnsubscribeObjectClassAttributes(ctx, "f", 2, 999999, nil); !errors.Is(err, core.ErrObjectClassNotFound) {
		t.Fatalf("wrong whole class accepted: %v", err)
	}
}

func TestUnsubscriptionCanceledEmptySetHasNoEffects(t *testing.T) {
	m, class, _ := unsubscribeFixture(t)
	api, ok := any(m).(explicitUnsubscription)
	if !ok {
		t.Fatal("missing explicit unsubscription")
	}
	before := m.ExportState("f")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := api.UnsubscribeObjectClassAttributeSet(ctx, "f", 2, class, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("canceled empty set changed state")
	}
}

func TestUnsubscriptionSelectedAndWholePreserveOtherFederates(t *testing.T) {
	m, class, attrs := unsubscribeFixture(t)
	api, ok := any(m).(explicitUnsubscription)
	if !ok {
		t.Fatal("missing explicit unsubscription")
	}
	ctx := context.Background()
	for _, h := range []core.FederateHandle{2, 3} {
		if err := m.SubscribeObjectClassAttributes(ctx, "f", h, class, attrs); err != nil {
			t.Fatal(err)
		}
	}
	if err := api.UnsubscribeObjectClassAttributeSet(ctx, "f", 2, class, []core.AttributeHandle{attrs[0], attrs[0]}); err != nil {
		t.Fatal(err)
	}
	if got := m.SubscribersFor(ctx, "f", class, attrs[:1]); !reflect.DeepEqual(got, []core.FederateHandle{3}) {
		t.Fatalf("removed attribute still routable: %v", got)
	}
	if got := m.SubscribersFor(ctx, "f", class, attrs[1:]); !reflect.DeepEqual(got, []core.FederateHandle{2, 3}) {
		t.Fatalf("other attribute lost: %v", got)
	}
	if err := api.UnsubscribeObjectClass(ctx, "f", 2, class); err != nil {
		t.Fatal(err)
	}
	if got := m.SubscribersFor(ctx, "f", class, attrs); !reflect.DeepEqual(got, []core.FederateHandle{3}) {
		t.Fatalf("whole removal damaged another subscriber: %v", got)
	}
	if err := m.UnsubscribeObjectClassAttributes(ctx, "f", 3, class, nil); err != nil {
		t.Fatal(err)
	}
	if got := m.SubscribersFor(ctx, "f", class, attrs); len(got) != 0 {
		t.Fatalf("legacy whole left subscriptions: %v", got)
	}
	if options := m.ExportState("f").ObjectOptions; len(options) != 0 {
		t.Fatalf("rate/active options retained: %v", options)
	}
}

func TestUnsubscriptionAdvisoryReservationFailureRollsBackAndRetries(t *testing.T) {
	for _, whole := range []bool{false, true} {
		t.Run(map[bool]string{false: "scoped", true: "whole"}[whole], func(t *testing.T) {
			m := declaration.New()
			api, ok := any(m).(explicitUnsubscription)
			if !ok {
				t.Fatal("missing explicit unsubscription")
			}
			ctx := context.Background()
			for _, h := range []core.FederateHandle{1, 2} {
				if err := m.PublishObjectClassAttributes(ctx, "f", h, 10, []core.AttributeHandle{1}); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.SubscribeObjectClassAttributes(ctx, "f", 3, 10, []core.AttributeHandle{1}); err != nil {
				t.Fatal(err)
			}
			out := delivery.NewWithBatch(1, 1, 0)
			first, stop1, err := out.Subscribe(ctx, "f", 1)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stop1() }()
			second, stop2, err := out.Subscribe(ctx, "f", 2)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stop2() }()
			if err := out.Send(ctx, "f", 2, &declaration.AdvisoryEvent{Sequence: 99}); err != nil {
				t.Fatal(err)
			}
			m.SetAdvisoryOutbox(out)
			invoke := func() error {
				if whole {
					return api.UnsubscribeObjectClass(ctx, "f", 3, 10)
				}
				return api.UnsubscribeObjectClassAttributeSet(ctx, "f", 3, 10, []core.AttributeHandle{1})
			}
			before := m.ExportState("f")
			if err := invoke(); !errors.Is(err, core.ErrFederateOverflow) {
				t.Fatalf("full queue error: %v", err)
			}
			if !reflect.DeepEqual(before, m.ExportState("f")) {
				t.Fatal("failed advisory changed subscriptions")
			}
			select {
			case <-first:
				t.Fatal("failed all-recipient admission emitted partial advice")
			default:
			}
			<-second
			if err := invoke(); err != nil {
				t.Fatal(err)
			}
			for _, q := range []<-chan []core.OutboundEvent{first, second} {
				select {
				case batch := <-q:
					if len(batch) != 1 || batch[0].(*declaration.AdvisoryEvent).Kind != declaration.AdvisoryStopRegistration {
						t.Fatalf("bad stop-registration advice: %v", batch)
					}
				default:
					t.Fatal("missing committed advice")
				}
			}
			if err := invoke(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-first:
				t.Fatal("repeat emitted duplicate advice")
			default:
			}
			select {
			case <-second:
				t.Fatal("repeat emitted duplicate advice")
			default:
			}
		})
	}
}
