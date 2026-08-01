"""Compare a local reference IEEE 1516e RTI, Portico, and gorti on one host."""

# Commands are assembled as argument vectors from caller-supplied local paths.
# ruff: noqa: S603

from __future__ import annotations

import argparse
import json
import math
import os
import platform
import random
import shutil
import socket
import statistics
import subprocess
import sys
import time
import uuid
from collections import Counter, defaultdict
from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any, TextIO

sys.path.insert(0, str(Path(__file__).resolve().parent))
from compare_receive_order import (  # noqa: E402
    OPERATIONS,
    read_ndjson,
    sha256_file,
    sha256_json,
    validate_run,
    write_json,
)

VERIFIER_CLASS = "gorti.verification.commercialrti.CommercialRtiVerifier"
IMPLEMENTATIONS = ("reference", "portico", "gorti")
MODES = ("receive-order", "time-management")
TIME_MANAGEMENT_OPERATIONS = (
    "updateAttributeValues",
    "sendInteraction",
    "timeAdvanceRequest",
    "timeAdvanceGrantLatency",
    "completed_delivery_batch_latency",
)
SCHEDULE = (
    ("reference", "portico", "gorti"),
    ("gorti", "portico", "reference"),
    ("portico", "gorti", "reference"),
    ("reference", "gorti", "portico"),
    ("gorti", "reference", "portico"),
    ("portico", "reference", "gorti"),
)


@dataclass
class RunningProcess:
    process: subprocess.Popen[str]
    stdout: TextIO
    stderr: TextIO


def start_process(
    command: Sequence[str],
    *,
    cwd: Path,
    stdout_path: Path,
    stderr_path: Path,
    env: dict[str, str] | None = None,
) -> RunningProcess:
    stdout = stdout_path.open("w", encoding="utf-8")
    stderr = stderr_path.open("w", encoding="utf-8")
    try:
        process = subprocess.Popen(
            list(command),
            cwd=cwd,
            env=env,
            text=True,
            encoding="utf-8",
            errors="replace",
            stdout=stdout,
            stderr=stderr,
        )
    except BaseException:
        stdout.close()
        stderr.close()
        raise
    return RunningProcess(process=process, stdout=stdout, stderr=stderr)


def close_process(item: RunningProcess) -> None:
    item.stdout.close()
    item.stderr.close()


def stop_process(item: RunningProcess) -> None:
    if item.process.poll() is None:
        item.process.terminate()
        try:
            item.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            item.process.kill()
            item.process.wait(timeout=5)
    close_process(item)


def wait_processes(items: Sequence[RunningProcess], timeout_seconds: float) -> None:
    deadline = time.monotonic() + timeout_seconds
    failure: str | None = None
    while any(item.process.poll() is None for item in items):
        failed = next(
            (item for item in items if item.process.poll() not in (None, 0)),
            None,
        )
        if failed is not None:
            failure = f"process exited with code {failed.process.returncode}"
            break
        if time.monotonic() >= deadline:
            failure = f"processes exceeded {timeout_seconds:.1f} seconds"
            break
        time.sleep(0.05)
    if failure:
        for item in items:
            if item.process.poll() is None:
                item.process.kill()
    for item in items:
        item.process.wait(timeout=10)
        close_process(item)
    return_codes = [int(item.process.returncode or 0) for item in items]
    if failure or any(return_codes):
        raise RuntimeError(f"{failure or 'federate failure'}; exit codes={return_codes}")


def wait_for_port(process: RunningProcess, port: int, timeout_seconds: float) -> None:
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if process.process.poll() is not None:
            raise RuntimeError(f"server exited with code {process.process.returncode}")
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                return
        except OSError:
            time.sleep(0.05)
    raise TimeoutError(f"server did not open port {port}")


def wait_for_join(path: Path, process: RunningProcess, timeout_seconds: float) -> None:
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if path.is_file() and '"event":"joined"' in path.read_text(
            encoding="utf-8", errors="replace"
        ):
            return
        if process.process.poll() is not None:
            raise RuntimeError("publisher exited before joining")
        time.sleep(0.05)
    raise TimeoutError("publisher did not join before the timeout")


def free_port() -> int:
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return int(listener.getsockname()[1])


def selected_mode(args: argparse.Namespace) -> str:
    return str(getattr(args, "mode", "receive-order"))


def configure_mode(args: argparse.Namespace) -> None:
    mode = selected_mode(args)
    if mode not in MODES:
        raise ValueError(f"unsupported mode: {mode}")
    if args.operation_warmup is None:
        args.operation_warmup = 128 if mode == "receive-order" else 0
    if mode == "time-management":
        if args.operation_warmup != 0:
            raise ValueError("time-management mode requires --operation-warmup=0")
        if args.gorti_local_lrc:
            raise ValueError("time-management mode forbids --gorti-local-lrc")


