from __future__ import annotations

from pathlib import Path

import pytest
from fair_comparison.analyzer import analyze_manifest
from fair_comparison.contract import ContractError, load_json, sha256_file

from tests.helpers import session, write_json


def test_analysis_reports_run_tails_paired_ci_order_effect_and_accounting(
    tmp_path: Path,
) -> None:
    manifest = session(tmp_path)

    report = analyze_manifest(manifest, min_measured_pairs=2, bootstrap_seed=1516, resamples=500)

    assert report["measured_pair_count"] == 2
    assert report["order_accounting"] == {"AB": 1, "BA": 1, "balanced": True}
    assert len(report["run_summaries"]) == 4
    first_summary = report["run_summaries"][0]["metrics"]
    metric_summary = next(iter(first_summary.values()))
    assert metric_summary == {
        "sample_count": 3,
        "median_ns": 20.0,
        "p95_ns": 30.0,
        "p99_ns": 30.0,
    }
    median = report["comparisons"][0]["statistics"]["median_ns"]
    assert median["paired_median_go_over_reference_rti"] == 1.5
    assert median["order_effect"]["go_second_over_go_first_ratio"] == 2.0
    assert len(median["paired_bootstrap_ci95"]) == 2
    assert report["accounting"]["by_implementation"]["reference_rti"]["delivered"] == 8
    assert report["accounting"]["by_implementation"]["go"]["delivered"] == 8
    assert report["accounting"]["complete"] is True


def test_analysis_rejects_content_valid_semantic_mismatch(tmp_path: Path) -> None:
    manifest = session(tmp_path, semantic_mismatch=True)
    with pytest.raises(ContractError, match="canonical semantic projection"):
        analyze_manifest(manifest, min_measured_pairs=2, resamples=100)


def test_analysis_rejects_result_workload_mismatch(tmp_path: Path) -> None:
    manifest = session(tmp_path, workload_mismatch=True)
    with pytest.raises(ContractError, match="workload does not match"):
        analyze_manifest(manifest, min_measured_pairs=2, resamples=100)


def test_analysis_rejects_different_raw_sample_counts(tmp_path: Path) -> None:
    manifest_path = session(tmp_path)
    manifest = load_json(manifest_path)
    invocation = next(
        run for run in manifest["runs"] if run["pair_index"] == 2 and run["implementation"] == "go"
    )
    result_path = tmp_path / invocation["result_path"]
    artifact = load_json(result_path)
    artifact["metrics"][0]["samples"].pop()
    write_json(result_path, artifact)
    invocation["result_sha256"] = sha256_file(result_path)
    write_json(manifest_path, manifest)

    with pytest.raises(ContractError, match="different raw sample counts"):
        analyze_manifest(manifest_path, min_measured_pairs=2, resamples=100)
