"""Analyze paired, balanced-order DEVStone-HLA benchmark result JSON."""

from __future__ import annotations

import argparse
import hashlib
import json
import math
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any

from benchmark_core import (
    ANALYSIS_SCHEMA_VERSION,
    RESULT_SCHEMA_VERSION,
    ResultValidationError,
    load_result_document,
    validate_result_document,
)

DEFAULT_BOOTSTRAP_RESAMPLES = 10_000
DEFAULT_BOOTSTRAP_SEED = 1516
_MASK64 = (1 << 64) - 1


class SplitMix64:
    """Small, fully specified PRNG used to make bootstrap samples reproducible."""

    def __init__(self, seed: int):
        self._state = seed & _MASK64

    def next_u64(self) -> int:
        self._state = (self._state + 0x9E3779B97F4A7C15) & _MASK64
        value = self._state
        value = ((value ^ (value >> 30)) * 0xBF58476D1CE4E5B9) & _MASK64
        value = ((value ^ (value >> 27)) * 0x94D049BB133111EB) & _MASK64
        return (value ^ (value >> 31)) & _MASK64

    def index(self, length: int) -> int:
        if length < 1:
            raise ValueError("cannot sample an empty sequence")
        modulus = 1 << 64
        limit = modulus - (modulus % length)
        while True:
            value = self.next_u64()
            if value < limit:
                return value % length


def quantile(values: Sequence[float], probability: float) -> float:
    """Return an R7/linear quantile, matching the common (n - 1) convention."""

    if not values:
        raise ValueError("quantile requires at least one value")
    if not 0.0 <= probability <= 1.0:
        raise ValueError("probability must be between zero and one")
    ordered = sorted(float(value) for value in values)
    position = (len(ordered) - 1) * probability
    lower = math.floor(position)
    upper = math.ceil(position)
    if lower == upper:
        return ordered[lower]
    fraction = position - lower
    return ordered[lower] + (ordered[upper] - ordered[lower]) * fraction


def _derived_seed(base_seed: int, *parts: str) -> int:
    material = "|".join((str(base_seed), *parts)).encode("utf-8")
    return int.from_bytes(hashlib.sha256(material).digest()[:8], "big")


def _resample(values: Sequence[Any], rng: SplitMix64) -> list[Any]:
    return [values[rng.index(len(values))] for _ in range(len(values))]


def _ci(estimates: Sequence[float]) -> dict[str, float]:
    return {
        "low": quantile(estimates, 0.025),
        "high": quantile(estimates, 0.975),
    }


def _statistic(estimate: float, bootstrap_estimates: Sequence[float]) -> dict[str, Any]:
    return {"estimate": float(estimate), "ci95": _ci(bootstrap_estimates)}


def _summarize(
    values: Sequence[float],
    *,
    resamples: int,
    seed: int,
) -> dict[str, Any]:
    functions: dict[str, Callable[[Sequence[float]], float]] = {
        "median": lambda sample: quantile(sample, 0.5),
        "p95": lambda sample: quantile(sample, 0.95),
        "p99": lambda sample: quantile(sample, 0.99),
    }
    estimates = {name: function(values) for name, function in functions.items()}
    distributions: dict[str, list[float]] = {name: [] for name in functions}
    rng = SplitMix64(seed)
    for _ in range(resamples):
        sample = _resample(values, rng)
        for name, function in functions.items():
            distributions[name].append(function(sample))
    return {
        name: _statistic(estimates[name], distributions[name])
        for name in functions
    }


