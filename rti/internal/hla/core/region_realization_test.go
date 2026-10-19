package core

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRegionMetadataJSONPreservesPresentEmptyCopies(t *testing.T) {
	for _, present := range []bool{false, true} {
		metadata := ObjectCallbackMetadata{Regions: []DDMRegionHandleCore{7}}
		if present {
			metadata.ConveyedRegions = []ConveyedRegionCopy{}
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			t.Fatal(err)
		}
		var decoded ObjectCallbackMetadata
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if (decoded.ConveyedRegions != nil) != present {
			t.Fatalf("copy presence changed: present=%v JSON=%s", present, encoded)
		}
		if got := decoded.ConveyedRegionDesignators(); present && len(got) != 0 || !present && (len(got) != 1 || got[0] != 7) {
			t.Fatalf("wrong legacy/copy projection: %v", got)
		}
	}
}

func TestRegionRealizationSnapshotIsDetachedAndExact(t *testing.T) {
	dimensions := []RegionDimensionRange{{Dimension: 2, Bounds: DDMRange{Lower: 1<<53 + 17, Upper: math.MaxUint64}}, {Dimension: 1, Bounds: DDMRange{Lower: 0, Upper: 10}}}
	snapshot, err := NewRegionRealizationSnapshot(7, dimensions)
	if err != nil {
		t.Fatal(err)
	}
	dimensions[0].Bounds.Upper = 3
	exposed := snapshot.DimensionRanges()
	if exposed[0].Dimension != 1 {
		t.Fatal("dimensions not canonical")
	}
	exposed[0].Bounds.Upper = 99
	for dim, want := range map[DDMDimensionHandle]DDMRange{1: {Lower: 0, Upper: 10}, 2: {Lower: 1<<53 + 17, Upper: math.MaxUint64}} {
		if got, found := snapshot.RangeBounds(dim); !found || got != want {
			t.Fatalf("snapshot aliased or lost precision: dimension=%d got=%+v found=%v", dim, got, found)
		}
	}
	if _, found := snapshot.RangeBounds(3); found {
		t.Fatal("absent dimension materialized")
	}
	saved, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var restored RegionRealizationSnapshot
	if err := json.Unmarshal(saved, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.SourceRegion() != 7 {
		t.Fatal("source provenance lost")
	}
	if got, _ := restored.RangeBounds(2); got != (DDMRange{Lower: 1<<53 + 17, Upper: math.MaxUint64}) {
		t.Fatalf("save/replay changed uint64 ranges: %+v", got)
	}
}

func TestRegionRealizationSnapshotRejectsMalformedState(t *testing.T) {
	for _, data := range []string{
		`{"sourceRegion":0,"dimensions":[]}`,
		`{"sourceRegion":1,"dimensions":[{"dimension":0,"bounds":{"Lower":0,"Upper":1}}]}`,
		`{"sourceRegion":1,"dimensions":[{"dimension":1,"bounds":{"Lower":9,"Upper":1}}]}`,
		`{"sourceRegion":1,"dimensions":[{"dimension":1,"bounds":{"Lower":0,"Upper":1}},{"dimension":1,"bounds":{"Lower":1,"Upper":2}}]}`,
		`{"sourceRegion":1,"dimensions":[],"liveReference":1}`,
	} {
		t.Run(data, func(t *testing.T) {
			var snapshot RegionRealizationSnapshot
			if err := json.Unmarshal([]byte(data), &snapshot); err == nil {
				t.Fatal("malformed realization accepted")
			}
		})
	}
	if _, err := NewRegionRealizationSnapshot(1, []RegionDimensionRange{{Dimension: 1, Bounds: DDMRange{}}}); err != nil {
		t.Fatalf("zero-width FDD default rejected: %v", err)
	}
}
