package federate

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func (f *Federate) GetUpdateRateValue(ctx context.Context, designator string) (float64, error) {
	resp, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetUpdateRateValue(ctx, &rtiv1.GetUpdateRateValueRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), UpdateRateDesignator: designator,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return resp.GetUpdateRate(), nil
}

func (f *Federate) GetUpdateRateValueForAttribute(ctx context.Context, objectHandle, attributeHandle uint64) (float64, error) {
	resp, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetUpdateRateValueForAttribute(ctx, &rtiv1.GetUpdateRateValueForAttributeRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: objectHandle, AttributeHandle: attributeHandle,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return resp.GetUpdateRate(), nil
}

func (f *Federate) GetAvailableDimensionsForClassAttribute(ctx context.Context, classHandle, attributeHandle uint64) ([]uint64, error) {
	resp, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetAvailableDimensionsForClassAttribute(ctx, &rtiv1.GetAvailableDimensionsForClassAttributeRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectClassHandle: classHandle, AttributeHandle: attributeHandle,
	})
	if err != nil {
		return nil, wrapStatusErr(err)
	}
	return resp.GetDimensionHandles(), nil
}

func (f *Federate) GetAvailableDimensionsForInteractionClass(ctx context.Context, classHandle uint64) ([]uint64, error) {
	resp, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetAvailableDimensionsForInteractionClass(ctx, &rtiv1.GetAvailableDimensionsForInteractionClassRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), InteractionClassHandle: classHandle,
	})
	if err != nil {
		return nil, wrapStatusErr(err)
	}
	return resp.GetDimensionHandles(), nil
}

func (f *Federate) GetDimensionHandleSet(ctx context.Context, regionHandle uint64) ([]uint64, error) {
	resp, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetDimensionHandleSet(f.sessionContext(ctx), &rtiv1.GetDimensionHandleSetRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), RegionHandle: regionHandle,
	})
	if err != nil {
		return nil, wrapStatusErr(err)
	}
	return resp.GetDimensionHandles(), nil
}
