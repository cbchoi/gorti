package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
)

const benchmarkSchema = "gorti.production-benchmark/v1"

type runMetadata struct {
	RunID       string         `json:"run_id"`
	Benchmark   string         `json:"benchmark"`
	StartedAt   string         `json:"started_at"`
	Environment map[string]any `json:"environment"`
	Workload    map[string]any `json:"workload"`
	Provenance  provenance     `json:"provenance"`
}

type provenance struct {
	Commit          string            `json:"commit"`
	BinarySHA256    string            `json:"binary_sha256"`
	RuntimeVersions map[string]string `json:"runtime_versions"`
	BuildFlags      []string          `json:"build_flags"`
}

type operationSample struct {
	Sequence   int            `json:"sequence"`
	Operation  string         `json:"operation"`
	DurationNS int64          `json:"duration_ns"`
	Dimensions map[string]any `json:"dimensions"`
}

type deliveryAccounting struct {
	ExpectedFanout     int `json:"expected_fanout"`
	Delivered          int `json:"delivered"`
	ExplicitlyRejected int `json:"explicitly_rejected"`
	Dropped            int `json:"dropped"`
}

type operationSummary struct {
	Operation  string         `json:"operation"`
	Dimensions map[string]any `json:"dimensions"`
	Count      int            `json:"count"`
	MedianNS   float64        `json:"median_ns"`
	P95NS      float64        `json:"p95_ns"`
	P99NS      float64        `json:"p99_ns"`
}

type benchmarkArtifact struct {
	Schema             string             `json:"schema"`
	Metadata           runMetadata        `json:"metadata"`
	Samples            []operationSample  `json:"samples"`
	DeliveryAccounting deliveryAccounting `json:"delivery_accounting"`
	Summaries          []operationSummary `json:"summaries"`
}

type sampleRecorder struct {
	mu      sync.Mutex
	samples []operationSample
}

func (r *sampleRecorder) record(operation string, durationNS int64, dimensions map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stableDimensions := make(map[string]any, len(dimensions))
	for name, value := range dimensions {
		stableDimensions[name] = value
	}
	r.samples = append(r.samples, operationSample{
		Sequence:   len(r.samples),
		Operation:  operation,
		DurationNS: durationNS,
		Dimensions: stableDimensions,
	})
}

func (r *sampleRecorder) snapshot() []operationSample {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]operationSample, len(r.samples))
	copy(result, r.samples)
	return result
}

func newBenchmarkArtifact(metadata runMetadata, samples []operationSample, accounting deliveryAccounting) (benchmarkArtifact, error) {
	artifact := benchmarkArtifact{
		Schema:             benchmarkSchema,
		Metadata:           metadata,
		Samples:            samples,
		DeliveryAccounting: accounting,
		Summaries:          summarizeSamples(samples),
	}
	if err := artifact.validate(); err != nil {
		return benchmarkArtifact{}, err
	}
	return artifact, nil
}

func (a benchmarkArtifact) validate() error {
	if a.Schema != benchmarkSchema {
		return fmt.Errorf("schema must be %q", benchmarkSchema)
	}
	if err := a.Metadata.validate(); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	if err := a.DeliveryAccounting.validate(); err != nil {
		return fmt.Errorf("delivery_accounting: %w", err)
	}

	sequences := make(map[int]struct{}, len(a.Samples))
	for index, sample := range a.Samples {
		if err := sample.validate(); err != nil {
			return fmt.Errorf("samples[%d]: %w", index, err)
		}
		if _, exists := sequences[sample.Sequence]; exists {
			return fmt.Errorf("sample sequence %d appears more than once", sample.Sequence)
		}
		sequences[sample.Sequence] = struct{}{}
	}
	return nil
}

