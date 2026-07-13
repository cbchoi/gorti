package main

import (
	"strings"
	"testing"
)

func TestSummarizeSamplesUsesMedianAndNearestRankTails(t *testing.T) {
	samples := make([]operationSample, 20)
	for index := range samples {
		samples[index] = operationSample{
			Sequence:   index,
			Operation:  "updateAttributeValues",
			DurationNS: int64(index + 1),
			Dimensions: map[string]any{"service": "OM", "sample_kind": "call"},
		}
	}

	summaries := summarizeSamples(samples)
	if len(summaries) != 1 {
		t.Fatalf("summary count = %d, want 1", len(summaries))
	}
	got := summaries[0]
	if got.Count != 20 || got.MedianNS != 10.5 || got.P95NS != 19 || got.P99NS != 20 {
		t.Fatalf("summary = %+v", got)
	}
}

func TestSummarizeSamplesSeparatesDimensions(t *testing.T) {
	samples := []operationSample{
		{Sequence: 0, Operation: "timeAdvanceRequest", DurationNS: 10, Dimensions: map[string]any{"service": "TM", "sample_kind": "call"}},
		{Sequence: 1, Operation: "timeAdvanceRequest", DurationNS: 20, Dimensions: map[string]any{"service": "TM", "sample_kind": "delivery"}},
	}

	summaries := summarizeSamples(samples)
	if len(summaries) != 2 {
		t.Fatalf("summary count = %d, want 2", len(summaries))
	}
	if summaries[0].Dimensions["sample_kind"] == summaries[1].Dimensions["sample_kind"] {
		t.Fatalf("dimensions were combined: %+v", summaries)
	}
}

func TestBenchmarkValidationRequiresCompleteAccounting(t *testing.T) {
	artifact := validTestArtifact()
	artifact.DeliveryAccounting.Dropped = 1

	err := artifact.validate()
	if err == nil || !strings.Contains(err.Error(), "incomplete accounting") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestBenchmarkValidationRejectsDuplicateSequences(t *testing.T) {
	artifact := validTestArtifact()
	artifact.Samples = append(artifact.Samples, artifact.Samples[0])

	err := artifact.validate()
	if err == nil || !strings.Contains(err.Error(), "appears more than once") {
		t.Fatalf("validation error = %v", err)
	}
}

func validTestArtifact() benchmarkArtifact {
	return benchmarkArtifact{
		Schema: benchmarkSchema,
		Metadata: runMetadata{
			RunID:       "test-run",
			Benchmark:   "gorti-tso-lockstep",
			StartedAt:   "2026-07-12T00:00:00Z",
			Environment: map[string]any{"os": "test"},
			Workload:    map[string]any{"count": 1},
			Provenance: provenance{
				Commit:          "test",
				BinarySHA256:    strings.Repeat("a", 64),
				RuntimeVersions: map[string]string{"go": "test"},
				BuildFlags:      []string{},
			},
		},
		Samples: []operationSample{{
			Sequence: 0, Operation: "sendInteraction", DurationNS: 5,
			Dimensions: map[string]any{"service": "OM", "sample_kind": "call"},
		}},
		DeliveryAccounting: deliveryAccounting{ExpectedFanout: 2, Delivered: 2},
		Summaries:          []operationSummary{},
	}
}
