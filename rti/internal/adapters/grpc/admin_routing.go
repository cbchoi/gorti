package grpc

import (
	"math"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func buildIntegerFederationSnapshot(opts AdminOptions, roster core.FederationRoster, outbox map[adminFedHandleKey]OutboxStat) (*rtiv1.FederationSnapshot, error) {
	shared := opts
	shared.Time, shared.Objects, shared.Savepoint = nil, nil, nil
	out := buildFederationSnapshot(shared, roster, outbox)
	backend := opts.Integer64
	if backend.Objects != nil {
		out.ObjectInstanceCount = backend.Objects.Snapshot(roster.Name).InstanceCount
	}
	if backend.Savepoint != nil {
		saved := backend.Savepoint.Snapshot(roster.Name)
		out.SaveState, out.RestoreState = adminSaveStateToProto(saved.SaveState), adminRestoreStateToProto(saved.RestoreState)
	}
	snapshot := backend.Time.Snapshot(roster.Name)
	out.Time = &rtiv1.TimeSnapshot{}
	if snapshot.LBTSUnbounded {
		// The existing admin schema has no bound flag. Absent exact LBTS with
		// the legacy +Inf sentinel represents unbounded, never finite MaxInt64.
		out.Time.Lbts = math.Inf(1)
	} else {
		legacy, exact, err := encodeScalarTime(snapshot.LBTS)
		if err != nil {
			return nil, err
		}
		out.Time.Lbts, out.Time.LbtsValue = legacy, exact
	}
	byHandle := make(map[core.FederateHandle]core.TimeFederateStateOf[int64], len(snapshot.Federates))
	for _, state := range snapshot.Federates {
		byHandle[state.Handle] = state
		if state.Regulating {
			out.Time.RegulatingHandles = append(out.Time.RegulatingHandles, uint64(state.Handle))
		}
		if state.Constrained {
			out.Time.ConstrainedHandles = append(out.Time.ConstrainedHandles, uint64(state.Handle))
		}
		if state.HasPendingRequest {
			legacy, exact, err := encodeScalarTime(state.PendingRequestTime)
			if err != nil {
				return nil, err
			}
			out.Time.PendingGrants = append(out.Time.PendingGrants, &rtiv1.PendingGrant{FederateHandle: uint64(state.Handle), RequestedTime: legacy, RequestedTimeValue: exact})
		}
	}
	if err := populateIntegerFederateTimes(out, byHandle); err != nil {
		return nil, err
	}
	return out, nil
}
func populateIntegerFederateTimes(out *rtiv1.FederationSnapshot, byHandle map[core.FederateHandle]core.TimeFederateStateOf[int64]) error {
	for _, row := range out.Federates {
		state, found := byHandle[core.FederateHandle(row.Handle)]
		if !found {
			continue
		}
		current, exactCurrent, err := encodeScalarTime(state.CurrentTime)
		if err != nil {
			return err
		}
		lookahead, exactLookahead, err := encodeScalarInterval(state.Lookahead)
		if err != nil {
			return err
		}
		row.CurrentTime, row.CurrentTimeValue = current, exactCurrent
		row.Lookahead, row.LookaheadValue = lookahead, exactLookahead
		row.Regulating, row.Constrained = state.Regulating, state.Constrained
		if state.HasPendingRequest {
			_, exact, err := encodeScalarTime(state.PendingRequestTime)
			if err != nil {
				return err
			}
			row.PendingRequestTimeValue = exact
		}
	}
	return nil
}
