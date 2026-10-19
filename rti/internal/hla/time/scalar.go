package time

import (
	"math"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func integerScalar[T core.LogicalTimeScalar]() bool { return T(1)/T(2) == 0 }

func scalarNaN[T core.LogicalTimeScalar](value T) bool {
	return !integerScalar[T]() && math.IsNaN(float64(value))
}

func scalarInfinite[T core.LogicalTimeScalar](value T) bool {
	return !integerScalar[T]() && math.IsInf(float64(value), 0)
}

func unboundedHorizon[T core.LogicalTimeScalar]() core.TimeBoundOf[T] {
	return core.TimeBoundOf[T]{Unbounded: true}
}

func horizonScalar[T core.LogicalTimeScalar](bound core.TimeBoundOf[T]) T {
	if !bound.Unbounded {
		return bound.Limit
	}
	if integerScalar[T]() {
		return T(int64(math.MaxInt64))
	}
	return T(math.Inf(1))
}

func minimumHorizon[T core.LogicalTimeScalar](a, b core.TimeBoundOf[T]) core.TimeBoundOf[T] {
	if a.Unbounded {
		return b
	}
	if b.Unbounded {
		return a
	}
	if b.Limit < a.Limit {
		return b
	}
	return a
}

func horizonBeyond[T core.LogicalTimeScalar](bound core.TimeBoundOf[T], value T, strict bool) bool {
	if bound.Unbounded && integerScalar[T]() {
		return true
	}
	limit := horizonScalar(bound)
	if strict {
		return limit > value
	}
	return limit >= value
}

func successor[T core.LogicalTimeScalar](at T) (T, bool) {
	if integerScalar[T]() {
		if int64(at) == math.MaxInt64 {
			return at, false
		}
		return at + 1, true
	}
	return T(math.Nextafter(float64(at), math.Inf(1))), true
}

func predecessor[T core.LogicalTimeScalar](at T) T {
	if integerScalar[T]() {
		return at - 1
	}
	return T(math.Nextafter(float64(at), math.Inf(-1)))
}
