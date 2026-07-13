"""Paired AB/BA analysis for strict fair-comparison session manifests."""

from __future__ import annotations

import math
import random
import statistics
from collections import defaultdict
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any, cast

from .contract import (
    ANALYSIS_SCHEMA,
    STATISTICS,
    ContractError,
    canonical_json,
    load_json,
    metric_identity,
    semantics_identity,
    sha256_file,
    validate_manifest,
    validate_result,
)


def _nearest_rank(values: Sequence[int], percentile: int) -> float:
    ordered = sorted(values)
    rank = max(0, math.ceil(percentile / 100 * len(ordered)) - 1)
    return float(ordered[rank])


def summarize_samples(values: Sequence[int]) -> dict[str, float | int]:
    if not values:
        raise ContractError("cannot summarize an empty metric")
    return {
        "sample_count": len(values),
        "median_ns": float(statistics.median(values)),
        "p95_ns": _nearest_rank(values, 95),
        "p99_ns": _nearest_rank(values, 99),
    }


def _bootstrap_median_ci(
    values: Sequence[float], *, rng: random.Random, resamples: int
) -> list[float]:
    if not values:
        raise ContractError("cannot bootstrap an empty sample")
    medians = sorted(
        statistics.median(rng.choices(values, k=len(values))) for _ in range(resamples)
    )
    return [_nearest_rank_float(medians, 2.5), _nearest_rank_float(medians, 97.5)]


def _nearest_rank_float(ordered: Sequence[float], percentile: float) -> float:
    rank = max(0, math.ceil(percentile / 100 * len(ordered)) - 1)
    return float(ordered[rank])


def _bootstrap_order_effect_ci(
    ab: Sequence[float], ba: Sequence[float], *, rng: random.Random, resamples: int
) -> list[float]:
    effects: list[float] = []
    for _ in range(resamples):
        ab_median = statistics.median(rng.choices(ab, k=len(ab)))
        ba_median = statistics.median(rng.choices(ba, k=len(ba)))
        effects.append(ab_median / ba_median)
    effects.sort()
    return [_nearest_rank_float(effects, 2.5), _nearest_rank_float(effects, 97.5)]


def _resolve_artifact(manifest_path: Path, recorded: str) -> Path:
    path = Path(recorded)
    if not path.is_absolute():
        path = manifest_path.parent / path
    return path.resolve()


def _load_session(manifest_path: Path) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    manifest = validate_manifest(load_json(manifest_path))
    workload = cast(dict[str, Any], manifest["workload"])
    loaded: list[dict[str, Any]] = []
    semantic_identities: set[str] = set()

    for invocation in manifest["runs"]:
        invocation = cast(dict[str, Any], invocation)
        result_path = _resolve_artifact(manifest_path, str(invocation["result_path"]))
        if not result_path.is_file():
            raise ContractError(f"missing result artifact: {result_path}")
        actual_hash = sha256_file(result_path)
        if actual_hash != invocation["result_sha256"]:
            raise ContractError(f"result artifact hash mismatch: {result_path}")
        result = validate_result(
            load_json(result_path),
            expected_workload=workload,
            expected_implementation=str(invocation["implementation"]),
            expected_run_id=str(invocation["run_id"]),
        )
        semantic_identities.add(semantics_identity(result))
        loaded.append({"invocation": invocation, "result": result, "path": str(result_path)})

    measured_by_pair: dict[int, set[str]] = defaultdict(set)
    for item in loaded:
        invocation = item["invocation"]
        if invocation["phase"] == "measured":
            measured_by_pair[int(invocation["pair_index"])].add(semantics_identity(item["result"]))
    mismatched_pairs = sorted(
        pair_index for pair_index, identities in measured_by_pair.items() if len(identities) != 1
    )
    if mismatched_pairs:
        raise ContractError(
            "canonical semantic projection differs within measured pair(s): "
            + ", ".join(map(str, mismatched_pairs))
        )
    if len(semantic_identities) != 1:
        raise ContractError("canonical semantic projection differs between runs")
    return manifest, loaded


def _metric_map(result: Mapping[str, Any]) -> dict[str, Mapping[str, Any]]:
    metrics = cast(list[Mapping[str, Any]], result["metrics"])
    return {metric_identity(metric): metric for metric in metrics}


