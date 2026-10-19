package declaration

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestSubscriptionOptionsPassiveStillRoutes(t *testing.T) {
	m := New()
	api, ok := any(m).(core.DeclarationSubscriptionOptions)
	if !ok {
		t.Fatal("manager does not implement explicit subscription options")
	}
	ctx := context.Background()
	if err := api.SubscribeObjectClassAttributesWithOptions(ctx, "f", 1, 10, []core.AttributeHandle{2}, core.SubscriptionOptions{Active: false}); err != nil {
		t.Fatal(err)
	}
	if got := m.SubscribersFor(ctx, "f", 10, []core.AttributeHandle{2}); !reflect.DeepEqual(got, []core.FederateHandle{1}) {
		t.Fatalf("passive routing = %v", got)
	}
	if err := api.SubscribeInteractionClassWithOptions(ctx, "f", 1, 20, false); err != nil {
		t.Fatal(err)
	}
	if got := m.InteractionSubscribersFor(ctx, "f", 20); !reflect.DeepEqual(got, []core.FederateHandle{1}) {
		t.Fatalf("passive interaction routing = %v", got)
	}
}

func TestSubscriptionOptionsInvalidRateAtomic(t *testing.T) {
	m := New()
	api, ok := any(m).(core.DeclarationSubscriptionOptions)
	if !ok {
		t.Fatal("manager does not implement explicit subscription options")
	}
	ctx := context.Background()
	_ = m.SubscribeObjectClassAttributes(ctx, "f", 1, 10, []core.AttributeHandle{1})
	before := m.ExportState("f")
	if err := api.SubscribeObjectClassAttributesWithOptions(ctx, "f", 1, 10, []core.AttributeHandle{1, 2}, core.SubscriptionOptions{UpdateRateDesignator: "missing"}); err == nil {
		t.Fatal("unknown rate accepted")
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("invalid rate changed subscription state")
	}
}

func TestSubscriptionOptionsSnapshotAndAdvisoryEdges(t *testing.T) {
	m := New()
	out := &optionsOutbox{}
	m.SetAdvisoryOutbox(out)
	ctx := context.Background()
	_ = m.PublishObjectClassAttributes(ctx, "f", 1, 10, []core.AttributeHandle{1})
	_ = m.PublishInteractionClass(ctx, "f", 1, 20)
	_ = m.SubscribeObjectClassAttributesWithOptions(ctx, "f", 2, 10, []core.AttributeHandle{1}, core.SubscriptionOptions{Active: false})
	_ = m.SubscribeInteractionClassWithOptions(ctx, "f", 2, 20, false)
	if len(out.events) != 0 {
		t.Fatal("passive caused relevance advisory")
	}
	if err := m.SetAdvisorySwitch(ctx, "f", 1, core.AdvisoryObjectClassRelevance, false); err != nil {
		t.Fatal(err)
	}
	saved := m.ExportState("f")
	_ = m.SubscribeObjectClassAttributes(ctx, "f", 2, 10, []core.AttributeHandle{1})
	_ = m.SubscribeInteractionClass(ctx, "f", 2, 20)
	if len(out.events) != 1 || out.events[0].Kind != AdvisoryTurnInteractionsOn {
		t.Fatalf("switch-filtered edges = %v", out.events)
	}
	if err := m.ImportState("f", saved); err != nil {
		t.Fatal(err)
	}
	options, ok := m.ObjectSubscriptionOptions("f", 2, 10, 1)
	if !ok || options.Active || m.AdvisorySwitchEnabled("f", 1, core.AdvisoryObjectClassRelevance) {
		t.Fatal("restore lost passive mode or disabled switch")
	}
	out.events = nil
	_ = m.SubscribeInteractionClass(ctx, "f", 2, 20)
	_ = m.SubscribeInteractionClass(ctx, "f", 2, 20)
	_ = m.SubscribeInteractionClassWithOptions(ctx, "f", 2, 20, false)
	if len(out.events) != 2 || out.events[0].Kind != AdvisoryTurnInteractionsOn || out.events[1].Kind != AdvisoryTurnInteractionsOff {
		t.Fatalf("mode transition edges = %v", out.events)
	}
}

func TestSubscriptionResignLastActiveAdvisory(t *testing.T) {
	m := New()
	out := &optionsOutbox{}
	m.SetAdvisoryOutbox(out)
	ctx := context.Background()
	_ = m.PublishInteractionClass(ctx, "f", 1, 20)
	_ = m.SubscribeInteractionClass(ctx, "f", 2, 20)
	out.events = nil
	m.OnFederateResign("f", 2)
	if len(out.events) != 1 || out.events[0].Kind != AdvisoryTurnInteractionsOff {
		t.Fatal("last active resign did not produce off edge")
	}
}

func TestDeclarationDuplicateSwitchAtomic(t *testing.T) {
	m := New()
	before := m.ExportState("f")
	if err := m.SetAdvisorySwitch(context.Background(), "f", 1, core.AdvisoryObjectClassRelevance, true); err == nil {
		t.Fatal("duplicate switch accepted")
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("rejected duplicate switch created service state")
	}
}

func TestDeclarationRegionalInterestDoesNotBroadenRouting(t *testing.T) {
	m := New()
	ctx := context.Background()
	out := &optionsOutbox{}
	m.SetAdvisoryOutbox(out)
	_ = m.PublishObjectClassAttributes(ctx, "f", 1, 10, []core.AttributeHandle{1})
	_ = m.PublishInteractionClass(ctx, "f", 1, 20)
	_ = m.SetRegionalObjectSubscriptionInterest(ctx, "f", 2, 10, []core.AttributeHandle{1})
	_ = m.SetRegionalInteractionSubscriptionInterest(ctx, "f", 2, 20, true)
	if len(out.events) != 2 {
		t.Fatal("regional active interests did not advise publishers")
	}
	if len(m.SubscribersFor(ctx, "f", 10, []core.AttributeHandle{1})) != 0 || len(m.InteractionSubscribersFor(ctx, "f", 20)) != 0 {
		t.Fatal("regional interest became ordinary routing subscription")
	}
	saved := m.ExportState("f")
	want := m.ExportState("f")
	commit, err := m.PrepareRestore("f", saved)
	if err != nil {
		t.Fatal(err)
	}
	saved.RegionalObjectInterests[0].Federates[0] = 9
	saved.RegionalInteractionInterests[0].Federates[0] = 9
	commit()
	if !reflect.DeepEqual(want, m.ExportState("f")) {
		t.Fatal("regional interests aliased restore input")
	}
	_ = m.SetRegionalObjectSubscriptionInterest(ctx, "f", 2, 10, nil)
	_ = m.SetRegionalInteractionSubscriptionInterest(ctx, "f", 2, 20, false)
	if len(out.events) != 4 {
		t.Fatal("last regional active interest removal lost off edges")
	}
}
