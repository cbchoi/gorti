package grpc

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *objectServiceOf[T]) ReleaseMultipleObjectInstanceNames(ctx context.Context, req *rtiv1.ReleaseMultipleObjectInstanceNamesRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	registry, ok := s.obj.(core.MultipleObjectNameReleaser)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "object registry does not support atomic multiple-name release")
	}
	if err := registry.ReleaseMultipleObjectInstanceNames(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), req.GetObjectNames()); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return emptyResponse, nil
}
