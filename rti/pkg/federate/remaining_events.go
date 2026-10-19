package federate

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

type RequestAttributeOwnershipAssumption struct {
	CallbackInfo
	ObjectHandle      uint64
	AttributeHandles  []uint64
	DivestingFederate uint64
	Tag               []byte
}

func (RequestAttributeOwnershipAssumption) isFederateEvent() {}

type AttributeOwnershipAcquisitionNotification struct {
	CallbackInfo
	ObjectHandle     uint64
	AttributeHandles []uint64
	OwningFederate   uint64
	Tag              []byte
}

func (AttributeOwnershipAcquisitionNotification) isFederateEvent() {}

type RequestDivestitureConfirmation struct {
	CallbackInfo
	ObjectHandle     uint64
	AttributeHandles []uint64
}

func (RequestDivestitureConfirmation) isFederateEvent() {}

type RequestAttributeOwnershipRelease struct {
	CallbackInfo
	ObjectHandle     uint64
	AttributeHandles []uint64
	Tag              []byte
}

func (RequestAttributeOwnershipRelease) isFederateEvent() {}

type AttributeOwnershipUnavailable struct {
	CallbackInfo
	ObjectHandle     uint64
	AttributeHandles []uint64
}

func (AttributeOwnershipUnavailable) isFederateEvent() {}

type ConfirmAttributeOwnershipAcquisitionCancellation struct {
	CallbackInfo
	ObjectHandle     uint64
	AttributeHandles []uint64
}

func (ConfirmAttributeOwnershipAcquisitionCancellation) isFederateEvent() {}

type SynchronizationPointRegistrationSucceeded struct {
	CallbackInfo
	Label string
}

func (SynchronizationPointRegistrationSucceeded) isFederateEvent() {}

type SynchronizationPointRegistrationFailed struct {
	CallbackInfo
	Label  string
	Reason int32
}

func (SynchronizationPointRegistrationFailed) isFederateEvent() {}

type InitiateFederateSave struct {
	CallbackInfo
	Label         string
	SaveTime      *float64
	SaveTimeValue *logicaltime.Time
}

func (InitiateFederateSave) isFederateEvent() {}

type FederationSaved struct {
	CallbackInfo
	Label string
}

func (FederationSaved) isFederateEvent() {}

type FederationNotSaved struct {
	CallbackInfo
	Label string
}

func (FederationNotSaved) isFederateEvent() {}

type InitiateFederateRestore struct {
	CallbackInfo
	Label          string
	FederateHandle uint64
	FederateName   string
	ClientState    []byte
}

func (InitiateFederateRestore) isFederateEvent() {}

type FederationNotRestored struct {
	CallbackInfo
	Label string
}

func (FederationNotRestored) isFederateEvent() {}

type RequestFederationRestoreSucceeded struct {
	CallbackInfo
	Label string
}

func (RequestFederationRestoreSucceeded) isFederateEvent() {}

type RequestFederationRestoreFailed struct {
	CallbackInfo
	Label, Reason string
}

func (RequestFederationRestoreFailed) isFederateEvent() {}

type FederationRestoreBegun struct{ CallbackInfo }

func (FederationRestoreBegun) isFederateEvent() {}

type MultipleObjectInstanceNameReservationSucceeded struct {
	CallbackInfo
	ObjectNames []string
}

func (MultipleObjectInstanceNameReservationSucceeded) isFederateEvent() {}

type MultipleObjectInstanceNameReservationFailed struct {
	CallbackInfo
	RequestedNames, CollidingNames []string
}

func (MultipleObjectInstanceNameReservationFailed) isFederateEvent() {}

type StartRegistrationForObjectClass struct {
	CallbackInfo
	ObjectClassHandle uint64
}

func (StartRegistrationForObjectClass) isFederateEvent() {}

type StopRegistrationForObjectClass struct {
	CallbackInfo
	ObjectClassHandle uint64
}

func (StopRegistrationForObjectClass) isFederateEvent() {}

type TurnInteractionsOn struct {
	CallbackInfo
	InteractionClassHandle uint64
}

func (TurnInteractionsOn) isFederateEvent() {}

type TurnInteractionsOff struct {
	CallbackInfo
	InteractionClassHandle uint64
}

func (TurnInteractionsOff) isFederateEvent() {}

type AttributesInScope struct {
	CallbackInfo
	ObjectHandle     uint64
	AttributeHandles []uint64
}

func (AttributesInScope) isFederateEvent() {}

type AttributesOutOfScope struct {
	CallbackInfo
	ObjectHandle     uint64
	AttributeHandles []uint64
}

func (AttributesOutOfScope) isFederateEvent() {}

