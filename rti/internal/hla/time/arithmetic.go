package time

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// addTimeBoundOf is the scalar compatibility view. Authoritative decisions use
// addHorizon so integer overflow remains distinct from finite final time.
func addTimeBoundOf[T core.LogicalTimeScalar](at, interval T) T {
	return horizonScalar(addHorizon(at, interval))
}

func addHorizon[T core.LogicalTimeScalar](at, interval T) core.TimeBoundOf[T] {
	value, valueErr := core.ScalarTimeValue(at)
	delta, deltaErr := core.ScalarIntervalValue(interval)
	if valueErr != nil || deltaErr != nil {
		// Only Float64 legacy internals retain non-finite values.
		if integerScalar[T]() {
			return unboundedHorizon[T]()
		}
		return core.TimeBoundOf[T]{Limit: at + interval}
	}
	bound, err := AddTypedTimeBound(value, delta)
	if err != nil {
		return unboundedHorizon[T]()
	}
	if bound.Unbounded {
		return unboundedHorizon[T]()
	}
	result, _ := core.ScalarFromTimeValue[T](bound.Limit)
	return core.TimeBoundOf[T]{Limit: result}
}
