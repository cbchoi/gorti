package mom

import (
	"context"
	"fmt"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func cloneTimeValue(value *logicaltime.Time) *logicaltime.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
func cloneIntervalValue(value *logicaltime.Interval) *logicaltime.Interval {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
func validateTimeValues(value *logicaltime.Time, lookahead *logicaltime.Interval) error {
	if (value == nil) != (lookahead == nil) {
		return fmt.Errorf("incomplete exact time values")
	}
	if value == nil {
		return nil
	}
	if value.Representation() != lookahead.Representation() {
		return logicaltime.ErrRepresentationMismatch
	}
	if _, err := value.MarshalBinary(); err != nil {
		return err
	}
	if _, err := lookahead.MarshalBinary(); err != nil {
		return err
	}
	return nil
}

func validateSnapshotTimeValues(value *logicaltime.Time, lookahead *logicaltime.Interval, legacyTime, legacyLookahead core.LogicalTime) error {
	if err := validateTimeValues(value, lookahead); err != nil {
		return err
	}
	if value == nil {
		return nil
	}
	if value.Representation() == logicaltime.HLAinteger64Time {
		if legacyTime != 0 || legacyLookahead != 0 {
			return fmt.Errorf("mixed integer and legacy time values")
		}
		return nil
	}
	current, _ := value.Float64()
	interval, _ := lookahead.Float64()
	if float64(legacyTime) != current || float64(legacyLookahead) != interval {
		return fmt.Errorf("conflicting Float64 exact and legacy time values")
	}
	return nil
}

// TimeStateChangedValue observes exact time values without narrowing Integer64.
func (m *Manager) TimeStateChangedValue(ctx context.Context, fed core.FederationName, h core.FederateHandle, regulating, constrained bool, lookahead logicaltime.Interval, value logicaltime.Time) error {
	if err := validateTimeValues(&value, &lookahead); err != nil {
		return err
	}
	if value.Representation() == logicaltime.HLAfloat64Time {
		current, _ := value.Float64()
		interval, _ := lookahead.Float64()
		return m.TimeStateChanged(ctx, fed, h, regulating, constrained, core.LogicalTime(interval), core.LogicalTime(current))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.fed[fed]
	if state == nil {
		return nil
	}
	entry := state.federates[h]
	if entry == nil {
		return nil
	}
	entry.timeRegulating = regulating
	entry.timeConstrained = constrained
	entry.logicalTime = 0
	entry.lookahead = 0
	entry.logicalTimeValue = cloneTimeValue(&value)
	entry.lookaheadValue = cloneIntervalValue(&lookahead)
	return nil
}
