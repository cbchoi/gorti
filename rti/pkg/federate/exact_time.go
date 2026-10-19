package federate

import (
	"context"
	"fmt"
	"math"

	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/proto"
)

func (f *Federate) timeRepresentation() logicaltime.Representation {
	if f != nil && f.logicalTimeImplementationName == HLAinteger64Time {
		return logicaltime.HLAinteger64Time
	}
	return logicaltime.HLAfloat64Time
}

func (f *Federate) requireFloatTime() error {
	if f.timeRepresentation() != logicaltime.HLAfloat64Time {
		return fmt.Errorf("%w: use the exact Value API for an Integer64 federation", logicaltime.ErrRepresentationMismatch)
	}
	return nil
}

func (f *Federate) optionalTimeWire(value any) (*float64, *rtiv1.LogicalTimeValue, error) {
	var selected *logicaltime.Time
	switch v := value.(type) {
	case nil:
	case *float64:
		if v != nil {
			if err := f.requireFloatTime(); err != nil {
				return nil, nil, err
			}
			if math.IsInf(*v, 1) {
				copied := *v
				return &copied, nil, nil
			}
			converted, err := logicaltime.Float64(*v)
			if err != nil {
				return nil, nil, err
			}
			selected = &converted
		}
	case *logicaltime.Time:
		selected = v
	default:
		return nil, nil, logicaltime.ErrRepresentationMismatch
	}
	return codec.EncodeOptionalLogicalTimePair(f.timeRepresentation(), selected)
}

func (f *Federate) EnableTimeRegulationValue(ctx context.Context, lookahead logicaltime.Interval) error {
	legacy, exact, err := codec.EncodeLogicalTimeIntervalPair(f.timeRepresentation(), lookahead)
	if err != nil {
		return err
	}
	_, err = f.conn.tm.EnableTimeRegulation(ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), Lookahead: legacy, LookaheadValue: exact})
	return wrapStatusErr(err)
}
func (f *Federate) ModifyLookaheadValue(ctx context.Context, lookahead logicaltime.Interval) error {
	legacy, exact, err := codec.EncodeLogicalTimeIntervalPair(f.timeRepresentation(), lookahead)
	if err != nil {
		return err
	}
	_, err = f.conn.tm.ModifyLookahead(ctx, &rtiv1.ModifyLookaheadRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), Lookahead: legacy, LookaheadValue: exact})
	return wrapStatusErr(err)
}

func (f *Federate) advanceValue(ctx context.Context, method string, value logicaltime.Time) error {
	legacy, exact, err := codec.EncodeLogicalTimePair(f.timeRepresentation(), value)
	if err != nil {
		return err
	}
	return f.advanceWire(ctx, method, legacy, exact)
}

func (f *Federate) advanceWire(ctx context.Context, method string, legacy float64, exact *rtiv1.LogicalTimeValue) error {
	var err error
	var req proto.Message
	switch method {
	case "NextMessageRequest":
		req = &rtiv1.NERRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), LogicalTime: legacy, LogicalTimeValue: exact}
	case "NextMessageRequestAvailable":
		req = &rtiv1.NMRARequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), LogicalTime: legacy, LogicalTimeValue: exact}
	case "TimeAdvanceRequest":
		req = &rtiv1.TARRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), LogicalTime: legacy, LogicalTimeValue: exact}
	case "TimeAdvanceRequestAvailable":
		req = &rtiv1.TARARequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), LogicalTime: legacy, LogicalTimeValue: exact}
	case "FlushQueueRequest":
		req = &rtiv1.FQRRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), LogicalTime: legacy, LogicalTimeValue: exact}
	default:
		return fmt.Errorf("unknown advance service %q", method)
	}
	pending := f.beginSDKAdvance(method == "FlushQueueRequest")
	defer f.timeRequestMu.Unlock()
	switch request := req.(type) {
	case *rtiv1.NERRequest:
		_, err = f.conn.tm.NextMessageRequest(ctx, request)
	case *rtiv1.NMRARequest:
		_, err = f.conn.tm.NextMessageRequestAvailable(ctx, request)
	case *rtiv1.TARRequest:
		_, err = f.conn.tm.TimeAdvanceRequest(ctx, request)
	case *rtiv1.TARARequest:
		_, err = f.conn.tm.TimeAdvanceRequestAvailable(ctx, request)
	case *rtiv1.FQRRequest:
		_, err = f.conn.tm.FlushQueueRequest(ctx, request)
	}
	f.finishSDKAdvance(pending, err)
	return wrapStatusErr(err)
}
func (f *Federate) NextMessageRequestValue(ctx context.Context, t logicaltime.Time) error {
	return f.advanceValue(ctx, "NextMessageRequest", t)
}
func (f *Federate) NextMessageRequestAvailableValue(ctx context.Context, t logicaltime.Time) error {
	return f.advanceValue(ctx, "NextMessageRequestAvailable", t)
}
func (f *Federate) TimeAdvanceRequestValue(ctx context.Context, t logicaltime.Time) error {
	return f.advanceValue(ctx, "TimeAdvanceRequest", t)
}
func (f *Federate) TimeAdvanceRequestAvailableValue(ctx context.Context, t logicaltime.Time) error {
	return f.advanceValue(ctx, "TimeAdvanceRequestAvailable", t)
}
func (f *Federate) FlushQueueRequestValue(ctx context.Context, t logicaltime.Time) error {
	return f.advanceValue(ctx, "FlushQueueRequest", t)
}

