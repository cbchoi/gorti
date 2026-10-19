package object

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// Existing names retain the Float64 object API. Integer registries use the same implementation.
type CallbackEvent = CallbackEventOf[core.LogicalTime]
type DeletionState = DeletionStateOf[core.LogicalTime]
type IntegerCallbackEvent = CallbackEventOf[int64]
type IntegerDeletionState = DeletionStateOf[int64]
type IntegerOptions = OptionsOf[int64]
type IntegerRegistry = RegistryOf[int64]
type IntegerRegistryState = RegistryStateOf[int64]
type Options = OptionsOf[core.LogicalTime]
type OutgoingTSOValidator = OutgoingTSOValidatorOf[core.LogicalTime]
type Registry = RegistryOf[core.LogicalTime]
type RegistryState = RegistryStateOf[core.LogicalTime]
type ResignDeletionPlan = ResignDeletionPlanOf[core.LogicalTime]

func newReceiveEvent(seq uint64, cls core.InteractionClassHandle, params map[uint64][]byte, ts *core.LogicalTime) *outboundEvent {
	return newReceiveEventOf[core.LogicalTime](seq, cls, params, ts)
}

func New(opts Options) (*Registry, error) { return NewOf[core.LogicalTime](opts) }

func NewInteger(opts IntegerOptions) (*IntegerRegistry, error) { return NewOf[int64](opts) }

func newReflectEvent(seq uint64, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[uint64][]byte, ts *core.LogicalTime) *outboundEvent {
	return newReflectEventOf[core.LogicalTime](seq, obj, cls, attrs, ts)
}
