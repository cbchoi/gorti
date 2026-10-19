package composition

import (
	"math"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func (h *fomHandle) InteractionDimensions(cls core.InteractionClassHandle) ([]core.DDMDimensionHandle, bool) {
	if !h.IsValid() || cls == 0 {
		return nil, false
	}
	classes := h.bindings.interactions
	if uint64(cls) > uint64(len(classes)) || cls > math.MaxInt {
		return nil, false
	}
	var result []core.DDMDimensionHandle
	seen := make(map[int]bool)
	for cur := int(cls) - 1; cur >= 0 && cur < len(classes) && !seen[cur]; { //nolint:gosec // G115: cls is explicitly bounded by MaxInt and the slice length above.
		seen[cur] = true
		for _, name := range classes[cur].Dimensions {
			dim, found := h.LookupDimension(name)
			if !found {
				return nil, false
			}
			result = append(result, core.DDMDimensionHandle(dim))
		}
		cur = interactionClassIndexByName(classes, classes[cur].ParentKey())
	}
	slices.Sort(result)
	return slices.Compact(result), true
}
