// HLArequest* counter handlers (M20.5).
//
// Per IEEE 1516.1-2010 §10.4.4, federates can request a count of
// {interactions, updates, reflections} {sent, received} via the
// HLAmanager.HLAfederate.HLArequest.* interaction tree. The handler
// produces an HLAreport* ResponseInteraction carrying the counter
// snapshot from the MOM's HLAfederate state.
//
// Counter responses don't fan out beyond the requesting federate —
// they go back to the sender as a private acknowledgement. M20.5
// emits ResponseInteraction structs; M20.6 wires the actual
// outbox send.

package mom

import (
	"context"
	"encoding/binary"

	"github.com/cbchoi/gorti/rti/internal/core"
)

const (
	ClassRequestInteractionsSent     = "HLAmanager.HLAfederate.HLArequest.HLArequestInteractionsSent"
	ClassRequestInteractionsReceived = "HLAmanager.HLAfederate.HLArequest.HLArequestInteractionsReceived"
	ClassRequestUpdatesSent          = "HLAmanager.HLAfederate.HLArequest.HLArequestUpdatesSent"
	ClassRequestReflectionsReceived  = "HLAmanager.HLAfederate.HLArequest.HLArequestReflectionsReceived"

	// HLAreport* response classes — emitted by the dispatcher (M20.6).
	ClassReportInteractionsSent     = "HLAmanager.HLAfederate.HLAreport.HLAreportInteractionsSent"
	ClassReportInteractionsReceived = "HLAmanager.HLAfederate.HLAreport.HLAreportInteractionsReceived"
	ClassReportUpdatesSent          = "HLAmanager.HLAfederate.HLAreport.HLAreportUpdatesSent"
	ClassReportReflectionsReceived  = "HLAmanager.HLAfederate.HLAreport.HLAreportReflectionsReceived"
)

func registerRequestHandlers(d *Dispatcher) {
	d.Register(ClassRequestInteractionsSent,
		makeCounterRequestHandler(ClassReportInteractionsSent, func(a FederateAttributes) uint32 {
			return a.InteractionsSent
		}))
	d.Register(ClassRequestInteractionsReceived,
		makeCounterRequestHandler(ClassReportInteractionsReceived, func(a FederateAttributes) uint32 {
			return a.InteractionsReceived
		}))
	d.Register(ClassRequestUpdatesSent,
		makeCounterRequestHandler(ClassReportUpdatesSent, func(a FederateAttributes) uint32 {
			return a.UpdatesSent
		}))
	d.Register(ClassRequestReflectionsReceived,
		makeCounterRequestHandler(ClassReportReflectionsReceived, func(a FederateAttributes) uint32 {
			return a.ReflectionsReceived
		}))
}

// encodeHLAhandle returns the 4-byte big-endian encoding of a
// FederateHandle. IEEE 1516.2 §B.2.4 (HLAhandle) is a 4-byte
// unsigned. Used in HLAreport* responses.
func encodeHLAhandle(h core.FederateHandle) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(h))
	return b
}

// encodeHLAcount returns the 4-byte big-endian encoding of a
// uint32 counter. IEEE 1516.2 §B.2.5 (HLAcount).
func encodeHLAcount(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// makeCounterRequestHandler builds a Handler that, on dispatch,
// snapshots the sender's HLAfederate MOM counters and returns a
// ResponseInteraction with the matching HLAreport* class name +
// {HLAfederate, HLAcount} parameter payload.
func makeCounterRequestHandler(
	responseClass string,
	pick func(FederateAttributes) uint32,
) Handler {
	return func(
		_ context.Context,
		dctx DispatchContext,
		sender core.FederateHandle,
		_ map[core.ParameterHandle][]byte,
	) ([]ResponseInteraction, error) {
		attrs, ok := dctx.MOM.QueryFederateAttributes(dctx.Federation, sender)
		if !ok {
			// Unknown sender — no response. Matches "no-op on
			// unknown federate" convention from M20.3/4.
			return nil, nil
		}
		count := pick(attrs)
		resp := ResponseInteraction{
			ClassName: responseClass,
			Params: map[string][]byte{
				"HLAfederate": encodeHLAhandle(sender),
				"HLAcount":    encodeHLAcount(count),
			},
		}
		return []ResponseInteraction{resp}, nil
	}
}
