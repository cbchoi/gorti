"""Compare gorti HLA core and audit/replay plugin profiles in paired runs."""

from __future__ import annotations

import argparse
import copy
import math
import os
import platform
import shutil
import statistics
import tempfile
import time
from collections.abc import Callable, Mapping, Sequence
from pathlib import Path
from typing import Any

import compare_receive_order as receive

SCHEMA = "gorti.audit-plugin-comparison/v1"
AUDIT_PROFILE_ID = "gorti-audit-replay"
CORE_PROFILE_ID = "gorti-hla-core"
PROFILE_IDS = (AUDIT_PROFILE_ID, CORE_PROFILE_ID)
BOOTSTRAP_RESAMPLES = 10_000
_MASK64 = (1 << 64) - 1

# The shared retry helper validates keys named portico/gorti. These aliases are
# internal only; every persisted record uses the runtime profile identifiers.
_ALIAS_TO_PROFILE = {
    "portico": AUDIT_PROFILE_ID,
    "gorti": CORE_PROFILE_ID,
}
_PROFILE_TO_ALIAS = {profile: alias for alias, profile in _ALIAS_TO_PROFILE.items()}


class SplitMix64:
    """Fully specified PRNG for reproducible bootstrap resampling."""

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
    """Return an R7 linear quantile."""

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


def bootstrap_median_ci(
    values: Sequence[float],
    *,
    seed: int,
    resamples: int = BOOTSTRAP_RESAMPLES,
    strata: Sequence[str] | None = None,
) -> dict[str, Any]:
    """Return a deterministic percentile-bootstrap CI for a sample median."""

    if not values:
        raise ValueError("bootstrap requires at least one value")
    if seed < 0:
        raise ValueError("bootstrap seed must be nonnegative")
    if resamples < 100:
        raise ValueError("bootstrap resamples must be at least 100")
    sample_values = [float(value) for value in values]
    if any(not math.isfinite(value) or value <= 0.0 for value in sample_values):
        raise ValueError("bootstrap values must be finite and positive")
    if strata is not None and len(strata) != len(sample_values):
        raise ValueError("bootstrap strata must align with values")

    rng = SplitMix64(seed)
    grouped: list[list[float]]
    if strata is None:
        grouped = [sample_values]
    else:
        grouped_by_name: dict[str, list[float]] = {}
        for stratum, value in zip(strata, sample_values, strict=True):
            grouped_by_name.setdefault(stratum, []).append(value)
        grouped = [grouped_by_name[name] for name in sorted(grouped_by_name)]
    medians: list[float] = []
    for _ in range(resamples):
        sample = [
            group[rng.index(len(group))]
            for group in grouped
            for _ in range(len(group))
        ]
        medians.append(float(statistics.median(sample)))
    return {
        "estimate": float(statistics.median(sample_values)),
        "ci95": {
            "low": quantile(medians, 0.025),
            "high": quantile(medians, 0.975),
        },
        "bootstrap": {
            "method": "percentile-bootstrap",
            "confidence_level": 0.95,
            "resamples": resamples,
            "seed": seed,
            "prng": "splitmix64",
            "quantile_method": "linear-r7",
            "resampling_unit": "paired-seed",
            "stratification": "first-profile" if strata is not None else "none",
        },
    }


def runtime_profile(profile_id: str, protobuf_validation: bool = True) -> dict[str, Any]:
    """Describe the only runtime difference admitted by this ablation."""

    if profile_id not in PROFILE_IDS:
        raise ValueError(f"unknown runtime profile {profile_id!r}")
    plugin = "event-journal" if profile_id == AUDIT_PROFILE_ID else "none"
    profile = receive.gorti_runtime_profile(plugin, protobuf_validation)
    if profile["profile_id"] != profile_id:
        raise AssertionError("shared runtime-profile identity drifted")
    return profile


def require_actual_runtime_profile(
    result: Mapping[str, Any],
    expected_profile_id: str,
    expected_runtime_profile: Mapping[str, Any],
) -> None:
    if result.get("profile_id") != expected_profile_id:
        raise ValueError(
            f"run profile_id={result.get('profile_id')!r}, want {expected_profile_id!r}"
        )
    actual = result.get("runtime_profile")
    if actual != expected_runtime_profile:
        raise ValueError(f"{expected_profile_id}: runtime profile differs from requested profile")


def profile_definitions(protobuf_validation: bool = True) -> list[dict[str, Any]]:
    return [
        {
            "profile_id": profile_id,
            "runtime_profile": runtime_profile(profile_id, protobuf_validation),
        }
        for profile_id in PROFILE_IDS
    ]


