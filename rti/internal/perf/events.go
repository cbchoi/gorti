package perf

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// rtiv1FederateEvent is a local alias for the generated proto type
// referenced by perfFederateEventCarrier. Centralizing the import here
// keeps baseline.go focused on the measurement harness.
type rtiv1FederateEvent = rtiv1.FederateEvent
