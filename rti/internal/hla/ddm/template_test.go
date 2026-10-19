package ddm

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func initializeRegion(t *testing.T, m *Manager, fed core.FederationName, owner core.FederateHandle, r RegionHandle, bounds map[DimensionHandle]Range) {
	t.Helper()
	for d, b := range bounds {
		if err := m.SetRangeBounds(fed, owner, r, d, b); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.CommitRegionModifications(context.Background(), fed, owner, []RegionHandle{r}); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateRequiresCompleteInitialCommit(t *testing.T) {
	m := newPermissiveManager(t)
	ctx := context.Background()
	const fed core.FederationName = "template"
	space, _ := m.LookupRoutingSpace(fed, "space")
	x, _ := m.LookupDimension(fed, space, "X")
	y, _ := m.LookupDimension(fed, space, "Y")
	r, err := m.CreateRegion(ctx, fed, 1, space, []DimensionHandle{x, y})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.QueryBounds(fed, r, x); ok {
		t.Fatal("template is not a specification")
	}
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{r}); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("template subscription: %v", err)
	}
	if err := m.SetRangeBounds(fed, 1, r, x, Range{Lower: 1, Upper: 10}); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState(fed)
	if err := m.CommitRegionModifications(ctx, fed, 1, []RegionHandle{r}); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("incomplete commit: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState(fed)) {
		t.Fatal("failed commit lost draft")
	}
	restored := newPermissiveManager(t)
	restored.LookupRoutingSpace(fed, "space")
	restored.LookupDimension(fed, space, "X")
	restored.LookupDimension(fed, space, "Y")
	restored.ImportState(fed, before)
	if err := restored.CommitRegionModifications(ctx, fed, 1, []RegionHandle{r}); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("restored draft: %v", err)
	}
	if err := m.SetRangeBounds(fed, 1, r, y, Range{Lower: 2, Upper: 20}); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.QueryBounds(fed, r, x); ok {
		t.Fatal("all initialized but uncommitted is still a template")
	}
	if err := m.CommitRegionModifications(ctx, fed, 1, []RegionHandle{r}); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{r}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRangeBounds(fed, 1, r, x, Range{Lower: 3, Upper: 9}); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitRegionModifications(ctx, fed, 1, []RegionHandle{r}); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.QueryBounds(fed, r, y); !ok || got != (Range{Lower: 2, Upper: 20}) {
		t.Fatalf("partial subsequent commit: %+v %v", got, ok)
	}
}

func TestFirstCommitAtomicAcrossTemplates(t *testing.T) {
	m := newPermissiveManager(t)
	ctx := context.Background()
	const fed core.FederationName = "atomic-template"
	space, _ := m.LookupRoutingSpace(fed, "space")
	x, _ := m.LookupDimension(fed, space, "X")
	first, _ := m.CreateRegion(ctx, fed, 1, space, []DimensionHandle{x})
	second, _ := m.CreateRegion(ctx, fed, 1, space, []DimensionHandle{x})
	if err := m.SetRangeBounds(fed, 1, first, x, Range{Lower: 1, Upper: 5}); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState(fed)
	if err := m.CommitRegionModifications(ctx, fed, 1, []RegionHandle{first, second}); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState(fed)) {
		t.Fatal("atomic first commit modified a complete sibling")
	}
	empty, _ := m.CreateRegion(ctx, fed, 1, space, nil)
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{empty}); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatal(err)
	}
	if err := m.CommitRegionModifications(ctx, fed, 1, []RegionHandle{empty}); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{empty}); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateRejectedAtEverySpecificationBoundary(t *testing.T) {
	m := w4Manager(t)
	ctx := context.Background()
	space, _ := m.LookupRoutingSpace(w4Fed, DefaultRoutingSpace)
	r, err := m.CreateRegion(ctx, w4Fed, 1, space, []DimensionHandle{1})
	if err != nil {
		t.Fatal(err)
	}
	before := m.ExportState(w4Fed)
	for name, operation := range map[string]func() error{
		"subscribe-object": func() error {
			return m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{r})
		},
		"unsubscribe-object": func() error {
			return m.UnsubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{r})
		},
		"subscribe-interaction":   func() error { return m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{r}) },
		"unsubscribe-interaction": func() error { return m.UnsubscribeInteractionClassWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{r}) },
		"associate": func() error {
			return m.AssociateRegionsForUpdates(ctx, w4Fed, 1, 9, map[core.AttributeHandle][]RegionHandle{1: {r}})
		},
		"unassociate": func() error {
			return m.UnassociateRegionsForUpdates(ctx, w4Fed, 1, 9, map[core.AttributeHandle][]RegionHandle{1: {r}})
		},
		"send-interaction": func() error {
			_, err := m.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{r})
			return err
		},
		"request-update": func() error {
			lease, err := m.PrepareRegionUpdateRequest(ctx, w4Fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{r})
			if lease != nil {
				lease.Release()
				t.Error("invalid template returned lease")
			}
			return err
		},
		"register": func() error {
			lease, err := m.PrepareObjectRegistration(ctx, w4Fed, 1, 1, map[core.AttributeHandle][]RegionHandle{1: {r}})
			if lease != nil {
				lease.Release()
				t.Error("invalid template returned registration")
			}
			return err
		},
		"get-bounds": func() error { _, err := m.QueryBoundsChecked(w4Fed, r, 1); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); !errors.Is(err, core.ErrRegionNotFound) {
				t.Fatalf("template accepted: %v", err)
			}
			if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
				t.Fatal("invalid use changed state")
			}
		})
	}
	if err := m.DeleteRegion(ctx, w4Fed, 1, r); err != nil {
		t.Fatal(err)
	}
}

func TestFirstCommitAdmissionRollbackKeepsTemplate(t *testing.T) {
	m := newPermissiveManager(t)
	const fed core.FederationName = "template-rate-rollback"
	space, _ := m.LookupRoutingSpace(fed, "space")
	x, _ := m.LookupDimension(fed, space, "X")
	r, _ := m.CreateRegion(context.Background(), fed, 1, space, []DimensionHandle{x})
	if err := m.SetRangeBounds(fed, 1, r, x, Range{Lower: 1, Upper: 10}); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState(fed)
	reject := errors.New("rate admission")
	m.SetUpdateRateHooks(func(context.Context, core.FederationName, bool) error { return reject }, nil, nil)
	if err := m.CommitRegionModifications(context.Background(), fed, 1, []RegionHandle{r}); !errors.Is(err, reject) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState(fed)) {
		t.Fatal("admission failure changed draft or template")
	}
	if _, ok := m.QueryBounds(fed, r, x); ok {
		t.Fatal("failed initial commit became a specification")
	}
}
