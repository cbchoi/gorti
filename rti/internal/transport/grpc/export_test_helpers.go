package grpc

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// FederationServiceForTest returns the FederationService server that
// Server registers on the gRPC handler. Cross-package tests
// (rti/spec/M19) call this so they can drive the handler directly
// without spinning up a real network listener — the same coverage at
// a fraction of the cost.
//
// EXPORTED FOR TEST CODE ONLY. Do not import from production paths.
func FederationServiceForTest(s *Server) rtiv1.FederationServiceServer {
	return s.fedService
}
