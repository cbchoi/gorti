package object_test

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

const testValueName = "value"

func TestConveyedDefaultServiceRejectsEmptySenderRealization(t *testing.T) {
	for _, defaults := range [][]string{{"Excluded", "Excluded"}, {"[0..10)", "Excluded"}} {
		t.Run(defaults[0]+"/"+defaults[1], func(t *testing.T) {
			ctx := context.Background()
			const fed core.FederationName = "conveyed-default"
			repo := composition.NewFOMRepository()
			fom := composition.NewModelHandle(model.NewFOMWithDimensions(nil,
				[]model.InteractionClass{{Name: "Signal", Dimensions: []string{"X", "Y"}, Parameters: []model.Parameter{{Name: testValueName}}}}, nil,
				[]model.Dimension{{Name: "X", UpperBound: 100, DefaultValue: defaults[0]}, {Name: "Y", UpperBound: 100, DefaultValue: defaults[1]}}))
			repo.RememberFor(fed, fom)
			cls, _ := fom.LookupInteractionClass("Signal")
			parameter, _ := fom.LookupParameter(cls, testValueName)
			out := delivery.NewWithBatch(16, 1, 0)
			queue, closeQueue, err := out.Subscribe(ctx, fed, 2)
			knownOK(t, err)
			t.Cleanup(func() { _ = closeQueue() })
			decl := declaration.New()
			decl.SetFOMRepository(repo)
			regions, err := ddm.New(ddm.Options{Outbox: out, FOMs: repo})
			knownOK(t, err)
			registry, err := object.New(object.Options{Clock: core.NewManualClock(time.Unix(0, 0)), Outbox: out, FOMs: repo, Declarations: decl, RegistrationRegions: regions})
			knownOK(t, err)
			knownOK(t, decl.PublishInteractionClass(ctx, fed, 1, cls))
			knownOK(t, decl.SubscribeInteractionClass(ctx, fed, 2, cls))
			space, _ := regions.LookupRoutingSpace(fed, ddm.DefaultRoutingSpace)
			pub, err := regions.CreateRegion(ctx, fed, 1, space, nil)
			knownOK(t, err)
			knownOK(t, regions.CommitRegionModifications(ctx, fed, 1, []ddm.RegionHandle{pub}))
			_, copies, _, err := regions.InteractionDeliverySnapshot(ctx, fed, 1, cls, []ddm.RegionHandle{pub})
			knownOK(t, err)
			if len(copies) != 1 {
				t.Fatalf("captured copies=%v", copies)
			}
			wantDimensions := 0
			if defaults[0] != "Excluded" {
				wantDimensions = 1
			}
			if got := len(copies[0].Realization.DimensionRanges()); got != wantDimensions {
				t.Fatalf("contextual exclusions lost: dimensions=%d want=%d", got, wantDimensions)
			}
			event := &object.CallbackEvent{Kind: object.CallbackReceive, InteractionClass: cls, Metadata: &core.ObjectCallbackMetadata{Producer: 1, Regions: []core.DDMRegionHandleCore{pub}, ConveyedRegions: copies}}
			want := defaults[0] == "[0..10)" && defaults[1] == "Excluded"
			if got := registry.FilterQueuedMessage(ctx, fed, 2, event) != nil; got != want {
				t.Fatalf("queued default delivery=%v want=%v", got, want)
			}
			knownOK(t, registry.SendInteractionWithRegions(ctx, fed, 1, cls, map[core.ParameterHandle][]byte{parameter: {1}}, nil, []ddm.RegionHandle{pub}))
			delivered := false
			select {
			case batch := <-queue:
				delivered = len(batch) > 0
			default:
			}
			if delivered != want {
				t.Fatalf("initial default delivery=%v want=%v", delivered, want)
			}
			// Current FOM admission rejects zero-width default declarations.
			// Legacy portable state can still contain empty ranges; its queued
			// callback must not become interested merely because an axis exists.
			for _, ranges := range [][]core.RegionDimensionRange{
				{{Dimension: 1, Bounds: core.DDMRange{Lower: 10, Upper: 10}}},
				{{Dimension: 1, Bounds: core.DDMRange{Lower: 0, Upper: 10}}, {Dimension: 2, Bounds: core.DDMRange{Lower: 5, Upper: 5}}},
			} {
				snapshot, err := core.NewRegionRealizationSnapshot(pub, ranges)
				knownOK(t, err)
				negative := *event
				metadata := *event.Metadata
				metadata.ConveyedRegions = []core.ConveyedRegionCopy{{Designator: 999, Realization: snapshot}}
				negative.Metadata = &metadata
				if registry.FilterQueuedMessage(ctx, fed, 2, &negative) != nil {
					t.Fatal("empty retained realization delivered to ordinary default subscriber")
				}
			}
		})
	}
}