def _canonical_sha256(document: dict[str, Any]) -> str:
    encoded = json.dumps(
        document,
        ensure_ascii=True,
        allow_nan=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _paired_records(
    document: dict[str, Any],
    metric_id: str,
    baseline: str,
    candidate: str,
) -> list[dict[str, Any]]:
    grouped: dict[int, list[dict[str, Any]]] = {}
    for run in document["runs"]:
        grouped.setdefault(run["pair_index"], []).append(run)

    records: list[dict[str, Any]] = []
    for pair_index in sorted(grouped):
        pair = grouped[pair_index]
        by_implementation = {run["implementation"]: run for run in pair}
        baseline_value = float(by_implementation[baseline]["measurements"][metric_id])
        candidate_value = float(by_implementation[candidate]["measurements"][metric_id])
        first = next(run["implementation"] for run in pair if run["position"] == 1)
        records.append(
            {
                "pair_index": pair_index,
                "first": first,
                "difference": candidate_value - baseline_value,
                "ratio": candidate_value / baseline_value if baseline_value > 0 else None,
            }
        )
    return records


def _order_adjusted_difference(records_by_first: dict[str, list[dict[str, Any]]]) -> float:
    stratum_medians = [
        quantile([record["difference"] for record in records], 0.5)
        for records in records_by_first.values()
    ]
    return sum(stratum_medians) / len(stratum_medians)


def _order_adjusted_ratio(records_by_first: dict[str, list[dict[str, Any]]]) -> float:
    stratum_medians = [
        quantile([float(record["ratio"]) for record in records], 0.5)
        for records in records_by_first.values()
    ]
    return math.sqrt(stratum_medians[0] * stratum_medians[1])


def _paired_comparison(
    records: Sequence[dict[str, Any]],
    baseline: str,
    candidate: str,
    *,
    resamples: int,
    seed: int,
) -> dict[str, Any]:
    by_first = {
        implementation: [record for record in records if record["first"] == implementation]
        for implementation in (baseline, candidate)
    }
    ratio_available = all(record["ratio"] is not None for record in records)
    differences = [record["difference"] for record in records]
    ratios = [float(record["ratio"]) for record in records] if ratio_available else []

    difference_distribution: list[float] = []
    adjusted_difference_distribution: list[float] = []
    ratio_distribution: list[float] = []
    adjusted_ratio_distribution: list[float] = []
    rng = SplitMix64(seed)
    for _ in range(resamples):
        sampled_by_first = {
            implementation: _resample(stratum, rng)
            for implementation, stratum in by_first.items()
        }
        sample = sampled_by_first[baseline] + sampled_by_first[candidate]
        difference_distribution.append(
            quantile([record["difference"] for record in sample], 0.5)
        )
        adjusted_difference_distribution.append(
            _order_adjusted_difference(sampled_by_first)
        )
        if ratio_available:
            ratio_distribution.append(
                quantile([float(record["ratio"]) for record in sample], 0.5)
            )
            adjusted_ratio_distribution.append(_order_adjusted_ratio(sampled_by_first))

    strata = []
    for first in (baseline, candidate):
        stratum = by_first[first]
        stratum_ratios = [record["ratio"] for record in stratum]
        strata.append(
            {
                "first_implementation": first,
                "n_pairs": len(stratum),
                "median_difference": quantile(
                    [record["difference"] for record in stratum], 0.5
                ),
                "median_ratio": (
                    quantile([float(value) for value in stratum_ratios], 0.5)
                    if all(value is not None for value in stratum_ratios)
                    else None
                ),
            }
        )

    return {
        "baseline": baseline,
        "candidate": candidate,
        "n_pairs": len(records),
        "difference_definition": "candidate-minus-baseline",
        "ratio_definition": "candidate-divided-by-baseline",
        "median_difference": _statistic(
            quantile(differences, 0.5), difference_distribution
        ),
        "median_ratio": (
            _statistic(quantile(ratios, 0.5), ratio_distribution)
            if ratio_available
            else None
        ),
        "order_adjusted_median_difference": _statistic(
            _order_adjusted_difference(by_first), adjusted_difference_distribution
        ),
        "order_adjusted_median_ratio": (
            _statistic(
                _order_adjusted_ratio(by_first), adjusted_ratio_distribution
            )
            if ratio_available
            else None
        ),
        "order_strata": strata,
    }


def analyze_document(
    document: dict[str, Any],
    *,
    bootstrap_resamples: int = DEFAULT_BOOTSTRAP_RESAMPLES,
    bootstrap_seed: int = DEFAULT_BOOTSTRAP_SEED,
) -> dict[str, Any]:
    """Validate and analyze one complete DEVStone-HLA experiment document."""

    validate_result_document(document)
    if not isinstance(bootstrap_resamples, int) or isinstance(bootstrap_resamples, bool):
        raise ValueError("bootstrap_resamples must be an integer")
    if bootstrap_resamples < 100:
        raise ValueError("bootstrap_resamples must be at least 100")
    if not isinstance(bootstrap_seed, int) or isinstance(bootstrap_seed, bool):
        raise ValueError("bootstrap_seed must be an integer")
    if bootstrap_seed < 0:
        raise ValueError("bootstrap_seed must be non-negative")

    implementation_metadata = document["implementations"]
    baseline = implementation_metadata[0]["id"]
    candidate = implementation_metadata[1]["id"]
    runs_by_implementation = {
        implementation_id: [
            run for run in document["runs"] if run["implementation"] == implementation_id
        ]
        for implementation_id in (baseline, candidate)
    }

    metric_analyses = []
    for metric in document["metrics"]:
        metric_id = metric["id"]
        summaries = []
        for implementation_id in (baseline, candidate):
            values = [
                float(run["measurements"][metric_id])
                for run in runs_by_implementation[implementation_id]
            ]
            summary = _summarize(
                values,
                resamples=bootstrap_resamples,
                seed=_derived_seed(
                    bootstrap_seed, "summary", metric_id, implementation_id
                ),
            )
            summaries.append(
                {
                    "implementation": implementation_id,
                    "n": len(values),
                    **summary,
                }
            )

        paired_records = _paired_records(document, metric_id, baseline, candidate)
        metric_analyses.append(
            {
                **metric,
                "summaries": summaries,
                "comparison": _paired_comparison(
                    paired_records,
                    baseline,
                    candidate,
                    resamples=bootstrap_resamples,
                    seed=_derived_seed(bootstrap_seed, "comparison", metric_id),
                ),
            }
        )

    return {
        "analysis_schema_version": ANALYSIS_SCHEMA_VERSION,
        "source_schema_version": RESULT_SCHEMA_VERSION,
        "source_sha256": _canonical_sha256(document),
        "experiment_id": document["experiment"]["id"],
        "benchmark": dict(document["benchmark"]),
        "bootstrap": {
            "method": "percentile-bootstrap",
            "confidence_level": 0.95,
            "resamples": bootstrap_resamples,
            "seed": bootstrap_seed,
            "prng": "splitmix64",
            "quantile_method": "linear-r7",
            "paired_resampling": "stratified-by-execution-order",
        },
        "implementations": [
            {
                "id": implementation["id"],
                "label": implementation["label"],
                "version": implementation["version"],
            }
            for implementation in implementation_metadata
        ],
        "design_checks": {
            "runs_per_implementation": 30,
            "paired_runs": 30,
            "baseline_first_pairs": 15,
            "candidate_first_pairs": 15,
            "fresh_process_per_run": True,
            "balanced_order": True,
        },
        "metrics": metric_analyses,
    }


def _write_json(path: str | Path, document: dict[str, Any]) -> None:
    output = Path(path)
    output.parent.mkdir(parents=True, exist_ok=True)
    encoded = json.dumps(
        document,
        ensure_ascii=False,
        allow_nan=False,
        indent=2,
    )
    output.write_text(encoded + "\n", encoding="utf-8")


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Analyze a validated 30+30 DEVStone-HLA benchmark result."
    )
    parser.add_argument("input", help="path to result-schema-v1 JSON")
    parser.add_argument("--output", required=True, help="analysis JSON output path")
    parser.add_argument(
        "--bootstrap-resamples",
        type=int,
        default=DEFAULT_BOOTSTRAP_RESAMPLES,
        help="number of bootstrap resamples (default: 10000)",
    )
    parser.add_argument(
        "--bootstrap-seed",
        type=int,
        default=DEFAULT_BOOTSTRAP_SEED,
        help="deterministic bootstrap seed (default: 1516)",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = _parser()
    args = parser.parse_args(argv)
    try:
        document = load_result_document(args.input)
        analysis = analyze_document(
            document,
            bootstrap_resamples=args.bootstrap_resamples,
            bootstrap_seed=args.bootstrap_seed,
        )
        _write_json(args.output, analysis)
    except (OSError, ResultValidationError, ValueError) as exc:
        parser.error(str(exc))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
