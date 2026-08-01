package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
)

const (
	workloadPlanMagic      = "DVSHLA1\x00"
	workloadPlanHeaderSize = 8 + 4 + 8 + sha256.Size
	workloadPlanRecordSize = 4*4 + 8 + 8
)

type workloadPlanRecord struct {
	Index              uint32
	EventSequence      uint32
	TargetOrdinal      uint32
	OccurrenceOrdinal  uint32
	AttributePayload   [8]byte
	InteractionPayload [8]byte
}

type workloadPlan struct {
	Count          uint32
	Seed           uint64
	TopologyDigest [sha256.Size]byte
	Digest         [sha256.Size]byte
	Records        []workloadPlanRecord
}

func loadWorkloadPlan(path string, expectedCount int, seed string) (*workloadPlan, error) {
	expectedSeed, err := strconv.ParseUint(seed, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("--seed must be an unsigned decimal integer in workload-plan mode: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workload plan: %w", err)
	}
	plan, err := parseWorkloadPlan(data, expectedCount, expectedSeed)
	if err != nil {
		return nil, fmt.Errorf("parse workload plan: %w", err)
	}
	return plan, nil
}

func parseWorkloadPlan(data []byte, expectedCount int, expectedSeed uint64) (*workloadPlan, error) {
	if expectedCount < 1 {
		return nil, errors.New("expected count must be at least 1")
	}
	if len(data) < workloadPlanHeaderSize {
		return nil, fmt.Errorf("truncated header: got %d bytes, want at least %d", len(data), workloadPlanHeaderSize)
	}
	if !bytes.Equal(data[:8], []byte(workloadPlanMagic)) {
		return nil, errors.New("invalid magic")
	}

	count := binary.BigEndian.Uint32(data[8:12])
	seed := binary.BigEndian.Uint64(data[12:20])
	if uint64(expectedCount) > math.MaxUint32 || count != uint32(expectedCount) {
		return nil, fmt.Errorf("count mismatch: plan=%d command=%d", count, expectedCount)
	}
	if seed != expectedSeed {
		return nil, fmt.Errorf("seed mismatch: plan=%d command=%d", seed, expectedSeed)
	}
	expectedSize := uint64(workloadPlanHeaderSize) + uint64(count)*uint64(workloadPlanRecordSize)
	if uint64(len(data)) < expectedSize {
		return nil, fmt.Errorf("truncated records: got %d bytes, want %d", len(data), expectedSize)
	}
	if uint64(len(data)) > expectedSize {
		return nil, fmt.Errorf("trailing data: got %d bytes after the plan", uint64(len(data))-expectedSize)
	}

	plan := &workloadPlan{Count: count, Seed: seed, Digest: sha256.Sum256(data)}
	copy(plan.TopologyDigest[:], data[20:workloadPlanHeaderSize])
	plan.Records = make([]workloadPlanRecord, count)
	var previous workloadPlanRecord
	for ordinal := uint32(0); ordinal < count; ordinal++ {
		offset := workloadPlanHeaderSize + int(ordinal)*workloadPlanRecordSize
		encoded := data[offset : offset+workloadPlanRecordSize]
		record := workloadPlanRecord{
			Index:             binary.BigEndian.Uint32(encoded[0:4]),
			EventSequence:     binary.BigEndian.Uint32(encoded[4:8]),
			TargetOrdinal:     binary.BigEndian.Uint32(encoded[8:12]),
			OccurrenceOrdinal: binary.BigEndian.Uint32(encoded[12:16]),
		}
		copy(record.AttributePayload[:], encoded[16:24])
		copy(record.InteractionPayload[:], encoded[24:32])
		if record.Index != ordinal {
			return nil, fmt.Errorf("record order error at ordinal %d: index=%d", ordinal, record.Index)
		}
		if ordinal > 0 && !planRecordFollows(previous, record) {
			return nil, fmt.Errorf(
				"record order error at index %d: event/target/occurrence tuple is not increasing",
				record.Index,
			)
		}
		plan.Records[ordinal] = record
		previous = record
	}
	return plan, nil
}

func planRecordFollows(previous, current workloadPlanRecord) bool {
	if current.EventSequence != previous.EventSequence {
		return current.EventSequence > previous.EventSequence
	}
	if current.TargetOrdinal != previous.TargetOrdinal {
		return current.TargetOrdinal > previous.TargetOrdinal
	}
	return current.OccurrenceOrdinal > previous.OccurrenceOrdinal
}

func preencodePlanWorkload(plan *workloadPlan) ([]encodedIteration, error) {
	if plan == nil {
		return nil, errors.New("workload plan is nil")
	}
	result := make([]encodedIteration, len(plan.Records))
	for ordinal, record := range plan.Records {
		index := int(record.Index)
		sequence, err := encodeInteger32BE(index)
		if err != nil {
			return nil, fmt.Errorf("encode workload plan record %d: %w", ordinal, err)
		}
		attribute := hex.EncodeToString(record.AttributePayload[:])
		interaction := hex.EncodeToString(record.InteractionPayload[:])
		attributeBytes, err := encodeASCIIString(attribute)
		if err != nil {
			return nil, err
		}
		interactionBytes, err := encodeASCIIString(interaction)
		if err != nil {
			return nil, err
		}
		result[ordinal] = encodedIteration{
			index: index, time: float64(index + 1),
			attribute: attribute, interaction: interaction,
			attributes: map[string][]byte{sequenceField: sequence, payloadField: attributeBytes},
			parameters: map[string][]byte{sequenceField: sequence, payloadField: interactionBytes},
		}
	}
	return result, nil
}

func verifyWorkloadPlanDigest(plan *workloadPlan, expected string) error {
	if expected == "" {
		return nil
	}
	if plan == nil {
		return errors.New("workload plan is nil")
	}
	actual := plan.digestHex()
	if actual != expected {
		return fmt.Errorf("workload plan SHA-256 mismatch: actual=%s expected=%s", actual, expected)
	}
	return nil
}

func (plan *workloadPlan) digestHex() string {
	return hex.EncodeToString(plan.Digest[:])
}

func (plan *workloadPlan) topologyDigestHex() string {
	return hex.EncodeToString(plan.TopologyDigest[:])
}
