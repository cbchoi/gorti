package server

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

// runtimeObjectOperations contains only representation-independent services.
// Wire timestamps are dispatched to typed registries by the transport adapter.
type runtimeObjectOperations interface {
	ClassOf(core.FederationName, core.ObjectHandle) core.ObjectClassHandle
	ProjectKnownObjectAttributes(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, []core.AttributeHandle, error)
	CaptureKnownObjectAttributes(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (object.KnownAttributeProjection, error)
	PrepareOwnershipAcquisition(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (func(), error)
	PrepareKnownOwnershipAcquisition(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, func(), error)
	PrepareKnownOwnershipOperation(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, func(), error)
	PrepareOwnershipReleaseDenied(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (func(), error)
	PrepareRegionAssociation(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, func(), error)
	PrepareKnownRegionAssociation(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, []core.AttributeHandle, map[core.AttributeHandle]core.FederateHandle, func(), error)
	LookupKnownObjectInstance(core.FederationName, core.FederateHandle, core.ObjectHandle) (string, core.ObjectClassHandle, bool)
	ModifyAttributeState(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, core.AttributeHandle, bool) error
	RefreshDiscoveries(context.Context, core.FederationName)
	ObjectClassSubscribed(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle)
	OnFederateResign(core.FederationName, core.FederateHandle)
	RecordRTIObjectDiscovery(core.FederationName, core.FederateHandle, core.ObjectHandle) error
}

type runtimeObjects struct {
	floating        *object.Registry
	integer         *object.RegistryOf[int64]
	implementations core.FederationTimeImplementation
}

var _ core.MOMPointAttributeDelivery = (*runtimeObjects)(nil)

func (r *runtimeObjects) PublishMOMAttributePoint(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte, point core.MOMAttributePoint) error {
	if r.isInteger(fed) {
		return r.integer.PublishMOMAttributePoint(ctx, fed, obj, cls, attrs, point)
	}
	return r.floating.PublishMOMAttributePoint(ctx, fed, obj, cls, attrs, point)
}

func (r *runtimeObjects) ReflectMOMAttributePoint(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte, point core.MOMAttributePoint) error {
	if r.isInteger(fed) {
		return r.integer.ReflectMOMAttributePoint(ctx, fed, recipient, obj, cls, attrs, point)
	}
	return r.floating.ReflectMOMAttributePoint(ctx, fed, recipient, obj, cls, attrs, point)
}

func (r *runtimeObjects) SendInteraction(ctx context.Context, fed core.FederationName, h core.FederateHandle, cls core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *core.LogicalTime) error {
	if r.isInteger(fed) {
		if ts != nil {
			return core.ErrTimeInvalidLogicalTime
		}
		return r.integer.SendInteraction(ctx, fed, h, cls, params, nil)
	}
	return r.floating.SendInteraction(ctx, fed, h, cls, params, ts)
}
func (r *runtimeObjects) UpdateAttributesRetractable(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs map[core.AttributeHandle][]byte, ts *core.LogicalTime, id uint64) error {
	if r.isInteger(fed) {
		if ts != nil {
			return core.ErrTimeInvalidLogicalTime
		}
		return r.integer.UpdateAttributesRetractable(ctx, fed, h, obj, attrs, nil, id)
	}
	return r.floating.UpdateAttributesRetractable(ctx, fed, h, obj, attrs, ts, id)
}
func (r *runtimeObjects) SendInteractionRetractable(ctx context.Context, fed core.FederationName, h core.FederateHandle, cls core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *core.LogicalTime, id uint64) error {
	if r.isInteger(fed) {
		if ts != nil {
			return core.ErrTimeInvalidLogicalTime
		}
		return r.integer.SendInteractionRetractable(ctx, fed, h, cls, params, nil, id)
	}
	return r.floating.SendInteractionRetractable(ctx, fed, h, cls, params, ts, id)
}
func (r *runtimeObjects) RetractMessage(fed core.FederationName, h core.FederateHandle, id uint64) int {
	if r.isInteger(fed) {
		return r.integer.RetractMessage(fed, h, id)
	}
	return r.floating.RetractMessage(fed, h, id)
}
func (r *runtimeObjects) RequestAttributeValueUpdate(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	if r.isInteger(fed) {
		return r.integer.RequestAttributeValueUpdate(ctx, fed, h, obj, attrs, tag)
	}
	return r.floating.RequestAttributeValueUpdate(ctx, fed, h, obj, attrs, tag)
}
func (r *runtimeObjects) RequestClassAttributeValueUpdate(ctx context.Context, fed core.FederationName, h core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, tag []byte) error {
	if r.isInteger(fed) {
		return r.integer.RequestClassAttributeValueUpdate(ctx, fed, h, cls, attrs, tag)
	}
	return r.floating.RequestClassAttributeValueUpdate(ctx, fed, h, cls, attrs, tag)
}
func (r *runtimeObjects) ChangeAttributeTransportType(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, transport core.TransportType) error {
	if r.isInteger(fed) {
		return r.integer.ChangeAttributeTransportType(ctx, fed, h, obj, attrs, transport)
	}
	return r.floating.ChangeAttributeTransportType(ctx, fed, h, obj, attrs, transport)
}
func (r *runtimeObjects) ChangeInteractionTransportType(ctx context.Context, fed core.FederationName, h core.FederateHandle, cls core.InteractionClassHandle, transport core.TransportType) error {
	if r.isInteger(fed) {
		return r.integer.ChangeInteractionTransportType(ctx, fed, h, cls, transport)
	}
	return r.floating.ChangeInteractionTransportType(ctx, fed, h, cls, transport)
}

func (r *runtimeObjects) isInteger(fed core.FederationName) bool {
	name, ok := r.implementations.LogicalTimeImplementationFor(fed)
	return ok && name == logicaltime.HLAinteger64Time.String()
}

func (r *runtimeObjects) selected(fed core.FederationName) runtimeObjectOperations {
	if r.isInteger(fed) {
		return r.integer
	}
	return r.floating
}

// instances projects only timeless instance identity, never a pending timestamp.
func (r *runtimeObjects) instances(fed core.FederationName) []object.InstanceState {
	if r.isInteger(fed) {
		return r.integer.ExportState(fed).Objects
	}
	return r.floating.ExportState(fed).Objects
}

func objectInstances(source any, fed core.FederationName) []object.InstanceState {
	if routed, ok := source.(interface {
		instances(core.FederationName) []object.InstanceState
	}); ok {
		return routed.instances(fed)
	}
	return source.(interface {
		ExportState(core.FederationName) *object.RegistryState
	}).ExportState(fed).Objects
}

func (r *runtimeObjects) ClassOf(fed core.FederationName, obj core.ObjectHandle) core.ObjectClassHandle {
	return r.selected(fed).ClassOf(fed, obj)
}
func (r *runtimeObjects) LookupKnownObjectInstance(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle) (string, core.ObjectClassHandle, bool) {
	return r.selected(fed).LookupKnownObjectInstance(fed, h, obj)
}
func (r *runtimeObjects) ModifyAttributeState(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, a core.AttributeHandle, owned bool) error {
	return r.selected(fed).ModifyAttributeState(ctx, fed, h, obj, a, owned)
}
func (r *runtimeObjects) RefreshDiscoveries(ctx context.Context, fed core.FederationName) {
	r.selected(fed).RefreshDiscoveries(ctx, fed)
}
func (r *runtimeObjects) ObjectClassSubscribed(ctx context.Context, fed core.FederationName, h core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) {
	r.selected(fed).ObjectClassSubscribed(ctx, fed, h, cls, attrs)
}
func (r *runtimeObjects) OnFederateResign(fed core.FederationName, h core.FederateHandle) {
	r.selected(fed).OnFederateResign(fed, h)
}
func (r *runtimeObjects) RecordRTIObjectDiscovery(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle) error {
	return r.selected(fed).RecordRTIObjectDiscovery(fed, h, obj)
}
func (r *runtimeObjects) PrepareObjectClassUnpublication(ctx context.Context, fed core.FederationName, h core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) (core.PreparedObjectClassUnpublication, error) {
	if r.isInteger(fed) {
		return r.integer.PrepareObjectClassUnpublication(ctx, fed, h, cls, attrs)
	}
	return r.floating.PrepareObjectClassUnpublication(ctx, fed, h, cls, attrs)
}

// MOM uses receive-order values. A non-nil Float timestamp must never be
// accepted through this facade for an Integer64 execution.
func (r *runtimeObjects) Register(ctx context.Context, fed core.FederationName, h core.FederateHandle, cls core.ObjectClassHandle, name string) (core.ObjectHandle, string, error) {
	if r.isInteger(fed) {
		return r.integer.Register(ctx, fed, h, cls, name)
	}
	return r.floating.Register(ctx, fed, h, cls, name)
}
func (r *runtimeObjects) RegisterWithRegions(ctx context.Context, fed core.FederationName, h core.FederateHandle, cls core.ObjectClassHandle, name string, regions map[core.AttributeHandle][]core.DDMRegionHandleCore) (core.ObjectHandle, string, error) {
	if r.isInteger(fed) {
		return r.integer.RegisterWithRegions(ctx, fed, h, cls, name, regions)
	}
	return r.floating.RegisterWithRegions(ctx, fed, h, cls, name, regions)
}
func (r *runtimeObjects) UpdateAttributes(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs map[core.AttributeHandle][]byte, ts *core.LogicalTime) error {
	if r.isInteger(fed) {
		if ts != nil {
			return fmt.Errorf("%w: Float timestamp on Integer64 federation", core.ErrTimeInvalidLogicalTime)
		}
		return r.integer.UpdateAttributes(ctx, fed, h, obj, attrs, nil)
	}
	return r.floating.UpdateAttributes(ctx, fed, h, obj, attrs, ts)
}
func (r *runtimeObjects) Delete(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, ts *core.LogicalTime, tag []byte) error {
	if r.isInteger(fed) {
		if ts != nil {
			return fmt.Errorf("%w: Float timestamp on Integer64 federation", core.ErrTimeInvalidLogicalTime)
		}
		return r.integer.Delete(ctx, fed, h, obj, nil, tag)
	}
	return r.floating.Delete(ctx, fed, h, obj, ts, tag)
}
func (r *runtimeObjects) LocalDelete(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle) error {
	if r.isInteger(fed) {
		return r.integer.LocalDelete(ctx, fed, h, obj)
	}
	return r.floating.LocalDelete(ctx, fed, h, obj)
}
func (r *runtimeObjects) Snapshot(fed core.FederationName) core.ObjectSnapshot {
	if r.isInteger(fed) {
		return r.integer.Snapshot(fed)
	}
	return r.floating.Snapshot(fed)
}
