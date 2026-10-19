package grpc

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// NewSupportServiceHandler constructs the Support wire adapter over a FOM
// repository. It is useful in compositions that do not need a full Server.
func NewSupportServiceHandler(foms core.FOMRepository) rtiv1.SupportServiceServer {
	return newSupportService(foms)
}

// FederationService returns the Federation wire adapter registered by Server.
// Alternate application compositions can expose the same adapter without
// opening a network listener.
func FederationService(s *Server) rtiv1.FederationServiceServer {
	return s.fedService
}