func (f *Federate) QueryLogicalTimeValue(ctx context.Context) (logicaltime.Time, error) {
	f.mu.Lock()
	epoch := f.objectKnowledgeEpoch
	f.mu.Unlock()
	r, err := f.conn.tm.QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle()})
	if err != nil {
		return logicaltime.Time{}, wrapStatusErr(err)
	}
	value, err := codec.DecodeLogicalTimePair(f.timeRepresentation(), r.LogicalTime, r.LogicalTimeValue)
	if err == nil {
		f.mu.Lock()
		if f.objectKnowledgeEpoch == epoch {
			f.retireSDKRemovalsLocked(value)
		}
		f.mu.Unlock()
	}
	return value, err
}
func (f *Federate) QueryLookaheadValue(ctx context.Context) (logicaltime.Interval, error) {
	r, err := f.conn.tm.QueryLookahead(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle()})
	if err != nil {
		return logicaltime.Interval{}, wrapStatusErr(err)
	}
	return codec.DecodeLogicalTimeIntervalPair(f.timeRepresentation(), r.Lookahead, r.LookaheadValue)
}
func (f *Federate) QueryLBTSValue(ctx context.Context) (logicaltime.Time, bool, error) {
	r, err := f.conn.tm.QueryLBTS(ctx, &rtiv1.QueryLBTSRequest{WireVersion: 1, FederationName: f.federationName})
	if err != nil {
		return logicaltime.Time{}, false, wrapStatusErr(err)
	}
	if !r.Finite {
		return logicaltime.Time{}, false, nil
	}
	value, err := codec.DecodeLogicalTimePair(f.timeRepresentation(), r.Lbts, r.LbtsValue)
	return value, true, err
}
func (f *Federate) QueryGALTValue(ctx context.Context) (logicaltime.Time, bool, error) {
	r, err := f.conn.tm.QueryGALT(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle()})
	if err != nil {
		return logicaltime.Time{}, false, wrapStatusErr(err)
	}
	if !r.Finite {
		return logicaltime.Time{}, false, nil
	}
	value, err := codec.DecodeLogicalTimePair(f.timeRepresentation(), r.Galt, r.GaltValue)
	return value, true, err
}
func (f *Federate) QueryLITSValue(ctx context.Context) (logicaltime.Time, bool, error) {
	r, err := f.conn.tm.QueryLITS(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle()})
	if err != nil {
		return logicaltime.Time{}, false, wrapStatusErr(err)
	}
	if !r.Finite {
		return logicaltime.Time{}, false, nil
	}
	value, err := codec.DecodeLogicalTimePair(f.timeRepresentation(), r.Lits, r.LitsValue)
	return value, true, err
}

func (f *Federate) RequestFederationSaveValue(ctx context.Context, label string, value *logicaltime.Time) error {
	legacy, exact, err := f.optionalTimeWire(value)
	if err != nil {
		return err
	}
	_, err = f.conn.savepoint.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), Label: label, SaveTime: legacy, SaveTimeValue: exact})
	return wrapStatusErr(err)
}

func (f *Federate) SendInteractionByHandleValue(ctx context.Context, class uint64, parameters map[uint64][]byte, value *logicaltime.Time, options MessageOptions) error {
	if value == nil && len(options.Tag) == 0 && options.RetractionHandle == 0 {
		return f.SendInteractionByHandle(ctx, class, parameters, nil)
	}
	return f.sendInteractionByHandleConfirmed(ctx, class, parameters, value, options)
}
func (f *Federate) SendInteractionValue(ctx context.Context, class string, parameters map[string][]byte, value *logicaltime.Time, options MessageOptions) error {
	handle, values, err := f.resolveInteractionValues(class, parameters)
	if err != nil {
		return err
	}
	return f.SendInteractionByHandleValue(ctx, handle, values, value, options)
}
func (f *Federate) UpdateAttributeValuesByHandleValue(ctx context.Context, object uint64, attributes map[uint64][]byte, value *logicaltime.Time, options MessageOptions) error {
	if value == nil && len(options.Tag) == 0 && options.RetractionHandle == 0 {
		return f.UpdateAttributeValuesByHandle(ctx, object, attributes, nil)
	}
	return f.updateAttributeValuesByHandleConfirmed(ctx, object, attributes, value, options)
}
func (f *Federate) UpdateAttributeValuesValue(ctx context.Context, object uint64, attributes map[string][]byte, value *logicaltime.Time, options MessageOptions) error {
	values, err := f.resolveAttributeValues(object, attributes)
	if err != nil {
		return err
	}
	return f.UpdateAttributeValuesByHandleValue(ctx, object, values, value, options)
}
func (f *Federate) SendInteractionWithRegionsValue(ctx context.Context, class uint64, parameters map[uint64][]byte, regions []uint64, value *logicaltime.Time, options MessageOptions) error {
	legacy, exact, err := f.optionalTimeWire(value)
	if err != nil {
		return err
	}
	_, err = f.conn.ddm.SendInteractionWithRegions(ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), InteractionClassHandle: class, Parameters: parameters, RegionHandles: regions, LogicalTime: legacy, LogicalTimeValue: exact, UserSuppliedTag: options.Tag, MessageRetractionHandle: options.RetractionHandle})
	return wrapStatusErr(err)
}
func (f *Federate) DeleteObjectInstanceValue(ctx context.Context, object uint64, value *logicaltime.Time, options MessageOptions) error {
	return f.deleteObjectInstanceWithTime(ctx, object, value, options)
}
