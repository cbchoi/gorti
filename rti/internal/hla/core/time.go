package core

import "math"

// LogicalTime is the existing Float64 service representation. IEEE 1516-2010
// also requires Integer64; generic service domains instantiate int64 directly.
// The legacy name remains for existing Float64 callers and adapters.
type LogicalTime float64

// TimeBoundOf separates an unbounded horizon from the representation's final
// finite time. Limit is meaningful only when Unbounded is false.
type TimeBoundOf[T LogicalTimeScalar] struct {
	Limit     T
	Unbounded bool
}

// PositiveInfinity represents an unbounded LBTS (no regulating federates).
var PositiveInfinity = LogicalTime(math.Inf(1))

// IsValid returns false for NaN. Implementations must reject NaN at the wire boundary.
func (t LogicalTime) IsValid() bool { return !math.IsNaN(float64(t)) }

// Mode is the federation operating mode, fixed at create time.
type Mode uint8

const (
	ModeUnspecified Mode = iota
	ModeVerbose
	ModeBestEffort
)

// ResignAction — IEEE 1516.1-2010 §4.10. M24 expanded from 1 accepted
// value to 6.
type ResignAction uint8

const (
	ResignActionUnspecified ResignAction = iota
	ResignActionUnconditionallyDivestAttributes
	ResignActionDeleteThenDivest       // M24
	ResignActionCancelThenDelete       // M24
	ResignActionCancelPendingOwnership // M24
	ResignActionNoAction               // M24
	ResignActionDeleteObjects          // M24
)
