package grpc

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fomViewProvider interface {
	FOMView(context.Context, core.FederationName) (core.FOMView, error)
}

func (s *federationService) GetFOMView(ctx context.Context, req *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	// The membership interceptor holds the operation lease for this request.
	// Taking another RLock here can deadlock behind a pending resign writer.
	provider, ok := s.foms.(fomViewProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "FOM view is unavailable")
	}
	view, err := provider.FOMView(ctx, core.FederationName(req.GetFederationName()))
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	return encodeFOMView(view), nil
}

func encodeFOMView(view core.FOMView) *rtiv1.FOMView {
	out := &rtiv1.FOMView{Revision: view.Revision, Digest: view.Digest}
	if view.MIMModule != nil {
		out.MimModule = &rtiv1.FOMModule{Path: view.MIMModule.Path, Xml: append([]byte(nil), view.MIMModule.XML...)}
	}
	for _, module := range view.Modules {
		out.Modules = append(out.Modules, &rtiv1.FOMModule{Path: module.Path, Xml: append([]byte(nil), module.XML...)})
	}
	for _, class := range view.ObjectClasses {
		wire := &rtiv1.FOMClassBinding{Name: class.Name, Handle: uint64(class.Handle)}
		for _, attr := range class.Attributes {
			wire.Members = append(wire.Members, &rtiv1.FOMMemberBinding{Name: attr.Name, Handle: uint64(attr.Handle)})
		}
		out.ObjectClasses = append(out.ObjectClasses, wire)
	}
	for _, class := range view.InteractionClasses {
		wire := &rtiv1.FOMClassBinding{Name: class.Name, Handle: uint64(class.Handle)}
		for _, param := range class.Parameters {
			wire.Members = append(wire.Members, &rtiv1.FOMMemberBinding{Name: param.Name, Handle: uint64(param.Handle)})
		}
		out.InteractionClasses = append(out.InteractionClasses, wire)
	}
	for _, dimension := range view.Dimensions {
		out.Dimensions = append(out.Dimensions, &rtiv1.FOMDimensionBinding{Name: dimension.Name, Handle: uint64(dimension.Handle), UpperBound: dimension.UpperBound})
	}
	return out
}
