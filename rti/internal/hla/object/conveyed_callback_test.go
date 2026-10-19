package object

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestConveyedCallbackCopiesAreSelectiveAndDetached(t *testing.T) {
	snapshot, err := core.NewRegionRealizationSnapshot(7, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 3, Upper: 9}}})
	if err != nil {
		t.Fatal(err)
	}
	metadata := &core.ObjectCallbackMetadata{Regions: []core.DDMRegionHandleCore{7}, ConveyedRegions: []core.ConveyedRegionCopy{{Designator: 99, Realization: snapshot}}}
	for _, kind := range []CallbackKind{CallbackReflect, CallbackReceive} {
		event := &CallbackEvent{Kind: kind, Metadata: metadata}
		copies := event.ConveyedRegionCopies()
		if len(copies) != 1 || copies[0].Designator != 99 {
			t.Fatalf("kind=%v copies=%v", kind, copies)
		}
		copies[0].Designator = 100
		if metadata.ConveyedRegions[0].Designator != 99 {
			t.Fatal("copy getter aliases event metadata")
		}
	}
	for _, event := range []*CallbackEvent{
		nil,
		{Kind: CallbackReflect},
		{Kind: CallbackReceive, Metadata: &core.ObjectCallbackMetadata{Regions: []core.DDMRegionHandleCore{7}}},
		{Kind: CallbackRemove, Metadata: metadata},
		{Kind: CallbackDiscover, Metadata: metadata},
	} {
		if got := event.ConveyedRegionCopies(); len(got) != 0 {
			t.Fatalf("unrelated callback exposes copies: %v", got)
		}
	}
}

func TestIntegerConveyedCallbackCopies(t *testing.T) {
	snapshot, err := core.NewRegionRealizationSnapshot(7, nil)
	if err != nil {
		t.Fatal(err)
	}
	event := &CallbackEventOf[int64]{Kind: CallbackReceive, Metadata: &core.ObjectCallbackMetadata{ConveyedRegions: []core.ConveyedRegionCopy{{Designator: 99, Realization: snapshot}}}}
	if got := event.ConveyedRegionCopies(); len(got) != 1 || got[0].Designator != 99 {
		t.Fatalf("copies=%v", got)
	}
}
