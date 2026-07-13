from __future__ import annotations

import json
from pathlib import Path

import pytest

from verification.common.analyze_benchmarks import analyze
from verification.common.perf_contract import SCHEMA_ID, BenchmarkContractError


def _artifact(path: Path, *, duration: int, dropped: int = 0) -> Path:
    expected = 1
    delivered = expected - dropped
    value = {
        "schema": SCHEMA_ID,
        "metadata": {
            "workload": {"count": 1, "logging_mode": "file"},
            "provenance": {"binary_sha256": "ab" * 32},
        },
        "delivery_accounting": {
            "expected_fanout": expected,
            "delivered": delivered,
            "explicitly_rejected": 0,
            "dropped": dropped,
        },
        "summaries": [
            {
                "operation": "sendInteraction",
                "dimensions": {"service": "OM"},
                "count": 1,
                "median_ns": duration,
                "p95_ns": duration,
                "p99_ns": duration,
            }
        ],
    }
    path.write_text(json.dumps(value), encoding="utf-8")
    return path


def test_analysis_reports_run_values_ci_cv_and_complete_accounting(
    tmp_path: Path,
) -> None:
    paths = [_artifact(tmp_path / f"run-{index}.json", duration=index) for index in range(1, 5)]

    report = analyze(paths, min_runs=4, seed=1516, resamples=500)

    assert report["run_count"] == 4
    assert report["accounting"] == {
        "expected_fanout": 4,
        "delivered": 4,
        "explicitly_rejected": 0,
        "dropped": 0,
    }
    median = report["summaries"][0]["statistics"]["median_ns"]
    assert median["run_values"] == [1.0, 2.0, 3.0, 4.0]
    assert median["median"] == 2.5
    assert median["bootstrap_median_ci95"][0] <= 2.5
    assert median["bootstrap_median_ci95"][1] >= 2.5
    assert median["coefficient_of_variation"] > 0


def test_analysis_rejects_too_few_runs_and_drops(tmp_path: Path) -> None:
    valid = _artifact(tmp_path / "valid.json", duration=1)
    with pytest.raises(BenchmarkContractError, match="at least 2"):
        analyze([valid], min_runs=2, seed=1, resamples=100)

    dropped = _artifact(tmp_path / "dropped.json", duration=1, dropped=1)
    with pytest.raises(BenchmarkContractError, match="reported drops"):
        analyze([dropped], min_runs=1, seed=1, resamples=100)