def federation_name(implementation: str, mode: str, run_id: str) -> str:
    prefixes = {
        ("reference", "receive-order"): "ReferenceRO",
        ("portico", "receive-order"): "PorticoRO",
        ("gorti", "receive-order"): "GR",
        ("reference", "time-management"): "ReferenceTM",
        ("portico", "time-management"): "PorticoTM",
        ("gorti", "time-management"): "GTM",
    }
    prefix = prefixes[(implementation, mode)]
    if implementation == "gorti":
        return f"{prefix}-{run_id[:12]}-{uuid.uuid4().hex[:6]}"
    return f"{prefix}-{run_id}-{uuid.uuid4().hex[:8]}"


def java_federate_command(
    java: Path,
    classpath: Sequence[Path],
    role: str,
    fom: Path,
    output: Path,
    federation: str,
    seed: str,
    count: int,
    operation_warmup: int,
    timeout_ms: int,
    *,
    all_federates_sync: bool,
    receive_order: bool = True,
    allow_grant_before_callbacks: bool = False,
    tm_advance_only: bool = False,
    teardown_ready_file: Path | None = None,
    java_options: Sequence[str] = (),
) -> list[str]:
    command = [
        str(java),
        *java_options,
        "-cp",
        os.pathsep.join(str(path) for path in classpath),
        VERIFIER_CLASS,
        "--role",
        role,
        "--seed",
        seed,
        "--count",
        str(count),
        "--operation-warmup",
        str(operation_warmup),
        "--all-federates-sync",
        str(all_federates_sync).lower(),
        "--receive-order",
        str(receive_order).lower(),
        "--federation",
        federation,
        "--fom",
        str(fom),
        "--output",
        str(output),
        "--timeout-ms",
        str(timeout_ms),
    ]
    if teardown_ready_file is not None:
        command.extend(["--teardown-ready-file", str(teardown_ready_file)])
    if allow_grant_before_callbacks:
        command.extend(["--allow-grant-before-callbacks", "true"])
    if tm_advance_only:
        command.extend(["--tm-advance-only", "true"])
    return command


def run_reference(
    args: argparse.Namespace, output: Path, run_id: str
) -> dict[str, Any]:
    output.mkdir(parents=True, exist_ok=False)
    runtime_home = output / "runtime-home"
    shutil.copytree(args.reference_home_template, runtime_home)
    server = start_process(
        [
            str(args.reference_server_java),
            f"-Duser.home={runtime_home}",
            "-jar",
            str(args.reference_server_jar),
            *args.reference_server_arg,
        ],
        cwd=runtime_home,
        stdout_path=output / "server.stdout.log",
        stderr_path=output / "server.stderr.log",
    )
    clients: list[RunningProcess] = []
    try:
        wait_for_port(server, args.reference_server_port, args.timeout_ms / 1000)
        federation = f"ReferenceRO-{run_id}-{uuid.uuid4().hex[:8]}"
        java_options = [f"-Duser.home={runtime_home}"]
        for role in ("subscriber", "publisher"):
            clients.append(
                start_process(
                    java_federate_command(
                        args.java,
                        (args.reference_verifier_jar, args.reference_api_jar),
                        role,
                        args.fom,
                        output,
                        federation,
                        args.seed,
                        args.count,
                        args.operation_warmup,
                        args.timeout_ms,
                        all_federates_sync=False,
                        java_options=java_options,
                    ),
                    cwd=args.repo,
                    stdout_path=output / f"{role}.stdout.log",
                    stderr_path=output / f"{role}.stderr.log",
                )
            )
            if role == "subscriber":
                time.sleep(0.1)
        wait_processes(clients, args.timeout_ms / 1000 * 3)
        clients.clear()
    finally:
        for client in clients:
            stop_process(client)
        stop_process(server)
    return validate_run(
        output, "reference", args.count, operation_warmup=args.operation_warmup
    )


def run_portico(args: argparse.Namespace, output: Path, run_id: str) -> dict[str, Any]:
    output.mkdir(parents=True, exist_ok=False)
    publisher_port = free_port()
    subscriber_port = free_port()
    while subscriber_port == publisher_port:
        subscriber_port = free_port()
    mode = selected_mode(args)
    federation = federation_name("portico", mode, run_id)
    teardown_ready_file = output.parent / f".{output.name}-subscriber-resigned"
    classpath = (args.portico_override_jar, args.portico_verifier_jar, args.portico_jar)
    common_options = [
        "-Dportico.loglevel=OFF",
        "-Dportico.jgroups.tcp.bindAddress=127.0.0.1",
        f"-Dportico.jgroups.tcp.initialHosts=127.0.0.1[{publisher_port}]",
    ]
    env = os.environ.copy()
    env["RTI_RID_FILE"] = str(args.portico_rid)
    clients: list[RunningProcess] = []
    try:
        publisher = start_process(
            java_federate_command(
                args.java,
                classpath,
                "publisher",
                args.fom,
                output,
                federation,
                args.seed,
                args.count,
                args.operation_warmup,
                args.timeout_ms,
                all_federates_sync=True,
                receive_order=mode == "receive-order",
                allow_grant_before_callbacks=mode == "time-management",
                tm_advance_only=mode == "time-management",
                teardown_ready_file=teardown_ready_file,
                java_options=[*common_options, f"-Dportico.jgroups.tcp.bindPort={publisher_port}"],
            ),
            cwd=args.repo,
            env=env,
            stdout_path=output / "publisher.stdout.log",
            stderr_path=output / "publisher.stderr.log",
        )
        clients.append(publisher)
        wait_for_join(output / "publisher-semantic.ndjson", publisher, args.timeout_ms / 1000)
        clients.append(
            start_process(
                java_federate_command(
                    args.java,
                    classpath,
                    "subscriber",
                    args.fom,
                    output,
                    federation,
                    args.seed,
                    args.count,
                    args.operation_warmup,
                    args.timeout_ms,
                    all_federates_sync=True,
                    receive_order=mode == "receive-order",
                    allow_grant_before_callbacks=mode == "time-management",
                    tm_advance_only=mode == "time-management",
                    teardown_ready_file=teardown_ready_file,
                    java_options=[
                        *common_options,
                        f"-Dportico.jgroups.tcp.bindPort={subscriber_port}",
                    ],
                ),
                cwd=args.repo,
                env=env,
                stdout_path=output / "subscriber.stdout.log",
                stderr_path=output / "subscriber.stderr.log",
            )
        )
        wait_processes(clients, args.timeout_ms / 1000 * 3)
        clients.clear()
    finally:
        for client in clients:
            stop_process(client)
    return validate_run(
        output, "portico", args.count, operation_warmup=args.operation_warmup
    )


