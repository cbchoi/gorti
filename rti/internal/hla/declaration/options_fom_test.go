package declaration_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

type fomOptionOutbox struct{ kinds []declaration.AdvisoryKind }

func (o *fomOptionOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	o.kinds = append(o.kinds, event.(*declaration.AdvisoryEvent).Kind)
	return nil
}

func TestDeclarationFOMRatesDefaultsAndInheritedInterest(t *testing.T) {
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(ctx, []core.FOMModule{{Path: "declaration-options.xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>position</name><dataType>HLAinteger32BE</dataType></attribute><objectClass><name>Car</name><attribute><name>speed</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><interactionClass><name>Alert</name></interactionClass></interactionClass></interactionClass></interactions>
<switches><objectClassRelevanceAdvisory isEnabled="false"/><attributeScopeAdvisory/></switches>
<updateRates><updateRate><name>Slow</name><rate>2.5</rate></updateRate></updateRates></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("f", model)
	m := declaration.New()
	m.SetFOMRepository(repo)
	out := &fomOptionOutbox{}
	m.SetAdvisoryOutbox(out)
	parent, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	child, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle.Car")
	parentAttr, _ := model.LookupAttribute(parent, "position")
	childAttr, _ := model.LookupAttribute(child, "position")
	if childAttr == parentAttr {
		t.Fatal("fixture must exercise different inherited handle numbering")
	}
	_ = m.PublishObjectClassAttributes(ctx, "f", 1, child, []core.AttributeHandle{childAttr})
	hooks := 0
	m.SetOnSubscribeObjectClass(func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) {
		hooks++
	})
	opts := core.SubscriptionOptions{Active: false, UpdateRateDesignator: "Slow"}
	if err := m.SubscribeObjectClassAttributesWithOptions(ctx, "f", 2, parent, []core.AttributeHandle{parentAttr}, opts); err != nil {
		t.Fatal(err)
	}
	if hooks != 1 {
		t.Fatal("passive subscription lost late discovery hook")
	}
	if m.AdvisorySwitchEnabled("f", 1, core.AdvisoryObjectClassRelevance) || m.AdvisorySwitchEnabled("f", 1, core.AdvisoryAttributeScope) {
		t.Fatal("FDD initial switches ignored")
	}
	if err := m.SetAdvisorySwitch(ctx, "f", 1, core.AdvisoryObjectClassRelevance, true); err != nil {
		t.Fatal(err)
	}
	opts.Active = true
	if err := m.SubscribeObjectClassAttributesWithOptions(ctx, "f", 2, parent, []core.AttributeHandle{parentAttr}, opts); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.kinds, []declaration.AdvisoryKind{declaration.AdvisoryStartRegistration}) {
		t.Fatalf("inherited active interest=%v", out.kinds)
	}
	before := m.ExportState("f")
	if err := m.SubscribeObjectClassAttributesWithOptions(ctx, "f", 2, parent, []core.AttributeHandle{parentAttr, 999999}, opts); err == nil {
		t.Fatal("undefined attribute accepted")
	}
	if hooks != 2 || !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("invalid mixed attributes changed state/hook")
	}
	data, err := (savebundle.Declaration{Manager: m}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	restored := declaration.New()
	restored.SetFOMRepository(repo)
	if err := (savebundle.Declaration{Manager: restored}).Unmarshal("f", data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, restored.ExportState("f")) {
		t.Fatal("named rate/defaults lost in durable restore")
	}
	base, _ := model.LookupInteractionClass("HLAinteractionRoot.Signal")
	derived, _ := model.LookupInteractionClass("HLAinteractionRoot.Signal.Alert")
	_ = m.PublishInteractionClass(ctx, "f", 1, derived)
	_ = m.SubscribeInteractionClassWithOptions(ctx, "f", 2, base, false)
	if len(out.kinds) != 1 {
		t.Fatal("passive inherited interaction advised publisher")
	}
	_ = m.SubscribeInteractionClass(ctx, "f", 2, base)
	if len(out.kinds) != 2 || out.kinds[1] != declaration.AdvisoryTurnInteractionsOn {
		t.Fatal("active inherited interaction did not advise derived publisher")
	}
}
