// Request/report layouts follow the embedded standard MIM. The requested
// HLAfederate identifies the subject; production routes reports through the
// configured subscription source. The private requester route remains only
// for legacy callers without a source. Regional MOM routing remains incomplete.

package mom

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
)

const (
	ClassRequestInteractionsSent     = "HLAmanager.HLAfederate.HLArequest.HLArequestInteractionsSent"
	ClassRequestInteractionsReceived = "HLAmanager.HLAfederate.HLArequest.HLArequestInteractionsReceived"
	ClassRequestUpdatesSent          = "HLAmanager.HLAfederate.HLArequest.HLArequestUpdatesSent"
	ClassRequestReflectionsReceived  = "HLAmanager.HLAfederate.HLArequest.HLArequestReflectionsReceived"

	// HLAreport* response classes — emitted by the dispatcher (M20.6).
	ClassReportInteractionsSent       = "HLAmanager.HLAfederate.HLAreport.HLAreportInteractionsSent"
	ClassReportInteractionsReceived   = "HLAmanager.HLAfederate.HLAreport.HLAreportInteractionsReceived"
	ClassReportUpdatesSent            = "HLAmanager.HLAfederate.HLAreport.HLAreportUpdatesSent"
	ClassReportReflectionsReceived    = "HLAmanager.HLAfederate.HLAreport.HLAreportReflectionsReceived"
	ClassRequestPublications          = "HLAmanager.HLAfederate.HLArequest.HLArequestPublications"
	ClassReportObjectClassPublication = "HLAmanager.HLAfederate.HLAreport.HLAreportObjectClassPublication"
	ClassReportInteractionPublication = "HLAmanager.HLAfederate.HLAreport.HLAreportInteractionPublication"
	ClassRequestMIMData               = "HLAmanager.HLAfederation.HLArequest.HLArequestMIMdata"
	ClassReportMIMData                = "HLAmanager.HLAfederation.HLAreport.HLAreportMIMdata"
)

var ErrCounterDataUnavailable = errors.New("mom: per-transport/class counter data unavailable")

func registerRequestHandlers(d *Dispatcher) {
	d.Register(ClassRequestInteractionsSent, makeCounterRequestHandler(ClassRequestInteractionsSent, ClassReportInteractionsSent, "HLAinteractionCounts", InteractionsSent))
	d.Register(ClassRequestInteractionsReceived, makeCounterRequestHandler(ClassRequestInteractionsReceived, ClassReportInteractionsReceived, "HLAinteractionCounts", InteractionsReceived))
	d.Register(ClassRequestUpdatesSent, makeCounterRequestHandler(ClassRequestUpdatesSent, ClassReportUpdatesSent, "HLAupdateCounts", UpdatesSent))
	d.Register(ClassRequestReflectionsReceived, makeCounterRequestHandler(ClassRequestReflectionsReceived, ClassReportReflectionsReceived, "HLAreflectCounts", ReflectionsReceived))
	d.Register(ClassRequestPublications, handleRequestPublications)
	d.Register(ClassRequestMIMData, handleRequestMIMData)
}

// encodeHLAhandle wraps the SDK's eight-byte BE handle payload in the
// MIM's HLAvariableArray<HLAbyte> encoding. This replaces the legacy raw
// uint32 MOM output, which lost the high bits of runtime federate handles.
func encodeHLAhandle(h core.FederateHandle) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b, 8)
	binary.BigEndian.PutUint64(b[4:], uint64(h))
	return b
}

