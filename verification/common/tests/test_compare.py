from __future__ import annotations

from verification.common import (
    PerformancePolicy,
    PerformanceSample,
    SemanticEvent,
    compare_records,
)


def test_comparison_normalizes_handles_timestamps_and_timing() -> None:
    expected = [
        SemanticEvent(
            0,
            "OM",
            "discover",
            {
                "object_handle": 401,
                "ownerHandle": 9,
                "wall_timestamp": "2026-07-11T01:02:03Z",
                "elapsed_ms": 2.1,
                "logical_time": 4.0,
            },
            runtime={"pid": 100},
            timing={"duration_ns": 20},
        ),
        SemanticEvent(1, "OM", "reflect", {"object_handle": 401, "value": "same"}),
    ]
    actual = [
        SemanticEvent(
            10,
            "OM",
            "discover",
            {
                "object_handle": 7,
                "ownerHandle": 55,
                "wall_timestamp": "2027-01-01T00:00:00Z",
                "elapsed_ms": 99.0,
                "logical_time": 4.0,
            },
            runtime={"pid": 200},
            timing={"duration_ns": 500},
        ),
        SemanticEvent(11, "OM", "reflect", {"object_handle": 7, "value": "same"}),
    ]

    result = compare_records(expected, actual)

    assert result.semantic_equal
    assert result.equal


def test_comparison_preserves_handle_alias_relationships() -> None:
    expected = [
        SemanticEvent(0, "OM", "discover", {"object_handle": 80}),
        SemanticEvent(1, "OM", "reflect", {"object_handle": 80}),
    ]
    actual = [
        SemanticEvent(0, "OM", "discover", {"object_handle": 2}),
        SemanticEvent(1, "OM", "reflect", {"object_handle": 3}),
    ]

    result = compare_records(expected, actual)

    assert not result.semantic_equal
    assert result.semantic_differences[0].index == 1


def test_comparison_preserves_hla_timestamp() -> None:
    expected = [SemanticEvent(0, "OM", "reflect", {"timestamp": 3.0})]
    actual = [SemanticEvent(0, "OM", "reflect", {"timestamp": 4.0})]

    assert not compare_records(expected, actual).semantic_equal


def test_comparison_is_strict_for_event_order_and_semantic_payload() -> None:
    expected = [
        SemanticEvent(0, "FM", "join", {"name": "alice"}),
        SemanticEvent(1, "TM", "grant", {"logical_time": 5.0}),
    ]
    actual = [
        SemanticEvent(0, "TM", "grant", {"logical_time": 6.0}),
        SemanticEvent(1, "FM", "join", {"name": "alice"}),
    ]

    result = compare_records(expected, actual)

    assert not result.semantic_equal
    assert len(result.semantic_differences) == 2


def test_performance_policy_applies_direction_and_metric_override() -> None:
    expected = [
        PerformanceSample(0, "throughput", 100.0, "events/s", "higher"),
        PerformanceSample(1, "p99", 10.0, "ms", "lower", {"size": 25}),
    ]
    actual = [
        PerformanceSample(0, "throughput", 91.0, "events/s", "higher"),
        PerformanceSample(1, "p99", 10.4, "ms", "lower", {"size": 25}),
    ]
    policy = PerformancePolicy(relative_tolerance=0.05, metric_tolerances={"throughput": 0.1})

    assert compare_records(expected, actual, performance_policy=policy).performance_equal

    regressed = [actual[0], PerformanceSample(1, "p99", 11.0, "ms", "lower", {"size": 25})]
    result = compare_records(expected, regressed, performance_policy=policy)
    assert not result.performance_equal
    assert result.performance_differences[0].index == 1


def test_performance_identity_is_exact() -> None:
    expected = [PerformanceSample(0, "p99", 10.0, "ms", "lower", {"size": 5})]
    actual = [PerformanceSample(8, "p99", 0.01, "seconds", "lower", {"size": 5})]

    result = compare_records(expected, actual, performance_policy=PerformancePolicy(1.0))

    assert not result.performance_equal
    assert result.performance_differences[0].message == "metric identity differs"
