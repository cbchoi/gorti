"""Compare Time Management performance for a reference RTI and gorti.

This pair runner is used when an additional RTI fails the strict semantic screen.
It preserves the same verifier, FOM, seed, process count, logging, and staged
timestamp-order choreography used by compare_three_way.py.
"""

from __future__ import annotations

import argparse
import platform
import statistics
import time
from pathlib import Path
from typing import Any

from compare_three_way import (
    TIME_MANAGEMENT_PAIRED_METRICS,
    bootstrap_median_ci,
    build_go,
    run_checked,
    run_gorti,
    run_reference,
    sha256_file,
    summarize,
    write_json,
)

IMPLEMENTATIONS = ("reference", "gorti")
METRIC_LABELS = {
    "updateAttributeValues": "updateAttributeValues caller",
    "sendInteraction": "sendInteraction caller",
    "timeAdvanceRequest": "TAR caller",
    "timeAdvanceGrantLatency": "TAR-to-TAG",
    "timeAdvanceGrantLatency.publisher": "publisher TAR-to-TAG",
    "timeAdvanceGrantLatency.subscriber": "subscriber TAR-to-TAG",
    "completed_delivery_batch_latency": "completed delivery batch",
}


def schedule_for(index: int) -> tuple[str, str]:
    return IMPLEMENTATIONS if index % 2 == 0 else tuple(reversed(IMPLEMENTATIONS))


def metric_samples(result: dict[str, Any], metric: str) -> list[int]:
    operation, separator, role = metric.partition(".")
    return [
        int(sample["duration_ns"])
        for sample in result["samples"]
        if sample["operation"] == operation
        and (not separator or sample.get("role") == role)
    ]


def aggregate(measured: list[dict[str, Any]]) -> dict[str, Any]:
    return {
        metric: {
            implementation: summarize(
                [
                    value
                    for result in measured
                    if result["implementation"] == implementation
                    for value in metric_samples(result, metric)
                ]
            )
            for implementation in IMPLEMENTATIONS
        }
        for metric in TIME_MANAGEMENT_PAIRED_METRICS
    }


def paired_analysis(measured: list[dict[str, Any]]) -> dict[str, Any]:
    by_pair: dict[int, dict[str, dict[str, Any]]] = {}
    for result in measured:
        by_pair.setdefault(int(result["pair"]), {})[result["implementation"]] = result
    output: dict[str, Any] = {}
    for metric_index, metric in enumerate(TIME_MANAGEMENT_PAIRED_METRICS):
        ratios: list[float] = []
        gorti_before: list[float] = []
        reference_before: list[float] = []
        for pair in sorted(by_pair):
            results = by_pair[pair]
            reference = float(results["reference"]["operation_medians_ns"][metric])
            gorti = float(results["gorti"]["operation_medians_ns"][metric])
            ratio = reference / gorti
            ratios.append(ratio)
            if results["gorti"]["order"].index("gorti") == 0:
                gorti_before.append(ratio)
            else:
                reference_before.append(ratio)
        output[metric] = {
            "count": len(ratios),
            "gorti_advantage_ratio_median": float(statistics.median(ratios)),
            "bootstrap_median_ci95": bootstrap_median_ci(
                ratios, seed=2516 + metric_index
            ),
            "pair_precedence": {
                "gorti_before": len(gorti_before),
                "reference_before": len(reference_before),
            },
            "gorti_before_median": float(statistics.median(gorti_before)),
            "reference_before_median": float(statistics.median(reference_before)),
        }
    return {
        "ratio_interpretation": "above 1.0 favors gorti",
        "experimental_unit": "run-level median paired by measured AB/BA pair",
        "bootstrap": {"seed": 2516, "resamples": 10_000},
        "metrics": output,
    }


def write_markdown(path: Path, comparison: dict[str, Any]) -> None:
    rows = []
    for metric, result in comparison["aggregate"].items():
        reference = result["reference"]["median"]
        gorti = result["gorti"]["median"]
        rows.append(
            f"| {METRIC_LABELS[metric]} | {reference:.0f} | {gorti:.0f} | "
            f"{reference / gorti:.3f}x |"
        )
    paired_rows = []
    for metric, result in comparison["paired_analysis"]["metrics"].items():
        ci = result["bootstrap_median_ci95"]
        paired_rows.append(
            f"| {METRIC_LABELS[metric]} | "
            f"{result['gorti_advantage_ratio_median']:.3f} "
            f"[{ci[0]:.3f}, {ci[1]:.3f}] |"
        )
    path.write_text(
        "\n".join(
            [
                "# IEEE 1516e Time-Management Pair Comparison",
                "",
                "This accepted-performance comparison includes the reference RTI and gorti.",
                "Portico is excluded from latency claims because it failed the strict",
                "timestamped-callback-before-grant semantic screen in the same environment.",
                "",
                "## Protocol",
                "",
                f"- Seed: `{comparison['workload']['seed']}`",
                f"- Count: `{comparison['workload']['count']}`",
                f"- FOM SHA-256: `{comparison['workload']['fom_sha256']}`",
                f"- Warm-up pairs: `{comparison['protocol']['warmup_pairs']}`",
                f"- Measured pairs: `{comparison['protocol']['measured_pairs']}`",
                "- Order: alternating AB/BA; 20 measurements give 10/10 precedence",
                "- Processes: two independent federates per implementation",
                "- Time policy: regulating and constrained, lookahead 1",
                "- Choreography: stage all TSO update/interaction traffic, cross",
                "  VERIFY_MEASURE, then issue lockstep TAR(1..count+1)",
                "- Boundary: callbacks are validated before subscriber TAG",
                "- In-process operation warm-up: 0; complete process pairs are discarded",
                "",
                "## Aggregate Samples",
                "",
                "| Metric (ns, lower) | Reference median | gorti median | gorti advantage |",
                "|---|---:|---:|---:|",
                *rows,
                "",
                "## Paired Analysis",
                "",
                "| Metric | gorti advantage median [95% bootstrap CI] |",
                "|---|---:|",
                *paired_rows,
                "",
                "A ratio above 1.0 favors gorti. Caller latency and TAR-to-TAG latency",
                "are distinct measurements; publisher and subscriber grant paths are also",
                "reported separately.",
                "",
            ]
        ),
        encoding="utf-8",
    )


