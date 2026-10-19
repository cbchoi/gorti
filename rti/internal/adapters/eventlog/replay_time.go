package eventlog

import (
	"fmt"
	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"math"
)

type Replayer = ReplayerOf[core.LogicalTime]
type ReplayerOptions = ReplayerOptionsOf[core.LogicalTime]

func NewReplayer(opts ReplayerOptions) (*Replayer, error) { return NewReplayerOf(opts) }

func decodeReplayTimestamp[T core.LogicalTimeScalar](legacy *float64, exact *rtiv1.LogicalTimeValue) (*T, error) {
	if core.ScalarRepresentation[T]() == logicaltime.HLAfloat64Time && legacy != nil && math.IsInf(*legacy, 1) && exact == nil {
		result := T(*legacy)
		return &result, nil
	}
	value, err := codec.DecodeOptionalLogicalTimePair(core.ScalarRepresentation[T](), legacy, exact)
	if err != nil {
		return nil, fmt.Errorf("eventlog: replay time: %w", err)
	}
	if value == nil {
		return nil, nil
	}
	result, err := core.ScalarFromTimeValue[T](*value)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