def run_gorti(args: argparse.Namespace, output: Path, run_id: str) -> dict[str, Any]:
    output.mkdir(parents=True, exist_ok=False)
    save_directory = output / "rtid-saves"
    save_directory.mkdir()
    port = free_port()
    address = f"127.0.0.1:{port}"
    server = start_process(
        [
            str(args.gorti_rtid),
            f"--listen={address}",
            "--metrics-listen=127.0.0.1:0",
            "--admin-listen=",
            f"--save-dir={save_directory}",
            "--log-dir=",
            "--log-format=text",
        ],
        cwd=args.repo,
        stdout_path=output / "rtid.stdout.log",
        stderr_path=output / "rtid.stderr.log",
    )
    clients: list[RunningProcess] = []
    client_env = os.environ.copy()
    if args.gorti_federate_gomaxprocs > 0:
        client_env["GOMAXPROCS"] = str(args.gorti_federate_gomaxprocs)
    else:
        client_env.pop("GOMAXPROCS", None)
    try:
        wait_for_port(server, port, args.timeout_ms / 1000)
        federation = f"GR-{run_id[:12]}-{uuid.uuid4().hex[:6]}"
        common = [
            str(args.gorti_client),
            f"--address={address}",
            f"--federation={federation}",
            f"--fom={args.fom}",
            f"--seed={args.seed}",
            f"--count={args.count}",
            f"--operation-warmup={args.operation_warmup}",
            f"--output={output}",
            f"--timeout={args.timeout_ms}ms",
            "--receive-order=true",
        ]
        if args.gorti_local_lrc:
            common.extend(
                [
                    "--local-lrc=true",
                    f"--local-lrc-queue={args.gorti_local_lrc_queue}",
                    f"--local-lrc-ack-every={args.gorti_local_lrc_ack_every}",
                ]
            )
        for role in ("subscriber", "publisher"):
            clients.append(
                start_process(
                    [*common, f"--role={role}"],
                    cwd=args.repo,
                    env=client_env,
                    stdout_path=output / f"{role}.stdout.log",
                    stderr_path=output / f"{role}.stderr.log",
                )
            )
        wait_processes(clients, args.timeout_ms / 1000 * 3)
        clients.clear()
    finally:
        for client in clients:
            stop_process(client)
        stop_process(server)
    return validate_run(
        output,
        "gorti",
        args.count,
        local_lrc=args.gorti_local_lrc,
        operation_warmup=args.operation_warmup,
    )


def nearest_rank(values: Iterable[int], percentile: int) -> int:
    ordered = sorted(values)
    rank = max(0, math.ceil(percentile / 100 * len(ordered)) - 1)
    return ordered[rank]


def summarize(values: list[int | float]) -> dict[str, float | int]:
    if not values:
        raise ValueError("cannot summarize an empty sample set")
    return {
        "count": len(values),
        "median": float(statistics.median(values)),
        "mean": float(statistics.fmean(values)),
        "p95": float(nearest_rank([int(value) for value in values], 95)),
        "min": float(min(values)),
        "max": float(max(values)),
    }


