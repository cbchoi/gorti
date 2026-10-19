package composition

import (
	"math"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.FOMTransportationLookup = (*fomHandle)(nil)

func (h *fomHandle) TransportationForAttribute(cls core.ObjectClassHandle, attr core.AttributeHandle) (core.TransportType, bool) {
	declaration, ok := h.attributeDeclaration(cls, attr)
	if !ok {
		return core.TransportTypeUnspecified, false
	}
	return declaredTransportation(declaration.Transportation)
}

func (h *fomHandle) TransportationForInteraction(cls core.InteractionClassHandle) (core.TransportType, bool) {
	if !h.IsValid() {
		return core.TransportTypeUnspecified, false
	}
	classes := h.bindings.interactions
	if cls == 0 || uint64(cls) > uint64(len(classes)) || cls > math.MaxInt {
		return core.TransportTypeUnspecified, false
	}
	visited := make(map[int]bool)
	for cur := int(cls) - 1; cur >= 0 && cur < len(classes) && !visited[cur]; { //nolint:gosec // G115: cls is explicitly bounded by MaxInt and the slice length above.
		visited[cur] = true
		if classes[cur].Transportation != "" {
			return declaredTransportation(classes[cur].Transportation)
		}
		cur = interactionClassIndexByName(classes, classes[cur].ParentKey())
	}
	return core.TransportTypeUnspecified, false
}

func declaredTransportation(name string) (core.TransportType, bool) {
	switch name {
	case "HLAreliable":
		return core.TransportTypeReliable, true
	case "HLAbestEffort":
		return core.TransportTypeBestEffort, true
	default:
		return core.TransportTypeUnspecified, false
	}
}