func (f *Federate) translateRemainingEvent(evt *rtiv1.FederateEvent) Event {
	info := f.callbackInfo(evt)
	switch v := evt.GetEvent().(type) {
	case *rtiv1.FederateEvent_TransportationChangeConfirmed:
		if v.TransportationChangeConfirmed == nil {
			return nil
		}
		return transportationEvent(info, v.TransportationChangeConfirmed)
	case *rtiv1.FederateEvent_OwnershipAssumption, *rtiv1.FederateEvent_OwnershipAcquired, *rtiv1.FederateEvent_OwnershipDivestConfirmed, *rtiv1.FederateEvent_OwnershipReleaseRequested, *rtiv1.FederateEvent_OwnershipUnavailable:
		return f.translateOwnershipNotification(evt, info)
	case *rtiv1.FederateEvent_SaveInitiate, *rtiv1.FederateEvent_SaveCompleted, *rtiv1.FederateEvent_SaveFailed:
		return f.translateSaveNotification(evt, info)
	case *rtiv1.FederateEvent_RestoreInitiate, *rtiv1.FederateEvent_RestoreFailed, *rtiv1.FederateEvent_RestoreRequestSucceeded, *rtiv1.FederateEvent_RestoreRequestFailed, *rtiv1.FederateEvent_RestoreBegun:
		return f.translateRestoreNotification(evt, info)
	case *rtiv1.FederateEvent_SyncRegistrationSucceeded, *rtiv1.FederateEvent_SyncRegistrationFailed, *rtiv1.FederateEvent_ReservationMultiSucceeded, *rtiv1.FederateEvent_ReservationMultiFailed:
		return f.translateReservationNotification(evt, info)
	case *rtiv1.FederateEvent_StartRegistration, *rtiv1.FederateEvent_StopRegistration, *rtiv1.FederateEvent_TurnInteractionsOn, *rtiv1.FederateEvent_TurnInteractionsOff, *rtiv1.FederateEvent_AttributesInScope, *rtiv1.FederateEvent_AttributesOutOfScope:
		return f.translateDeclarationNotification(evt, info)
	default:
		return nil
	}
}

func (f *Federate) translateOwnershipNotification(evt *rtiv1.FederateEvent, info CallbackInfo) Event {
	switch v := evt.GetEvent().(type) {
	case *rtiv1.FederateEvent_OwnershipAssumption:
		e := v.OwnershipAssumption
		if e == nil {
			return nil
		}
		return RequestAttributeOwnershipAssumption{CallbackInfo: info, ObjectHandle: e.ObjectHandle, AttributeHandles: append([]uint64(nil), e.AttributeHandles...), DivestingFederate: e.DivestingFederate, Tag: append([]byte(nil), e.Tag...)}
	case *rtiv1.FederateEvent_OwnershipAcquired:
		e := v.OwnershipAcquired
		if e == nil {
			return nil
		}
		if evt.OwnershipReceipt == nil && e.OwningFederate == f.CurrentHandle() {
			f.updateOwnedAttributes(e.ObjectHandle, e.AttributeHandles, true)
		}
		return AttributeOwnershipAcquisitionNotification{CallbackInfo: info, ObjectHandle: e.ObjectHandle, AttributeHandles: append([]uint64(nil), e.AttributeHandles...), OwningFederate: e.OwningFederate, Tag: append([]byte(nil), e.Tag...)}
	case *rtiv1.FederateEvent_OwnershipDivestConfirmed:
		e := v.OwnershipDivestConfirmed
		if e == nil {
			return nil
		}
		return RequestDivestitureConfirmation{CallbackInfo: info, ObjectHandle: e.ObjectHandle, AttributeHandles: append([]uint64(nil), e.AttributeHandles...)}
	case *rtiv1.FederateEvent_OwnershipReleaseRequested:
		e := v.OwnershipReleaseRequested
		if e == nil {
			return nil
		}
		return RequestAttributeOwnershipRelease{CallbackInfo: info, ObjectHandle: e.ObjectHandle, AttributeHandles: append([]uint64(nil), e.AttributeHandles...), Tag: append([]byte(nil), e.Tag...)}
	case *rtiv1.FederateEvent_OwnershipUnavailable:
		e := v.OwnershipUnavailable
		if e == nil {
			return nil
		}
		return AttributeOwnershipUnavailable{CallbackInfo: info, ObjectHandle: e.ObjectHandle, AttributeHandles: append([]uint64(nil), e.AttributeHandles...)}
	default:
		return nil
	}
}

func (f *Federate) translateSaveNotification(evt *rtiv1.FederateEvent, info CallbackInfo) Event {
	switch v := evt.GetEvent().(type) {
	case *rtiv1.FederateEvent_SaveInitiate:
		e := v.SaveInitiate
		if e == nil {
			return nil
		}
		var time *float64
		if e.SaveTime != nil {
			value := *e.SaveTime
			time = &value
		}
		return InitiateFederateSave{CallbackInfo: info, Label: e.Label, SaveTime: time}
	case *rtiv1.FederateEvent_SaveCompleted:
		if v.SaveCompleted == nil {
			return nil
		}
		return FederationSaved{CallbackInfo: info, Label: v.SaveCompleted.Label}
	case *rtiv1.FederateEvent_SaveFailed:
		if v.SaveFailed == nil {
			return nil
		}
		return FederationNotSaved{CallbackInfo: info, Label: v.SaveFailed.Label}
	default:
		return nil
	}
}

