package savebundle_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func TestDeclarationOptionsDurableRoundTrip(t *testing.T) {
	m := declaration.New()
	ctx := context.Background()
	_ = m.SubscribeObjectClassAttributesWithOptions(ctx, "f", 2, 10, []core.AttributeHandle{1}, core.SubscriptionOptions{Active: false})
	_ = m.SubscribeInteractionClassWithOptions(ctx, "f", 2, 20, false)
	_ = m.SetAdvisorySwitch(ctx, "f", 2, core.AdvisoryAttributeScope, false)
	before := m.ExportState("f")
	data, err := (savebundle.Declaration{Manager: m}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	restored := declaration.New()
	if err := (savebundle.Declaration{Manager: restored}).Unmarshal("f", data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, restored.ExportState("f")) {
		t.Fatal("durable round trip lost options or switches")
	}
}

func TestDeclarationOptionsLegacySnapshotDefaults(t *testing.T) {
	m := declaration.New()
	data := []byte(`{"version":1,"object_subscriptions":[{"class":10,"attribute":1,"federates":[2]}]}`)
	if err := (savebundle.Declaration{Manager: m}).Unmarshal("f", data); err != nil {
		t.Fatal(err)
	}
	options, ok := m.ObjectSubscriptionOptions("f", 2, 10, 1)
	if !ok || !options.Active || options.UpdateRateDesignator != "" {
		t.Fatalf("legacy options=%+v,%t", options, ok)
	}
}

func TestDeclarationVersion2OptionsRemainReadable(t *testing.T) {
	m := declaration.New()
	data := []byte(`{"version":2,"object_subscriptions":[{"class":10,"attribute":1,"federates":[2]}],"object_options":[{"Class":10,"Attribute":1,"Federate":2,"Options":{"Active":false}}],"advisory_switches":[{"Federate":2,"Kind":3,"Enabled":false}]}`)
	if err := (savebundle.Declaration{Manager: m}).Unmarshal("f", data); err != nil {
		t.Fatal(err)
	}
	opts, ok := m.ObjectSubscriptionOptions("f", 2, 10, 1)
	if !ok || opts.Active || m.AdvisorySwitchEnabled("f", 2, core.AdvisoryAttributeScope) {
		t.Fatal("v2 subscription/switch changed")
	}
	state := m.ExportState("f")
	if len(state.UpdateRateObjects) != 0 || len(state.PendingAdvisories) != 0 {
		t.Fatal("legacy snapshot invented runtime rate state")
	}
}

func TestDeclarationOptionsRejectOrphanBeforeMutation(t *testing.T) {
	m := declaration.New()
	_ = m.SubscribeInteractionClass(context.Background(), "f", 1, 20)
	before := m.ExportState("f")
	data := []byte(`{"version":2,"object_options":[{"Class":10,"Attribute":1,"Federate":2,"Options":{"Active":false}}]}`)
	if _, err := (savebundle.Declaration{Manager: m}).PrepareRestore("f", data); err == nil {
		t.Fatal("orphan options accepted")
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("invalid snapshot changed state")
	}
}
