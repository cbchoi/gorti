package federate

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

// GetKnownObjectClassHandle resolves only objects known to this federate.
func (f *Federate) GetKnownObjectClassHandle(ctx context.Context, objectHandle uint64) (uint64, error) {
	resp, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetKnownObjectClassHandle(ctx, &rtiv1.GetKnownObjectClassHandleRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: objectHandle,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return resp.GetClassHandle(), nil
}

func (f *Federate) GetObjectInstanceHandle(ctx context.Context, name string) (uint64, error) {
	resp, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetObjectInstanceHandle(ctx, &rtiv1.GetObjectInstanceHandleRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectName: name,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return resp.GetObjectHandle(), nil
}

func (f *Federate) GetObjectInstanceName(ctx context.Context, objectHandle uint64) (string, error) {
	resp, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetObjectInstanceName(ctx, &rtiv1.GetObjectInstanceNameRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: objectHandle,
	})
	if err != nil {
		return "", wrapStatusErr(err)
	}
	return resp.GetObjectName(), nil
}
