// TASK-206 (M21) — Go federate SDK time-management surface.
// See docs/M21_DISPATCH_PLAN.md §2.7.1.
//
// Mirrors the gRPC TimeService RPCs onto the Federate type. All
// methods translate gRPC status detail strings to typed federate
// errors via wrapStatusErr (errors.go).

package federate

import (
	"context"
	"math"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// EnableTimeRegulation enables time regulation with the supplied lookahead.
// Returns ErrTimeRegulationAlreadyEnabled if already regulating,
// ErrInvalidLookahead on bad input.
func (f *Federate) EnableTimeRegulation(ctx context.Context, lookahead float64) error {
	_, err := f.conn.tm.EnableTimeRegulation(ctx, &rtiv1.EnableRegulationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		Lookahead:      lookahead,
	})
	return wrapStatusErr(err)
}

// DisableTimeRegulation. Returns ErrTimeRegulationNotEnabled if not regulating.
func (f *Federate) DisableTimeRegulation(ctx context.Context) error {
	_, err := f.conn.tm.DisableTimeRegulation(ctx, &rtiv1.DisableRegulationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
	})
	return wrapStatusErr(err)
}

// EnableTimeConstrained.
func (f *Federate) EnableTimeConstrained(ctx context.Context) error {
	_, err := f.conn.tm.EnableTimeConstrained(ctx, &rtiv1.EnableConstrainedRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
	})
	return wrapStatusErr(err)
}

// DisableTimeConstrained.
func (f *Federate) DisableTimeConstrained(ctx context.Context) error {
	_, err := f.conn.tm.DisableTimeConstrained(ctx, &rtiv1.DisableConstrainedRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
	})
	return wrapStatusErr(err)
}

// ModifyLookahead updates the current lookahead without round-tripping
// through Disable + Enable.
func (f *Federate) ModifyLookahead(ctx context.Context, lookahead float64) error {
	_, err := f.conn.tm.ModifyLookahead(ctx, &rtiv1.ModifyLookaheadRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		Lookahead:      lookahead,
	})
	return wrapStatusErr(err)
}

// NextMessageRequest issues NER(t). Grant arrives later on Events().
func (f *Federate) NextMessageRequest(ctx context.Context, t float64) error {
	_, err := f.conn.tm.NextMessageRequest(ctx, &rtiv1.NERRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		LogicalTime:    t,
	})
	return wrapStatusErr(err)
}

// NextMessageRequestAvailable issues NMRA(t).
func (f *Federate) NextMessageRequestAvailable(ctx context.Context, t float64) error {
	_, err := f.conn.tm.NextMessageRequestAvailable(ctx, &rtiv1.NMRARequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		LogicalTime:    t,
	})
	return wrapStatusErr(err)
}

// TimeAdvanceRequest issues TAR(t).
func (f *Federate) TimeAdvanceRequest(ctx context.Context, t float64) error {
	_, err := f.conn.tm.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		LogicalTime:    t,
	})
	return wrapStatusErr(err)
}

// TimeAdvanceRequestAvailable issues TARA(t).
func (f *Federate) TimeAdvanceRequestAvailable(ctx context.Context, t float64) error {
	_, err := f.conn.tm.TimeAdvanceRequestAvailable(ctx, &rtiv1.TARARequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		LogicalTime:    t,
	})
	return wrapStatusErr(err)
}

// FlushQueueRequest issues FQR(t).
func (f *Federate) FlushQueueRequest(ctx context.Context, t float64) error {
	_, err := f.conn.tm.FlushQueueRequest(ctx, &rtiv1.FQRRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		LogicalTime:    t,
	})
	return wrapStatusErr(err)
}

// QueryLogicalTime returns the federate's current logical time.
func (f *Federate) QueryLogicalTime(ctx context.Context) (float64, error) {
	resp, err := f.conn.tm.QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return resp.GetLogicalTime(), nil
}

// QueryLookahead returns the federate's current lookahead.
// Returns (0, ErrTimeRegulationNotEnabled) if the federate is not
// currently regulating (server-side; see plan §2.3.1 / TASK-203.14).
func (f *Federate) QueryLookahead(ctx context.Context) (float64, error) {
	resp, err := f.conn.tm.QueryLookahead(ctx, &rtiv1.QueryFederateTimeRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return resp.GetLookahead(), nil
}

// QueryLBTS returns the federation-wide Lower Bound on Time Stamp.
// finite is false when no federate is regulating; lbts is 0 in that
// case (the manager returns +Inf internally; the wire wrapper
// translates per plan §2.2 / TASK-203.12).
func (f *Federate) QueryLBTS(ctx context.Context) (lbts float64, finite bool, err error) {
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
	return v, true, nil
}

// EnableAsynchronousDelivery — M22. See IEEE 1516.1-2010 §8.16.
// Default at federate join is OFF per spec; calling this opts into
// gorti's pre-M22 immediate-TSO-delivery behavior. Returns
// ErrTimeAlreadyAsynchronous if already on.
func (f *Federate) EnableAsynchronousDelivery(ctx context.Context) error {
	_, err := f.conn.tm.EnableAsynchronousDelivery(ctx, &rtiv1.EnableAsynchronousDeliveryRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
	})
	return wrapStatusErr(err)
}

// DisableAsynchronousDelivery — M22. See IEEE 1516.1-2010 §8.17.
// Returns ErrTimeNotAsynchronous if already off.
func (f *Federate) DisableAsynchronousDelivery(ctx context.Context) error {
	_, err := f.conn.tm.DisableAsynchronousDelivery(ctx, &rtiv1.DisableAsynchronousDeliveryRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
	})
	return wrapStatusErr(err)
}