// encodeHLAcount returns the 4-byte big-endian encoding of a
// counter using the MIM's HLAinteger32BE representation.
func encodeHLAcount(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func requestTarget(dctx DispatchContext, class string, params map[core.ParameterHandle][]byte) (core.FederateHandle, error) {
	target, err := decodeSwitchTarget(dctx, class, params)
	if err != nil {
		return 0, malformed("%v", err)
	}
	if len(params) != 1 {
		return 0, malformed("HLAfederate: request requires only the target parameter")
	}
	if dctx.MOM == nil {
		return 0, fmt.Errorf("HLAfederate: MOM unavailable")
	}
	if _, ok := dctx.MOM.QueryFederateAttributes(dctx.Federation, target); !ok {
		return 0, fmt.Errorf("HLAfederate: target %d is not joined to federation %q", target, dctx.Federation)
	}
	return target, nil
}

func makeCounterRequestHandler(requestClass, responseClass, countParameter string, kind CounterKind) Handler {
	return func(ctx context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
		target, err := requestTarget(dctx, requestClass, params)
		if err != nil {
			return nil, err
		}
		if dctx.Counters == nil {
			return nil, ErrCounterDataUnavailable
		}
		counts, err := dctx.Counters(ctx, dctx.Federation, target, kind)
		if err != nil {
			return nil, err
		}
		if len(counts) == 0 {
			return nil, ErrCounterDataUnavailable
		}
		responses := make([]ResponseInteraction, 0, len(counts))
		seen := make(map[string]bool)
		// Validate and encode the entire source snapshot before emitting anything.
		for _, transport := range counts {
			if (transport.Transportation != "HLAreliable" && transport.Transportation != "HLAbestEffort") || seen[transport.Transportation] {
				return nil, fmt.Errorf("HLAtransportation: unsupported or duplicate source name %q", transport.Transportation)
			}
			seen[transport.Transportation] = true
			encoded, err := encodeClassCounts(transport.Counts)
			if err != nil {
				return nil, err
			}
			responses = append(responses, ResponseInteraction{ClassName: responseClass, Params: map[string][]byte{
				"HLAfederate":       encodeHLAhandle(target),
				"HLAtransportation": encodeHLAunicodeString(transport.Transportation),
				countParameter:      encoded,
			}})
		}
		return responses, nil
	}
}

// Both MIM count arrays contain fixed records of {HLAhandle, HLAcount}.
// The four-byte array header, twelve-byte handle and four-byte count are all
// naturally aligned. Zero-count entries are explicitly excluded by the MIM.
func encodeClassCounts(counts map[uint64]uint32) ([]byte, error) {
	var classes []uint64
	for class, count := range counts {
		if class == 0 {
			return nil, fmt.Errorf("MOM class count: invalid zero class handle")
		}
		if count > math.MaxInt32 {
			return nil, ErrCounterOverflow
		}
		if count != 0 {
			classes = append(classes, class)
		}
	}
	sort.Slice(classes, func(i, j int) bool { return classes[i] < classes[j] })
	b, err := encodeArrayLength(len(classes))
	if err != nil {
		return nil, err
	}
	for _, class := range classes {
		b = append(b, encodeHLAhandle(core.FederateHandle(class))...)
		b = append(b, encodeHLAcount(counts[class])...)
	}
	return b, nil
}

// The declaration manager already exposes a coherent detached snapshot for
// persistence. Reuse it instead of racing separate class/attribute lookups.
type publicationStateSource interface {
	ExportState(core.FederationName) *declaration.ManagerState
}

func handleRequestPublications(_ context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
	target, err := requestTarget(dctx, ClassRequestPublications, params)
	if err != nil {
		return nil, err
	}
	source, ok := dctx.MOM.fanout.decls.(publicationStateSource)
	if !ok {
		return nil, fmt.Errorf("mom: publication state unavailable")
	}
	state := source.ExportState(dctx.Federation)
	if state == nil {
		return nil, fmt.Errorf("mom: publication state unavailable")
	}
	objects := make(map[core.ObjectClassHandle][]core.FederateHandle)
	var interactions []core.FederateHandle
	for _, entry := range state.ObjectPublications {
		for _, publisher := range entry.Federates {
			if publisher == target {
				objects[entry.Class] = append(objects[entry.Class], core.FederateHandle(entry.Attribute))
				break
			}
		}
	}
	for _, entry := range state.InteractionPublications {
		for _, publisher := range entry.Federates {
			if publisher == target {
				interactions = append(interactions, core.FederateHandle(entry.Class))
				break
			}
		}
	}
	responses := []ResponseInteraction{{ClassName: ClassReportInteractionPublication, Params: map[string][]byte{
		"HLAfederate": encodeHLAhandle(target), "HLAinteractionClassList": encodeFederateHandleList(interactions),
	}}}
	var classes []core.ObjectClassHandle
	for class := range objects {
		classes = append(classes, class)
	}
	sort.Slice(classes, func(i, j int) bool { return classes[i] < classes[j] })
	count, err := encodeArrayLength(len(classes))
	if err != nil {
		return nil, err
	}
	for _, class := range classes {
		responses = append(responses, ResponseInteraction{ClassName: ClassReportObjectClassPublication, Params: map[string][]byte{
			"HLAfederate": encodeHLAhandle(target), "HLAnumberOfClasses": count,
			"HLAobjectClass": encodeHLAhandle(core.FederateHandle(class)), "HLAattributeList": encodeFederateHandleList(objects[class]),
		}})
	}
	if len(classes) == 0 {
		responses = append(responses, ResponseInteraction{ClassName: ClassReportObjectClassPublication, Params: map[string][]byte{
			"HLAfederate": encodeHLAhandle(target), "HLAnumberOfClasses": encodeHLAcount(0),
		}})
	}
	return responses, nil
}

func handleRequestMIMData(_ context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
	if len(params) != 0 {
		return nil, fmt.Errorf("HLArequestMIMdata: no parameters expected")
	}
	if dctx.MOM == nil {
		return nil, fmt.Errorf("mom: federation state unavailable")
	}
	if _, ok := dctx.MOM.QueryFederationAttributes(dctx.Federation); !ok {
		return nil, core.ErrFederationNotFound
	}
	// The production FOM repository always merges this embedded MIM. This
	// handler does not claim to support an alternate, per-federation MIM.
	return []ResponseInteraction{{ClassName: ClassReportMIMData, Params: map[string][]byte{
		"HLAMIMdata": encodeHLAunicodeString(string(mim.StandardMIMBytes())),
	}}}, nil
}
