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

// ResignAction. Cut 1 supports only UnconditionallyDivestAttributes.
type ResignAction uint8

const (
	ResignActionUnspecified ResignAction = iota
	ResignActionUnconditionallyDivestAttributes
	// Other actions deferred to cut 2; see proto/rti/v1/common.proto.
)
