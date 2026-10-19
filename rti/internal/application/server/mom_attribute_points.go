package server

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

type momFederateAttributePublisher struct {
	delivery core.MOMPointAttributeDelivery
	foms     core.FOMRepository
	members  func(core.FederationName) []core.FederateHandle
}

func newMOMFederateAttributePublisher(delivery core.MOMPointAttributeDelivery, foms core.FOMRepository, members func(core.FederationName) []core.FederateHandle) (*momFederateAttributePublisher, error) {
	if delivery == nil || foms == nil || members == nil {
		return nil, fmt.Errorf("MOM: point delivery, FOMs and membership are required")
	}
	return &momFederateAttributePublisher{delivery: delivery, foms: foms, members: members}, nil
}

func (p *momFederateAttributePublisher) prepare(ctx context.Context, fed core.FederationName, subject, subscriber core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte) (core.MOMAttributePoint, map[core.AttributeHandle][]byte, error) {
	var point core.MOMAttributePoint
	if err := ctx.Err(); err != nil {
		return point, nil, err
	}
	if err := p.validateRecipients(fed, subject, subscriber); err != nil {
		return point, nil, err
	}
	if obj == 0 {
		return point, nil, core.ErrObjectHandleInvalid
	}
	fom, err := p.foms.Get(ctx, fed)
	if err != nil {
		return point, nil, err
	}
	if fom == nil {
		return point, nil, core.ErrFederationNotFound
	}
	federateClass, found := fom.LookupObjectClass(mom.ClassHLAfederate)
	if !found || cls != federateClass {
		return point, nil, core.ErrObjectClassNotFound
	}
	names, named := fom.(core.FOMHandleNameLookup)
	dimensions, dimensional := fom.(core.FOMAttributeDimensions)
	if !named || !dimensional {
		return point, nil, fmt.Errorf("MOM: attribute point metadata unavailable")
	}
	dimension, found := names.LookupDimension("HLAfederate")
	if !found {
		return point, nil, fmt.Errorf("MOM: HLAfederate dimension unavailable")
	}
	// HLAfederate coordinates intentionally use the handle's low 32 bits.
	point = core.MOMAttributePoint{Subject: subject, Dimension: core.DDMDimensionHandle(dimension), Coordinate: uint64(subject) & math.MaxUint32}
	values, err := cloneMOMPointAttributes(dimensions, cls, point.Dimension, attrs)
	if err != nil {
		return point, nil, err
	}
	if err := ctx.Err(); err != nil {
		return point, nil, err
	}
	return point, values, nil
}

func (p *momFederateAttributePublisher) PublishFederateAttributes(ctx context.Context, fed core.FederationName, subject core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte) error {
	point, values, err := p.prepare(ctx, fed, subject, 0, obj, cls, attrs)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return p.delivery.PublishMOMAttributePoint(ctx, fed, obj, cls, values, point)
}

func (p *momFederateAttributePublisher) ReflectFederateAttributes(ctx context.Context, fed core.FederationName, subject, subscriber core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte) error {
	if subscriber == 0 {
		return core.ErrFederateNotJoined
	}
	point, values, err := p.prepare(ctx, fed, subject, subscriber, obj, cls, attrs)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return p.delivery.ReflectMOMAttributePoint(ctx, fed, subscriber, obj, cls, values, point)
}

var _ core.MOMFederateAttributePublisher = (*momFederateAttributePublisher)(nil)

func cloneMOMPointAttributes(dimensions core.FOMAttributeDimensions, cls core.ObjectClassHandle, dimension core.DDMDimensionHandle, attrs map[core.AttributeHandle][]byte) (map[core.AttributeHandle][]byte, error) {
	values := make(map[core.AttributeHandle][]byte, len(attrs))
	for attr, value := range attrs {
		dims, found := dimensions.AttributeDimensions(cls, attr)
		if !found {
			return nil, core.ErrAttributeNotFound
		}
		if len(dims) != 1 || dims[0] != dimension {
			return nil, fmt.Errorf("MOM: federate attribute %d must specify only HLAfederate", attr)
		}
		values[attr] = slices.Clone(value)
	}
	return values, nil
}

func (p *momFederateAttributePublisher) validateRecipients(fed core.FederationName, subject, subscriber core.FederateHandle) error {
	roster := p.members(fed)
	if subject == 0 || !slices.Contains(roster, subject) || subscriber != 0 && !slices.Contains(roster, subscriber) {
		return core.ErrFederateNotJoined
	}
	return nil
}