def build_profile_schedule(
    base_seed: int,
    warmup_pairs: int,
    measured_pairs: int,
) -> list[dict[str, Any]]:
    """Map the shared phase-local AB/BA schedule onto core/plugin profiles."""

    schedule = receive.build_pair_schedule(base_seed, warmup_pairs, measured_pairs)
    return [
        {
            **item,
            "order": tuple(_ALIAS_TO_PROFILE[alias] for alias in item["order"]),
        }
        for item in schedule
    ]


def assert_profile_pair_evidence(
    phase: str,
    pair_index: int,
    pair_results: Mapping[str, Mapping[str, Any]],
) -> None:
    if set(pair_results) != set(PROFILE_IDS):
        raise ValueError(f"{phase} pair {pair_index}: both runtime profiles are required")
    if (
        pair_results[AUDIT_PROFILE_ID].get("evidence")
        != pair_results[CORE_PROFILE_ID].get("evidence")
    ):
        raise ValueError(f"{phase} pair {pair_index}: profile semantic evidence differs")


def run_profile_pair_with_retries(
    *,
    phase: str,
    pair_index: int,
    order: tuple[str, ...],
    max_pair_attempts: int,
    create_network_for_attempt: Callable[[int], str],
    remove_network_for_attempt: Callable[[str], None],
    run_profile: Callable[
        [str, str, int, int],
        tuple[dict[str, Any], dict[str, Any]],
    ],
    retry_ledger: list[dict[str, Any]] | None = None,
) -> tuple[list[dict[str, Any]], dict[str, dict[str, Any]], int]:
    """Reuse the shared atomic retry loop while exposing runtime profile IDs."""

    if set(order) != set(PROFILE_IDS) or len(order) != len(PROFILE_IDS):
        raise ValueError("profile pair order must contain HLA core and audit/replay once")
    alias_order = tuple(_PROFILE_TO_ALIAS[profile_id] for profile_id in order)

    def run_alias(
        alias: str,
        network: str,
        position: int,
        pair_attempt: int,
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        return run_profile(_ALIAS_TO_PROFILE[alias], network, position, pair_attempt)

    shared_retry_ledger: list[dict[str, Any]] = []
    try:
        records, alias_results, discarded = receive.run_pair_with_retries(
            phase=phase,
            pair_index=pair_index,
            order=alias_order,
            max_pair_attempts=max_pair_attempts,
            create_network_for_attempt=create_network_for_attempt,
            remove_network_for_attempt=remove_network_for_attempt,
            run_implementation=run_alias,
            retry_ledger=shared_retry_ledger,
        )
    finally:
        if retry_ledger is not None:
            retry_ledger.extend(
                {
                    **item,
                    "implementation": (
                        _ALIAS_TO_PROFILE[str(item["implementation"])]
                        if item["implementation"] is not None
                        else None
                    ),
                    "order": [_ALIAS_TO_PROFILE[str(alias)] for alias in item["order"]],
                }
                for item in shared_retry_ledger
            )
    profile_results = {
        _ALIAS_TO_PROFILE[alias]: result for alias, result in alias_results.items()
    }
    assert_profile_pair_evidence(phase, pair_index, profile_results)
    return records, profile_results, discarded


def analyze_measured_pairs(
    measured_pairs: Sequence[Mapping[str, Any]],
    *,
    bootstrap_seed: int,
    bootstrap_resamples: int = BOOTSTRAP_RESAMPLES,
) -> dict[str, Any]:
    if len(measured_pairs) < 4 or len(measured_pairs) % 2 != 0:
        raise ValueError("measured pairs must be an even count of at least four")

    pair_rows: list[dict[str, Any]] = []
    ratios: list[float] = []
    first_profiles: list[str] = []
    for expected_pair, item in enumerate(measured_pairs, start=1):
        pair_index = int(item["pair"])
        if pair_index != expected_pair:
            raise ValueError("measured pairs must be contiguous and ordered")
        batches = item["batch_ns"]
        if not isinstance(batches, Mapping):
            raise ValueError(f"measured pair {pair_index}: batch_ns must be an object")
        audit_ns = int(batches[AUDIT_PROFILE_ID])
        core_ns = int(batches[CORE_PROFILE_ID])
        if audit_ns <= 0 or core_ns <= 0:
            raise ValueError(f"measured pair {pair_index}: batch_ns must be positive")
        ratio = core_ns / audit_ns
        ratios.append(ratio)
        order = [str(profile_id) for profile_id in item["order"]]
        if len(order) != 2 or set(order) != set(PROFILE_IDS):
            raise ValueError(f"measured pair {pair_index}: invalid profile order")
        first_profiles.append(order[0])
        pair_rows.append(
            {
                "pair": pair_index,
                "seed": int(item["seed"]),
                "order": order,
                "audit_batch_ns": audit_ns,
                "core_batch_ns": core_ns,
                "core_over_audit_ratio": ratio,
            }
        )

    summary = bootstrap_median_ci(
        ratios,
        seed=bootstrap_seed,
        resamples=bootstrap_resamples,
        strata=first_profiles,
    )
    order_strata = [
        {
            "first_profile": profile_id,
            "n_pairs": sum(first == profile_id for first in first_profiles),
            "median": float(
                statistics.median(
                    ratio
                    for ratio, first in zip(ratios, first_profiles, strict=True)
                    if first == profile_id
                )
            ),
        }
        for profile_id in PROFILE_IDS
        if profile_id in first_profiles
    ]
    return {
        "primary_metric": {
            "id": "completed_receive_order_batch_ns",
            "boundary": "subscriber final callback arrival",
            "direction": "lower-is-better",
        },
        "paired_core_over_audit": {
            "ratio_definition": "hla-core-divided-by-audit-replay",
            "n_pairs": len(pair_rows),
            "pairs": pair_rows,
            "median": summary["estimate"],
            "bootstrap_ci95": summary["ci95"],
            "bootstrap": summary["bootstrap"],
            "order_strata": order_strata,
        },
    }


def summarize_secondary_metrics(run_records: Sequence[Mapping[str, Any]]) -> dict[str, Any]:
    measured = [record for record in run_records if record.get("phase") == "measured"]
    profiles: dict[str, Any] = {}
    for profile_id in PROFILE_IDS:
        records = [record for record in measured if record.get("profile_id") == profile_id]
        caller = [record["secondary_metrics"]["caller_latency_ns"] for record in records]
        throughput = [
            float(record["secondary_metrics"]["throughput_deliveries_per_second"])
            for record in records
        ]
        profiles[profile_id] = {
            "n": len(records),
            "caller_latency_ns": {
                operation: {
                    "median_of_run_medians": float(
                        statistics.median(int(value[operation]) for value in caller)
                    )
                }
                for operation in ("updateAttributeValues", "sendInteraction")
            },
            "throughput_deliveries_per_second": {
                "median": float(statistics.median(throughput))
            },
        }
    return {
        "classification": "secondary-only",
        "reason": "caller completion is not the subscriber batch boundary",
        "profiles": profiles,
    }


def prepare_binaries(
    args: argparse.Namespace,
    repo: Path,
    build_root: Path,
) -> tuple[Path, Path]:
    """Build only the gorti RTI server and Go verifier client."""

    binary_dir = build_root / "bin"
    binary_dir.mkdir(parents=True)
    rtid = binary_dir / "rtid-linux-amd64"
    client = binary_dir / "gorti-go-fair-linux-amd64"
    go_cache = build_root / "go-cache"
    go_cache.mkdir()
    go_env = os.environ.copy()
    go_env.update(
        {
            "GOOS": "linux",
            "GOARCH": "amd64",
            "CGO_ENABLED": "0",
            "GOCACHE": str(go_cache),
        }
    )
    receive.run_checked(
        [args.go, "build", "-trimpath", "-o", str(rtid), "./rti/cmd/rtid"],
        cwd=repo,
        env=go_env,
    )
    receive.run_checked(
        [
            args.go,
            "build",
            "-trimpath",
            "-o",
            str(client),
            "./verification/gorti-go-fair",
        ],
        cwd=repo,
        env=go_env,
    )
    return rtid, client


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--fom", type=Path, required=True)
    parser.add_argument("--workload", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--seed", type=receive.nonnegative_int, default=1516)
    parser.add_argument("--operation-warmup", type=receive.positive_int, default=128)
    parser.add_argument("--warmup", type=receive.nonnegative_int, default=5)
    parser.add_argument("--measured", type=balanced_measured_pairs, default=30)
    parser.add_argument("--max-pair-attempts", type=receive.positive_int, default=3)
    parser.add_argument("--timeout-ms", type=receive.positive_int, default=300000)
    parser.add_argument(
        "--gorti-transport",
        choices=receive.GORTI_TRANSPORT_MODES,
        default="confirmed",
    )
    parser.add_argument("--local-lrc-queue", type=receive.positive_int, default=1024)
    parser.add_argument("--local-lrc-ack-every", type=receive.positive_int, default=32)
    parser.add_argument(
        "--local-lrc-batch-size",
        type=int,
        choices=(32, 64, 128, 256),
        default=32,
    )
    parser.add_argument(
        "--gorti-callback-representation",
        choices=("names", "handles"),
        default="handles",
    )
    parser.add_argument(
        "--gorti-outbox-event-capacity",
        type=receive.positive_int,
        default=8192,
    )
    parser.add_argument(
        "--gorti-outbox-batch-size",
        type=receive.positive_int,
        default=32,
    )
    parser.add_argument(
        "--gorti-outbox-flush-interval-ms",
        type=receive.positive_int,
        default=1,
    )
    parser.add_argument(
        "--gorti-event-log-protobuf-validation",
        action=argparse.BooleanOptionalAction,
        default=True,
    )
    parser.add_argument("--docker", default="docker")
    parser.add_argument("--go", default="go")
    return parser


def balanced_measured_pairs(value: str) -> int:
    parsed = receive.positive_int(value)
    if parsed < 4 or parsed % 2 != 0:
        raise argparse.ArgumentTypeError("must be an even integer of at least four")
    return parsed


def _validated_inputs(args: argparse.Namespace) -> tuple[Path, Path]:
    repo = args.repo.resolve()
    args.fom = args.fom.resolve()
    args.workload = args.workload.resolve()
    output = args.output.resolve()
    if output.exists():
        raise FileExistsError(f"comparison output already exists: {output}")
    if args.timeout_ms < 1000:
        raise ValueError("--timeout-ms must be at least 1000")
    for path in (args.fom, args.workload):
        if not path.is_file():
            raise FileNotFoundError(path)
        path.relative_to(repo)
    if shutil.which(args.docker) is None or shutil.which(args.go) is None:
        raise FileNotFoundError("docker and go must be available on PATH")
    return repo, output


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    repo, output = _validated_inputs(args)
    plan_module = receive.load_devstone_plan_module(repo)
    workload = plan_module.load_workload(args.workload)
    workload_metadata = receive._workload_metadata(plan_module, args.workload, workload)
    profiles = {
        definition["profile_id"]: definition["runtime_profile"]
        for definition in profile_definitions(args.gorti_event_log_protobuf_validation)
    }

    scratch_parent = repo / ".tmp"
    scratch_parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(
        prefix="gorti-audit-plugin-ablation-",
        dir=scratch_parent,
    ) as temporary:
        scratch = Path(temporary)
        rtid, client = prepare_binaries(args, repo, scratch)
        container_identity = receive.inspect_container_image_identity(args.docker, repo)
        volume = receive.create_benchmark_volume(args.docker, repo)
        run_records: list[dict[str, Any]] = []
        measured_pairs: list[dict[str, Any]] = []
        retry_ledger: list[dict[str, Any]] = []
        discarded_pair_attempts = 0
        schedule = build_profile_schedule(args.seed, args.warmup, args.measured)
        try:
            archive = receive.create_staging_archive(
                repo,
                (args.fom, rtid, client),
                scratch / "static-artifacts.tar.gz",
            )
            receive.stage_archive(args.docker, repo, volume, archive)

            for schedule_position, pair_spec in enumerate(schedule, start=1):
                phase = str(pair_spec["phase"])
                pair_index = int(pair_spec["pair"])
                pair_seed = int(pair_spec["seed"])
                order = tuple(str(item) for item in pair_spec["order"])
                pair_identity = f"{phase}-{pair_index:02d}-{pair_seed}"
                plan_path = scratch / "plans" / f"{pair_identity}.dvshla"
                pair_plan = receive.materialize_pair_plan(
                    plan_module,
                    workload,
                    pair_seed,
                    plan_path,
                )
                if (
                    pair_plan.count != workload_metadata["count"]
                    or pair_plan.topology_sha256
                    != workload_metadata["topology_identity_sha256"]
                ):
                    raise ValueError(f"{pair_identity}: plan differs from workload metadata")
                receive.stage_artifacts(args.docker, repo, volume, (pair_plan.path,))
                staged_plan_path = receive.staged_path(pair_plan.path, repo)

                def create_attempt_network(
                    pair_attempt: int,
                    *,
                    schedule_position: int = schedule_position,
                    pair_identity: str = pair_identity,
                    phase: str = phase,
                    pair_index: int = pair_index,
                    pair_seed: int = pair_seed,
                    order: tuple[str, ...] = order,
                ) -> str:
                    print(
                        f"[{schedule_position}/{len(schedule)}] {phase} "
                        f"{pair_index} seed={pair_seed}: {' -> '.join(order)} "
                        f"(attempt {pair_attempt}/{args.max_pair_attempts})",
                        flush=True,
                    )
                    return receive.create_pair_network(
                        args.docker,
                        repo,
                        f"audit-plugin-{pair_identity}-attempt-{pair_attempt}",
                    )

                def remove_attempt_network(network: str) -> None:
                    receive.remove_pair_network(args.docker, repo, network)

                def run_profile(
                    profile_id: str,
                    network: str,
                    position: int,
                    pair_attempt: int,
                    *,
                    pair_identity: str = pair_identity,
                    pair_plan: receive.PairPlan = pair_plan,
                    phase: str = phase,
                    pair_index: int = pair_index,
                    pair_seed: int = pair_seed,
                    order: tuple[str, ...] = order,
                ) -> tuple[dict[str, Any], dict[str, Any]]:
                    receive.assert_network_empty(args.docker, repo, network)
                    run_id = f"{pair_identity}-attempt-{pair_attempt}-{profile_id}"
                    transient_output = scratch / "summaries" / run_id
                    started = time.time()
                    result = receive.run_gorti(
                        args.docker,
                        repo,
                        network,
                        volume,
                        rtid,
                        client,
                        args.fom,
                        transient_output,
                        run_id,
                        pair_plan,
                        args.operation_warmup,
                        args.timeout_ms,
                        args.gorti_transport,
                        args.local_lrc_queue,
                        args.local_lrc_ack_every,
                        args.local_lrc_batch_size,
                        args.gorti_callback_representation,
                        args.gorti_outbox_event_capacity,
                        args.gorti_outbox_batch_size,
                        args.gorti_outbox_flush_interval_ms,
                        args.gorti_event_log_protobuf_validation,
                        "event-journal" if profile_id == AUDIT_PROFILE_ID else "none",
                    )
                    require_actual_runtime_profile(
                        result,
                        profile_id,
                        profiles[profile_id],
                    )
                    receive.assert_network_empty(args.docker, repo, network)
                    pair_result = {
                        "profile_id": profile_id,
                        "batch_ns": int(result["batch_ns"]),
                        "evidence": result["evidence"],
                        "secondary_metrics": {
                            "caller_latency_ns": result["operation_medians_ns"],
                            "throughput_deliveries_per_second": result[
                                "throughput_deliveries_per_second"
                            ],
                        },
                    }
                    record = {
                        **pair_result,
                        "runtime_profile": copy.deepcopy(result["runtime_profile"]),
                        "phase": phase,
                        "pair": pair_index,
                        "position": position,
                        "pair_attempt": pair_attempt,
                        "seed": pair_seed,
                        "order": list(order),
                        "wall_seconds": time.time() - started,
                        "participant_exit_codes": result["participant_exit_codes"],
                        "server_lifecycle": result["server_lifecycle"],
                        "cleanup_verified": result["cleanup_verified"],
                        "artifacts_retained": False,
                    }
                    return pair_result, record

                try:
                    pair_records, pair_results, discarded = (
                        run_profile_pair_with_retries(
                            phase=phase,
                            pair_index=pair_index,
                            order=order,
                            max_pair_attempts=args.max_pair_attempts,
                            create_network_for_attempt=create_attempt_network,
                            remove_network_for_attempt=remove_attempt_network,
                            run_profile=run_profile,
                            retry_ledger=retry_ledger,
                        )
                    )
                    discarded_pair_attempts += discarded
                    run_records.extend(pair_records)
                    if phase == "measured":
                        measured_pairs.append(
                            {
                                "pair": pair_index,
                                "seed": pair_seed,
                                "order": list(order),
                                "batch_ns": {
                                    profile_id: int(pair_results[profile_id]["batch_ns"])
                                    for profile_id in PROFILE_IDS
                                },
                            }
                        )
                finally:
                    receive.remove_volume_path(
                        args.docker,
                        repo,
                        volume,
                        staged_plan_path,
                    )
        finally:
            receive.remove_benchmark_volume(args.docker, repo, volume)

        commit = receive.run_checked(["git", "rev-parse", "HEAD"], cwd=repo)
        worktree_dirty = bool(
            receive.run_checked(
                ["git", "-c", "core.excludesFile=", "status", "--porcelain"],
                cwd=repo,
            )
        )
        comparison = {
            "schema": SCHEMA,
            "metadata": {
                "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "host": platform.node(),
                "host_os": platform.platform(),
                **container_identity,
                "docker_client": receive.run_checked(
                    [args.docker, "version", "--format", "{{.Client.Version}}"],
                    cwd=repo,
                ),
                "docker_server": receive.run_checked(
                    [args.docker, "version", "--format", "{{.Server.Version}}"],
                    cwd=repo,
                ),
                "go_version": receive.run_checked([args.go, "version"], cwd=repo),
                "gorti_commit": commit,
                "gorti_worktree_dirty": worktree_dirty,
                "gorti_rtid_sha256": receive.sha256_file(rtid),
                "gorti_client_sha256": receive.sha256_file(client),
                "comparison_harness_sha256": receive.sha256_file(Path(__file__).resolve()),
                "shared_runner_sha256": receive.sha256_file(Path(receive.__file__).resolve()),
            },
            "profiles": [
                {
                    "profile_id": profile_id,
                    "runtime_profile": copy.deepcopy(profiles[profile_id]),
                }
                for profile_id in PROFILE_IDS
            ],
            "workload": {
                **workload_metadata,
                "base_seed": args.seed,
                "operation_warmup": args.operation_warmup,
                "fom_sha256": receive.sha256_file(args.fom),
                "fom_bytes": args.fom.stat().st_size,
                "same_fom_bytes": True,
                "same_dvshla_plan_within_pair": True,
                "same_seed_within_pair": True,
                "choreography": (
                    "sequential_local_admission_update_then_interaction_then_flush"
                    if args.gorti_transport == "local-lrc"
                    else "sequential_receive_order_update_then_interaction"
                ),
                "callback_model": "HLA_IMMEDIATE",
                "primary_metric_boundary": "subscriber final callback arrival",
                "caller_latency_classification": "secondary-only",
                "time_management": "excluded",
            },
            "gorti_configuration": {
                "receive_order_transport": args.gorti_transport,
                "local_lrc_queue_capacity": args.local_lrc_queue,
                "local_lrc_ack_every": args.local_lrc_ack_every,
                "local_lrc_batch_size": args.local_lrc_batch_size,
                "callback_representation": args.gorti_callback_representation,
                "outbox_event_capacity": args.gorti_outbox_event_capacity,
                "outbox_batch_size": args.gorti_outbox_batch_size,
                "outbox_flush_interval_ms": args.gorti_outbox_flush_interval_ms,
                "event_log_protobuf_validation": (
                    args.gorti_event_log_protobuf_validation
                ),
            },
            "protocol": {
                "warmup_pairs": args.warmup,
                "measured_pairs": args.measured,
                "max_pair_attempts": args.max_pair_attempts,
                "discarded_pair_attempts": discarded_pair_attempts,
                "discarded_pair_attempt_ledger": retry_ledger,
                "ordering": (
                    "phase-local audit-replay/HLA-core AB/BA alternating; "
                    "audit-replay first in pair 1"
                ),
                "seed_schedule": (
                    "measured pair i uses base_seed+i-1; warm-up seeds start after "
                    "the measured seed range"
                ),
                "replacement_policy": (
                    "Only participant process exit or timeout after verified cleanup "
                    "replays the complete profile pair with the same seed, plan, and "
                    "AB/BA order; semantic and cleanup failures abort"
                ),
                "fresh_processes_per_run": True,
                "independent_federate_processes": 2,
                "same_go_binaries_across_profiles": True,
                "same_fom_plan_seed_within_pair": True,
                "pair_evidence_must_match_exactly": True,
                "cleanup_verified_all_runs": True,
                "cleanup_control": (
                    "participant and server containers, transient volume paths, "
                    "pair networks, and the benchmark volume are proven absent"
                ),
                "transcripts_retained": False,
                "participant_summaries_retained": False,
                "output_files": ["comparison.json"],
            },
            "analysis": {
                **analyze_measured_pairs(
                    measured_pairs,
                    bootstrap_seed=args.seed,
                ),
                "secondary_metrics": summarize_secondary_metrics(run_records),
            },
            "runs": run_records,
        }

    output.mkdir(parents=True)
    receive.write_json(output / "comparison.json", comparison)
    if {path.name for path in output.iterdir()} != {"comparison.json"}:
        raise RuntimeError("comparison workspace contains unexpected files")
    print(f"PASS: {output / 'comparison.json'}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