def _validate_metric_sets(measured: Sequence[dict[str, Any]]) -> list[str]:
    expected: set[str] | None = None
    expected_sample_counts: dict[str, int] = {}
    for loaded in measured:
        metrics = _metric_map(loaded["result"])
        keys = set(metrics)
        if expected is None:
            expected = keys
            expected_sample_counts = {
                key: len(cast(list[int], metric["samples"])) for key, metric in metrics.items()
            }
        elif keys != expected:
            raise ContractError("measured artifacts have different metric definitions")
        elif any(
            len(cast(list[int], metrics[key]["samples"])) != expected_sample_counts[key]
            for key in keys
        ):
            raise ContractError("measured artifacts have different raw sample counts")
    if not expected:
        raise ContractError("no measured metrics were found")
    return sorted(expected)


def _aggregate_accounting(measured: Sequence[dict[str, Any]]) -> dict[str, Any]:
    fields = (
        "expected_fanout",
        "delivered",
        "explicitly_rejected",
        "dropped",
        "duplicates",
        "invalid",
    )
    by_implementation: dict[str, dict[str, int]] = {
        implementation: dict.fromkeys(fields, 0) for implementation in ("pitch", "go")
    }
    expected_per_run: int | None = None
    for loaded in measured:
        result = loaded["result"]
        accounting = cast(dict[str, Any], result["accounting"])
        implementation = str(result["implementation"])
        current_expected = int(accounting["expected_fanout"])
        if expected_per_run is None:
            expected_per_run = current_expected
        elif current_expected != expected_per_run:
            raise ContractError("expected_fanout differs between measured artifacts")
        for field in fields:
            by_implementation[implementation][field] += int(accounting[field])
    return {
        "expected_fanout_per_run": expected_per_run,
        "by_implementation": by_implementation,
        "complete": all(
            values["expected_fanout"]
            == values["delivered"] + values["explicitly_rejected"] + values["dropped"]
            for values in by_implementation.values()
        ),
    }


