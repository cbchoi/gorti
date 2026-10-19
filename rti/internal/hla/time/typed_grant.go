package time

import (
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

// TimeBound separates an unbounded horizon from the representation's finite
// final time. Limit retains the representation even when Unbounded is true.
//
// Preserve the established HLA API name.
//
//revive:disable-next-line:exported
type TimeBound struct {
	Limit     logicaltime.Time
	Unbounded bool
}

type TypedRegulatingFederate struct {
	Time      logicaltime.Time
	Lookahead logicaltime.Interval
}

type TypedGrantContext struct {
	Mode        AdvanceMode
	CurrentTime logicaltime.Time
	Requested   logicaltime.Time
	LBTS        TimeBound
	NextTSOTime logicaltime.Time
	HasTSO      bool
}

type TypedGrantDecision struct {
	Fire         bool
	Time         logicaltime.Time
	ClearPending bool
}

func unboundedTime(kind logicaltime.Representation) (TimeBound, error) {
	initial, err := kind.Initial()
	return TimeBound{Limit: initial, Unbounded: true}, err
}

// AddTypedTimeBound computes the send horizon without changing representation.
// A horizon beyond finite final time is unbounded, not a wrapped or rounded time.
func AddTypedTimeBound(at logicaltime.Time, interval logicaltime.Interval) (TimeBound, error) {
	value, err := at.Add(interval)
	if errors.Is(err, logicaltime.ErrIllegalArithmetic) {
		return unboundedTime(at.Representation())
	}
	return TimeBound{Limit: value}, err
}

func TypedLBTS(kind logicaltime.Representation, set []TypedRegulatingFederate) (TimeBound, error) {
	minimum, err := unboundedTime(kind)
	if err != nil {
		return TimeBound{}, err
	}
	for _, regulator := range set {
		if regulator.Time.Representation() != kind || regulator.Lookahead.Representation() != kind {
			return TimeBound{}, logicaltime.ErrRepresentationMismatch
		}
		bound, err := AddTypedTimeBound(regulator.Time, regulator.Lookahead)
		if err != nil {
			return TimeBound{}, err
		}
		if bound.Unbounded {
			continue
		}
		order, err := bound.Limit.Compare(minimum.Limit)
		if err != nil {
			return TimeBound{}, err
		}
		if minimum.Unbounded || order < 0 {
			minimum = bound
		}
	}
	return minimum, nil
}

// DecideTypedGrant is the representation-aware entry to the production grant
// algorithm. It does not imply that transport or stored manager state accepts
// Integer64 yet; those boundaries must migrate before enabling that federation.
func DecideTypedGrant(c TypedGrantContext) (TypedGrantDecision, error) {
	if c.Mode < ModeNER || c.Mode > ModeFQR {
		return TypedGrantDecision{}, fmt.Errorf("time: invalid advance mode %d", c.Mode)
	}
	kind := c.CurrentTime.Representation()
	if kind != logicaltime.HLAfloat64Time && kind != logicaltime.HLAinteger64Time {
		return TypedGrantDecision{}, logicaltime.ErrUnknownRepresentation
	}
	if c.Requested.Representation() != kind || c.LBTS.Limit.Representation() != kind || (c.HasTSO && c.NextTSOTime.Representation() != kind) {
		return TypedGrantDecision{}, logicaltime.ErrRepresentationMismatch
	}
	order, err := c.Requested.Compare(c.CurrentTime)
	if err != nil {
		return TypedGrantDecision{}, err
	}
	if order < 0 {
		return TypedGrantDecision{}, fmt.Errorf("time: request precedes current time")
	}
	if kind == logicaltime.HLAinteger64Time {
		return decideIntegerGrant(c)
	}
	return decideFloatGrant(c)
}

func decideIntegerGrant(c TypedGrantContext) (TypedGrantDecision, error) {
	current, _ := c.CurrentTime.Integer64()
	requested, _ := c.Requested.Integer64()
	bound, _ := c.LBTS.Limit.Integer64()
	var next int64
	if c.HasTSO {
		next, _ = c.NextTSOTime.Integer64()
	}
	d := decideScalarGrant(c.Mode, current, requested, bound, c.LBTS.Unbounded, next, c.HasTSO)
	if !d.fire {
		return TypedGrantDecision{}, nil
	}
	value, err := logicaltime.Integer64(d.time)
	return TypedGrantDecision{Fire: true, Time: value, ClearPending: d.clearPending}, err
}

func decideFloatGrant(c TypedGrantContext) (TypedGrantDecision, error) {
	current, _ := c.CurrentTime.Float64()
	requested, _ := c.Requested.Float64()
	bound, _ := c.LBTS.Limit.Float64()
	var next float64
	if c.HasTSO {
		next, _ = c.NextTSOTime.Float64()
	}
	d := decideScalarGrant(c.Mode, current, requested, bound, c.LBTS.Unbounded, next, c.HasTSO)
	if !d.fire {
		return TypedGrantDecision{}, nil
	}
	value, err := logicaltime.Float64(d.time)
	return TypedGrantDecision{Fire: true, Time: value, ClearPending: d.clearPending}, err
}

type logicalScalar interface{ ~float64 | ~int64 }

type scalarGrantDecision[T logicalScalar] struct {
	fire         bool
	time         T
	clearPending bool
}

// Both the legacy Float64 manager and typed decisions execute this algorithm.
// Integer64 instantiations perform only integer comparisons and assignments.
func decideScalarGrant[T logicalScalar](mode AdvanceMode, current, requested, bound T, unbounded bool, next T, hasTSO bool) scalarGrantDecision[T] {
	if mode == ModeFQR {
		target := requested
		if !unbounded {
			target = min(target, bound)
		}
		if hasTSO {
			target = min(target, next)
		}
		return scalarGrantDecision[T]{fire: true, time: max(current, target), clearPending: true}
	}
	target := requested
	if mode.usesNextMessageTarget() && hasTSO && next >= current && next < requested {
		target = next
	}
	fire := unbounded || bound > target
	if mode.inclusiveLBTS() {
		fire = unbounded || bound >= target
	}
	if fire {
		return scalarGrantDecision[T]{fire: true, time: target, clearPending: true}
	}
	return scalarGrantDecision[T]{}
}