def aggregate(
    measured: Sequence[dict[str, Any]],
    mode: str = "receive-order",
) -> dict[str, Any]:
    operations: dict[str, dict[str, list[int]]] = {
        implementation: defaultdict(list) for implementation in IMPLEMENTATIONS
    }
    for result in measured:
        implementation = result["implementation"]
        for sample in result["samples"]:
            operations[implementation][sample["operation"]].append(
                int(sample["duration_ns"])
            )

    if mode == "time-management":
        return {
            "operations_ns": {
                operation: {
                    implementation: summarize(operations[implementation][operation])
                    for implementation in IMPLEMENTATIONS
                }
                for operation in TIME_MANAGEMENT_OPERATIONS[:2]
            },
            "time_management": {
                "tar_caller_ns": {
                    implementation: summarize(
                        operations[implementation]["timeAdvanceRequest"]
                    )
                    for implementation in IMPLEMENTATIONS
                },
                "tar_to_tag_ns": {
                    implementation: summarize(
                        operations[implementation]["timeAdvanceGrantLatency"]
                    )
                    for implementation in IMPLEMENTATIONS
                },
            },
            "delivery": {
                "completed_delivery_batch_ns": {
                    implementation: summarize(
                        operations[implementation]["completed_delivery_batch_latency"]
                    )
                    for implementation in IMPLEMENTATIONS
                }
            },
        }
    if mode != "receive-order":
        raise ValueError(f"unsupported mode: {mode}")

    throughput: dict[str, list[float]] = {implementation: [] for implementation in IMPLEMENTATIONS}
    batch: dict[str, list[int]] = {implementation: [] for implementation in IMPLEMENTATIONS}
    for result in measured:
        implementation = result["implementation"]
        throughput[implementation].append(float(result["throughput_deliveries_per_second"]))
        batch[implementation].append(int(result["batch_ns"]))

    return {
        "operations_ns": {
            operation: {
                implementation: summarize(operations[implementation][operation])
                for implementation in IMPLEMENTATIONS
            }
            for operation in OPERATIONS[:2]
        },
        "delivery": {
            "completed_batch_ns": {
                implementation: summarize(batch[implementation])
                for implementation in IMPLEMENTATIONS
            },
            "throughput_deliveries_per_second": {
                implementation: summarize(throughput[implementation])
                for implementation in IMPLEMENTATIONS
            },
        },
    }


def bootstrap_median_ci(
    values: Sequence[float], *, seed: int, resamples: int = 10_000
) -> list[float]:
    rng = random.Random(seed)  # noqa: S311 - deterministic statistical resampling
    medians = sorted(
        statistics.median(rng.choices(values, k=len(values))) for _ in range(resamples)
    )
    return [
        medians[max(0, math.ceil(0.025 * len(medians)) - 1)],
        medians[max(0, math.ceil(0.975 * len(medians)) - 1)],
    ]


def paired_analysis(
    measured: Sequence[dict[str, Any]],
    mode: str = "receive-order",
) -> dict[str, Any]:
    by_trio: dict[int, dict[str, dict[str, Any]]] = defaultdict(dict)
    for result in measured:
        by_trio[int(result["trio"])][result["implementation"]] = result
    if any(set(results) != set(IMPLEMENTATIONS) for results in by_trio.values()):
        raise ValueError("paired analysis requires one result per implementation and trio")

    if mode == "time-management":
        metrics = {operation: False for operation in TIME_MANAGEMENT_OPERATIONS}
    elif mode == "receive-order":
        metrics = {
            "updateAttributeValues": False,
            "sendInteraction": False,
            "completedReceiveOrderBatch": False,
            "callbackThroughput": True,
        }
    else:
        raise ValueError(f"unsupported mode: {mode}")

    def metric_value(result: dict[str, Any], metric: str) -> float:
        if mode == "time-management":
            return float(result["operation_medians_ns"][metric])
        if metric in OPERATIONS[:2]:
            return float(result["operation_medians_ns"][metric])
        if metric == "completedReceiveOrderBatch":
            return float(result["batch_ns"])
        return float(result["throughput_deliveries_per_second"])

    output: dict[str, Any] = {}
    for metric_index, (metric, higher_is_better) in enumerate(metrics.items()):
        comparisons: dict[str, Any] = {}
        for comparator_index, comparator in enumerate(("reference", "portico")):
            ratios: list[float] = []
            gorti_before: list[float] = []
            comparator_before: list[float] = []
            for trio in sorted(by_trio):
                results = by_trio[trio]
                gorti = metric_value(results["gorti"], metric)
                other = metric_value(results[comparator], metric)
                ratio = gorti / other if higher_is_better else other / gorti
                ratios.append(ratio)
                order = results["gorti"]["order"]
                if order.index("gorti") < order.index(comparator):
                    gorti_before.append(ratio)
                else:
                    comparator_before.append(ratio)
            before_median = statistics.median(gorti_before)
            after_median = statistics.median(comparator_before)
            overall = statistics.median(ratios)
            comparisons[comparator] = {
                "count": len(ratios),
                "gorti_advantage_ratio_median": overall,
                "bootstrap_median_ci95": bootstrap_median_ci(
                    ratios,
                    seed=1516 + metric_index * 10 + comparator_index,
                ),
                "pair_precedence": {
                    "gorti_before": len(gorti_before),
                    "comparator_before": len(comparator_before),
                },
                "gorti_before_median": before_median,
                "comparator_before_median": after_median,
                "order_effect_fraction": abs(before_median - after_median) / overall,
            }
        output[metric] = comparisons
    return {
        "ratio_interpretation": "above 1.0 favors gorti",
        "experimental_unit": "run-level median paired by measured trio",
        "bootstrap": {"seed": 1516, "resamples": 10_000},
        "metrics": output,
    }


