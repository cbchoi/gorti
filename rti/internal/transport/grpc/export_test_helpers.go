package grpc

import (
	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// NewSupportServiceForTest constructs a SupportService directly,
// bypassing the full Server compose. Used by rti/spec/M25 to exercise
// the §10.2 handler against a stub FOMRepository without dragging in
// federation / object / outbox stubs.
func NewSupportServiceForTest(foms core.FOMRepository) rtiv1.SupportServiceServer {
	return newSupportService(foms)
}

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

