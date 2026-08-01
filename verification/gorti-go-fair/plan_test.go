package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type workloadPlanGoldenVector struct {
	RuntimeSeedDecimal     uint64 `json:"runtime_seed_decimal"`
	TopologyIdentitySHA256 string `json:"topology_identity_sha256"`
	HeaderHex              string `json:"header_hex"`
	RecordHex              string `json:"record_hex"`
	AttributePayloadHex    string `json:"attribute_payload_hex"`
	InteractionPayloadHex  string `json:"interaction_payload_hex"`
	PlanSizeBytes          int    `json:"plan_size_bytes"`
	PlanSHA256             string `json:"plan_sha256"`
}

func TestWorkloadPlanGoldenVector(t *testing.T) {
	golden := loadWorkloadPlanGoldenVector(t)
	encoded, err := hex.DecodeString(golden.HeaderHex + golden.RecordHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != golden.PlanSizeBytes {
		t.Fatalf("golden plan size = %d, want %d", len(encoded), golden.PlanSizeBytes)
	}
	plan, err := parseWorkloadPlan(encoded, 1, golden.RuntimeSeedDecimal)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Count != 1 || plan.Seed != golden.RuntimeSeedDecimal ||
		plan.topologyDigestHex() != golden.TopologyIdentitySHA256 {
		t.Fatalf("plan header = %+v", plan)
	}
	if plan.Digest != sha256.Sum256(encoded) || plan.digestHex() != golden.PlanSHA256 {
		t.Fatalf("plan digest = %s, want %s", plan.digestHex(), golden.PlanSHA256)
	}
	payloads, err := preencodePlanWorkload(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].index != 0 ||
		payloads[0].attribute != golden.AttributePayloadHex ||
		payloads[0].interaction != golden.InteractionPayloadHex {
		t.Fatalf("encoded plan payload = %+v", payloads)
	}
	seed := fmt.Sprint(golden.RuntimeSeedDecimal)
	warmup, err := preencodeWorkloadRange(seed, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if warmup[0].attribute != deterministicPayload(seed, "attribute", 1) ||
		warmup[0].interaction != deterministicPayload(seed, "interaction", 1) {
		t.Fatal("plan mode changed deterministic warmup encoding")
	}
}

func loadWorkloadPlanGoldenVector(t *testing.T) workloadPlanGoldenVector {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate plan test source")
	}
	path := filepath.Join(
		filepath.Dir(source),
		"..", "..", "benchmark", "devstone", "workload", "tests", "golden-vector-v1.json",
	)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read shared golden vector: %v", err)
	}
	var golden workloadPlanGoldenVector
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("decode shared golden vector: %v", err)
	}
	return golden
}

func TestVerifyWorkloadPlanDigest(t *testing.T) {
	plan, _ := compactTestPlan(t, 1)
	if err := verifyWorkloadPlanDigest(plan, plan.digestHex()); err != nil {
		t.Fatal(err)
	}
	if err := verifyWorkloadPlanDigest(plan, strings.Repeat("0", sha256.Size*2)); err == nil ||
		!strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("mismatched digest error = %v", err)
	}
	if err := verifyWorkloadPlanDigest(plan, ""); err != nil {
		t.Fatalf("legacy unpinned plan rejected: %v", err)
	}
}