def analyze_manifest(
    manifest_path: Path,
    *,
    min_measured_pairs: int = 20,
    bootstrap_seed: int = 1516,
    resamples: int = 10_000,
) -> dict[str, Any]:
    """Validate one complete session and compute paired, order-aware statistics."""
    if min_measured_pairs < 1:
        raise ContractError("min_measured_pairs must be at least 1")
    if resamples < 100:
        raise ContractError("bootstrap resamples must be at least 100")
    manifest, loaded = _load_session(manifest_path.resolve())
    measured = [item for item in loaded if item["invocation"]["phase"] == "measured"]
    measured_pairs = int(manifest["schedule"]["measured_pairs"])
    if measured_pairs < min_measured_pairs:
        raise ContractError(
            f"need at least {min_measured_pairs} measured pairs, received {measured_pairs}"
        )
    if len(measured) != measured_pairs * 2:
        raise ContractError("measured run count does not equal two arms per measured pair")

    metric_keys = _validate_metric_sets(measured)
    by_pair: dict[int, dict[str, dict[str, Any]]] = defaultdict(dict)
    run_summaries: list[dict[str, Any]] = []
    metric_metadata: dict[str, dict[str, Any]] = {}
    for loaded_run in measured:
        invocation = loaded_run["invocation"]
        result = loaded_run["result"]
        implementation = str(result["implementation"])
        metrics: dict[str, Any] = {}
        for key, metric in _metric_map(result).items():
            samples = cast(list[int], metric["samples"])
            metrics[key] = summarize_samples(samples)
            metric_metadata[key] = {
                name: metric[name]
                for name in ("name", "unit", "direction", "sample_scope", "dimensions")
            }
        pair_index = int(invocation["pair_index"])
        by_pair[pair_index][implementation] = {
            "order": invocation["order"],
            "slot": invocation["slot"],
            "run_id": invocation["run_id"],
            "metrics": metrics,
        }
        run_summaries.append(
            {
                "pair_index": pair_index,
                "order": invocation["order"],
                "slot": invocation["slot"],
                "implementation": implementation,
                "run_id": invocation["run_id"],
                "metrics": metrics,
            }
        )

    if sorted(by_pair) != list(range(1, measured_pairs + 1)):
        raise ContractError("measured pair indexes are not contiguous")
    if any(set(arms) != {"pitch", "go"} for arms in by_pair.values()):
        raise ContractError("a measured pair is missing Pitch or Go")

    comparisons: list[dict[str, Any]] = []
    for metric_index, metric_key in enumerate(metric_keys):
        statistic_reports: dict[str, Any] = {}
        for statistic_index, statistic in enumerate(STATISTICS):
            ratios: list[float] = []
            ratio_records: list[dict[str, Any]] = []
            arm_values: dict[str, list[float]] = {"pitch": [], "go": []}
            by_order: dict[str, list[float]] = {"AB": [], "BA": []}
            for pair_index in sorted(by_pair):
                arms = by_pair[pair_index]
                pitch_value = float(arms["pitch"]["metrics"][metric_key][statistic])
                go_value = float(arms["go"]["metrics"][metric_key][statistic])
                if pitch_value <= 0 or go_value <= 0:
                    raise ContractError(
                        f"paired ratios require positive {statistic} values for {metric_key}"
                    )
                ratio = go_value / pitch_value
                order = str(arms["pitch"]["order"])
                ratios.append(ratio)
                by_order[order].append(ratio)
                arm_values["pitch"].append(pitch_value)
                arm_values["go"].append(go_value)
                ratio_records.append(
                    {"pair_index": pair_index, "order": order, "go_over_pitch": ratio}
                )

            rng_seed = bootstrap_seed + metric_index * 101 + statistic_index
            ratio_ci = _bootstrap_median_ci(
                ratios,
                rng=random.Random(rng_seed),  # noqa: S311 - reproducible bootstrap
                resamples=resamples,
            )
            order_effect: dict[str, Any] = {
                "AB_count": len(by_order["AB"]),
                "BA_count": len(by_order["BA"]),
                "AB_median_go_over_pitch": statistics.median(by_order["AB"]),
                "BA_median_go_over_pitch": statistics.median(by_order["BA"]),
            }
            effect = (
                order_effect["AB_median_go_over_pitch"] / order_effect["BA_median_go_over_pitch"]
            )
            order_effect["go_second_over_go_first_ratio"] = effect
            order_effect["bootstrap_ci95"] = _bootstrap_order_effect_ci(
                by_order["AB"],
                by_order["BA"],
                rng=random.Random(rng_seed + 50_000),  # noqa: S311
                resamples=resamples,
            )
            statistic_reports[statistic] = {
                "pitch_run_values": arm_values["pitch"],
                "go_run_values": arm_values["go"],
                "pitch_run_median": statistics.median(arm_values["pitch"]),
                "go_run_median": statistics.median(arm_values["go"]),
                "paired_ratios": ratio_records,
                "paired_median_go_over_pitch": statistics.median(ratios),
                "paired_bootstrap_ci95": ratio_ci,
                "order_effect": order_effect,
            }
        comparisons.append({**metric_metadata[metric_key], "statistics": statistic_reports})

    run_summaries.sort(key=lambda item: (item["pair_index"], item["slot"]))
    measured_orders = [by_pair[index]["pitch"]["order"] for index in sorted(by_pair)]
    semantic = loaded[0]["result"]["semantics"]
    return {
        "schema": ANALYSIS_SCHEMA,
        "session_id": manifest["session_id"],
        "manifest_sha256": sha256_file(manifest_path.resolve()),
        "workload": manifest["workload"],
        "semantic_identity": semantic,
        "measured_pair_count": measured_pairs,
        "order_accounting": {
            "AB": measured_orders.count("AB"),
            "BA": measured_orders.count("BA"),
            "balanced": abs(measured_orders.count("AB") - measured_orders.count("BA")) <= 1,
        },
        "bootstrap": {"seed": bootstrap_seed, "resamples": resamples, "method": "paired"},
        "accounting": _aggregate_accounting(measured),
        "run_summaries": run_summaries,
        "comparisons": comparisons,
    }


def write_analysis(path: Path, report: Mapping[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        canonical_json(report) + "\n",
        encoding="utf-8",
        newline="\n",
    )


__all__ = ["analyze_manifest", "summarize_samples", "write_analysis"]