func (m runMetadata) validate() error {
	for name, value := range map[string]string{
		"run_id": m.RunID, "benchmark": m.Benchmark, "started_at": m.StartedAt,
		"commit": m.Provenance.Commit,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must be a non-empty string", name)
		}
	}
	if len(m.Provenance.BinarySHA256) != 64 {
		return errors.New("binary_sha256 must be a 64-character hex digest")
	}
	if _, err := hex.DecodeString(m.Provenance.BinarySHA256); err != nil {
		return errors.New("binary_sha256 must be a 64-character hex digest")
	}
	if len(m.Provenance.RuntimeVersions) == 0 {
		return errors.New("runtime_versions must contain at least one version")
	}
	for runtimeName, version := range m.Provenance.RuntimeVersions {
		if strings.TrimSpace(runtimeName) == "" || strings.TrimSpace(version) == "" {
			return errors.New("runtime_versions keys and values must be non-empty")
		}
	}
	for index, buildFlag := range m.Provenance.BuildFlags {
		if strings.TrimSpace(buildFlag) == "" {
			return fmt.Errorf("build_flags[%d] must be a non-empty string", index)
		}
	}
	if err := validateJSONValue(m.Environment, "environment"); err != nil {
		return err
	}
	return validateJSONValue(m.Workload, "workload")
}

func (s operationSample) validate() error {
	if s.Sequence < 0 {
		return errors.New("sequence must be a non-negative integer")
	}
	if strings.TrimSpace(s.Operation) == "" {
		return errors.New("operation must be a non-empty string")
	}
	if s.DurationNS < 0 {
		return errors.New("duration_ns must be a non-negative integer")
	}
	for name, value := range s.Dimensions {
		if strings.TrimSpace(name) == "" {
			return errors.New("dimension names must be non-empty")
		}
		switch value.(type) {
		case nil, bool, string, int, int64, uint64, float64:
		default:
			return errors.New("sample dimensions must contain JSON scalars")
		}
		if err := validateJSONValue(value, "dimensions."+name); err != nil {
			return err
		}
	}
	return nil
}

func (a deliveryAccounting) validate() error {
	if a.ExpectedFanout < 0 || a.Delivered < 0 || a.ExplicitlyRejected < 0 || a.Dropped < 0 {
		return errors.New("accounting values must be non-negative integers")
	}
	accounted := a.Delivered + a.ExplicitlyRejected + a.Dropped
	if accounted != a.ExpectedFanout {
		return fmt.Errorf(
			"incomplete accounting: delivered + explicitly_rejected + dropped = %d, expected %d",
			accounted, a.ExpectedFanout,
		)
	}
	return nil
}

func summarizeSamples(samples []operationSample) []operationSummary {
	type group struct {
		operation  string
		dimensions map[string]any
		values     []int64
	}

	groups := make(map[string]*group)
	for _, sample := range samples {
		key := summaryKey(sample.Operation, sample.Dimensions)
		current := groups[key]
		if current == nil {
			current = &group{operation: sample.Operation, dimensions: sample.Dimensions}
			groups[key] = current
		}
		current.values = append(current.values, sample.DurationNS)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	summaries := make([]operationSummary, 0, len(keys))
	for _, key := range keys {
		current := groups[key]
		sort.Slice(current.values, func(i, j int) bool { return current.values[i] < current.values[j] })
		summaries = append(summaries, operationSummary{
			Operation:  current.operation,
			Dimensions: current.dimensions,
			Count:      len(current.values),
			MedianNS:   median(current.values),
			P95NS:      nearestRank(current.values, 95),
			P99NS:      nearestRank(current.values, 99),
		})
	}
	return summaries
}

func summaryKey(operation string, dimensions map[string]any) string {
	encoded, _ := json.Marshal(dimensions)
	return operation + "\x00" + string(encoded)
}

func median(ordered []int64) float64 {
	middle := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return float64(ordered[middle])
	}
	return float64(ordered[middle-1])/2 + float64(ordered[middle])/2
}

func nearestRank(ordered []int64, percentile int) float64 {
	rank := int(math.Ceil(float64(percentile)/100*float64(len(ordered)))) - 1
	if rank < 0 {
		rank = 0
	}
	return float64(ordered[rank])
}

func validateJSONValue(value any, path string) error {
	switch typed := value.(type) {
	case nil, bool, string, int, int64, uint64:
		return nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return fmt.Errorf("%s must not contain NaN or infinity", path)
		}
		return nil
	case []string:
		return nil
	case []any:
		for index, item := range typed {
			if err := validateJSONValue(item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
		return nil
	case map[string]string:
		return nil
	case map[string]any:
		for name, item := range typed {
			if err := validateJSONValue(item, path+"."+name); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s is not a JSON value", path)
	}
}

func writeBenchmark(path string, artifact benchmarkArtifact) error {
	if err := artifact.validate(); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal benchmark: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("write benchmark: %w", err)
	}
	return nil
}