def write_time_management_markdown(path: Path, comparison: dict[str, Any]) -> None:
    aggregate_result = comparison["aggregate"]
    metric_rows: list[tuple[str, dict[str, Any]]] = [
        (
            "updateAttributeValues caller latency",
            aggregate_result["operations_ns"]["updateAttributeValues"],
        ),
        (
            "sendInteraction caller latency",
            aggregate_result["operations_ns"]["sendInteraction"],
        ),
        ("TAR caller latency", aggregate_result["time_management"]["tar_caller_ns"]),
        ("TAR-to-TAG latency", aggregate_result["time_management"]["tar_to_tag_ns"]),
        (
            "completed delivery batch latency",
            aggregate_result["delivery"]["completed_delivery_batch_ns"],
        ),
    ]
    rows = [
        f"| {label} | ns, lower | {result['reference']['median']:.0f} | "
        f"{result['portico']['median']:.0f} | {result['gorti']['median']:.0f} | "
        f"{result['reference']['median'] / result['gorti']['median']:.2f}x | "
        f"{result['portico']['median'] / result['gorti']['median']:.2f}x |"
        for label, result in metric_rows
    ]
    paired_rows = []
    for metric, result in comparison["paired_analysis"]["metrics"].items():
        reference = result["reference"]
        portico = result["portico"]
        paired_rows.append(
            f"| {metric} | {reference['gorti_advantage_ratio_median']:.3f} "
            f"[{reference['bootstrap_median_ci95'][0]:.3f}, "
            f"{reference['bootstrap_median_ci95'][1]:.3f}] | "
            f"{portico['gorti_advantage_ratio_median']:.3f} "
            f"[{portico['bootstrap_median_ci95'][0]:.3f}, "
            f"{portico['bootstrap_median_ci95'][1]:.3f}] |"
        )
    fairness = comparison["protocol"]["fairness"]
    measured_fairness = fairness["measured"]
    permutation_text = ", ".join(
        f"{permutation}={runs}"
        for permutation, runs in measured_fairness["permutation_counts"].items()
    )
    path.write_text(
        "\n".join(
            [
                "# Three-way IEEE 1516e Time-Management Comparison",
                "",
                "This local report compares a licensed reference RTI, Portico, and gorti.",
                "It covers FM, DM, timestamp-order OM, and Time Management semantics.",
                "",
                "## Protocol",
                "",
                f"- Seed: `{comparison['workload']['seed']}`",
                f"- Count per channel: `{comparison['workload']['count']}`",
                "- In-process operation warm-up: `0`",
                f"- FOM SHA-256: `{comparison['workload']['fom_sha256']}`",
                f"- Warm-up trios: `{comparison['protocol']['warmup_trios']}`",
                f"- Measured trios: `{comparison['protocol']['measured_trios']}`",
                "- Execution order: deterministic six-permutation rotation",
                f"- Measured permutation counts: `{permutation_text}`",
                "- Environment: same Windows host, same Java executable, two federate processes",
                "- Time policy: regulation and constrained enabled with lookahead 1",
                "- Grant policy: each federate validates logical times 1 through count+1",
                "- Delivery policy: both timestamped callbacks complete before each subscriber grant",
                "- Java and Go verifier receive-order setting: `false`",
                "- gorti transport mode: `standard`; LocalLRC is forbidden",
                "",
                "## Results",
                "",
                "| Metric | Unit/direction | Reference median | Portico median | gorti median | "
                "gorti/reference | gorti/Portico |",
                "|---|---:|---:|---:|---:|---:|---:|",
                *rows,
                "",
                "A ratio above 1.0 favors gorti. TAR caller, TAR-to-TAG, and delivery batch",
                "latencies use distinct samples and boundaries.",
                "",
                "## Paired Run Analysis",
                "",
                "| Metric | gorti/reference median ratio [95% CI] | "
                "gorti/Portico median ratio [95% CI] |",
                "|---|---:|---:|",
                *paired_rows,
                "",
                "Ratios use each run's median, pair implementations by measured trio, and use",
                "10,000 deterministic bootstrap resamples. Detailed order effects and all",
                "six-permutation fairness metadata are in `comparison.json`.",
                "",
                "## Functional Result",
                "",
                "Every accepted trio used the same FOM and seed in two independent federate",
                "processes. Each arm enabled regulation and constrained time, received exact",
                "grants 1..count+1, and passed callback-before-grant delivery checks.",
                "",
                "## Scope",
                "",
                "Server topology remains part of the measured product architecture. TAR caller",
                "latency measures service return, TAR-to-TAG measures the requested advance to",
                "its grant, and completed delivery batch measures subscriber callback completion.",
                "",
            ]
        ),
        encoding="utf-8",
    )


