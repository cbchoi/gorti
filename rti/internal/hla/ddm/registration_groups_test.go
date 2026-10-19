package ddm

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestGroupedRegistrationEmptyFormsUseDefaultRegion(t *testing.T) {
	m := w4Manager(t)
	r := w4Region(t, m, 1, map[DimensionHandle]Range{3: {Lower: 10, Upper: 20}})
	before := m.ExportState(w4Fed)
	for _, groups := range [][]core.AttributeRegionGroup{
		nil, {}, {{Attributes: []core.AttributeHandle{1}}}, {{Regions: []RegionHandle{r}}},
		{{Attributes: []core.AttributeHandle{1}}, {Regions: []RegionHandle{r}}},
	} {
		prepared, err := m.PrepareObjectRegistrationGroups(context.Background(), w4Fed, 1, 1, groups)
		if err != nil {
			t.Fatal(err)
		}
		prepared.Commit(9)
		prepared.Release()
		if m.HasObjectAssociations(w4Fed, 9) || !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
			t.Fatal("empty product created explicit bindings")
		}
	}
}

func TestGroupedRegistrationValidatesAllHandlesBeforeCommit(t *testing.T) {
	m := w4Manager(t)
	r := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}})
	foreign := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}})
	wrong := w4Region(t, m, 1, map[DimensionHandle]Range{3: {Lower: 10, Upper: 20}})
	first := core.AttributeRegionGroup{Attributes: []core.AttributeHandle{1}, Regions: []RegionHandle{r}}
	for _, tc := range []struct {
		group core.AttributeRegionGroup
		want  error
	}{
		{core.AttributeRegionGroup{Regions: []RegionHandle{999}}, core.ErrRegionNotFound},
		{core.AttributeRegionGroup{Regions: []RegionHandle{foreign}}, core.ErrRegionNotOwnedByFederate},
		{core.AttributeRegionGroup{Attributes: []core.AttributeHandle{999}}, core.ErrAttributeNotFound},
		{core.AttributeRegionGroup{Attributes: []core.AttributeHandle{1}, Regions: []RegionHandle{wrong}}, core.ErrInvalidRegionContext},
	} {
		before := m.ExportState(w4Fed)
		prepared, err := m.PrepareObjectRegistrationGroups(context.Background(), w4Fed, 1, 1, []core.AttributeRegionGroup{first, tc.group})
		if prepared != nil {
			prepared.Release()
			t.Fatal("invalid request returned preparation")
		}
		if !errors.Is(err, tc.want) || !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
			t.Fatalf("invalid later group: %v", err)
		}
	}
	groups := []core.AttributeRegionGroup{first, {Attributes: []core.AttributeHandle{2}}, first}
	prepared, err := m.PrepareObjectRegistrationGroups(context.Background(), w4Fed, 1, 1, groups)
	if err != nil {
		t.Fatal(err)
	}
	groups[0].Regions[0] = 999
	prepared.Commit(9)
	prepared.Release()
	if !reflect.DeepEqual(m.PublisherRegionsFor(w4Fed, 9, 1), []RegionHandle{r}) {
		t.Fatal("registration bindings were not detached/deduplicated")
	}
	if len(m.PublisherRegionsFor(w4Fed, 9, 2)) != 0 {
		t.Fatal("empty attribute region set did not retain default")
	}
}
