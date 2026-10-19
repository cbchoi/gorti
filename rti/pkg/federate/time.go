// TASK-206 (M21) — Go federate SDK time-management surface.
// See docs/M21_DISPATCH_PLAN.md §2.7.1.
//
// Mirrors the gRPC TimeService RPCs onto the Federate type. All
// methods translate gRPC status detail strings to typed federate
// errors via wrapStatusErr (errors.go).

package federate

import (
	"context"
	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"math"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

// EnableTimeRegulation requests time regulation with the supplied lookahead.
// Request acceptance does not mean activation; wait for TimeRegulationEnabled
// on Events before relying on regulation.
// Returns ErrTimeRegulationAlreadyEnabled if already regulating,
// ErrInvalidLookahead on bad input.
func (f *Federate) EnableTimeRegulation(ctx context.Context, lookahead float64) error {
	if err := f.requireFloatTime(); err != nil {
		return err
	}
	v, err := logicaltime.Float64Interval(lookahead)
	if err != nil {
		return ErrInvalidLookahead
	}
	return f.EnableTimeRegulationValue(ctx, v)
}

// DisableTimeRegulation. Returns ErrTimeRegulationNotEnabled if not regulating.
func (f *Federate) DisableTimeRegulation(ctx context.Context) error {
	_, err := f.conn.tm.DisableTimeRegulation(ctx, &rtiv1.DisableRegulationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(),
	})
	return wrapStatusErr(err)
}

// EnableTimeConstrained requests constraint. TimeConstrainedEnabled on Events
// reports actual activation at the RTI-selected logical time.
func (f *Federate) EnableTimeConstrained(ctx context.Context) error {
	_, err := f.conn.tm.EnableTimeConstrained(ctx, &rtiv1.EnableConstrainedRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(),
	})
	return wrapStatusErr(err)
}

// DisableTimeConstrained.
func (f *Federate) DisableTimeConstrained(ctx context.Context) error {
	f.mu.Lock()
	epoch := f.objectKnowledgeEpoch
	f.mu.Unlock()
	_, err := f.conn.tm.DisableTimeConstrained(ctx, &rtiv1.DisableConstrainedRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(),
	})
	if err == nil {
		f.completeSDKConstraintDisable(epoch)
	}
	return wrapStatusErr(err)
}

// ModifyLookahead updates the current lookahead without round-tripping
// through Disable + Enable.
func (f *Federate) ModifyLookahead(ctx context.Context, lookahead float64) error {
	if err := f.requireFloatTime(); err != nil {
		return err
	}
	v, err := logicaltime.Float64Interval(lookahead)
	if err != nil {
		return ErrInvalidLookahead
	}
	return f.ModifyLookaheadValue(ctx, v)
}

// NextMessageRequest issues NER(t). Grant arrives later on Events().
func (f *Federate) NextMessageRequest(ctx context.Context, t float64) error {
	if err := f.requireFloatTime(); err != nil {
		return err
	}
	if math.IsInf(t, 1) {
		return f.advanceWire(ctx, "NextMessageRequest", t, nil)
	}
	v, err := logicaltime.Float64(t)
	if err != nil {
		return err
	}
	return f.NextMessageRequestValue(ctx, v)
}

// NextMessageRequestAvailable issues NMRA(t).
func (f *Federate) NextMessageRequestAvailable(ctx context.Context, t float64) error {
	if err := f.requireFloatTime(); err != nil {
		return err
	}
	if math.IsInf(t, 1) {
		return f.advanceWire(ctx, "NextMessageRequestAvailable", t, nil)
	}
	v, err := logicaltime.Float64(t)
	if err != nil {
		return err
	}
	return f.NextMessageRequestAvailableValue(ctx, v)
}

// TimeAdvanceRequest issues TAR(t).
func (f *Federate) TimeAdvanceRequest(ctx context.Context, t float64) error {
	if err := f.requireFloatTime(); err != nil {
		return err
	}
	if math.IsInf(t, 1) {
		return f.advanceWire(ctx, "TimeAdvanceRequest", t, nil)
	}
	v, err := logicaltime.Float64(t)
	if err != nil {
		return err
	}
	return f.TimeAdvanceRequestValue(ctx, v)
}

// TimeAdvanceRequestAvailable issues TARA(t).
func (f *Federate) TimeAdvanceRequestAvailable(ctx context.Context, t float64) error {
	if err := f.requireFloatTime(); err != nil {
		return err
	}
	if math.IsInf(t, 1) {
		return f.advanceWire(ctx, "TimeAdvanceRequestAvailable", t, nil)
	}
	v, err := logicaltime.Float64(t)
	if err != nil {
		return err
	}
	return f.TimeAdvanceRequestAvailableValue(ctx, v)
}

