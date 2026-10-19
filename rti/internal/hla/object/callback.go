package object

import (
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// CallbackKind identifies an object-management callback independently of its
// network representation.
type CallbackKind uint8

const (
	CallbackDiscover CallbackKind = iota + 1
	CallbackReflect
	CallbackReceive
	CallbackRemove
	CallbackProvideUpdate
	CallbackAttributesInScope
	CallbackAttributesOutOfScope
	CallbackReservationSucceeded
	CallbackReservationFailed
	CallbackMultipleReservationSucceeded
	CallbackMultipleReservationFailed
	CallbackDeleteMarker
)

// CallbackEvent is the transport-neutral OM callback envelope. Fields are
// populated according to Kind; byte maps are owned by the event and immutable
// after emission.
type CallbackEventOf[T core.LogicalTimeScalar] struct {
	Metadata         *core.ObjectCallbackMetadata
	Sequence         uint64
	Kind             CallbackKind
	Object           core.ObjectHandle
	ObjectClass      core.ObjectClassHandle
	ObjectName       string
	Attributes       map[uint64][]byte
	AttributeHandles []core.AttributeHandle
	InteractionClass core.InteractionClassHandle
	Parameters       map[uint64][]byte
	LogicalTime      *T
	UserSuppliedTag  []byte
	ObjectNames      []string
	CollidingNames   []string
}

// outboundEvent is retained as an internal alias while producers migrate to
// the domain name.
type outboundEvent = CallbackEvent

func (e *CallbackEventOf[T]) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}

// ConveyedRegionCopies returns detached copy descriptors. Source-region handles
// in legacy metadata never imply a copied-region callback lifetime.
func (e *CallbackEventOf[T]) ConveyedRegionCopies() []core.ConveyedRegionCopy {
	if e == nil || e.Metadata == nil || e.Kind != CallbackReflect && e.Kind != CallbackReceive {
		return nil
	}
	return slices.Clone(e.Metadata.ConveyedRegions)
}

var _ core.ConveyedRegionEvent = (*CallbackEvent)(nil)
var _ core.ConveyedRegionEvent = (*IntegerCallbackEvent)(nil)