func (f *Federate) translateRestoreNotification(evt *rtiv1.FederateEvent, info CallbackInfo) Event {
	switch v := evt.GetEvent().(type) {
	case *rtiv1.FederateEvent_RestoreInitiate:
		e := v.RestoreInitiate
		if e == nil {
			return nil
		}
		return InitiateFederateRestore{CallbackInfo: info, Label: e.Label, FederateHandle: e.FederateHandle, FederateName: e.FederateName, ClientState: append([]byte(nil), e.ClientState...)}
	case *rtiv1.FederateEvent_RestoreFailed:
		if v.RestoreFailed == nil {
			return nil
		}
		return FederationNotRestored{CallbackInfo: info, Label: v.RestoreFailed.Label}
	case *rtiv1.FederateEvent_RestoreRequestSucceeded:
		if v.RestoreRequestSucceeded == nil {
			return nil
		}
		return RequestFederationRestoreSucceeded{CallbackInfo: info, Label: v.RestoreRequestSucceeded.Label}
	case *rtiv1.FederateEvent_RestoreRequestFailed:
		if v.RestoreRequestFailed == nil {
			return nil
		}
		return RequestFederationRestoreFailed{CallbackInfo: info, Label: v.RestoreRequestFailed.Label, Reason: v.RestoreRequestFailed.Reason}
	case *rtiv1.FederateEvent_RestoreBegun:
		if v.RestoreBegun == nil {
			return nil
		}
		return FederationRestoreBegun{CallbackInfo: info}
	default:
		return nil
	}
}

func (f *Federate) translateReservationNotification(evt *rtiv1.FederateEvent, info CallbackInfo) Event {
	switch v := evt.GetEvent().(type) {
	case *rtiv1.FederateEvent_SyncRegistrationSucceeded:
		if v.SyncRegistrationSucceeded == nil {
			return nil
		}
		return SynchronizationPointRegistrationSucceeded{CallbackInfo: info, Label: v.SyncRegistrationSucceeded.Label}
	case *rtiv1.FederateEvent_SyncRegistrationFailed:
		if v.SyncRegistrationFailed == nil {
			return nil
		}
		return SynchronizationPointRegistrationFailed{CallbackInfo: info, Label: v.SyncRegistrationFailed.Label, Reason: int32(v.SyncRegistrationFailed.Reason)}
	case *rtiv1.FederateEvent_ReservationMultiSucceeded:
		if v.ReservationMultiSucceeded == nil {
			return nil
		}
		return MultipleObjectInstanceNameReservationSucceeded{CallbackInfo: info, ObjectNames: append([]string(nil), v.ReservationMultiSucceeded.ObjectNames...)}
	case *rtiv1.FederateEvent_ReservationMultiFailed:
		if v.ReservationMultiFailed == nil {
			return nil
		}
		return MultipleObjectInstanceNameReservationFailed{CallbackInfo: info, RequestedNames: append([]string(nil), v.ReservationMultiFailed.RequestedNames...), CollidingNames: append([]string(nil), v.ReservationMultiFailed.CollidingNames...)}
	default:
		return nil
	}
}

func (f *Federate) translateDeclarationNotification(evt *rtiv1.FederateEvent, info CallbackInfo) Event {
	switch v := evt.GetEvent().(type) {
	case *rtiv1.FederateEvent_StartRegistration:
		if v.StartRegistration == nil {
			return nil
		}
		return StartRegistrationForObjectClass{CallbackInfo: info, ObjectClassHandle: v.StartRegistration.ObjectClassHandle}
	case *rtiv1.FederateEvent_StopRegistration:
		if v.StopRegistration == nil {
			return nil
		}
		return StopRegistrationForObjectClass{CallbackInfo: info, ObjectClassHandle: v.StopRegistration.ObjectClassHandle}
	case *rtiv1.FederateEvent_TurnInteractionsOn:
		if v.TurnInteractionsOn == nil {
			return nil
		}
		return TurnInteractionsOn{CallbackInfo: info, InteractionClassHandle: v.TurnInteractionsOn.InteractionClassHandle}
	case *rtiv1.FederateEvent_TurnInteractionsOff:
		if v.TurnInteractionsOff == nil {
			return nil
		}
		return TurnInteractionsOff{CallbackInfo: info, InteractionClassHandle: v.TurnInteractionsOff.InteractionClassHandle}
	case *rtiv1.FederateEvent_AttributesInScope:
		if v.AttributesInScope == nil {
			return nil
		}
		return AttributesInScope{CallbackInfo: info, ObjectHandle: v.AttributesInScope.ObjectHandle, AttributeHandles: append([]uint64(nil), v.AttributesInScope.AttributeHandles...)}
	case *rtiv1.FederateEvent_AttributesOutOfScope:
		if v.AttributesOutOfScope == nil {
			return nil
		}
		return AttributesOutOfScope{CallbackInfo: info, ObjectHandle: v.AttributesOutOfScope.ObjectHandle, AttributeHandles: append([]uint64(nil), v.AttributesOutOfScope.AttributeHandles...)}
	default:
		return nil
	}
}