def resolve_file(path: Path) -> Path:
    resolved = path.resolve()
    if not resolved.is_file():
        raise FileNotFoundError(resolved)
    return resolved


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--fom", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--java", type=Path, required=True)
    parser.add_argument("--go", type=Path, required=True)
    parser.add_argument("--reference-api-jar", type=Path, required=True)
    parser.add_argument("--reference-verifier-jar", type=Path, required=True)
    parser.add_argument("--reference-server-java", type=Path, required=True)
    parser.add_argument("--reference-server-jar", type=Path, required=True)
    parser.add_argument("--reference-home-template", type=Path, required=True)
    parser.add_argument("--reference-server-port", type=int, default=8989)
    parser.add_argument("--reference-server-arg", action="append", default=["-nogui"])
    parser.add_argument("--reference-existing-server", action="store_true")
    parser.add_argument("--seed", default="1516")
    parser.add_argument("--count", type=int, default=100)
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--measured", type=int, default=20)
    parser.add_argument("--timeout-ms", type=int, default=60000)
    parser.add_argument("--gorti-federate-gomaxprocs", type=int, default=0)
    args = parser.parse_args()
    args.mode = "time-management"
    args.operation_warmup = 0
    args.gorti_local_lrc = False
    args.gorti_local_lrc_queue = 1024
    args.gorti_local_lrc_ack_every = 32
    args.repo = args.repo.resolve()
    args.output = args.output.resolve()
    args.reference_home_template = args.reference_home_template.resolve()
    if not args.reference_home_template.is_dir():
        raise FileNotFoundError(args.reference_home_template)
    for name in (
        "fom", "java", "go", "reference_api_jar", "reference_verifier_jar",
        "reference_server_java", "reference_server_jar",
    ):
        setattr(args, name, resolve_file(getattr(args, name)))
    if args.output.exists():
        raise FileExistsError(args.output)
    if args.count < 1 or args.warmup < 0 or args.measured < 2 or args.measured % 2:
        raise ValueError("count must be positive and measured pairs must be positive and even")
    return args


def main() -> int:
    args = parse_args()
    args.output.mkdir(parents=True)
    build_go(args, args.output)
    runners = {"reference": run_reference, "gorti": run_gorti}
    measured: list[dict[str, Any]] = []
    records: list[dict[str, Any]] = []
    for run_index in range(args.warmup + args.measured):
        phase = "warmup" if run_index < args.warmup else "measured"
        phase_index = run_index if phase == "warmup" else run_index - args.warmup
        order = schedule_for(phase_index)
        print(f"[{run_index + 1}/{args.warmup + args.measured}] {phase}: {' -> '.join(order)}", flush=True)
        pair_results = {}
        for implementation in order:
            output = args.output / "runs" / phase / f"{phase_index + 1:02d}-{implementation}"
            started = time.time()
            result = runners[implementation](
                args, output, f"{phase}-{phase_index + 1:02d}-{implementation}"
            )
            pair_results[implementation] = result
            records.append({
                "implementation": implementation,
                "phase": phase,
                "pair": phase_index + 1,
                "order": list(order),
                "wall_seconds": time.time() - started,
                "output": str(output),
            })
            if phase == "measured":
                result.update({"pair": phase_index + 1, "order": list(order)})
                measured.append(result)
        if len({item["payload_projection_sha256"] for item in pair_results.values()}) != 1:
            raise ValueError(f"{phase} pair {phase_index + 1}: payload semantics differ")
        if len({item["behavior_projection_sha256"] for item in pair_results.values()}) != 1:
            raise ValueError(f"{phase} pair {phase_index + 1}: behavior semantics differ")

    comparison = {
        "schema": "gorti.time-management-pair-comparison/v1",
        "metadata": {
            "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "host": platform.node(),
            "host_os": platform.platform(),
            "gorti_commit": run_checked(["git", "rev-parse", "HEAD"], args.repo),
            "reference_api_sha256": sha256_file(args.reference_api_jar),
            "reference_server_jar_sha256": sha256_file(args.reference_server_jar),
            "reference_verifier_sha256": sha256_file(args.reference_verifier_jar),
            "gorti_rtid_sha256": sha256_file(args.gorti_rtid),
            "gorti_client_sha256": sha256_file(args.gorti_client),
        },
        "workload": {
            "seed": args.seed,
            "count": args.count,
            "fom_sha256": sha256_file(args.fom),
            "fom_bytes": args.fom.stat().st_size,
            "lookahead": 1,
            "choreography": "stage_tso_then_lockstep_tar",
        },
        "protocol": {
            "warmup_pairs": args.warmup,
            "measured_pairs": args.measured,
            "ordering": "alternating AB/BA",
            "independent_federate_processes": 2,
            "reference_server_lifecycle": "existing_persistent"
            if args.reference_existing_server else "managed_per_arm",
        },
        "aggregate": aggregate(measured),
        "paired_analysis": paired_analysis(measured),
        "runs": records,
    }
    write_json(args.output / "comparison.json", comparison)
    write_markdown(args.output / "comparison.md", comparison)
    print(f"PASS: {args.output / 'comparison.md'}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
