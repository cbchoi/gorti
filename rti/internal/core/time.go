package core

import "math"

// LogicalTime is HLA logical time. IEEE 1516.2-2010 leaves the representation
// implementation-defined; this RTI uses IEEE 754 double.
type LogicalTime float64

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
	ResignActionDeleteThenDivest          // M24
	ResignActionCancelThenDelete          // M24
	ResignActionCancelPendingOwnership    // M24
	ResignActionNoAction                  // M24
	ResignActionDeleteObjects             // M24
)