def write_markdown(path: Path, comparison: dict[str, Any]) -> None:
    if comparison["workload"].get("mode", "receive-order") == "time-management":
        write_time_management_markdown(path, comparison)
        return
    aggregate_result = comparison["aggregate"]
    gorti_transport = (
        "LocalLRC queue admission"
        if comparison["workload"]["gorti_local_lrc"]
        else "standard confirmed call"
    )
    rows: list[str] = []
    for operation, result in aggregate_result["operations_ns"].items():
        rows.append(
            f"| {operation} caller latency | ns, lower | "
            f"{result['reference']['median']:.0f} | {result['portico']['median']:.0f} | "
            f"{result['gorti']['median']:.0f} | "
            f"{result['reference']['median'] / result['gorti']['median']:.2f}x | "
            f"{result['portico']['median'] / result['gorti']['median']:.2f}x |"
        )
    batch = aggregate_result["delivery"]["completed_batch_ns"]
    rows.append(
        f"| completed two-channel batch | ns, lower | {batch['reference']['median']:.0f} | "
        f"{batch['portico']['median']:.0f} | {batch['gorti']['median']:.0f} | "
        f"{batch['reference']['median'] / batch['gorti']['median']:.2f}x | "
        f"{batch['portico']['median'] / batch['gorti']['median']:.2f}x |"
    )
    throughput = aggregate_result["delivery"]["throughput_deliveries_per_second"]
    rows.append(
        "| callback throughput | deliveries/s, higher | "
        f"{throughput['reference']['median']:.0f} | {throughput['portico']['median']:.0f} | "
        f"{throughput['gorti']['median']:.0f} | "
        f"{throughput['gorti']['median'] / throughput['reference']['median']:.2f}x | "
        f"{throughput['gorti']['median'] / throughput['portico']['median']:.2f}x |"
    )
    paired_rows = []
    for metric, result in comparison["paired_analysis"]["metrics"].items():
        reference = result["reference"]
        portico = result["portico"]
        paired_rows.append(
            f"| {metric} | {reference['gorti_advantage_ratio_median']:.3f} "
            f"[{reference['bootstrap_median_ci95'][0]:.3f}, "
            f"{reference['bootstrap_median_ci95'][1]:.3f}] | "
            f"{portico['gorti_advantage_ratio_median']:.3f} "
            f"[{portico['bootstrap_median_ci95'][0]:.3f}, "
            f"{portico['bootstrap_median_ci95'][1]:.3f}] |"
        )
    precedence_metric = comparison["paired_analysis"]["metrics"]["completedReceiveOrderBatch"]
    reference_precedence = precedence_metric["reference"]["pair_precedence"]
    portico_precedence = precedence_metric["portico"]["pair_precedence"]
    precedence_text = (
        "Pair precedence (gorti before/comparator before): "
        f"reference {reference_precedence['gorti_before']}/"
        f"{reference_precedence['comparator_before']}, Portico "
        f"{portico_precedence['gorti_before']}/"
        f"{portico_precedence['comparator_before']}."
    )
    path.write_text(
        "\n".join(
            [
                "# Three-way IEEE 1516e Receive-Order Comparison",
                "",
                "This local report compares a licensed reference RTI, Portico, and gorti.",
                "It covers FM, DM, and receive-order OM semantics; TM is excluded.",
                "",
                "## Protocol",
                "",
                f"- Seed: `{comparison['workload']['seed']}`",
                f"- Count per channel: `{comparison['workload']['count']}`",
                f"- In-process operation warm-up per channel: `{comparison['workload']['operation_warmup']}`",
                f"- FOM SHA-256: `{comparison['workload']['fom_sha256']}`",
                f"- Warm-up trios: `{comparison['protocol']['warmup_trios']}`",
                f"- Measured trios: `{comparison['protocol']['measured_trios']}`",
                "- Execution order: deterministic six-permutation rotation; measured pair "
                "precedence is recorded below",
                "- Environment: same Windows host, same Java executable, two federate processes",
                "- Payload construction and HLA encoding: before VERIFY_READY for all arms",
                "- Measurement starts after validated warm-up callbacks and VERIFY_MEASURE",
                "- Delivery boundary: after payload and timestamp validation",
                "- Verifier logging enabled identically; implementation logging disabled",
                f"- gorti transport mode: `{gorti_transport}`",
                "- gorti federate GOMAXPROCS: "
                f"`{comparison['workload']['gorti_federate_gomaxprocs']}`",
                "- gorti LocalLRC queue/ACK interval: "
                f"`{comparison['workload']['gorti_local_lrc_queue']}`/"
                f"`{comparison['workload']['gorti_local_lrc_ack_every']}`",
                "",
                "## Results",
                "",
                "| Metric | Unit/direction | Reference median | Portico median | gorti median | "
                "gorti/reference | gorti/Portico |",
                "|---|---:|---:|---:|---:|---:|---:|",
                *rows,
                "",
                "A ratio above 1.0 favors gorti.",
                "",
                "## Paired Run Analysis",
                "",
                "| Metric | gorti/reference median ratio [95% CI] | gorti/Portico median ratio [95% CI] |",
                "|---|---:|---:|",
                *paired_rows,
                "",
                "Ratios use each run's median, pair implementations by measured trio, and use",
                f"10,000 deterministic bootstrap resamples. {precedence_text}",
                "Detailed order effects are in `comparison.json`.",
                "",
                "## Functional Result",
                "",
                "Every accepted trio had identical FM/DM/OM feature summaries and complete ordered",
                "attribute and interaction payload projections. Each arm passed object lifecycle,",
                "publish/subscribe, synchronization, receive-order update, and interaction checks.",
                "",
                "## Scope",
                "",
                "Server topology remains part of the measured product architecture. The reference",
                "RTI and gorti use separate server processes; Portico coordinates peer LRCs.",
                "When LocalLRC is enabled, the gorti caller rows are Queue* local-admission",
                "samples and are not like-for-like confirmed-call latency claims. Completed batch",
                "retains the common validated subscriber boundary. Callback throughput is its",
                "inverse, not an independent measurement.",
                "Pair precedence and complete-permutation counts depend on the measured trio",
                "count; the observed pair precedence is reported above.",
                "This result does not establish Time Management equivalence or performance.",
                "",
            ]
        ),
        encoding="utf-8",
    )


