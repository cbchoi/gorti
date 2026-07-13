from __future__ import annotations

import json

import pytest

from verification.common.perf_contract import (
    BenchmarkContractError,
    BenchmarkRecorder,
    BenchmarkRun,
    DeliveryAccounting,
    OperationSample,
    RunMetadata,
    dumps_benchmark,
    summarize_samples,
)


def _metadata() -> RunMetadata:
    return RunMetadata(
        run_id="gorti-ro-007",
        benchmark="ro-delivery",
        started_at="2026-07-12T04:05:06Z",
        commit="2a4cd42",
        binary_sha256="ab" * 32,
        runtime_versions={"go": "go1.24.5", "rtid": "0.1.0"},
        build_flags=("-trimpath",),
        environment={"cpu": "example-cpu", "gomaxprocs": 8, "logging": "discard"},
        workload={"seed": 20260712, "payload_bytes": 256, "subscriber_count": 1},
    )


def test_benchmark_artifact_preserves_raw_samples_and_provenance() -> None:
    recorder = BenchmarkRecorder(_metadata())
    recorder.record("send_interaction", 300, {"payload_bytes": 256})
    recorder.record("send_interaction", 100, {"payload_bytes": 256})

    run = recorder.finish(DeliveryAccounting(2, 2, 0, 0))
    encoded = dumps_benchmark(run)
    artifact = json.loads(encoded)

    assert artifact["samples"] == [
        {
            "sequence": 0,
            "operation": "send_interaction",
            "duration_ns": 300,
            "dimensions": {"payload_bytes": 256},
        },
        {
            "sequence": 1,
            "operation": "send_interaction",
            "duration_ns": 100,
            "dimensions": {"payload_bytes": 256},
        },
    ]
    assert artifact["metadata"]["provenance"] == {
        "commit": "2a4cd42",
        "binary_sha256": "ab" * 32,
        "runtime_versions": {"go": "go1.24.5", "rtid": "0.1.0"},
        "build_flags": ["-trimpath"],
    }
    assert encoded.endswith("\n")


def test_delivery_accounting_requires_every_expected_delivery_disposition() -> None:
    accounting = DeliveryAccounting(
        expected_fanout=10,
        delivered=7,
        explicitly_rejected=2,
        dropped=1,
    )

    assert accounting.rejected == 2
    assert accounting.to_dict()["expected_fanout"] == 10

    with pytest.raises(BenchmarkContractError, match="must equal expected_fanout"):
        DeliveryAccounting(10, 7, 1, 0)


@pytest.mark.parametrize(
    "counts",
    [(-1, 0, 0, -1), (True, 1, 0, 0), (1, 0, 0, 1.0)],
)
def test_delivery_accounting_rejects_invalid_counts(counts: tuple[object, ...]) -> None:
    with pytest.raises(BenchmarkContractError, match="non-negative integer"):
        DeliveryAccounting(*counts)  # type: ignore[arg-type]


def test_summary_reports_median_p95_and_p99_with_nearest_rank_tails() -> None:
    samples = [OperationSample(index, "update", index + 1) for index in range(20)]

    summary = summarize_samples(samples)

    assert len(summary) == 1
    assert summary[0].count == 20
    assert summary[0].median_ns == 10.5
    assert summary[0].p95_ns == 19.0
    assert summary[0].p99_ns == 20.0


def test_summary_keeps_different_operation_dimensions_separate() -> None:
    samples = (
        OperationSample(0, "update", 10, {"size": 2}),
        OperationSample(1, "update", 30, {"size": 2}),
        OperationSample(2, "update", 100, {"size": 25}),
        OperationSample(3, "interaction", 7),
    )

    summaries = summarize_samples(samples)

    assert [(item.operation, item.dimensions, item.median_ns) for item in summaries] == [
        ("interaction", {}, 7.0),
        ("update", {"size": 2}, 20.0),
        ("update", {"size": 25}, 100.0),
    ]


def test_benchmark_run_rejects_duplicate_raw_sample_sequences() -> None:
    samples = (OperationSample(0, "update", 10), OperationSample(0, "update", 20))

    with pytest.raises(BenchmarkContractError, match="appears more than once"):
        BenchmarkRun(_metadata(), samples, DeliveryAccounting(2, 2, 0, 0))


def test_metadata_requires_binary_and_runtime_provenance() -> None:
    values = _metadata()

    with pytest.raises(BenchmarkContractError, match="64-character hex digest"):
        RunMetadata(
            values.run_id,
            values.benchmark,
            values.started_at,
            values.commit,
            "not-a-digest",
            values.runtime_versions,
        )
    with pytest.raises(BenchmarkContractError, match="at least one version"):
        RunMetadata(
            values.run_id,
            values.benchmark,
            values.started_at,
            values.commit,
            values.binary_sha256,
            {},
        )


def test_recorder_is_sealed_after_finish() -> None:
    recorder = BenchmarkRecorder(_metadata())
    recorder.finish(DeliveryAccounting(0, 0, 0, 0))

    with pytest.raises(BenchmarkContractError, match="cannot record"):
        recorder.record("late", 1)