// FlushQueueRequest issues FQR(t).
func (f *Federate) FlushQueueRequest(ctx context.Context, t float64) error {
	if err := f.requireFloatTime(); err != nil {
		return err
	}
	if math.IsInf(t, 1) {
		return f.advanceWire(ctx, "FlushQueueRequest", t, nil)
	}
	v, err := logicaltime.Float64(t)
	if err != nil {
		return err
	}
	return f.FlushQueueRequestValue(ctx, v)
}

// QueryLogicalTime returns the federate's current logical time.
func (f *Federate) QueryLogicalTime(ctx context.Context) (float64, error) {
	if err := f.requireFloatTime(); err != nil {
		return 0, err
	}
	f.mu.Lock()
	epoch := f.objectKnowledgeEpoch
	f.mu.Unlock()
	r, err := f.conn.tm.QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle()})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	if f.legacyFinalTime(r.LogicalTime, r.LogicalTimeValue) {
		f.mu.Lock()
		if epoch == f.objectKnowledgeEpoch && len(f.timeAdvances) == 0 {
			f.retireSDKFinalRemovalsLocked()
		}
		f.mu.Unlock()
		return r.LogicalTime, nil
	}
	v, err := codec.DecodeLogicalTimePair(f.timeRepresentation(), r.LogicalTime, r.LogicalTimeValue)
	if err != nil {
		return 0, err
	}
	f.mu.Lock()
	if epoch == f.objectKnowledgeEpoch && len(f.timeAdvances) == 0 {
		f.retireSDKRemovalsLocked(v)
	}
	f.mu.Unlock()
	return v.Float64()
}

// QueryLookahead returns the federate's current lookahead.
// Returns (0, ErrTimeRegulationNotEnabled) if the federate is not
// currently regulating (server-side; see plan §2.3.1 / TASK-203.14).
func (f *Federate) QueryLookahead(ctx context.Context) (float64, error) {
	if err := f.requireFloatTime(); err != nil {
		return 0, err
	}
	v, err := f.QueryLookaheadValue(ctx)
	if err != nil {
		return 0, err
	}
	return v.Float64()
}

// QueryLBTS returns the federation-wide Lower Bound on Time Stamp.
// finite is false when no federate is regulating; lbts is 0 in that
// case (the manager returns +Inf internally; the wire wrapper
// translates per plan §2.2 / TASK-203.12).
func (f *Federate) QueryLBTS(ctx context.Context) (lbts float64, finite bool, err error) {
	if err := f.requireFloatTime(); err != nil {
		return 0, false, err
	}
	resp, err := f.conn.tm.QueryLBTS(ctx, &rtiv1.QueryLBTSRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
	})
	if err != nil {
		return 0, false, wrapStatusErr(err)
	}
	v := resp.GetLbts()
	// Defensive: a misbehaving server could set Lbts but Finite=false.
	// Treat finite=false as "no regulators"; ignore Lbts.
	if !resp.GetFinite() {
		return 0, false, nil
	}
	// Sanity-guard against +Inf making it through (shouldn't happen
	// because the server wrapper translates; if it does, surface it
	// as not-finite rather than letting NaN/Inf propagate to callers).
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false, nil
	}
	exact, decodeErr := codec.DecodeLogicalTimePair(f.timeRepresentation(), resp.Lbts, resp.LbtsValue)
	if decodeErr != nil {
		return 0, false, decodeErr
	}
	v, decodeErr = exact.Float64()
	return v, true, decodeErr
}

// EnableAsynchronousDelivery — M22. See IEEE 1516.1-2010 §8.16.
// Default at federate join is OFF per spec; calling this opts into
// gorti's pre-M22 immediate-TSO-delivery behavior. Returns
// ErrTimeAlreadyAsynchronous if already on.
func (f *Federate) EnableAsynchronousDelivery(ctx context.Context) error {
	_, err := f.conn.tm.EnableAsynchronousDelivery(ctx, &rtiv1.EnableAsynchronousDeliveryRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(),
	})
	return wrapStatusErr(err)
}

// DisableAsynchronousDelivery — M22. See IEEE 1516.1-2010 §8.17.
// Returns ErrTimeNotAsynchronous if already off.
func (f *Federate) DisableAsynchronousDelivery(ctx context.Context) error {
	_, err := f.conn.tm.DisableAsynchronousDelivery(ctx, &rtiv1.DisableAsynchronousDeliveryRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.CurrentHandle(),
	})
	return wrapStatusErr(err)
}