def schedule_for(index: int) -> tuple[str, str, str]:
    return SCHEDULE[index % len(SCHEDULE)]


def fairness_metadata(warmup: int, measured: int) -> dict[str, Any]:
    def summarize_phase(trios: int) -> dict[str, Any]:
        orders = [schedule_for(index) for index in range(trios)]
        permutation_counts = {
            " > ".join(permutation): orders.count(permutation) for permutation in SCHEDULE
        }
        pair_precedence: dict[str, dict[str, int]] = {}
        for first_index, first in enumerate(IMPLEMENTATIONS):
            for second in IMPLEMENTATIONS[first_index + 1 :]:
                first_before = sum(order.index(first) < order.index(second) for order in orders)
                pair_precedence[f"{first}|{second}"] = {
                    f"{first}_before_{second}": first_before,
                    f"{second}_before_{first}": trios - first_before,
                }
        return {
            "trios": trios,
            "complete_six_permutation_cycles": trios // len(SCHEDULE),
            "rotation_remainder": trios % len(SCHEDULE),
            "permutation_counts": permutation_counts,
            "pair_precedence": pair_precedence,
        }

    return {
        "design": "deterministic six-permutation rotation",
        "permutations": [list(permutation) for permutation in SCHEDULE],
        "warmup": summarize_phase(warmup),
        "measured": summarize_phase(measured),
    }


def run_checked(command: Sequence[str], cwd: Path, env: dict[str, str] | None = None) -> str:
    completed = subprocess.run(
        list(command),
        cwd=cwd,
        env=env,
        text=True,
        encoding="utf-8",
        errors="replace",
        capture_output=True,
        check=False,
    )
    if completed.returncode:
        raise RuntimeError(completed.stderr or completed.stdout)
    return completed.stdout.strip() or completed.stderr.strip()


def build_go(args: argparse.Namespace, output: Path) -> None:
    binary_directory = output / "bin"
    binary_directory.mkdir()
    args.gorti_rtid = binary_directory / "rtid.exe"
    args.gorti_client = binary_directory / "gorti-go-fair.exe"
    env = os.environ.copy()
    env["GOCACHE"] = str(output / "go-cache")
    env.setdefault("GOMODCACHE", str(args.repo / ".tools" / "go-mod"))
    Path(env["GOCACHE"]).mkdir()
    run_checked(
        [str(args.go), "build", "-trimpath", "-o", str(args.gorti_rtid), "./rti/cmd/rtid"],
        args.repo,
        env,
    )
    run_checked(
        [
            str(args.go),
            "build",
            "-trimpath",
            "-o",
            str(args.gorti_client),
            "./verification/gorti-go-fair",
        ],
        args.repo,
        env,
    )


def resolve_file(path: Path) -> Path:
    resolved = path.resolve()
    if not resolved.is_file():
        raise FileNotFoundError(resolved)
    return resolved


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=MODES, default="receive-order")
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
    parser.add_argument("--portico-jar", type=Path, required=True)
    parser.add_argument("--portico-verifier-jar", type=Path, required=True)
    parser.add_argument("--portico-override-jar", type=Path, required=True)
    parser.add_argument("--portico-rid", type=Path, required=True)
    parser.add_argument("--seed", default="1516")
    parser.add_argument("--count", type=int, default=1000)
    parser.add_argument("--operation-warmup", type=int)
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--measured", type=int, default=20)
    parser.add_argument("--timeout-ms", type=int, default=30000)
    parser.add_argument("--gorti-local-lrc", action="store_true")
    parser.add_argument("--gorti-local-lrc-queue", type=int, default=1024)
    parser.add_argument("--gorti-local-lrc-ack-every", type=int, default=32)
    parser.add_argument("--gorti-federate-gomaxprocs", type=int, default=0)
    args = parser.parse_args()
    configure_mode(args)
    args.repo = args.repo.resolve()
    args.output = args.output.resolve()
    args.reference_home_template = args.reference_home_template.resolve()
    if not args.reference_home_template.is_dir():
        raise FileNotFoundError(args.reference_home_template)
    for name in (
        "fom",
        "java",
        "go",
        "reference_api_jar",
        "reference_verifier_jar",
        "reference_server_java",
        "reference_server_jar",
        "portico_jar",
        "portico_verifier_jar",
        "portico_override_jar",
        "portico_rid",
    ):
        setattr(args, name, resolve_file(getattr(args, name)))
    if args.output.exists():
        raise FileExistsError(args.output)
    if (
        args.count < 1
        or args.operation_warmup < 0
        or args.warmup < 0
        or args.measured < 1
        or args.timeout_ms < 1000
        or args.gorti_local_lrc_queue < 1
        or args.gorti_local_lrc_ack_every < 1
        or args.gorti_federate_gomaxprocs < 0
    ):
        raise ValueError("invalid count, repetition count, or timeout")
    return args


