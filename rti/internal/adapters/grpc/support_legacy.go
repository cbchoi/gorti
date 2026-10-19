package grpc

import rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"

// Only the two pre-existing administrative lookups permit an omitted caller.
func legacyGlobalObjectLookup(request any) bool {
	switch req := request.(type) {
	case *rtiv1.GetObjectInstanceHandleRequest:
		return req != nil && req.GetFederateHandle() == 0
	case *rtiv1.GetObjectInstanceNameRequest:
		return req != nil && req.GetFederateHandle() == 0
	default:
		return false
	}
}
