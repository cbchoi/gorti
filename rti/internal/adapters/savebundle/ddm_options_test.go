package savebundle

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"google.golang.org/protobuf/proto"
)

func ddmOptionsCodec(t *testing.T) DDM {
	t.Helper()
	repo := composition.NewFOMRepository()
	repo.RememberFor("fed", composition.NewModelHandle(model.NewFOMWithDimensions(
		[]model.ObjectClass{{Name: "Root", Attributes: []model.Attribute{{Name: "Position", Dimensions: []string{"X"}}}}},
		[]model.InteractionClass{{Name: "Signal", Dimensions: []string{"X"}}}, nil, []model.Dimension{{Name: "X", UpperBound: 100}})))
	mgr, err := ddm.New(ddm.Options{Outbox: objectStateOutbox{}, FOMs: repo,
		ValidateSubscriptionOptions: func(_ context.Context, _ core.FederationName, opts core.SubscriptionOptions) error {
			if opts.UpdateRateDesignator != "" && opts.UpdateRateDesignator != "fast" {
				return core.ErrInvalidUpdateRateDesignator
			}
			return nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	return DDM{Manager: mgr}
}

func TestDDMSubscriptionOptionsSnapshotRoundTrip(t *testing.T) {
	source := ddmOptionsCodec(t)
	ctx := context.Background()
	region, err := source.Manager.CreateRegion(ctx, "fed", 1, 1, []ddm.DimensionHandle{1})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Manager.SetRangeBounds("fed", 1, region, 1, ddm.Range{Upper: 100}); err != nil {
		t.Fatal(err)
	}
	if err := source.Manager.CommitRegionModifications(ctx, "fed", 1, []ddm.RegionHandle{region}); err != nil {
		t.Fatal(err)
	}
	if err := source.Manager.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, "fed", 1, 1, []core.AttributeHandle{1}, []ddm.RegionHandle{region}, core.SubscriptionOptions{Active: false, UpdateRateDesignator: "fast"}); err != nil {
		t.Fatal(err)
	}
	if err := source.Manager.SubscribeInteractionClassWithRegionsAndOptions(ctx, "fed", 1, 1, []ddm.RegionHandle{region}, false); err != nil {
		t.Fatal(err)
	}
	data, err := source.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	destination := ddmOptionsCodec(t)
	if err := destination.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	for name, manager := range map[string]*ddm.Manager{"source": source.Manager, "restored": destination.Manager} {
		state := manager.ExportState("fed")
		if len(state.ObjectSubscriptions) != 1 || !state.ObjectSubscriptions[0].Passive || state.ObjectSubscriptions[0].UpdateRateDesignator != "fast" {
			t.Errorf("%s object subscription options lost: %+v", name, state.ObjectSubscriptions)
		}
		if len(state.InteractionSubscriptions) != 1 || !state.InteractionSubscriptions[0].Passive {
			t.Errorf("%s interaction options lost: %+v", name, state.InteractionSubscriptions)
		}
	}
}

func TestDDMLegacySubscriptionSnapshotDefaultsActive(t *testing.T) {
	codec := ddmOptionsCodec(t)
	data, err := proto.Marshal(&rtiv1.DDMManagerState{
		ObjectSubscriptions:      []*rtiv1.DDMSubscriptionEntry{{ObjectClassHandle: 1, AttributeHandle: 1, SubscriberFederate: 1, RegionHandles: []uint64{1}}},
		InteractionSubscriptions: []*rtiv1.DDMInteractionSubscriptionEntry{{InteractionClassHandle: 1, SubscriberFederate: 1, RegionHandles: []uint64{1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	state := codec.Manager.ExportState("fed")
	if state.ObjectSubscriptions[0].Passive || state.ObjectSubscriptions[0].UpdateRateDesignator != "" || state.InteractionSubscriptions[0].Passive {
		t.Fatalf("legacy defaults changed: %+v", state)
	}
}