def main() -> int:
    args = parse_args()
    mode = selected_mode(args)
    args.output.mkdir(parents=True)
    build_go(args, args.output)
    runners = {
        "reference": run_reference,
        "portico": run_portico,
        "gorti": run_gorti,
    }
    measured_results: list[dict[str, Any]] = []
    run_records: list[dict[str, Any]] = []
    total = args.warmup + args.measured
    for trio in range(total):
        phase = "warmup" if trio < args.warmup else "measured"
        phase_index = trio if phase == "warmup" else trio - args.warmup
        order = schedule_for(phase_index)
        print(
            f"[{trio + 1}/{total}] {phase} {phase_index + 1}: {' -> '.join(order)}",
            flush=True,
        )
        trio_results: dict[str, dict[str, Any]] = {}
        for implementation in order:
            run_id = f"{phase}-{phase_index + 1:02d}-{implementation}"
            run_output = args.output / "runs" / phase / f"{phase_index + 1:02d}-{implementation}"
            started = time.time()
            result = runners[implementation](args, run_output, run_id)
            record = {key: value for key, value in result.items() if key != "samples"}
            record.update(
                {
                    "phase": phase,
                    "trio": phase_index + 1,
                    "order": list(order),
                    "wall_seconds": time.time() - started,
                }
            )
            run_records.append(record)
            trio_results[implementation] = result
            if phase == "measured":
                result["trio"] = phase_index + 1
                result["order"] = list(order)
                measured_results.append(result)
        summaries = {
            sha256_file(Path(item["output"]) / "feature-summary.json")
            for item in trio_results.values()
        }
        payloads = {item["payload_projection_sha256"] for item in trio_results.values()}
        behaviors = {item["behavior_projection_sha256"] for item in trio_results.values()}
        if len(summaries) != 1 or len(payloads) != 1 or len(behaviors) != 1:
            raise ValueError(f"{phase} trio {phase_index + 1}: cross-product semantics differ")

    commit = run_checked(["git", "rev-parse", "HEAD"], args.repo)
    dirty = bool(
        run_checked(["git", "-c", "core.excludesFile=", "status", "--porcelain"], args.repo)
    )
    comparison = {
        "schema": f"gorti.three-way-{mode}-comparison/v1",
        "metadata": {
            "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "host": platform.node(),
            "host_os": platform.platform(),
            "java": run_checked([str(args.java), "-version"], args.repo),
            "go": run_checked([str(args.go), "version"], args.repo),
            "gorti_commit": commit,
            "gorti_worktree_dirty": dirty,
            "reference_api_sha256": sha256_file(args.reference_api_jar),
            "portico_jar_sha256": sha256_file(args.portico_jar),
            "gorti_rtid_sha256": sha256_file(args.gorti_rtid),
            "gorti_client_sha256": sha256_file(args.gorti_client),
            "harness_sha256": sha256_file(Path(__file__).resolve()),
        },
        "workload": {
            "mode": mode,
            "seed": args.seed,
            "count": args.count,
            "operation_warmup": args.operation_warmup,
            "fom_sha256": sha256_file(args.fom),
            "fom_bytes": args.fom.stat().st_size,
            "choreography": "sequential_receive_order_update_then_interaction"
            if mode == "receive-order"
            else "sequential_timestamp_order_update_interaction_then_time_advance",
            "callback_model": "HLA_IMMEDIATE",
            "delivery_boundary": (
                "subscriber_after_measure_sync_to_validated_final_callback"
                if args.operation_warmup > 0
                else "subscriber_after_ready_to_validated_final_callback"
            )
            if mode == "receive-order"
            else "subscriber_tar_to_timestamped_callbacks_before_grant",
            "time_management": "excluded" if mode == "receive-order" else "included",
            "federation_namespace": "RO" if mode == "receive-order" else "TM",
            "gorti_local_lrc": args.gorti_local_lrc,
            "gorti_local_lrc_queue": args.gorti_local_lrc_queue,
            "gorti_local_lrc_ack_every": args.gorti_local_lrc_ack_every,
            "gorti_federate_gomaxprocs": args.gorti_federate_gomaxprocs,
        },
        "protocol": {
            "warmup_trios": args.warmup,
            "measured_trios": args.measured,
            "ordering": "six-permutation rotation",
            "fairness": fairness_metadata(args.warmup, args.measured),
            "independent_federate_processes": 2,
            "host_environment": "same Windows host",
        },
        "feature_comparison": {
            implementation: next(
                item["feature_summary"]
                for item in measured_results
                if item["implementation"] == implementation
            )
            for implementation in IMPLEMENTATIONS
        },
        "aggregate": aggregate(measured_results, mode),
        "paired_analysis": paired_analysis(measured_results, mode),
        "runs": run_records,
    }
    write_json(args.output / "comparison.json", comparison)
    write_markdown(args.output / "comparison.md", comparison)
    print(f"PASS: {args.output / 'comparison.md'}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