func TestExecuteRejectsPinnedPlanMismatchBeforeRunSetup(t *testing.T) {
	directory := t.TempDir()
	planPath := filepath.Join(directory, "plan.dvshla")
	planBytes := encodePlanForTest(t, 1516, []workloadPlanRecord{
		{Index: 0, EventSequence: 1, TargetOrdinal: 1, OccurrenceOrdinal: 1},
	})
	if err := os.WriteFile(planPath, planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	fomPath := filepath.Join(directory, "fom.xml")
	if err := os.WriteFile(fomPath, []byte("<fom/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(directory, "output")
	err := execute([]string{
		"--role=publisher",
		"--federation=digest-mismatch",
		"--fom=" + fomPath,
		"--seed=1516",
		"--count=1",
		"--output=" + outputPath,
		"--receive-order=true",
		"--workload-plan=" + planPath,
		"--workload-plan-sha256=" + strings.Repeat("0", sha256.Size*2),
		"--compact-summary=true",
	})
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("mismatched pinned plan error = %v", err)
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("run setup occurred before plan pin validation: %v", statErr)
	}
}

func TestWorkloadPlanRejectsMalformedInputs(t *testing.T) {
	base := encodePlanForTest(t, 1516, []workloadPlanRecord{
		{Index: 0, EventSequence: 1, TargetOrdinal: 1, OccurrenceOrdinal: 1},
		{Index: 1, EventSequence: 1, TargetOrdinal: 2, OccurrenceOrdinal: 1},
	})
	tests := []struct {
		name          string
		data          []byte
		expectedCount int
		expectedSeed  uint64
		message       string
	}{
		{name: "truncated header", data: base[:workloadPlanHeaderSize-1], expectedCount: 2, expectedSeed: 1516, message: "truncated header"},
		{name: "wrong count", data: base, expectedCount: 3, expectedSeed: 1516, message: "count mismatch"},
		{name: "wrong seed", data: base, expectedCount: 2, expectedSeed: 1517, message: "seed mismatch"},
		{name: "truncated record", data: base[:len(base)-1], expectedCount: 2, expectedSeed: 1516, message: "truncated records"},
		{name: "trailing data", data: append(append([]byte(nil), base...), 0), expectedCount: 2, expectedSeed: 1516, message: "trailing data"},
	}
	badMagic := append([]byte(nil), base...)
	badMagic[0] = 'X'
	tests = append(tests, struct {
		name          string
		data          []byte
		expectedCount int
		expectedSeed  uint64
		message       string
	}{name: "magic", data: badMagic, expectedCount: 2, expectedSeed: 1516, message: "invalid magic"})
	badIndex := append([]byte(nil), base...)
	binary.BigEndian.PutUint32(badIndex[workloadPlanHeaderSize:workloadPlanHeaderSize+4], 7)
	tests = append(tests, struct {
		name          string
		data          []byte
		expectedCount int
		expectedSeed  uint64
		message       string
	}{name: "index order", data: badIndex, expectedCount: 2, expectedSeed: 1516, message: "record order error"})
	badTuple := append([]byte(nil), base...)
	second := workloadPlanHeaderSize + workloadPlanRecordSize
	binary.BigEndian.PutUint32(badTuple[second+8:second+12], 0)
	tests = append(tests, struct {
		name          string
		data          []byte
		expectedCount int
		expectedSeed  uint64
		message       string
	}{name: "tuple order", data: badTuple, expectedCount: 2, expectedSeed: 1516, message: "record order error"})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseWorkloadPlan(test.data, test.expectedCount, test.expectedSeed)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func encodePlanForTest(t *testing.T, seed uint64, records []workloadPlanRecord) []byte {
	t.Helper()
	encoded := make([]byte, workloadPlanHeaderSize+len(records)*workloadPlanRecordSize)
	copy(encoded[:8], workloadPlanMagic)
	binary.BigEndian.PutUint32(encoded[8:12], uint32(len(records)))
	binary.BigEndian.PutUint64(encoded[12:20], seed)
	for index := 0; index < sha256.Size; index++ {
		encoded[20+index] = byte(index)
	}
	for ordinal, record := range records {
		offset := workloadPlanHeaderSize + ordinal*workloadPlanRecordSize
		binary.BigEndian.PutUint32(encoded[offset:offset+4], record.Index)
		binary.BigEndian.PutUint32(encoded[offset+4:offset+8], record.EventSequence)
		binary.BigEndian.PutUint32(encoded[offset+8:offset+12], record.TargetOrdinal)
		binary.BigEndian.PutUint32(encoded[offset+12:offset+16], record.OccurrenceOrdinal)
		copy(encoded[offset+16:offset+24], record.AttributePayload[:])
		copy(encoded[offset+24:offset+32], record.InteractionPayload[:])
	}
	return encoded
}
