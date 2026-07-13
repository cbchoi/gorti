"""Semantic and performance comparison for canonical verification logs."""

from __future__ import annotations

import argparse
import json
import math
import sys
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path

from .normalize import normalize_semantic_events
from .records import JSONValue, PerformanceSample, Record, SemanticEvent, load_ndjson


@dataclass(frozen=True, slots=True)
class PerformancePolicy:
    """PerformancePolicy defines accepted measurement drift."""

    relative_tolerance: float = 0.0
    absolute_tolerance: float = 0.0
    metric_tolerances: Mapping[str, float] | None = None

    def tolerance_for(self, metric: str) -> float:
        """Return the metric override or the default relative tolerance."""
        if self.metric_tolerances and metric in self.metric_tolerances:
            return self.metric_tolerances[metric]
        return self.relative_tolerance

    def validate(self) -> None:
        """Reject negative or non-finite tolerances."""
        values = [self.relative_tolerance, self.absolute_tolerance]
        if self.metric_tolerances:
            values.extend(self.metric_tolerances.values())
        if any(not math.isfinite(value) or value < 0 for value in values):
            raise ValueError("performance tolerances must be finite and non-negative")


@dataclass(frozen=True, slots=True)
class Difference:
    """Difference describes one semantic or performance mismatch."""

    kind: str
    index: int
    message: str
    expected: object | None = None
    actual: object | None = None


@dataclass(frozen=True, slots=True)
class ComparisonResult:
    """ComparisonResult contains all mismatches without failing fast."""

    semantic_differences: tuple[Difference, ...]
    performance_differences: tuple[Difference, ...]

    @property
    def semantic_equal(self) -> bool:
        return not self.semantic_differences

    @property
    def performance_equal(self) -> bool:
        return not self.performance_differences

    @property
    def equal(self) -> bool:
        return self.semantic_equal and self.performance_equal

    @property
    def differences(self) -> tuple[Difference, ...]:
        return self.semantic_differences + self.performance_differences


def _sequence_differences(
    expected: Sequence[dict[str, JSONValue]], actual: Sequence[dict[str, JSONValue]]
) -> list[Difference]:
    differences: list[Difference] = []
    common = min(len(expected), len(actual))
    for index in range(common):
        if expected[index] != actual[index]:
            differences.append(
                Difference(
                    kind="semantic",
                    index=index,
                    message="event or payload differs",
                    expected=expected[index],
                    actual=actual[index],
                )
            )
    for index in range(common, max(len(expected), len(actual))):
        expected_item = expected[index] if index < len(expected) else None
        actual_item = actual[index] if index < len(actual) else None
        differences.append(
            Difference(
                kind="semantic",
                index=index,
                message="event is missing" if actual_item is None else "unexpected event",
                expected=expected_item,
                actual=actual_item,
            )
        )
    return differences


def _sample_identity(sample: PerformanceSample) -> tuple[object, ...]:
    dimensions = tuple(sorted(sample.dimensions.items()))
    return sample.metric, sample.unit, sample.direction, dimensions


def _performance_passes(
    expected: PerformanceSample, actual: PerformanceSample, policy: PerformancePolicy
) -> bool:
    relative = policy.tolerance_for(expected.metric)
    absolute = policy.absolute_tolerance
    if expected.direction == "higher":
        threshold = expected.value * (1.0 - relative) - absolute
        return actual.value >= threshold
    if expected.direction == "lower":
        threshold = expected.value * (1.0 + relative) + absolute
        return actual.value <= threshold
    return math.isclose(
        actual.value,
        expected.value,
        rel_tol=relative,
        abs_tol=absolute,
    )


def _performance_differences(
    expected: Sequence[PerformanceSample],
    actual: Sequence[PerformanceSample],
    policy: PerformancePolicy,
) -> list[Difference]:
    differences: list[Difference] = []
    common = min(len(expected), len(actual))
    for index in range(common):
        expected_sample = expected[index]
        actual_sample = actual[index]
        if _sample_identity(expected_sample) != _sample_identity(actual_sample):
            differences.append(
                Difference(
                    kind="performance",
                    index=index,
                    message="metric identity differs",
                    expected=_sample_identity(expected_sample),
                    actual=_sample_identity(actual_sample),
                )
            )
        elif not _performance_passes(expected_sample, actual_sample, policy):
            differences.append(
                Difference(
                    kind="performance",
                    index=index,
                    message="measurement is outside the accepted tolerance",
                    expected=expected_sample.value,
                    actual=actual_sample.value,
                )
            )
    for index in range(common, max(len(expected), len(actual))):
        expected_item = expected[index] if index < len(expected) else None
        actual_item = actual[index] if index < len(actual) else None
        differences.append(
            Difference(
                kind="performance",
                index=index,
                message="sample is missing" if actual_item is None else "unexpected sample",
                expected=expected_item,
                actual=actual_item,
            )
        )
    return differences


def compare_records(
    expected: Sequence[Record],
    actual: Sequence[Record],
    *,
    performance_policy: PerformancePolicy | None = None,
) -> ComparisonResult:
    """Compare semantic event order/payload and policy-governed performance."""
    policy = performance_policy or PerformancePolicy()
    policy.validate()
    expected_events = [record for record in expected if isinstance(record, SemanticEvent)]
    actual_events = [record for record in actual if isinstance(record, SemanticEvent)]
    expected_performance = [record for record in expected if isinstance(record, PerformanceSample)]
    actual_performance = [record for record in actual if isinstance(record, PerformanceSample)]

    semantic = _sequence_differences(
        normalize_semantic_events(expected_events),
        normalize_semantic_events(actual_events),
    )
    performance = _performance_differences(expected_performance, actual_performance, policy)
    return ComparisonResult(tuple(semantic), tuple(performance))


def compare_logs(
    expected: str | Path,
    actual: str | Path,
    *,
    performance_policy: PerformancePolicy | None = None,
) -> ComparisonResult:
    """Load and compare two canonical NDJSON files."""
    return compare_records(
        load_ndjson(expected),
        load_ndjson(actual),
        performance_policy=performance_policy,
    )


def _difference_json(difference: Difference) -> str:
    return json.dumps(
        {
            "kind": difference.kind,
            "index": difference.index,
            "message": difference.message,
            "expected": difference.expected,
            "actual": difference.actual,
        },
        ensure_ascii=False,
        sort_keys=True,
        default=str,
    )


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Compare canonical verification NDJSON logs")
    parser.add_argument("expected", type=Path)
    parser.add_argument("actual", type=Path)
    parser.add_argument("--performance-tolerance", type=float, default=0.0)
    parser.add_argument("--performance-absolute-tolerance", type=float, default=0.0)
    args = parser.parse_args(argv)
    policy = PerformancePolicy(
        relative_tolerance=args.performance_tolerance,
        absolute_tolerance=args.performance_absolute_tolerance,
    )
    result = compare_logs(args.expected, args.actual, performance_policy=policy)
    for difference in result.differences:
        sys.stdout.write(_difference_json(difference) + "\n")
    return 0 if result.equal else 1


if __name__ == "__main__":
    sys.exit(main())
