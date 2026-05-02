package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// federationService translates rti.v1.FederationService RPCs into
// core.FederationStore calls and maps sentinels back to gRPC status codes
// per proto/rti/v1/errors.proto.
type federationService struct {
	rtiv1.UnimplementedFederationServiceServer
	fed core.FederationStore
}

func newFederationService(fed core.FederationStore) *federationService {
	return &federationService{fed: fed}
}

// CreateFederation implements rti.v1.FederationService.
func (s *federationService) CreateFederation(_ context.Context, _ *rtiv1.CreateFederationRequest) (*rtiv1.CreateFederationResponse, error) {
	return nil, status.Error(codes.Unimplemented, "federation: CreateFederation pending TASK-034 phase 2")
}

// DestroyFederation implements rti.v1.FederationService.
func (s *federationService) DestroyFederation(_ context.Context, _ *rtiv1.DestroyFederationRequest) (*rtiv1.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "federation: DestroyFederation pending TASK-034 phase 2")
}

// JoinFederation implements rti.v1.FederationService.
func (s *federationService) JoinFederation(_ context.Context, _ *rtiv1.JoinFederationRequest) (*rtiv1.JoinFederationResponse, error) {
	return nil, status.Error(codes.Unimplemented, "federation: JoinFederation pending TASK-034 phase 2")
}

// ResignFederation implements rti.v1.FederationService.
func (s *federationService) ResignFederation(_ context.Context, _ *rtiv1.ResignFederationRequest) (*rtiv1.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "federation: ResignFederation pending TASK-034 phase 2")
}

// ListFederations implements rti.v1.FederationService.
func (s *federationService) ListFederations(_ context.Context, _ *rtiv1.ListFederationsRequest) (*rtiv1.ListFederationsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "federation: ListFederations pending TASK-034 phase 2")
}
