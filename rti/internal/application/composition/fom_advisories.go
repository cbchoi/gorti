package composition

import "github.com/cbchoi/gorti/rti/internal/hla/core"

var _ core.FOMAdvisoryDefaults = (*fomHandle)(nil)
var _ core.FOMClassAncestry = (*fomHandle)(nil)

func (h *fomHandle) AdvisorySwitchDefault(kind core.AdvisorySwitchKind) (bool, bool) {
	if !h.IsValid() || kind == 0 || int(kind) >= len(h.bindings.advisoryDefaults) {
		return false, false
	}
	value := h.bindings.advisoryDefaults[kind]
	return value.enabled, value.present
}

func (h *fomHandle) ObjectClassAncestors(cls core.ObjectClassHandle) []core.ObjectClassHandle {
	if !h.IsValid() || cls == 0 {
		return nil
	}
	if uint64(cls) > uint64(len(h.bindings.objectAncestors)) {
		return nil
	}
	return append([]core.ObjectClassHandle(nil), h.bindings.objectAncestors[cls-1]...)
}

func (h *fomHandle) InteractionClassAncestors(cls core.InteractionClassHandle) []core.InteractionClassHandle {
	if !h.IsValid() || cls == 0 {
		return nil
	}
	if uint64(cls) > uint64(len(h.bindings.interactionAncestors)) {
		return nil
	}
	return append([]core.InteractionClassHandle(nil), h.bindings.interactionAncestors[cls-1]...)
}
