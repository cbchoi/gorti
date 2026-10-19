package composition

import "github.com/cbchoi/gorti/rti/internal/hla/core"

func (h *fomHandle) AutomaticResignAction() core.ResignAction {
	if h == nil || !h.IsValid() {
		return core.ResignActionNoAction
	}
	switch h.fom.AutomaticResignAction() {
	case "UnconditionallyDivestAttributes":
		return core.ResignActionUnconditionallyDivestAttributes
	case "DeleteObjects":
		return core.ResignActionDeleteObjects
	case "CancelPendingOwnershipAcquisitions":
		return core.ResignActionCancelPendingOwnership
	case "DeleteObjectsThenDivest":
		return core.ResignActionDeleteThenDivest
	case "CancelThenDeleteThenDivest":
		return core.ResignActionCancelThenDelete
	default:
		return core.ResignActionNoAction
	}
}
