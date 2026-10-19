package savebundle

import (
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func validateConveyedCallbackMetadata(metadata *core.ObjectCallbackMetadata) error {
	seen := make(map[core.DDMRegionHandleCore]core.RegionRealizationSnapshot, len(metadata.ConveyedRegions))
	for _, copy := range metadata.ConveyedRegions {
		if copy.Designator == 0 || copy.Realization.SourceRegion() == 0 || copy.Designator == copy.Realization.SourceRegion() {
			return fmt.Errorf("time snapshot: invalid conveyed region identity")
		}
		if prior, found := seen[copy.Designator]; found && (prior.SourceRegion() != copy.Realization.SourceRegion() || !slices.Equal(prior.DimensionRanges(), copy.Realization.DimensionRanges())) {
			return fmt.Errorf("time snapshot: conflicting conveyed realizations")
		}
		seen[copy.Designator] = copy.Realization
	}
	for attr, copies := range metadata.AttributeConveyedRegions {
		if attr == 0 {
			return fmt.Errorf("time snapshot: zero conveyed attribute")
		}
		for _, copy := range copies {
			prior, found := seen[copy.Designator]
			if !found || prior.SourceRegion() != copy.Realization.SourceRegion() || !slices.Equal(prior.DimensionRanges(), copy.Realization.DimensionRanges()) {
				return fmt.Errorf("time snapshot: attribute realization absent from conveyed union")
			}
		}
	}
	return nil
}
