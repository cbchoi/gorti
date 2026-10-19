package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type retractionSnapshotWireOf[N core.LogicalTimeScalar] struct {
	LogicalTimeImplementationName string                      `json:"logical_time_implementation_name,omitempty"`
	Version                       uint32                      `json:"version"`
	History                       *retractionHistoryWireOf[N] `json:"history"`
}

type retractionHistoryWireOf[N core.LogicalTimeScalar] struct {
	Complete  bool                          `json:"complete"`
	Producers []retractionProducerWireOf[N] `json:"producers"`
}

type retractionProducerWireOf[N core.LogicalTimeScalar] struct {
	Producer core.FederateHandle         `json:"producer"`
	Accepted []timepkg.RetractionIDRange `json:"accepted"`
	Messages retractionMessagesWireOf[N] `json:"messages,omitempty"`
}

type retractionMessagesWireOf[N core.LogicalTimeScalar] []retractionMessageWireOf[N]

func (m *retractionMessagesWireOf[N]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*m = retractionMessagesWireOf[N]{}
		return nil
	}
	type plain retractionMessagesWireOf[N]
	return decodeSnapshotJSON(data, (*plain)(m))
}

type retractionMessageWireOf[N core.LogicalTimeScalar] struct {
	ID        uint64                `json:"id"`
	Timestamp *N                    `json:"timestamp"`
	Delivered []core.FederateHandle `json:"delivered,omitempty"`
	Retracted bool                  `json:"retracted,omitempty"`
}

func (s retractionSnapshotOf[T, N]) MarshalJSON() ([]byte, error) {
	if integerScalar[T]() != integerScalar[N]() {
		return nil, fmt.Errorf("retraction snapshot: scalar representation mismatch")
	}
	wire := retractionSnapshotWireOf[N]{Version: s.Version}
	if integerScalar[T]() {
		wire.LogicalTimeImplementationName = checkpointHLAinteger64Time
	}
	if s.History != nil {
		wire.History = &retractionHistoryWireOf[N]{Complete: s.History.Complete}
		for _, p := range s.History.Producers {
			producer := retractionProducerWireOf[N]{Producer: p.Producer, Accepted: p.Accepted}
			for _, m := range p.Messages {
				timestamp := N(m.Timestamp)
				producer.Messages = append(producer.Messages, retractionMessageWireOf[N]{ID: m.ID, Timestamp: &timestamp, Delivered: m.Delivered, Retracted: m.Retracted})
			}
			wire.History.Producers = append(wire.History.Producers, producer)
		}
	}
	return json.Marshal(wire)
}

func (s *retractionSnapshotOf[T, N]) UnmarshalJSON(data []byte) error {
	if integerScalar[T]() != integerScalar[N]() {
		return fmt.Errorf("retraction snapshot: scalar representation mismatch")
	}
	var wire retractionSnapshotWireOf[N]
	if err := decodeSnapshotJSON(data, &wire); err != nil {
		return err
	}
	if err := validateRetractionWireVersion(wire); err != nil {
		return err
	}
	decoded := retractionSnapshotOf[T, N]{Version: wire.Version}
	hasMessages := false
	if wire.History != nil {
		decoded.History = &timepkg.RetractionHistoryOf[T]{Complete: wire.History.Complete}
		for _, p := range wire.History.Producers {
			if wire.Version < 2 && p.Messages != nil {
				return fmt.Errorf("retraction snapshot: message ledger requires version 2")
			}
			producer := timepkg.RetractionProducerHistoryOf[T]{Producer: p.Producer, Accepted: p.Accepted}
			for _, m := range p.Messages {
				if m.Timestamp == nil {
					return fmt.Errorf("retraction snapshot: missing message timestamp")
				}
				producer.Messages = append(producer.Messages, timepkg.RetractionMessageStateOf[T]{ID: m.ID, Timestamp: T(*m.Timestamp), Delivered: m.Delivered, Retracted: m.Retracted})
				hasMessages = true
			}
			decoded.History.Producers = append(decoded.History.Producers, producer)
		}
	}
	if wire.Version == 2 && !hasMessages {
		return fmt.Errorf("retraction snapshot: version 2 has no message ledger")
	}
	*s = decoded
	return nil
}

func marshalRetractionSnapshotOf[T core.LogicalTimeScalar, N core.LogicalTimeScalar](history *timepkg.RetractionHistoryOf[T]) ([]byte, error) {
	if err := timepkg.ValidateRetractionHistoryOf[T](history); err != nil {
		return nil, err
	}
	version := uint32(1)
	for _, producer := range history.Producers {
		if len(producer.Messages) > 0 {
			version = 2
			break
		}
	}
	if integerScalar[T]() {
		version = 3
	}
	return json.Marshal(retractionSnapshotOf[T, N]{Version: version, History: history})
}

func readRetractionsOf[T core.LogicalTimeScalar](data []byte) (*timepkg.RetractionHistoryOf[T], error) {
	if integerScalar[T]() {
		return decodeRetractionSnapshotOf[T, integerSnapshotNumber](data)
	}
	return decodeRetractionSnapshotOf[T, timeSnapshotNumber](data)
}

func writeRetractionsOf[T core.LogicalTimeScalar](history *timepkg.RetractionHistoryOf[T]) ([]byte, error) {
	if integerScalar[T]() {
		return marshalRetractionSnapshotOf[T, integerSnapshotNumber](history)
	}
	return marshalRetractionSnapshotOf[T, timeSnapshotNumber](history)
}

func validateRetractionWireVersion[N core.LogicalTimeScalar](wire retractionSnapshotWireOf[N]) error {
	if integerScalar[N]() {
		if wire.Version != 3 || wire.LogicalTimeImplementationName != checkpointHLAinteger64Time {
			return fmt.Errorf("retraction snapshot: integer time requires version 3 and HLAinteger64Time")
		}
	} else if wire.Version < 1 || wire.Version > 2 || wire.LogicalTimeImplementationName != "" && wire.LogicalTimeImplementationName != "HLAfloat64Time" {
		return fmt.Errorf("retraction snapshot: unsupported version %d", wire.Version)
	}
	return nil
}
