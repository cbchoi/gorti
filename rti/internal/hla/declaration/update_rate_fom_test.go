package declaration_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func TestUpdateRateMaximumSwitchOwnershipAndDurableRetry(t *testing.T) {
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(ctx, []core.FOMModule{{Path: "rate-control.xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>position</name></attribute></objectClass></objectClass></objects><updateRates><updateRate><name>Slow</name><rate>2.5</rate></updateRate><updateRate><name>Fast</name><rate>20</rate></updateRate></updateRates></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("f", model)
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	attr, _ := model.LookupAttribute(cls, "position")
	m := declaration.New()
	m.SetFOMRepository(repo)
	out := delivery.NewWithBatch(1, 1, 0)
	m.SetAdvisoryOutbox(out)
	queue, cancel, err := out.Subscribe(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	owners := map[core.AttributeHandle]core.FederateHandle{attr: 1}
	if err := m.SyncObjectUpdateRateOwners(ctx, "f", 100, cls, owners); err != nil {
		t.Fatal(err)
	}
	owners[attr] = 99 // lifecycle input must be detached
	expect := func(enabled bool, name string) {
		t.Helper()
		select {
		case batch := <-queue:
			e, ok := batch[0].(*core.UpdateRateAdvisory)
			if !ok || e.Enabled != enabled {
				t.Fatalf("advice=%#v", batch)
			}
			if name == "" {
				if e.UpdateRateDesignator != nil {
					t.Fatal("default advice carried named limit")
				}
			} else if e.UpdateRateDesignator == nil || *e.UpdateRateDesignator != name {
				t.Fatalf("rate=%v want=%s", e.UpdateRateDesignator, name)
			}
		default:
			t.Fatal("missing rate edge")
		}
	}
	subscribe := func(h core.FederateHandle, name string, active bool) error {
		return m.SubscribeObjectClassAttributesWithOptions(ctx, "f", h, cls, []core.AttributeHandle{attr}, core.SubscriptionOptions{Active: active, UpdateRateDesignator: name})
	}
	if err := subscribe(2, "Slow", true); err != nil {
		t.Fatal(err)
	}
	expect(true, "Slow")
	if err := subscribe(3, "Fast", true); err != nil {
		t.Fatal(err)
	}
	expect(true, "Fast")
	if demand, ok := m.EffectiveAttributeUpdateRate("f", 100, attr); !ok || demand.Rate != 20 {
		t.Fatalf("effective=%+v found=%v", demand, ok)
	}
	if err := subscribe(3, "Fast", false); err != nil {
		t.Fatal(err)
	}
	expect(true, "Slow")
	if err := m.SetAdvisorySwitch(ctx, "f", 1, core.AdvisoryAttributeRelevance, false); err != nil {
		t.Fatal(err)
	}
	if err := subscribe(3, "Fast", true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-queue:
		t.Fatal("disabled switch emitted rate edge")
	default:
	}
	if err := m.SetAdvisorySwitch(ctx, "f", 1, core.AdvisoryAttributeRelevance, true); err != nil {
		t.Fatal(err)
	}
	expect(true, "Fast")
	if err := subscribe(4, "", true); err != nil {
		t.Fatal(err)
	}
	expect(true, "")
	if err := subscribe(4, "", false); err != nil {
		t.Fatal(err)
	}
	expect(true, "Fast")
	if err := out.Send(ctx, "f", 1, &declaration.AdvisoryEvent{Sequence: 99}); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("f")
	if err := subscribe(3, "Fast", false); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("admission=%v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("failed ordinary rate transition changed state")
	}
	m.OnFederateResign("f", 3) // mandatory cleanup, Slow advice stays pending
	if len(m.ExportState("f").PendingAdvisories) != 1 {
		t.Fatal("lifecycle advice not retained")
	}
	data, err := (savebundle.Declaration{Manager: m}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	restored := declaration.New()
	restored.SetFOMRepository(repo)
	restored.SetAdvisoryOutbox(out)
	if err := (savebundle.Declaration{Manager: restored}).Unmarshal("f", data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.ExportState("f"), restored.ExportState("f")) {
		t.Fatal("rate/lifecycle state lost across JSON restore")
	}
	<-queue
	if err := restored.RetryAdvisories(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	expect(true, "Slow")
	if err := restored.RetryAdvisories(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-queue:
		t.Fatal("duplicate retry edge")
	default:
	}
	second, cancel2, err := out.Subscribe(ctx, "f", 5)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel2() }()
	if err := restored.SyncObjectUpdateRateOwners(ctx, "f", 100, cls, map[core.AttributeHandle]core.FederateHandle{attr: 5}); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-second:
		if !batch[0].(*core.UpdateRateAdvisory).Enabled {
			t.Fatal("new owner not advised")
		}
	default:
		t.Fatal("ownership transfer lost rate advice")
	}
	if err := restored.RemoveObjectUpdateRate(ctx, "f", 100); err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.EffectiveAttributeUpdateRate("f", 100, attr); ok {
		t.Fatal("deleted object rate state retained")
	}
}
