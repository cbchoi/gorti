"""Validate and summarize a set of production benchmark artifacts."""

from __future__ import annotations

import argparse
import json
import math
import random
import statistics
from collections import defaultdict
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any

from .perf_contract import SCHEMA_ID, BenchmarkContractError


def _percentile(ordered: Sequence[float], percentile: float) -> float:
    rank = max(0, math.ceil(percentile / 100.0 * len(ordered)) - 1)
    return ordered[rank]


def _bootstrap_median_ci(
    values: Sequence[float], *, seed: int, resamples: int
) -> tuple[float, float]:
    if not values:
        raise BenchmarkContractError("cannot bootstrap an empty sample")
    rng = random.Random(seed)  # noqa: S311 - deterministic statistical resampling
    medians = sorted(
        statistics.median(rng.choices(values, k=len(values))) for _ in range(resamples)
    )
    return _percentile(medians, 2.5), _percentile(medians, 97.5)


def _load(path: Path) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict) or value.get("schema") != SCHEMA_ID:
        raise BenchmarkContractError(f"{path}: unsupported benchmark schema")
    accounting = value.get("delivery_accounting")
    if not isinstance(accounting, dict):
        raise BenchmarkContractError(f"{path}: missing delivery accounting")
    expected = accounting.get("expected_fanout")
    accounted = sum(
        int(accounting.get(name, -1)) for name in ("delivered", "explicitly_rejected", "dropped")
    )
    if accounted != expected:
        raise BenchmarkContractError(f"{path}: incomplete delivery accounting")
    if accounting.get("dropped") != 0:
        raise BenchmarkContractError(f"{path}: no-drop workload reported drops")
    return value


def analyze(paths: Sequence[Path], *, min_runs: int, seed: int, resamples: int) -> dict[str, Any]:
    if len(paths) < min_runs:
        raise BenchmarkContractError(
            f"need at least {min_runs} measured runs, received {len(paths)}"
        )
    if resamples < 100:
        raise BenchmarkContractError("bootstrap resamples must be at least 100")

    runs = [_load(path) for path in paths]
    binaries = {str(run["metadata"]["provenance"]["binary_sha256"]) for run in runs}
    workloads = {json.dumps(run["metadata"]["workload"], sort_keys=True) for run in runs}
    if len(binaries) != 1:
        raise BenchmarkContractError("measured runs used different server binaries")
    if len(workloads) != 1:
        raise BenchmarkContractError("measured runs used different workloads")

    grouped: dict[tuple[str, str], dict[str, list[float]]] = defaultdict(
        lambda: {"median_ns": [], "p95_ns": [], "p99_ns": []}
    )
    for run in runs:
        for summary in run["summaries"]:
            dimensions = json.dumps(summary["dimensions"], sort_keys=True)
            key = str(summary["operation"]), dimensions
            for statistic in ("median_ns", "p95_ns", "p99_ns"):
                grouped[key][statistic].append(float(summary[statistic]))

    summaries: list[dict[str, Any]] = []
    for group_index, key in enumerate(sorted(grouped)):
        operation, dimensions = key
        statistics_by_name: dict[str, Any] = {}
        for statistic, values in grouped[key].items():
            mean = statistics.fmean(values)
            deviation = statistics.stdev(values) if len(values) > 1 else 0.0
            lower, upper = _bootstrap_median_ci(
                values,
                seed=seed + group_index * 10 + len(statistics_by_name),
                resamples=resamples,
            )
            statistics_by_name[statistic] = {
                "run_values": values,
                "median": statistics.median(values),
                "mean": mean,
                "coefficient_of_variation": 0.0 if mean == 0 else deviation / mean,
                "bootstrap_median_ci95": [lower, upper],
            }
        summaries.append(
            {
                "operation": operation,
                "dimensions": json.loads(dimensions),
                "statistics": statistics_by_name,
            }
        )

    return {
        "schema": "gorti.production-benchmark-analysis/v1",
        "run_count": len(runs),
        "bootstrap": {"seed": seed, "resamples": resamples},
        "server_binary_sha256": next(iter(binaries)),
        "workload": json.loads(next(iter(workloads))),
        "accounting": {
            "expected_fanout": sum(
                int(run["delivery_accounting"]["expected_fanout"]) for run in runs
            ),
            "delivered": sum(int(run["delivery_accounting"]["delivered"]) for run in runs),
            "explicitly_rejected": sum(
                int(run["delivery_accounting"]["explicitly_rejected"]) for run in runs
            ),
            "dropped": sum(int(run["delivery_accounting"]["dropped"]) for run in runs),
        },
        "summaries": summaries,
    }


def write_analysis(path: Path, report: Mapping[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(report, ensure_ascii=True, allow_nan=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
        newline="\n",
    )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("benchmarks", nargs="+", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--min-runs", type=int, default=20)
    parser.add_argument("--seed", type=int, default=1516)
    parser.add_argument("--resamples", type=int, default=10_000)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    report = analyze(
        [path.resolve() for path in args.benchmarks],
        min_runs=args.min_runs,
        seed=args.seed,
        resamples=args.resamples,
    )
    write_analysis(args.output.resolve(), report)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
