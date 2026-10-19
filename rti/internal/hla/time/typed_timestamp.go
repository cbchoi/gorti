package time

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

// ValidateTypedOutgoingTSO checks a regulating sender's already selected time
// base and effective lookahead. Mode/history selection belongs to the manager.
func ValidateTypedOutgoingTSO(base logicaltime.Time, lookahead logicaltime.Interval, timestamp logicaltime.Time, exclusive bool) error {
	bound, err := AddTypedTimeBound(base, lookahead)
	if err != nil {
		return err
	}
	if timestamp.Representation() != bound.Limit.Representation() {
		return logicaltime.ErrRepresentationMismatch
	}
	order, err := timestamp.Compare(bound.Limit)
	if err != nil {
		return err
	}
	if !bound.Unbounded && (order > 0 || (!exclusive && order == 0)) {
		return nil
	}
	return fmt.Errorf("%w: timestamp %s violates base %s plus lookahead %s (exclusive=%t)", core.ErrTimeInvalidLogicalTime, timestamp, base, lookahead, exclusive)
}

func timestampAtOrBeyond[T logicalScalar](timestamp, lower T, exclusive bool) bool {
	return timestamp > lower || (!exclusive && timestamp == lower)
}
