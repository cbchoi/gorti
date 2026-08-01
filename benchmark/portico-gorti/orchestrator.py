"""Run and package the paired Portico/gorti DEVStone-HLA benchmark."""

# All subprocess argument vectors use validated local tool and artifact paths.
# ruff: noqa: S603

from __future__ import annotations

import argparse
import contextlib
import ctypes
import datetime as dt
import hashlib
import json
import math
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import uuid
import zipfile
from collections import Counter, defaultdict
from collections.abc import Callable, Iterator, Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

RESULT_NAMES = frozenset({"manifest.json", "results.json", "analysis.json", "comparison.tex"})
PLACEHOLDER = re.compile(r"^__REQUIRED_[A-Z0-9_]+__$")
SHA256 = re.compile(r"^[0-9a-f]{64}$")
DEFAULT_OPERATION_WARMUP = 128
DEFAULT_TIMEOUT_MS = 300000
MAX_PAIR_ATTEMPTS = 3
CONTAINER_IMAGE = "ubuntu:24.04"
CONTAINER_PLATFORM = "linux/amd64"
PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS = 5000
PORTICO_PUBLISHER_READY_GATE = True
PORTICO_ORDERED_TEARDOWN_GATE = True
PORTICO_ORDERED_TEARDOWN_PHASES = (
    "subscriber_resigned",
    "publisher_resigned",
    "subscriber_disconnected",
)
CHOREOGRAPHY = "sequential_receive_order_update_then_interaction"
LOCAL_LRC_CHOREOGRAPHY = "sequential_local_admission_update_then_interaction_then_flush"
GORTI_TRANSPORT_MODES = frozenset({"confirmed", "local-lrc"})
PRIMARY_METRIC_IDS = frozenset({"completed_delivery_batch_ns", "deliveries_per_second"})
PRIMARY_COMPLETION_METRIC_ID = "completed_delivery_batch_ns"
PRACTICAL_RATIO_LOWER = 0.9090909090909091
PRACTICAL_RATIO_UPPER = 1.1
CALLBACK_MODEL = "HLA_IMMEDIATE"
TIME_MANAGEMENT = "excluded"
TIMER_BOUNDARY_ID = "subscriber_timer_armed_before_VERIFY_START_release_to_final_callback_arrival"
RUNTIME_PROFILE_SCHEMA = "gorti.benchmark-runtime-profile/v1"
GORTI_AUDIT_REPLAY_PLUGIN = "none"
GORTI_PROFILE_ID = "gorti-hla-core"
PORTICO_PROFILE_ID = "portico-2.1.4-diagnostics-off-auditor-off"
PAIR_REPLACEMENT_POLICY = (
    "Only participant process exit or timeout after verified cleanup replays the entire "
    "pair with the same seed, materialized plan, and AB/BA order on a fresh network; "
    "semantic, summary, plan, evidence, and cleanup failures abort without replacement"
)
TRANSPORT_OVERRIDE_BUILDER = Path("verification/portico/build_transport_override.py")
TRANSPORT_OVERRIDE_RESOURCE = "etc/jgroups-udp.xml"
VERIFIER_MAIN_SOURCE = Path("gorti/verification/commercialrti/CommercialRtiVerifier.java")
VERIFIER_COMPILER_FLAGS = ("--release", "8", "-encoding", "UTF-8")
VERIFIER_BUILD_INPUT_SCHEMA = "gorti.java-verifier-build-input/v1"
PROJECTION_NAME = "DEVStone-HLA paired OM projection"
PROJECTION_SCOPE = (
    "one object update plus one interaction per DEVStone atomic event delivery; "
    "not a DEVS simulator-kernel score"
)
SEMANTIC_EVIDENCE_FIELDS = (
    "workload_instance_sha256",
    "expected_callbacks",
    "delivered_callbacks",
    "rejected_callbacks",
    "dropped_callbacks",
    "unexpected_callbacks",
    "duplicate_callbacks",
    "invalid_callbacks",
    "ready_synchronized",
    "start_synchronized",
    "measure_synchronized",
    "done_synchronized",
    "attribute_callback_sha256",
    "interaction_callback_sha256",
    "callback_trace_sha256",
    "terminal_state_sha256",
)


class BenchmarkError(RuntimeError):
    """Raised when the benchmark contract or runtime evidence is invalid."""


@dataclass(frozen=True)
class Contract:
    repo: Path
    experiment_path: Path
    prerequisites_path: Path
    workload_path: Path
    experiment_document: dict[str, Any]
    prerequisites_document: dict[str, Any]
    workload_document: dict[str, Any]
    workload_file_sha256: str
    workload_identity_sha256: str
    count_per_channel: int
    base_seed: int
    warmup_pairs: int
    measured_pairs: int
    max_pair_attempts: int
    replacement_policy: str
    operation_warmup: int
    choreography: str
    callback_model: str
    time_management: str
    timer_boundary_id: str
    gorti_transport: str
    local_lrc_queue_capacity: int
    local_lrc_ack_every: int
    local_lrc_batch_size: int
    callback_representation: str
    outbox_event_capacity: int
    outbox_batch_size: int
    outbox_flush_interval_ms: int
    metric_ids: tuple[str, ...]

    @property
    def experiment(self) -> dict[str, Any]:
        return self.experiment_document["experiment"]


@dataclass(frozen=True)
class RuntimePaths:
    output: Path | None
    portico_home: Path
    portico_jar: Path
    portico_java: Path
    fom: Path
    verifier_jar: Path | None
    verifier_source_root: Path
    comparator: Path
    docker: str | None
    go: str | None
    javac: str | None
    jar: str | None


@dataclass(frozen=True)
class VerifierBuildInputs:
    sources: tuple[Path, ...]
    source_records: tuple[tuple[str, str], ...]
    main_source_sha256: str
    portico_api_jar_sha256: str
    compiler_version: str
    compiler_args: tuple[str, ...]
    build_input_sha256: str


@dataclass(frozen=True)
class VerifierBuild:
    jar: Path
    jar_sha256: str
    inputs: VerifierBuildInputs


@dataclass(frozen=True)
class GitSourceState:
    commit: str
    dirty: bool
    source_tree_sha256: str


def _read_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise BenchmarkError(f"cannot read JSON from {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise BenchmarkError(f"{path}: JSON root must be an object")
    return value


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def sha256_text(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()


def transport_override_build_evidence(contract: Contract, runtime: RuntimePaths) -> dict[str, Any]:
    builder = (contract.repo / TRANSPORT_OVERRIDE_BUILDER).resolve()
    if not builder.is_file():
        raise BenchmarkError(f"transport override builder is missing: {builder}")
    if not runtime.portico_jar.is_file():
        raise BenchmarkError(f"Portico jar is missing: {runtime.portico_jar}")
    try:
        with zipfile.ZipFile(runtime.portico_jar) as archive:
            source_resource = archive.read(TRANSPORT_OVERRIDE_RESOURCE)
    except (OSError, KeyError, zipfile.BadZipFile) as exc:
        raise BenchmarkError(
            f"cannot read {TRANSPORT_OVERRIDE_RESOURCE} from {runtime.portico_jar}: {exc}"
        ) from exc

    inputs = {
        "builder_path": TRANSPORT_OVERRIDE_BUILDER.as_posix(),
        "builder_sha256": sha256_file(builder),
        "portico_jar_sha256": sha256_file(runtime.portico_jar),
        "source_resource": TRANSPORT_OVERRIDE_RESOURCE,
        "source_resource_sha256": hashlib.sha256(source_resource).hexdigest(),
    }
    canonical = json.dumps(
        inputs,
        ensure_ascii=True,
        allow_nan=False,
        sort_keys=True,
        separators=(",", ":"),
    )
    return {
        "schema": "gorti.portico-transport-override-build-input/v1",
        "build_input_sha256": sha256_text(canonical),
        **inputs,
    }


def _verifier_build_inputs(runtime: RuntimePaths, compiler_version: str) -> VerifierBuildInputs:
    source_root = runtime.verifier_source_root.resolve()
    sources = tuple(
        sorted(
            (path.resolve() for path in source_root.rglob("*.java")),
            key=lambda path: path.relative_to(source_root).as_posix(),
        )
    )
    if not sources:
        raise BenchmarkError("no Java verifier sources found")

    main_source = (source_root / VERIFIER_MAIN_SOURCE).resolve()
    if main_source not in sources:
        raise BenchmarkError(f"Java verifier main source is missing: {main_source}")
    if not runtime.portico_jar.is_file():
        raise BenchmarkError(f"Portico API jar is missing: {runtime.portico_jar}")
    if not compiler_version.strip():
        raise BenchmarkError("Java compiler version is missing")

    source_records = tuple(
        (
            source.relative_to(source_root).as_posix(),
            sha256_file(source),
        )
        for source in sources
    )
    compiler_args = (
        *VERIFIER_COMPILER_FLAGS,
        "-cp",
        "${PORTICO_API_JAR}",
        "-d",
        "${CLASSES_DIR}",
        *(record[0] for record in source_records),
    )
    portico_api_jar_sha256 = sha256_file(runtime.portico_jar)

    digest = hashlib.sha256()

    def add(label: str, value: bytes) -> None:
        for field in (label.encode("ascii"), value):
            digest.update(len(field).to_bytes(8, "big"))
            digest.update(field)

    add("schema", VERIFIER_BUILD_INPUT_SCHEMA.encode("ascii"))
    for relative_path, source in zip(
        (record[0] for record in source_records), sources, strict=True
    ):
        add("source_path", relative_path.encode("utf-8"))
        add("source_content", source.read_bytes())
    add("portico_api_jar_sha256", portico_api_jar_sha256.encode("ascii"))
    add("compiler_version", compiler_version.strip().encode("utf-8"))
    for argument in compiler_args:
        add("compiler_arg", argument.encode("utf-8"))

    return VerifierBuildInputs(
        sources=sources,
        source_records=source_records,
        main_source_sha256=sha256_file(main_source),
        portico_api_jar_sha256=portico_api_jar_sha256,
        compiler_version=compiler_version.strip(),
        compiler_args=compiler_args,
        build_input_sha256=digest.hexdigest(),
    )


def capture_git_source_state(repo: Path) -> GitSourceState:
    git = shutil.which("git")
    if git is None:
        raise BenchmarkError("git is required to capture the benchmark source state")

    def git_bytes(arguments: Sequence[str]) -> bytes:
        completed = subprocess.run(
            [git, "-c", "core.excludesFile=", *arguments],
            cwd=repo,
            capture_output=True,
            check=False,
        )
        if completed.returncode:
            detail = completed.stderr.decode("utf-8", errors="replace").strip()
            raise BenchmarkError(f"git {' '.join(arguments)} failed: {detail}")
        return completed.stdout

    commit = git_bytes(["rev-parse", "HEAD"]).decode("ascii").strip()
    if re.fullmatch(r"[0-9a-f]{40}", commit) is None:
        raise BenchmarkError("git HEAD is not a resolved commit")
    status = git_bytes(["status", "--porcelain=v1", "-z", "--untracked-files=all"])
    listed = git_bytes(["ls-files", "-z", "--cached", "--others", "--exclude-standard"])
    relative_names = sorted(name for name in listed.split(b"\0") if name)
    digest = hashlib.sha256()
    digest.update(b"gorti.git-source-state/v1\0")
    digest.update(commit.encode("ascii"))
    digest.update(b"\0")
    digest.update(status)
    for encoded_name in relative_names:
        relative_name = encoded_name.decode("utf-8", errors="surrogateescape")
        path = repo / relative_name
        digest.update(len(encoded_name).to_bytes(4, "big"))
        digest.update(encoded_name)
        if path.is_file():
            digest.update(b"F")
            digest.update(bytes.fromhex(sha256_file(path)))
        else:
            digest.update(b"MISSING")
    return GitSourceState(
        commit=commit,
        dirty=bool(status),
        source_tree_sha256=digest.hexdigest(),
    )


def _canonical_workload_identity(workload: Mapping[str, Any]) -> str:
    unsigned = {key: value for key, value in workload.items() if key != "identity"}
    encoded = json.dumps(
        unsigned,
        ensure_ascii=True,
        allow_nan=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _require_int(value: Any, label: str, *, minimum: int = 0) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < minimum:
        raise BenchmarkError(f"{label} must be an integer >= {minimum}")
    return value


def load_contract(
    repo: Path,
    experiment_path: Path | None = None,
    prerequisites_path: Path | None = None,
    workload_path: Path | None = None,
) -> Contract:
    repo = repo.resolve()
    experiment_path = (
        experiment_path or repo / "benchmark" / "environment" / "experiment.json"
    ).resolve()
    prerequisites_path = (
        prerequisites_path or repo / "benchmark" / "environment" / "prerequisites.json"
    ).resolve()
    workload_path = (
        workload_path or repo / "benchmark" / "devstone" / "workload" / "workload.json"
    ).resolve()
    experiment_document = _read_json(experiment_path)
    prerequisites_document = _read_json(prerequisites_path)
    workload_document = _read_json(workload_path)

    if experiment_document.get("schema_version") != "gorti.benchmark.environment/v1":
        raise BenchmarkError("unsupported experiment schema_version")
    if prerequisites_document.get("schema_version") != "gorti.benchmark.prerequisites/v1":
        raise BenchmarkError("unsupported prerequisites schema_version")
    if workload_document.get("schema_version") != "devstone-hla-workload/v1":
        raise BenchmarkError("unsupported workload schema_version")

    try:
        experiment = experiment_document["experiment"]
        benchmark = experiment["benchmark"]
        design = experiment["design"]
        measurement = experiment["measurement"]
        identity = workload_document["identity"]
        total = workload_document["expected_counts"]["total"]
    except (KeyError, TypeError) as exc:
        raise BenchmarkError(f"incomplete benchmark contract: {exc}") from exc

    if (
        benchmark.get("name") != "DEVStone-HLA"
        or benchmark.get("source_benchmark") != "DEVStone"
        or benchmark.get("requested_alias") != "DEVSOne"
    ):
        raise BenchmarkError("experiment must define the DEVStone-HLA benchmark mapping")
    if workload_document.get("benchmark", {}).get("resolved_name") != "DEVStone":
        raise BenchmarkError("workload benchmark must resolve to DEVStone")
    if design.get("type") != "paired-balanced-ab-ba":
        raise BenchmarkError("experiment design must be paired-balanced-ab-ba")

    warmup_pairs = _require_int(
        design.get("warmup_runs_per_implementation"),
        "design.warmup_runs_per_implementation",
    )
    measured_pairs = _require_int(
        design.get("measured_runs_per_implementation"),
        "design.measured_runs_per_implementation",
        minimum=1,
    )
    declared_pairs = _require_int(
        design.get("measured_pair_count"), "design.measured_pair_count", minimum=1
    )
    base_seed = _require_int(design.get("base_seed"), "design.base_seed")
    max_pair_attempts = _require_int(
        design.get("max_pair_attempts"), "design.max_pair_attempts", minimum=1
    )
    if warmup_pairs != 5 or measured_pairs != 30 or declared_pairs != 30:
        raise BenchmarkError("experiment must specify 5 warm-up and 30 measured pairs")
    if max_pair_attempts != MAX_PAIR_ATTEMPTS:
        raise BenchmarkError(f"experiment must specify max_pair_attempts={MAX_PAIR_ATTEMPTS}")
    replacement_policy = design.get("replacement_policy")
    if replacement_policy != PAIR_REPLACEMENT_POLICY:
        raise BenchmarkError("experiment replacement policy differs from the bounded pair policy")
    if design.get("portico_first_pairs") != 15 or design.get("gorti_first_pairs") != 15:
        raise BenchmarkError("experiment must balance first position 15:15")
    if design.get("same_seed_within_pair") is not True:
        raise BenchmarkError("paired implementations must use the same seed")
    order_pattern = design.get("order_pattern")
    if order_pattern != [["portico", "gorti"], ["gorti", "portico"]]:
        raise BenchmarkError("experiment order_pattern must contain AB and BA")
    if design.get("replacement_run_uses_same_pair_seed_and_order") is not True:
        raise BenchmarkError("replacement pairs must preserve their seed and AB/BA order")
    operation_warmup = _require_int(
        measurement.get("operation_warmup_iterations_per_channel"),
        "measurement.operation_warmup_iterations_per_channel",
        minimum=1,
    )
    gorti_config = next(
        (item for item in experiment.get("implementations", []) if item.get("id") == "gorti"),
        None,
    )
    if not isinstance(gorti_config, dict):
        raise BenchmarkError("experiment must define the gorti implementation")
    gorti_transport = str(gorti_config.get("receive_order_transport", "confirmed"))
    if gorti_transport not in GORTI_TRANSPORT_MODES:
        raise BenchmarkError("unsupported gorti receive-order transport")
    local_lrc_queue_capacity = _require_int(
        gorti_config.get("local_lrc_queue_capacity", 1024),
        "implementations.gorti.local_lrc_queue_capacity",
        minimum=1,
    )
    local_lrc_ack_every = _require_int(
        gorti_config.get("local_lrc_ack_every", 32),
        "implementations.gorti.local_lrc_ack_every",
        minimum=1,
    )
    local_lrc_batch_size = _require_int(
        gorti_config.get("local_lrc_batch_size", 32),
        "implementations.gorti.local_lrc_batch_size",
        minimum=1,
    )
    if local_lrc_batch_size not in {32, 64, 128, 256}:
        raise BenchmarkError(
            "implementations.gorti.local_lrc_batch_size must be 32, 64, 128, or 256"
        )
    callback_representation = str(gorti_config.get("callback_representation", "handles"))
    if callback_representation not in {"names", "handles"}:
        raise BenchmarkError(
            "implementations.gorti.callback_representation must be names or handles"
        )
    outbox_event_capacity = _require_int(
        gorti_config.get("outbox_event_capacity", 8192),
        "implementations.gorti.outbox_event_capacity",
        minimum=1,
    )
    if outbox_event_capacity > 1 << 20:
        raise BenchmarkError("implementations.gorti.outbox_event_capacity must not exceed 1048576")
    outbox_batch_size = _require_int(
        gorti_config.get("outbox_batch_size", 32),
        "implementations.gorti.outbox_batch_size",
        minimum=1,
    )
    if outbox_batch_size > 1024:
        raise BenchmarkError("implementations.gorti.outbox_batch_size must not exceed 1024")
    if outbox_event_capacity < outbox_batch_size:
        raise BenchmarkError(
            "implementations.gorti.outbox_event_capacity must be at least outbox_batch_size"
        )
    outbox_flush_interval_ms = _require_int(
        gorti_config.get("outbox_flush_interval_ms", 1),
        "implementations.gorti.outbox_flush_interval_ms",
        minimum=1,
    )
    expected_choreography = (
        LOCAL_LRC_CHOREOGRAPHY if gorti_transport == "local-lrc" else CHOREOGRAPHY
    )
    exact_measurement_contract = {
        "operation_warmup_iterations_per_channel": DEFAULT_OPERATION_WARMUP,
        "choreography": expected_choreography,
        "callback_model": CALLBACK_MODEL,
        "time_management": TIME_MANAGEMENT,
        "timer_boundary_id": TIMER_BOUNDARY_ID,
    }
    for key, expected in exact_measurement_contract.items():
        if measurement.get(key) != expected:
            raise BenchmarkError(f"experiment measurement.{key} must be {expected!r}")
    included_metrics = measurement.get("included_metrics")
    if not isinstance(included_metrics, list) or not included_metrics:
        raise BenchmarkError("experiment must define measured metrics")
    metric_ids = tuple(
        str(metric.get("id")) for metric in included_metrics if isinstance(metric, dict)
    )
    if len(metric_ids) != len(included_metrics) or len(set(metric_ids)) != len(metric_ids):
        raise BenchmarkError("experiment metric IDs must be unique strings")
    if not PRIMARY_METRIC_IDS.issubset(metric_ids):
        raise BenchmarkError("experiment must include both common subscriber-completion metrics")
    if gorti_transport == "local-lrc" and set(metric_ids) != PRIMARY_METRIC_IDS:
        raise BenchmarkError("LocalLRC comparison may publish only common completion metrics")
    if gorti_transport == "local-lrc":
        if measurement.get("primary_metric") != PRIMARY_COMPLETION_METRIC_ID:
            raise BenchmarkError("LocalLRC primary metric must be completed delivery batch time")
        interval = measurement.get("practical_equivalence_ratio_interval")
        if interval != [PRACTICAL_RATIO_LOWER, PRACTICAL_RATIO_UPPER]:
            raise BenchmarkError("LocalLRC practical-equivalence interval differs from contract")
    log_policy = experiment.get("log_policy", {})
    for key in (
        "measurement_from_logs",
        "parse_stdout",
        "parse_stderr",
        "capture_stdout",
        "capture_stderr",
    ):
        if log_policy.get(key) is not False:
            raise BenchmarkError(f"experiment.log_policy.{key} must be false")
    for key in ("rti_event_logging", "federate_event_logging"):
        if log_policy.get(key) != "disabled":
            raise BenchmarkError(f"experiment.log_policy.{key} must be 'disabled'")
    output_policy = experiment.get("output_policy", {})
    if set(output_policy.get("allowed_artifacts", [])) != RESULT_NAMES:
        raise BenchmarkError("experiment output policy must allow exactly four artifacts")

    count_per_channel = _require_int(
        total.get("hla_interactions"),
        "expected_counts.total.hla_interactions",
        minimum=1,
    )
    atomic_deliveries = _require_int(
        total.get("atomic_event_deliveries"),
        "expected_counts.total.atomic_event_deliveries",
        minimum=1,
    )
    if count_per_channel != atomic_deliveries:
        raise BenchmarkError("DEVStone-HLA interaction and atomic-delivery totals must match")

    declared_identity = identity.get("digest")
    calculated_identity = _canonical_workload_identity(workload_document)
    if identity.get("algorithm") != "sha256" or declared_identity != calculated_identity:
        raise BenchmarkError("workload canonical identity does not validate")
    configured_identity = benchmark.get("configuration_identity_sha256")
    if configured_identity != declared_identity:
        raise BenchmarkError("experiment and workload identities differ")
    if benchmark.get("configuration_sha256") != sha256_file(workload_path):
        raise BenchmarkError("experiment and workload file hashes differ")

    return Contract(
        repo=repo,
        experiment_path=experiment_path,
        prerequisites_path=prerequisites_path,
        workload_path=workload_path,
        experiment_document=experiment_document,
        prerequisites_document=prerequisites_document,
        workload_document=workload_document,
        workload_file_sha256=sha256_file(workload_path),
        workload_identity_sha256=declared_identity,
        count_per_channel=count_per_channel,
        base_seed=base_seed,
        warmup_pairs=warmup_pairs,
        measured_pairs=measured_pairs,
        max_pair_attempts=max_pair_attempts,
        replacement_policy=replacement_policy,
        operation_warmup=operation_warmup,
        choreography=measurement["choreography"],
        callback_model=measurement["callback_model"],
        time_management=measurement["time_management"],
        timer_boundary_id=measurement["timer_boundary_id"],
        gorti_transport=gorti_transport,
        local_lrc_queue_capacity=local_lrc_queue_capacity,
        local_lrc_ack_every=local_lrc_ack_every,
        local_lrc_batch_size=local_lrc_batch_size,
        callback_representation=callback_representation,
        outbox_event_capacity=outbox_event_capacity,
        outbox_batch_size=outbox_batch_size,
        outbox_flush_interval_ms=outbox_flush_interval_ms,
        metric_ids=metric_ids,
    )


def _inside(path: Path, root: Path) -> bool:
    try:
        path.resolve().relative_to(root.resolve())
        return True
    except ValueError:
        return False


def _resolve_tool(value: str, which: Callable[[str], str | None]) -> str | None:
    candidate = Path(value).expanduser()
    if candidate.parent != Path(".") or candidate.is_absolute():
        return str(candidate.resolve()) if candidate.is_file() else None
    return which(value)


def _configured_fom(contract: Contract) -> Path:
    configured = contract.experiment["benchmark"].get("fom_path")
    if isinstance(configured, str) and configured:
        candidate = (contract.repo / configured).resolve()
        if candidate.is_file():
            return candidate
    return (
        contract.repo / "verification" / "commercial-rti" / "fom" / "CommercialRtiVerifier.xml"
    ).resolve()


def resolve_runtime_paths(
    args: argparse.Namespace,
    contract: Contract,
    *,
    environ: Mapping[str, str] | None = None,
    which: Callable[[str], str | None] = shutil.which,
) -> RuntimePaths:
    environ = environ or os.environ
    output_value = args.output or environ.get("GORTI_BENCHMARK_OUTPUT")
    output = Path(output_value).expanduser().resolve() if output_value else None

    portico_value = (
        args.portico_home
        or environ.get("PORTICO_HOME")
        or str(contract.repo / ".tools" / "portico-extracted" / "portico-2.1.4")
    )
    portico_home = Path(portico_value).expanduser().resolve()
    portico_jar = portico_home / "lib" / "portico.jar"
    portico_java = portico_home / "jre" / "bin" / "java"

    fom_value = args.fom or environ.get("GORTI_BENCHMARK_FOM")
    fom = Path(fom_value).expanduser().resolve() if fom_value else _configured_fom(contract)
    verifier_value = args.verifier_jar or environ.get("GORTI_VERIFIER_JAR")
    verifier_jar = Path(verifier_value).expanduser().resolve() if verifier_value else None

    return RuntimePaths(
        output=output,
        portico_home=portico_home,
        portico_jar=portico_jar,
        portico_java=portico_java,
        fom=fom,
        verifier_jar=verifier_jar,
        verifier_source_root=(contract.repo / "verification" / "commercial-rti" / "src").resolve(),
        comparator=(
            contract.repo / "verification" / "portico" / "compare_receive_order.py"
        ).resolve(),
        docker=_resolve_tool(args.docker, which),
        go=_resolve_tool(args.go, which),
        javac=_resolve_tool(args.javac, which),
        jar=_resolve_tool(args.jar, which),
    )


def preflight_report(contract: Contract, runtime: RuntimePaths) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    benchmark_config = contract.experiment["benchmark"]
    portico_config = next(
        item for item in contract.experiment["implementations"] if item["id"] == "portico"
    )
    container_pin = contract.prerequisites_document.get("container_pin", {})

    def add(name: str, ok: bool, detail: str, *, runtime_required: bool = True) -> None:
        checks.append(
            {
                "name": name,
                "ok": bool(ok),
                "runtime_required": runtime_required,
                "detail": detail,
            }
        )

    add("experiment contract", True, str(contract.experiment_path), runtime_required=False)
    add(
        "workload identity",
        True,
        contract.workload_identity_sha256,
        runtime_required=False,
    )
    add(
        "paired OM count",
        True,
        f"{contract.count_per_channel} updates + {contract.count_per_channel} interactions per run",
        runtime_required=False,
    )
    output_ok = runtime.output is not None and not _inside(runtime.output, contract.repo)
    output_new = runtime.output is not None and not runtime.output.exists()
    add(
        "external output",
        output_ok and output_new,
        str(runtime.output) if runtime.output else "GORTI_BENCHMARK_OUTPUT/--output not set",
    )
    add("Portico home", runtime.portico_home.is_dir(), str(runtime.portico_home))
    add("Portico jar", runtime.portico_jar.is_file(), str(runtime.portico_jar))
    add(
        "Portico jar hash",
        runtime.portico_jar.is_file()
        and sha256_file(runtime.portico_jar) == portico_config.get("artifact_sha256"),
        str(portico_config.get("artifact_sha256", "missing pin")),
    )
    add(
        "Portico bundled Java",
        runtime.portico_java.is_file(),
        str(runtime.portico_java),
    )
    portico_release = runtime.portico_home / "jre" / "release"
    pinned_portico_java = str(
        contract.prerequisites_document.get("runtime_pins", {})
        .get("portico_bundled_java", {})
        .get("version", "")
    ).split("+", 1)[0]
    release_text = (
        portico_release.read_text(encoding="utf-8", errors="replace")
        if portico_release.is_file()
        else ""
    )
    add(
        "Portico bundled Java version",
        bool(pinned_portico_java) and f'JAVA_VERSION="{pinned_portico_java}"' in release_text,
        pinned_portico_java or "missing pin",
    )
    add(
        "Portico home inside repository",
        _inside(runtime.portico_home, contract.repo),
        "required by the existing container-mount comparator",
    )
    add("FOM", runtime.fom.is_file(), str(runtime.fom))
    add(
        "FOM hash",
        runtime.fom.is_file() and sha256_file(runtime.fom) == benchmark_config.get("fom_sha256"),
        str(benchmark_config.get("fom_sha256", "missing pin")),
    )
    add(
        "FOM inside repository",
        _inside(runtime.fom, contract.repo),
        "required by the existing container-mount comparator",
    )
    add("comparison harness", runtime.comparator.is_file(), str(runtime.comparator))
    override_builder = contract.repo / TRANSPORT_OVERRIDE_BUILDER
    add(
        "transport override builder",
        override_builder.is_file(),
        str(override_builder),
    )
    add("Docker", runtime.docker is not None, runtime.docker or "not found")
    add("Go", runtime.go is not None, runtime.go or "not found")
    add(
        "container image reference",
        container_pin.get("image") == CONTAINER_IMAGE,
        str(container_pin.get("image", "missing pin")),
    )
    add(
        "container platform",
        container_pin.get("platform") == CONTAINER_PLATFORM,
        str(container_pin.get("platform", "missing pin")),
    )
    pinned_digest = container_pin.get("digest")
    add(
        "container image digest pin",
        isinstance(pinned_digest, str)
        and pinned_digest.startswith("sha256:")
        and bool(SHA256.fullmatch(pinned_digest.removeprefix("sha256:"))),
        str(pinned_digest or "missing pin"),
    )

    add(
        "prebuilt Java verifier override absent",
        runtime.verifier_jar is None,
        (
            "not set; verifier will be compiled from current sources"
            if runtime.verifier_jar is None
            else f"prohibited override: {runtime.verifier_jar}"
        ),
    )
    java_sources = list(runtime.verifier_source_root.rglob("*.java"))
    add(
        "Java verifier sources",
        bool(java_sources),
        f"{len(java_sources)} sources under {runtime.verifier_source_root}",
    )
    add("javac", runtime.javac is not None, runtime.javac or "not found")
    add("jar", runtime.jar is not None, runtime.jar or "not found")

    return {
        "mode": "preflight",
        "projection": {
            "name": PROJECTION_NAME,
            "scope": PROJECTION_SCOPE,
            "count_per_channel": contract.count_per_channel,
            "om_service_calls_per_run": 2 * contract.count_per_channel,
            "gorti_receive_order_transport": contract.gorti_transport,
        },
        "schedule": {
            "warmup_pairs": contract.warmup_pairs,
            "measured_pairs": contract.measured_pairs,
            "measured_runs_per_implementation": contract.measured_pairs,
            "first_position": {"portico": 15, "gorti": 15},
            "base_seed": contract.base_seed,
        },
        "checks": checks,
        "runtime_ready": all(item["ok"] for item in checks if item["runtime_required"]),
    }


def require_runtime_ready(report: Mapping[str, Any]) -> None:
    failures = [item for item in report["checks"] if item["runtime_required"] and not item["ok"]]
    if failures:
        detail = "; ".join(f"{item['name']}: {item['detail']}" for item in failures)
        raise BenchmarkError(f"runtime preflight failed: {detail}")


@contextlib.contextmanager
def transient_workspace(repo: Path) -> Iterator[Path]:
    parent = repo / ".tmp"
    created_parent = not parent.exists()
    parent.mkdir(parents=True, exist_ok=True)
    path = Path(tempfile.mkdtemp(prefix=".run-", dir=parent))
    try:
        yield path
    finally:
        shutil.rmtree(path, ignore_errors=True)
        if created_parent:
            with contextlib.suppress(OSError):
                parent.rmdir()


def _run_inherited(command: Sequence[str], *, cwd: Path) -> None:
    completed = subprocess.run(list(command), cwd=cwd, check=False)
    if completed.returncode:
        raise BenchmarkError(f"command failed with exit code {completed.returncode}: {command[0]}")


def _run_text(command: Sequence[str], *, cwd: Path) -> str:
    completed = subprocess.run(
        list(command),
        cwd=cwd,
        text=True,
        encoding="utf-8",
        errors="replace",
        capture_output=True,
        check=False,
    )
    if completed.returncode:
        detail = (completed.stderr or completed.stdout).strip()
        raise BenchmarkError(f"command failed: {command[0]}: {detail}")
    return (completed.stdout or completed.stderr).strip()


def prepare_verifier(
    contract: Contract,
    runtime: RuntimePaths,
    transient: Path,
    *,
    compiler_version: str,
) -> VerifierBuild:
    if runtime.verifier_jar is not None:
        raise BenchmarkError(
            "prebuilt Java verifier overrides are prohibited; remove "
            "--verifier-jar and GORTI_VERIFIER_JAR"
        )
    if runtime.javac is None or runtime.jar is None:
        raise BenchmarkError("javac and jar are required to build the Java verifier")

    build_inputs = _verifier_build_inputs(runtime, compiler_version)
    build = transient / "java-verifier"
    classes = build / "classes"
    classes.mkdir(parents=True)
    verifier_jar = build / "reference-rti-verifier.jar"
    manifest = build / "MANIFEST.MF"
    manifest.write_text(
        "Manifest-Version: 1.0\n"
        "Main-Class: gorti.verification.commercialrti.CommercialRtiVerifier\n\n",
        encoding="ascii",
    )
    _run_inherited(
        [
            runtime.javac,
            *VERIFIER_COMPILER_FLAGS,
            "-cp",
            str(runtime.portico_jar),
            "-d",
            str(classes),
            *(str(source) for source in build_inputs.sources),
        ],
        cwd=contract.repo,
    )
    _run_inherited(
        [
            runtime.jar,
            "cfm",
            str(verifier_jar),
            str(manifest),
            "-C",
            str(classes),
            ".",
        ],
        cwd=contract.repo,
    )
    if not verifier_jar.is_file():
        raise BenchmarkError("jar did not produce the Java verifier artifact")
    return VerifierBuild(
        jar=verifier_jar,
        jar_sha256=sha256_file(verifier_jar),
        inputs=build_inputs,
    )


def verifier_build_runtime_evidence(verifier: VerifierBuild) -> dict[str, Any]:
    return {
        "schema": VERIFIER_BUILD_INPUT_SCHEMA,
        "build_input_sha256": verifier.inputs.build_input_sha256,
        "jar_sha256": verifier.jar_sha256,
        "main_source_sha256": verifier.inputs.main_source_sha256,
        "sources": [
            {"path": path, "sha256": digest} for path, digest in verifier.inputs.source_records
        ],
        "portico_api_jar_sha256": verifier.inputs.portico_api_jar_sha256,
        "compiler_version": verifier.inputs.compiler_version,
        "compiler_args": list(verifier.inputs.compiler_args),
    }


def invoke_comparator(
    args: argparse.Namespace,
    contract: Contract,
    runtime: RuntimePaths,
    verifier_jar: Path,
    transient: Path,
) -> dict[str, Any]:
    if runtime.docker is None or runtime.go is None:
        raise BenchmarkError("Docker and Go are required")
    if args.operation_warmup != contract.operation_warmup:
        raise BenchmarkError(
            "operation warm-up override differs from the tracked measurement contract"
        )
    output = transient / "comparison"
    command = [
        sys.executable,
        str(runtime.comparator),
        "--repo",
        str(contract.repo),
        "--portico-home",
        str(runtime.portico_home),
        "--verifier-jar",
        str(verifier_jar),
        "--fom",
        str(runtime.fom),
        "--output",
        str(output),
        "--seed",
        str(contract.base_seed),
        "--workload",
        str(contract.workload_path),
        "--operation-warmup",
        str(contract.operation_warmup),
        "--warmup",
        str(contract.warmup_pairs),
        "--measured",
        str(contract.measured_pairs),
        "--max-pair-attempts",
        str(contract.max_pair_attempts),
        "--timeout-ms",
        str(args.timeout_ms),
        "--docker",
        runtime.docker,
        "--go",
        runtime.go,
        "--gorti-transport",
        contract.gorti_transport,
        "--local-lrc-queue",
        str(contract.local_lrc_queue_capacity),
        "--local-lrc-ack-every",
        str(contract.local_lrc_ack_every),
        "--local-lrc-batch-size",
        str(contract.local_lrc_batch_size),
        "--gorti-callback-representation",
        contract.callback_representation,
        "--gorti-outbox-event-capacity",
        str(contract.outbox_event_capacity),
        "--gorti-outbox-batch-size",
        str(contract.outbox_batch_size),
        "--gorti-outbox-flush-interval-ms",
        str(contract.outbox_flush_interval_ms),
        "--gorti-audit-replay-plugin",
        GORTI_AUDIT_REPLAY_PLUGIN,
    ]
    _run_inherited(command, cwd=contract.repo)
    comparison_path = output / "comparison.json"
    if not comparison_path.is_file():
        raise BenchmarkError("comparison harness did not produce comparison.json")
    return _read_json(comparison_path)


def _number(value: Any, label: str) -> float:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise BenchmarkError(f"{label} must be numeric")
    result = float(value)
    if not math.isfinite(result) or result < 0:
        raise BenchmarkError(f"{label} must be finite and non-negative")
    return result


def _sha(value: Any, label: str) -> str:
    if not isinstance(value, str) or SHA256.fullmatch(value) is None:
        raise BenchmarkError(f"{label} must be a resolved lowercase SHA-256")
    return value


def _validate_gorti_server_lifecycle(value: Any, label: str) -> None:
    expected_keys = {
        "alive_before_shutdown",
        "shutdown_requested",
        "exit_code_after_shutdown",
        "cleanup_verified",
    }
    if not isinstance(value, dict) or set(value) != expected_keys:
        raise BenchmarkError(f"{label} must contain exact gorti server lifecycle evidence")
    for key in ("alive_before_shutdown", "shutdown_requested", "cleanup_verified"):
        if value.get(key) is not True:
            raise BenchmarkError(f"{label}.{key} must be true")
    _require_int(value.get("exit_code_after_shutdown"), f"{label}.exit_code_after_shutdown")


def _validate_run_runtime_profile(
    record: Mapping[str, Any], implementation: str, label: str
) -> None:
    expected_profile_id = GORTI_PROFILE_ID if implementation == "gorti" else PORTICO_PROFILE_ID
    if record.get("profile_id") != expected_profile_id:
        raise BenchmarkError(f"{label}.profile_id must be {expected_profile_id!r}")

    profile = record.get("runtime_profile")
    if not isinstance(profile, dict):
        raise BenchmarkError(f"{label}.runtime_profile must be an object")
    if profile.get("schema") != RUNTIME_PROFILE_SCHEMA:
        raise BenchmarkError(
            f"{label}.runtime_profile.schema must be {RUNTIME_PROFILE_SCHEMA!r}"
        )
    if profile.get("profile_id") != expected_profile_id:
        raise BenchmarkError(
            f"{label}.runtime_profile.profile_id must be {expected_profile_id!r}"
        )

    if implementation == "gorti":
        expected_fields = {
            "audit_replay_plugin": GORTI_AUDIT_REPLAY_PLUGIN,
            "event_journal_sink": "none",
            "event_journal_persistence": False,
            "event_journal_replay_available": False,
            "event_journal_failure_boundary": False,
        }
    else:
        expected_fields = {
            "diagnostic_logging_level": "off",
            "traffic_auditing": "off",
        }
    for key, expected in expected_fields.items():
        actual = profile.get(key)
        matches = actual is expected if isinstance(expected, bool) else actual == expected
        if not matches:
            raise BenchmarkError(
                f"{label}.runtime_profile.{key} must be {expected!r}"
            )


def validate_comparison_runs(
    comparison: Mapping[str, Any], contract: Contract
) -> list[dict[str, Any]]:
    if comparison.get("schema") != "gorti.portico-receive-order-comparison/v2":
        raise BenchmarkError("unsupported comparison schema")
    workload = comparison.get("workload")
    protocol = comparison.get("protocol")
    records = comparison.get("runs")
    if not isinstance(workload, dict) or not isinstance(protocol, dict):
        raise BenchmarkError("comparison workload/protocol metadata is missing")
    metadata = comparison.get("metadata")
    if not isinstance(metadata, dict):
        raise BenchmarkError("comparison runtime metadata is missing")
    if metadata.get("gorti_receive_order_transport") != contract.gorti_transport:
        raise BenchmarkError("comparison gorti transport differs from contract")
    if metadata.get("gorti_local_lrc_queue_capacity") != contract.local_lrc_queue_capacity:
        raise BenchmarkError("comparison LocalLRC queue capacity differs from contract")
    if metadata.get("gorti_local_lrc_ack_every") != contract.local_lrc_ack_every:
        raise BenchmarkError("comparison LocalLRC ACK interval differs from contract")
    if metadata.get("gorti_local_lrc_batch_size") != contract.local_lrc_batch_size:
        raise BenchmarkError("comparison LocalLRC batch size differs from contract")
    if metadata.get("gorti_callback_representation") != contract.callback_representation:
        raise BenchmarkError("comparison callback representation differs from contract")
    if metadata.get("gorti_outbox_event_capacity") != contract.outbox_event_capacity:
        raise BenchmarkError("comparison outbox event capacity differs from contract")
    if metadata.get("gorti_outbox_batch_size") != contract.outbox_batch_size:
        raise BenchmarkError("comparison outbox batch size differs from contract")
    if metadata.get("gorti_outbox_flush_interval_ms") != contract.outbox_flush_interval_ms:
        raise BenchmarkError("comparison outbox flush interval differs from contract")
    if metadata.get("gorti_audit_replay_plugin") != GORTI_AUDIT_REPLAY_PLUGIN:
        raise BenchmarkError(
            "comparison metadata.gorti_audit_replay_plugin must be "
            f"{GORTI_AUDIT_REPLAY_PLUGIN!r}"
        )
    if workload.get("count") != contract.count_per_channel:
        raise BenchmarkError("comparison count differs from DEVStone paired OM projection")
    if workload.get("configuration_sha256") != contract.workload_file_sha256:
        raise BenchmarkError("comparison static workload hash differs from contract")
    if (
        workload.get("topology_identity_sha256")
        != contract.workload_document["topology_identity"]["digest"]
    ):
        raise BenchmarkError("comparison topology identity differs from contract")
    if workload.get("plan_format") != "DVSHLA1":
        raise BenchmarkError("comparison must use the DVSHLA1 binary plan")
    if int(workload.get("seed", -1)) != contract.base_seed:
        raise BenchmarkError("comparison base seed differs from experiment contract")
    exact_workload_contract = {
        "operation_warmup": contract.operation_warmup,
        "choreography": contract.choreography,
        "callback_model": contract.callback_model,
        "time_management": contract.time_management,
        "delivery_boundary": contract.timer_boundary_id,
    }
    for key, expected in exact_workload_contract.items():
        if workload.get(key) != expected:
            raise BenchmarkError(f"comparison workload.{key} must be {expected!r}")
    if workload.get("primary_metric_boundary") != "subscriber final callback arrival":
        raise BenchmarkError(
            "comparison primary metric boundary is not the common subscriber boundary"
        )
    if workload.get("caller_latency_comparable") is not False:
        raise BenchmarkError("comparison must mark caller latency boundaries as non-comparable")
    caller_boundaries = workload.get("caller_latency_boundaries")
    if not isinstance(caller_boundaries, dict) or set(caller_boundaries) != {"portico", "gorti"}:
        raise BenchmarkError("comparison must record both caller-latency boundaries")
    if protocol.get("warmup_pairs") != 5 or protocol.get("measured_pairs") != 30:
        raise BenchmarkError("comparison must contain 5 warm-up and 30 measured pairs")
    if protocol.get("max_pair_attempts") != contract.max_pair_attempts:
        raise BenchmarkError(
            f"comparison protocol.max_pair_attempts must be {contract.max_pair_attempts}"
        )
    if protocol.get("replacement_policy") != contract.replacement_policy:
        raise BenchmarkError("comparison replacement policy differs from the tracked contract")
    if protocol.get("portico_jgroups_response_timeout_ms") != PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS:
        raise BenchmarkError(
            "comparison Portico JGroups response timeout differs from the startup control"
        )
    if protocol.get("portico_publisher_ready_gate") is not PORTICO_PUBLISHER_READY_GATE:
        raise BenchmarkError(
            "comparison protocol.portico_publisher_ready_gate must be exactly true"
        )
    if protocol.get("portico_ordered_teardown_gate") is not PORTICO_ORDERED_TEARDOWN_GATE:
        raise BenchmarkError(
            "comparison protocol.portico_ordered_teardown_gate must be exactly true"
        )
    discarded_pair_attempts = _require_int(
        protocol.get("discarded_pair_attempts"),
        "comparison protocol.discarded_pair_attempts",
    )
    if protocol.get("retain_run_artifacts") is not False:
        raise BenchmarkError("comparison must discard transient run artifacts")
    if protocol.get("fresh_processes_per_run") is not True:
        raise BenchmarkError("comparison must use fresh processes for every run")
    if protocol.get("logging_disabled") is not True:
        raise BenchmarkError("comparison must disable measured-path logging")
    if protocol.get("cleanup_verified_all_runs") is not True:
        raise BenchmarkError("comparison must verify cleanup for every run")
    if workload.get("process_transcripts") != "not_written":
        raise BenchmarkError("comparison must not write process transcripts")
    if not isinstance(records, list):
        raise BenchmarkError("comparison runs must be an array")
    if len(records) != 2 * (contract.warmup_pairs + contract.measured_pairs):
        raise BenchmarkError("comparison must contain exactly 10 warm-up and 60 measured runs")
    unknown_phases = {
        record.get("phase")
        for record in records
        if record.get("phase") not in {"warmup", "measured"}
    }
    if unknown_phases:
        unknown = sorted(map(str, unknown_phases))
        raise BenchmarkError(f"comparison contains unknown phases: {unknown}")

    all_plan_hashes: list[str] = []
    derived_discarded_pair_attempts = 0
    measured: list[dict[str, Any]] = []
    for phase, pair_count, seed_offset in (
        ("warmup", contract.warmup_pairs, contract.measured_pairs),
        ("measured", contract.measured_pairs, 0),
    ):
        phase_records = [record for record in records if record.get("phase") == phase]
        expected_implementation_counts = Counter({"portico": pair_count, "gorti": pair_count})
        implementation_counts: Counter[str] = Counter()
        pairs: defaultdict[int, list[dict[str, Any]]] = defaultdict(list)
        for index, record in enumerate(phase_records):
            label = f"{phase} run {index}"
            if record.get("status") != "ok":
                raise BenchmarkError(f"{label}.status must be 'ok'")
            participant_exit_codes = record.get("participant_exit_codes")
            if not isinstance(participant_exit_codes, dict):
                raise BenchmarkError(f"{label}.participant_exit_codes must be an object")
            expected_participants = {"publisher", "subscriber"}
            if set(participant_exit_codes) != expected_participants:
                raise BenchmarkError(
                    f"{label}.participant_exit_codes must contain exactly publisher and subscriber"
                )
            for participant, exit_code in participant_exit_codes.items():
                if not isinstance(exit_code, int) or isinstance(exit_code, bool) or exit_code != 0:
                    raise BenchmarkError(
                        f"{label}.participant_exit_codes.{participant} must be zero"
                    )
            implementation = record.get("implementation")
            if implementation not in {"portico", "gorti"}:
                raise BenchmarkError(f"{label}: unknown implementation")
            _validate_run_runtime_profile(record, implementation, label)
            pair = _require_int(record.get("pair"), f"{label}.pair", minimum=1)
            if pair > pair_count:
                raise BenchmarkError(f"{label}.pair must be <= {pair_count}")
            expected_order = ["portico", "gorti"] if pair % 2 == 1 else ["gorti", "portico"]
            if record.get("order") != expected_order:
                raise BenchmarkError(f"{label}.order does not follow alternating AB/BA")
            position = _require_int(record.get("position"), f"{label}.position", minimum=1)
            if position not in {1, 2} or expected_order[position - 1] != implementation:
                raise BenchmarkError(f"{label}: position and order disagree")
            pair_attempt = _require_int(
                record.get("pair_attempt"), f"{label}.pair_attempt", minimum=1
            )
            if pair_attempt > contract.max_pair_attempts:
                raise BenchmarkError(
                    f"{label}.pair_attempt must be <= {contract.max_pair_attempts}"
                )
            seed = _require_int(record.get("seed"), f"{label}.seed")
            expected_seed = contract.base_seed + seed_offset + pair - 1
            if seed != expected_seed:
                raise BenchmarkError(
                    f"{phase} pair {pair}: seed {seed} != expected {expected_seed}"
                )
            medians = record.get("operation_medians_ns")
            if not isinstance(medians, dict):
                raise BenchmarkError(f"{label}: operation medians are missing")
            _number(medians.get("updateAttributeValues"), "updateAttributeValues median")
            _number(medians.get("sendInteraction"), "sendInteraction median")
            _number(record.get("batch_ns"), "completed batch")
            _number(record.get("throughput_deliveries_per_second"), "throughput")
            evidence = record.get("evidence")
            if not isinstance(evidence, dict):
                raise BenchmarkError(f"{label}: semantic evidence is missing")
            missing_evidence = set(SEMANTIC_EVIDENCE_FIELDS) - set(evidence)
            if missing_evidence:
                raise BenchmarkError(f"{label}: missing evidence {sorted(missing_evidence)}")
            for key in (
                "workload_instance_sha256",
                "attribute_callback_sha256",
                "interaction_callback_sha256",
                "callback_trace_sha256",
                "terminal_state_sha256",
            ):
                _sha(evidence.get(key), f"{label}.{key}")
            expected_callbacks = 2 * contract.count_per_channel
            if (
                evidence.get("expected_callbacks") != expected_callbacks
                or evidence.get("delivered_callbacks") != expected_callbacks
            ):
                raise BenchmarkError(f"{label}: incomplete callback accounting")
            for key in (
                "rejected_callbacks",
                "dropped_callbacks",
                "unexpected_callbacks",
                "duplicate_callbacks",
                "invalid_callbacks",
            ):
                if evidence.get(key) != 0:
                    raise BenchmarkError(f"{label}: {key} must be zero")
            for key in (
                "ready_synchronized",
                "start_synchronized",
                "measure_synchronized",
                "done_synchronized",
            ):
                if evidence.get(key) is not True:
                    raise BenchmarkError(f"{label}: {key} must be true")
            if record.get("logging_disabled") is not True:
                raise BenchmarkError(f"{label}: logging_disabled must be true")
            if record.get("cleanup_verified") is not True:
                raise BenchmarkError(f"{label}: cleanup_verified must be true")
            if record.get("artifacts_retained") is not False or "artifact_path" in record:
                raise BenchmarkError(f"{label}: transient artifacts were retained")
            if implementation == "gorti":
                _validate_gorti_server_lifecycle(
                    record.get("server_lifecycle"), f"{label}.server_lifecycle"
                )
            implementation_counts[implementation] += 1
            pairs[pair].append(record)

        if implementation_counts != expected_implementation_counts:
            raise BenchmarkError(
                f"{phase} runs must contain exactly {pair_count} Portico and "
                f"{pair_count} gorti runs"
            )
        if set(pairs) != set(range(1, pair_count + 1)):
            raise BenchmarkError(f"{phase} pair indices must cover 1 through {pair_count}")
        for pair_index, pair_records in sorted(pairs.items()):
            if len(pair_records) != 2:
                raise BenchmarkError(f"{phase} pair {pair_index}: expected two runs")
            if {record["implementation"] for record in pair_records} != {"portico", "gorti"}:
                raise BenchmarkError(f"{phase} pair {pair_index}: implementations differ")
            if {record["position"] for record in pair_records} != {1, 2}:
                raise BenchmarkError(f"{phase} pair {pair_index}: positions differ")
            if len({record["seed"] for record in pair_records}) != 1:
                raise BenchmarkError(f"{phase} pair {pair_index}: seeds differ")
            pair_attempts = {record["pair_attempt"] for record in pair_records}
            if len(pair_attempts) != 1:
                raise BenchmarkError(f"{phase} pair {pair_index}: pair attempts differ")
            derived_discarded_pair_attempts += next(iter(pair_attempts)) - 1
            first_evidence = pair_records[0]["evidence"]
            second_evidence = pair_records[1]["evidence"]
            for key in SEMANTIC_EVIDENCE_FIELDS:
                if first_evidence.get(key) != second_evidence.get(key):
                    raise BenchmarkError(
                        f"{phase} pair {pair_index}: semantic evidence differs for {key}"
                    )
            all_plan_hashes.append(first_evidence["workload_instance_sha256"])
        if phase == "measured":
            measured = phase_records

    if len(set(all_plan_hashes)) != contract.warmup_pairs + contract.measured_pairs:
        raise BenchmarkError("all 35 warm-up and measured pairs need unique workload plans")
    if discarded_pair_attempts != derived_discarded_pair_attempts:
        raise BenchmarkError(
            "comparison protocol.discarded_pair_attempts does not equal the accepted "
            "pair-attempt count"
        )
    return sorted(measured, key=lambda item: (item["pair"], item["position"]))


def _physical_memory_bytes() -> int:
    if os.name == "nt":

        class MemoryStatus(ctypes.Structure):
            _fields_ = [
                ("length", ctypes.c_ulong),
                ("memory_load", ctypes.c_ulong),
                ("total_phys", ctypes.c_ulonglong),
                ("avail_phys", ctypes.c_ulonglong),
                ("total_page_file", ctypes.c_ulonglong),
                ("avail_page_file", ctypes.c_ulonglong),
                ("total_virtual", ctypes.c_ulonglong),
                ("avail_virtual", ctypes.c_ulonglong),
                ("avail_extended_virtual", ctypes.c_ulonglong),
            ]

        status = MemoryStatus()
        status.length = ctypes.sizeof(MemoryStatus)
        if ctypes.windll.kernel32.GlobalMemoryStatusEx(ctypes.byref(status)):
            return max(1, int(status.total_phys))
    try:
        return max(1, int(os.sysconf("SC_PHYS_PAGES") * os.sysconf("SC_PAGE_SIZE")))
    except (AttributeError, OSError, ValueError):
        return 1


def adapt_results(comparison: Mapping[str, Any], contract: Contract) -> dict[str, Any]:
    measured = validate_comparison_runs(comparison, contract)
    metadata = comparison.get("metadata")
    workload = comparison.get("workload")
    protocol = comparison.get("protocol")
    if (
        not isinstance(metadata, dict)
        or not isinstance(workload, dict)
        or not isinstance(protocol, dict)
    ):
        raise BenchmarkError("comparison runtime metadata is missing")

    portico_hash = _sha(metadata.get("portico_jar_sha256"), "Portico jar hash")
    gorti_hash = _sha(metadata.get("gorti_rtid_sha256"), "gorti server hash")
    _sha(metadata.get("transport_override_sha256"), "Portico transport override hash")
    fom_hash = _sha(workload.get("fom_sha256"), "FOM hash")
    gorti_commit = metadata.get("gorti_commit")
    if not isinstance(gorti_commit, str) or not gorti_commit:
        raise BenchmarkError("gorti commit is missing")

    config = contract.workload_document["configuration"]
    profile = (
        f"DEVStone-HLA {config['topology']} w{config['width']} d{config['depth']} "
        f"n{config['external_event_count']} paired OM projection"
    )
    created_at = metadata.get("generated_at")
    if not isinstance(created_at, str) or not created_at:
        created_at = dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")

    all_metric_definitions = {
        "update_attribute_values_median_ns": {
            "id": "update_attribute_values_median_ns",
            "label": "updateAttributeValues caller-return median (diagnostic boundary)",
            "unit": "ns",
            "direction": "lower-is-better",
        },
        "send_interaction_median_ns": {
            "id": "send_interaction_median_ns",
            "label": "sendInteraction caller-return median (diagnostic boundary)",
            "unit": "ns",
            "direction": "lower-is-better",
        },
        "completed_delivery_batch_ns": {
            "id": "completed_delivery_batch_ns",
            "label": "Subscriber OM callback completion time",
            "unit": "ns",
            "direction": "lower-is-better",
        },
        "deliveries_per_second": {
            "id": "deliveries_per_second",
            "label": "Subscriber OM callback throughput",
            "unit": "deliveries/s",
            "direction": "higher-is-better",
        },
    }
    unknown_metric_ids = set(contract.metric_ids) - set(all_metric_definitions)
    if unknown_metric_ids:
        raise BenchmarkError(f"unsupported experiment metrics: {sorted(unknown_metric_ids)}")
    metric_definitions = [all_metric_definitions[metric_id] for metric_id in contract.metric_ids]
    runs = []
    for record in measured:
        pair_index = int(record["pair"])
        implementation = str(record["implementation"])
        pair_attempt = int(record["pair_attempt"])
        medians = record["operation_medians_ns"]
        source_evidence = record["evidence"]
        all_measurements = {
            "update_attribute_values_median_ns": _number(
                medians["updateAttributeValues"], "updateAttributeValues median"
            ),
            "send_interaction_median_ns": _number(
                medians["sendInteraction"], "sendInteraction median"
            ),
            "completed_delivery_batch_ns": _number(record["batch_ns"], "completed batch"),
            "deliveries_per_second": _number(
                record["throughput_deliveries_per_second"], "throughput"
            ),
        }
        runs.append(
            {
                "run_id": f"pair-{pair_index:02d}-attempt-{pair_attempt}-{implementation}",
                "pair_id": f"pair-{pair_index:02d}",
                "pair_index": pair_index,
                "implementation": implementation,
                "position": int(record["position"]),
                "seed": int(record["seed"]),
                "status": "ok",
                "measurements": {
                    metric_id: all_measurements[metric_id] for metric_id in contract.metric_ids
                },
                "evidence": {
                    "fom_sha256": fom_hash,
                    "workload_sha256": contract.workload_file_sha256,
                    **{key: source_evidence[key] for key in SEMANTIC_EVIDENCE_FIELDS},
                },
            }
        )

    cpu = platform.processor() or os.environ.get("PROCESSOR_IDENTIFIER") or "unknown-cpu"
    host_material = f"{metadata.get('host', platform.node())}|{platform.machine()}"
    host_id = "host-" + hashlib.sha256(host_material.encode("utf-8")).hexdigest()[:12]
    portico_config = next(
        item for item in contract.experiment["implementations"] if item["id"] == "portico"
    )
    gorti_config = next(
        item for item in contract.experiment["implementations"] if item["id"] == "gorti"
    )
    container_image = str(metadata.get("container_image_reference") or CONTAINER_IMAGE)
    container_platform = str(metadata.get("container_platform") or CONTAINER_PLATFORM)
    go_version = str(metadata.get("go_version") or "Go compiler")
    go_version_parts = go_version.split()
    go_toolchain = " ".join(go_version_parts[:3]) if len(go_version_parts) >= 3 else go_version
    gorti_label = str(gorti_config["product"])
    if contract.gorti_transport == "local-lrc":
        gorti_label += (
            f" LocalLRC (queue {contract.local_lrc_queue_capacity}, "
            f"ACK {contract.local_lrc_ack_every}, batch {contract.local_lrc_batch_size})"
        )
    else:
        gorti_label += " confirmed"
    return {
        "schema_version": "gorti.benchmark.results/v1",
        "benchmark": {
            "name": "DEVStone-HLA",
            "version": contract.experiment["benchmark"]["version"],
            "profile": profile,
            "configuration_sha256": contract.workload_file_sha256,
        },
        "experiment": {
            "id": contract.experiment["id"],
            "created_at": created_at,
            "design": "paired-balanced-ab-ba",
            "runs_per_implementation": 30,
            "warmup_runs": 5,
            "base_seed": contract.base_seed,
            "fom_sha256": fom_hash,
            "completion_boundary": str(workload["delivery_boundary"]),
            "process_model": (
                "fresh-process-per-run" if protocol["fresh_processes_per_run"] else "invalid"
            ),
        },
        "environment": {
            "host_id": host_id,
            "os": str(metadata.get("host_os") or platform.platform()),
            "cpu": cpu,
            "logical_cores": max(1, os.cpu_count() or 1),
            "memory_bytes": _physical_memory_bytes(),
            "python_version": platform.python_version(),
        },
        "implementations": [
            {
                "id": "portico",
                "label": portico_config["product"],
                "version": str(metadata.get("portico_version") or portico_config["version"]),
                "runtime": f"Portico LRC; {container_image}; {container_platform}",
                "artifact_sha256": portico_hash,
                "source_revision": str(portico_config["source_revision"]),
            },
            {
                "id": "gorti",
                "label": gorti_label,
                "version": str(gorti_config["version"]),
                "runtime": (
                    f"{go_toolchain} build; {container_image}; {container_platform} execution"
                ),
                "artifact_sha256": gorti_hash,
                "source_revision": gorti_commit,
            },
        ],
        "metrics": metric_definitions,
        "runs": runs,
    }


def _load_common(repo: Path) -> tuple[Callable[..., Any], Callable[..., Any], Callable[..., Any]]:
    common = repo / "benchmark" / "common"
    common_text = str(common)
    if common_text not in sys.path:
        sys.path.insert(0, common_text)
    from analyze import analyze_document
    from benchmark_core import validate_result_document
    from render_latex import render_latex

    return validate_result_document, analyze_document, render_latex


def build_artifact_payloads(
    results: dict[str, Any],
    contract: Contract,
    comparison: Mapping[str, Any],
    runtime_evidence: Mapping[str, Any],
    *,
    bootstrap_resamples: int = 10000,
) -> dict[str, str]:
    validate_result, analyze_document, render_latex = _load_common(contract.repo)
    validate_result(results)
    analysis = analyze_document(
        results,
        bootstrap_resamples=bootstrap_resamples,
        bootstrap_seed=contract.base_seed,
    )
    latex = render_latex(analysis)
    results_text = json.dumps(results, ensure_ascii=False, allow_nan=False, indent=2) + "\n"
    analysis_text = json.dumps(analysis, ensure_ascii=False, allow_nan=False, indent=2) + "\n"

    metadata = comparison["metadata"]
    workload = comparison["workload"]
    protocol = comparison["protocol"]
    verifier_build = runtime_evidence.get("java_verifier_build")
    if not isinstance(verifier_build, Mapping):
        raise BenchmarkError("Java verifier build runtime evidence is missing")
    transport_override_build = runtime_evidence.get("transport_override_build")
    if not isinstance(transport_override_build, Mapping):
        raise BenchmarkError("Portico transport override build evidence is missing")
    if transport_override_build.get("schema") != (
        "gorti.portico-transport-override-build-input/v1"
    ):
        raise BenchmarkError("unsupported Portico transport override build-input schema")
    for key in (
        "build_input_sha256",
        "builder_sha256",
        "portico_jar_sha256",
        "source_resource_sha256",
    ):
        _sha(transport_override_build.get(key), f"transport override {key}")
    if transport_override_build.get("builder_path") != TRANSPORT_OVERRIDE_BUILDER.as_posix():
        raise BenchmarkError("transport override builder path differs from the tracked path")
    if transport_override_build.get("source_resource") != TRANSPORT_OVERRIDE_RESOURCE:
        raise BenchmarkError("transport override source resource differs from the tracked path")
    if transport_override_build.get("portico_jar_sha256") != metadata.get("portico_jar_sha256"):
        raise BenchmarkError("transport override input differs from the selected Portico jar")
    manifest = {
        "schema_version": "gorti.benchmark.manifest/v1",
        "generated_at": results["experiment"]["created_at"],
        "benchmark": {
            "requested_name": "DEVSOne",
            "resolved_name": "DEVStone",
            "profile": results["benchmark"]["profile"],
            "classification": "DEVStone-derived HLA/RTI traffic benchmark",
        },
        "projection": {
            "name": PROJECTION_NAME,
            "scope": PROJECTION_SCOPE,
            "devstone_atomic_event_deliveries": contract.count_per_channel,
            "object_updates_per_run": contract.count_per_channel,
            "interactions_per_run": contract.count_per_channel,
            "om_service_calls_per_run": 2 * contract.count_per_channel,
        },
        "design": {
            "warmup_pairs": 5,
            "measured_pairs": 30,
            "runs_per_implementation": 30,
            "order": "paired-balanced-ab-ba-15-15",
            "measured_pair_seed_rule": "base_seed + pair_index - 1",
            "warmup_pair_seed_rule": ("base_seed + measured_pair_count + pair_index - 1"),
            "base_seed": contract.base_seed,
            "operation_warmup": workload["operation_warmup"],
            "max_pair_attempts": contract.max_pair_attempts,
            "replacement_policy": contract.replacement_policy,
            "discarded_pair_attempts": _require_int(
                protocol.get("discarded_pair_attempts"),
                "comparison protocol.discarded_pair_attempts",
            ),
            "retry_evidence_retained": "counts-and-policy-only",
            "portico_jgroups_response_timeout_ms": (PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS),
        },
        "decision_rule": {
            "primary_metric": PRIMARY_COMPLETION_METRIC_ID,
            "ratio": "gorti-divided-by-portico",
            "practical_equivalence_interval": [
                PRACTICAL_RATIO_LOWER,
                PRACTICAL_RATIO_UPPER,
            ],
            "gorti_superior": "upper 95% CI below interval lower bound",
            "portico_superior": "lower 95% CI above interval upper bound",
            "practically_equivalent": "entire 95% CI inside interval",
            "otherwise": "inconclusive",
        },
        "startup_controls": {
            "portico_publisher_ready_gate": PORTICO_PUBLISHER_READY_GATE,
            "portico_jgroups_response_timeout_ms": (PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS),
            "measurement_boundary": "outside",
            "ready_marker_lifecycle": "removed-before-subscriber-launch",
            "ready_marker_retained": False,
            "ready_marker_is_log": False,
        },
        "teardown_controls": {
            "portico_ordered_teardown_gate": PORTICO_ORDERED_TEARDOWN_GATE,
            "portico_jgroups_response_timeout_ms": PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS,
            "measurement_boundary": "outside",
            "handshake_kind": "three-phase-transient",
            "handshake_phases": list(PORTICO_ORDERED_TEARDOWN_PHASES),
            "handshake_evidence_disposition": "discarded",
            "handshake_evidence_retained": False,
            "handshake_evidence_is_log": False,
        },
        "resolved_inputs": {
            "experiment": {
                "path": str(contract.experiment_path.relative_to(contract.repo)).replace("\\", "/"),
                "sha256": sha256_file(contract.experiment_path),
            },
            "prerequisites": {
                "path": str(contract.prerequisites_path.relative_to(contract.repo)).replace(
                    "\\", "/"
                ),
                "sha256": sha256_file(contract.prerequisites_path),
            },
            "workload": {
                "path": str(contract.workload_path.relative_to(contract.repo)).replace("\\", "/"),
                "file_sha256": contract.workload_file_sha256,
                "identity_sha256": contract.workload_identity_sha256,
                "topology_identity_sha256": contract.workload_document["topology_identity"][
                    "digest"
                ],
                "plan_format": "DVSHLA1",
            },
            "fom": {
                "path": runtime_evidence["fom_path"],
                "sha256": _sha(workload["fom_sha256"], "FOM hash"),
            },
            "comparison_harness": {
                "path": "verification/portico/compare_receive_order.py",
                "sha256": _sha(metadata["comparison_harness_sha256"], "comparison harness hash"),
            },
            "portico_transport_override": {
                "sha256": _sha(
                    metadata.get("transport_override_sha256"),
                    "Portico transport override hash",
                ),
                "build_input": dict(transport_override_build),
            },
            "orchestrator": {
                "path": "benchmark/portico-gorti/orchestrator.py",
                "sha256": sha256_file(Path(__file__).resolve()),
            },
            "java_verifier": {
                "sha256": _sha(metadata["verifier_jar_sha256"], "verifier jar hash"),
                "source_sha256": _sha(
                    metadata["java_verifier_source_sha256"], "verifier source hash"
                ),
                "build_input_sha256": _sha(
                    verifier_build.get("build_input_sha256"),
                    "verifier build-input hash",
                ),
                "portico_api_jar_sha256": _sha(
                    verifier_build.get("portico_api_jar_sha256"),
                    "verifier compile-time Portico API jar hash",
                ),
            },
        },
        "implementations": {
            "portico": {
                "version": str(metadata["portico_version"]),
                "jar_sha256": _sha(metadata["portico_jar_sha256"], "Portico jar hash"),
            },
            "gorti": {
                "commit": str(metadata["gorti_commit"]),
                "worktree_dirty": bool(runtime_evidence["worktree_dirty"]),
                "receive_order_transport": contract.gorti_transport,
                "local_lrc_queue_capacity": contract.local_lrc_queue_capacity,
                "local_lrc_ack_every": contract.local_lrc_ack_every,
                "local_lrc_batch_size": contract.local_lrc_batch_size,
                "callback_representation": contract.callback_representation,
                "outbox_event_capacity": contract.outbox_event_capacity,
                "outbox_batch_size": contract.outbox_batch_size,
                "outbox_flush_interval_ms": contract.outbox_flush_interval_ms,
                "source_tree_sha256": _sha(
                    runtime_evidence["source_tree_sha256"], "gorti source tree hash"
                ),
                "server_sha256": _sha(metadata["gorti_rtid_sha256"], "gorti server hash"),
                "federate_sha256": _sha(metadata["gorti_client_sha256"], "gorti federate hash"),
            },
        },
        "runtime": {
            **dict(runtime_evidence["versions"]),
            "java_verifier_build": dict(verifier_build),
        },
        "output_policy": {
            "structured_artifacts_only": True,
            "stdout_stderr_event_logs_retained": False,
            "allowed_artifacts": sorted(RESULT_NAMES),
            "transient_container_artifacts_deleted": bool(protocol["cleanup_verified_all_runs"]),
        },
        "artifacts": {
            "results.json": sha256_text(results_text),
            "analysis.json": sha256_text(analysis_text),
            "comparison.tex": sha256_text(latex),
        },
    }
    _reject_placeholders(manifest)
    manifest_text = json.dumps(manifest, ensure_ascii=False, allow_nan=False, indent=2) + "\n"
    return {
        "manifest.json": manifest_text,
        "results.json": results_text,
        "analysis.json": analysis_text,
        "comparison.tex": latex,
    }


def _reject_placeholders(value: Any, path: str = "$") -> None:
    if isinstance(value, str) and (PLACEHOLDER.fullmatch(value) or "__REQUIRED_" in value):
        raise BenchmarkError(f"unresolved placeholder in runtime manifest at {path}")
    if isinstance(value, dict):
        for key, child in value.items():
            _reject_placeholders(child, f"{path}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _reject_placeholders(child, f"{path}[{index}]")


def publish_artifacts(output: Path, payloads: Mapping[str, str], repo: Path) -> None:
    output = output.resolve()
    if _inside(output, repo):
        raise BenchmarkError("benchmark output must be outside the repository")
    if output.exists():
        raise BenchmarkError(f"benchmark output already exists: {output}")
    if set(payloads) != RESULT_NAMES:
        raise BenchmarkError(f"output artifact set must be exactly {sorted(RESULT_NAMES)}")
    output.parent.mkdir(parents=True, exist_ok=True)
    staging = output.parent / f".{output.name}.staging-{uuid.uuid4().hex}"
    staging.mkdir()
    try:
        for name in sorted(RESULT_NAMES):
            temporary = staging / f".{name}.{uuid.uuid4().hex}.tmp"
            temporary.write_text(payloads[name], encoding="utf-8", newline="\n")
            os.replace(temporary, staging / name)
        if {path.name for path in staging.iterdir()} != RESULT_NAMES:
            raise BenchmarkError("staged output contains an unexpected artifact")
        os.replace(staging, output)
    except BaseException:
        shutil.rmtree(staging, ignore_errors=True)
        raise


def _runtime_versions(comparison: Mapping[str, Any], probes: Mapping[str, str]) -> dict[str, Any]:
    metadata = comparison["metadata"]
    return {
        "host_os": str(metadata.get("host_os") or platform.platform()),
        "python": platform.python_version(),
        "go": probes["go"],
        "java_compiler": probes["java_compiler"],
        "jar_tool": probes["jar_tool"],
        "portico_bundled_java": str(metadata["portico_bundled_java_version"]),
        "docker_client": probes["docker_client"],
        "docker_server": probes["docker_server"],
        "container_image_reference": str(metadata["container_image_reference"]),
        "container_image_reference_digest": probes["container_image_reference_digest"],
        "container_platform": probes["container_platform"],
        "container_platform_image_id": probes["container_platform_image_id"],
    }


def probe_runtime_tools(contract: Contract, runtime: RuntimePaths) -> dict[str, str]:
    if runtime.docker is None or runtime.go is None:
        raise BenchmarkError("Docker and Go are required")
    if runtime.verifier_jar is not None:
        raise BenchmarkError(
            "prebuilt Java verifier overrides are prohibited; remove "
            "--verifier-jar and GORTI_VERIFIER_JAR"
        )
    if runtime.javac is None or runtime.jar is None:
        raise BenchmarkError("javac and jar are required to build the verifier")
    docker_client = _run_text(
        [runtime.docker, "version", "--format", "{{.Client.Version}}"],
        cwd=contract.repo,
    )
    docker_server = _run_text(
        [runtime.docker, "version", "--format", "{{.Server.Version}}"],
        cwd=contract.repo,
    )
    image_reference_digest = _run_text(
        [
            runtime.docker,
            "image",
            "inspect",
            "--format",
            "{{.Id}}",
            CONTAINER_IMAGE,
        ],
        cwd=contract.repo,
    )
    platform_image_id = _run_text(
        [
            runtime.docker,
            "image",
            "inspect",
            "--platform",
            CONTAINER_PLATFORM,
            "--format",
            "{{.Id}}",
            CONTAINER_IMAGE,
        ],
        cwd=contract.repo,
    )
    image_platform = _run_text(
        [
            runtime.docker,
            "image",
            "inspect",
            "--platform",
            CONTAINER_PLATFORM,
            "--format",
            "{{.Os}}/{{.Architecture}}",
            CONTAINER_IMAGE,
        ],
        cwd=contract.repo,
    )
    go_version = _run_text([runtime.go, "version"], cwd=contract.repo)
    java_compiler = _run_text([runtime.javac, "-version"], cwd=contract.repo)
    jar_tool = _run_text([runtime.jar, "--version"], cwd=contract.repo)
    runtime_pins = contract.prerequisites_document.get("runtime_pins", {})
    container_pin = contract.prerequisites_document.get("container_pin", {})
    expected_docker = runtime_pins.get("docker", {}).get("server_version")
    expected_docker_client = runtime_pins.get("docker", {}).get("client_version")
    expected_go = runtime_pins.get("go", {}).get("version")
    expected_python = runtime_pins.get("python", {}).get("version")
    expected_java = runtime_pins.get("java", {}).get("version", "").split("+", 1)[0]
    expected_image_reference_digest = str(container_pin.get("digest", ""))
    expected_platform = str(container_pin.get("platform", ""))
    if docker_server != expected_docker:
        raise BenchmarkError(
            f"Docker server {docker_server!r} differs from pin {expected_docker!r}"
        )
    if docker_client != expected_docker_client:
        raise BenchmarkError(
            f"Docker client {docker_client!r} differs from pin {expected_docker_client!r}"
        )
    if not go_version.startswith(f"go version go{expected_go} "):
        raise BenchmarkError(f"Go runtime differs from pin {expected_go!r}")
    if platform.python_version() != expected_python:
        raise BenchmarkError(f"Python runtime differs from pin {expected_python!r}")
    if expected_java not in java_compiler:
        raise BenchmarkError(f"Java compiler differs from pin {expected_java!r}")
    if image_reference_digest != expected_image_reference_digest:
        raise BenchmarkError(
            "container image reference digest "
            f"{image_reference_digest!r} differs from pin "
            f"{expected_image_reference_digest!r}"
        )
    for value, label in (
        (image_reference_digest, "container image reference digest"),
        (platform_image_id, "container platform image id"),
    ):
        if (
            not value.startswith("sha256:")
            or SHA256.fullmatch(value.removeprefix("sha256:")) is None
        ):
            raise BenchmarkError(f"{label} is not an immutable SHA-256 identity: {value!r}")
    if image_platform != expected_platform or image_platform != CONTAINER_PLATFORM:
        raise BenchmarkError(
            f"container image platform {image_platform!r} differs from pin {expected_platform!r}"
        )
    return {
        "docker_client": docker_client,
        "docker_server": docker_server,
        "container_image_reference_digest": image_reference_digest,
        "container_platform": image_platform,
        "container_platform_image_id": platform_image_id,
        "go": go_version,
        "java_compiler": java_compiler,
        "jar_tool": jar_tool,
    }


def require_configured_source_revision(contract: Contract, source_state: GitSourceState) -> None:
    gorti_config = next(
        item for item in contract.experiment["implementations"] if item["id"] == "gorti"
    )
    if source_state.commit != gorti_config.get("source_revision"):
        raise BenchmarkError("gorti HEAD differs from experiment source_revision")


def verify_runtime_evidence(
    contract: Contract,
    runtime: RuntimePaths,
    verifier: VerifierBuild,
    comparison: Mapping[str, Any],
    source_state: GitSourceState,
    probes: Mapping[str, str],
) -> dict[str, Any]:
    metadata = comparison.get("metadata")
    workload = comparison.get("workload")
    if not isinstance(metadata, dict) or not isinstance(workload, dict):
        raise BenchmarkError("comparison runtime evidence is missing")
    container_pin = contract.prerequisites_document.get("container_pin", {})
    expected_container = {
        "container_image_reference": str(container_pin.get("image", "")),
        "container_image_reference_digest": str(container_pin.get("digest", "")),
        "container_platform": str(container_pin.get("platform", "")),
    }
    for key, expected_value in expected_container.items():
        if metadata.get(key) != expected_value:
            raise BenchmarkError(f"comparison evidence mismatch: {key}")
    if (
        probes.get("container_image_reference_digest")
        != expected_container["container_image_reference_digest"]
    ):
        raise BenchmarkError("preflight container reference digest differs from prerequisite pin")
    if probes.get("container_platform") != expected_container["container_platform"]:
        raise BenchmarkError("preflight container platform differs from prerequisite pin")
    platform_image_id = metadata.get("container_platform_image_id")
    if platform_image_id != probes.get("container_platform_image_id"):
        raise BenchmarkError("comparison platform image ID differs from preflight")
    for value, label in (
        (
            metadata.get("container_image_reference_digest"),
            "comparison container image reference digest",
        ),
        (platform_image_id, "comparison container platform image ID"),
    ):
        if (
            not isinstance(value, str)
            or not value.startswith("sha256:")
            or SHA256.fullmatch(value.removeprefix("sha256:")) is None
        ):
            raise BenchmarkError(f"{label} is not an immutable SHA-256 identity")
    expected = {
        "portico_jar_sha256": sha256_file(runtime.portico_jar),
        "verifier_jar_sha256": verifier.jar_sha256,
        "comparison_harness_sha256": sha256_file(runtime.comparator),
    }
    for key, digest in expected.items():
        if metadata.get(key) != digest:
            raise BenchmarkError(f"comparison evidence mismatch: {key}")
    _sha(metadata.get("transport_override_sha256"), "Portico transport override hash")
    if workload.get("fom_sha256") != sha256_file(runtime.fom):
        raise BenchmarkError("comparison evidence mismatch: FOM hash")
    if sha256_file(verifier.jar) != verifier.jar_sha256:
        raise BenchmarkError("Java verifier jar changed after compilation")
    current_build_inputs = _verifier_build_inputs(runtime, verifier.inputs.compiler_version)
    if current_build_inputs != verifier.inputs:
        raise BenchmarkError("Java verifier build inputs changed after compilation")
    if metadata.get("java_verifier_source_sha256") != current_build_inputs.main_source_sha256:
        raise BenchmarkError("comparison evidence mismatch: Java verifier source hash")
    pinned_portico_java = str(
        contract.prerequisites_document["runtime_pins"]["portico_bundled_java"]["version"]
    )
    observed_portico_java = metadata.get("portico_bundled_java_version")
    if not isinstance(observed_portico_java, str) or not observed_portico_java.startswith(
        pinned_portico_java
    ):
        raise BenchmarkError("comparison evidence mismatch: Portico bundled Java")
    require_configured_source_revision(contract, source_state)
    if metadata.get("gorti_commit") != source_state.commit:
        raise BenchmarkError("comparison evidence mismatch: gorti commit")
    if metadata.get("gorti_worktree_dirty") is not source_state.dirty:
        raise BenchmarkError("comparison evidence mismatch: gorti worktree state")
    for key in (
        "gorti_rtid_sha256",
        "gorti_client_sha256",
    ):
        _sha(metadata.get(key), key)
    return transport_override_build_evidence(contract, runtime)


def execute(args: argparse.Namespace) -> int:
    repo = Path(__file__).resolve().parents[2]
    contract = load_contract(
        repo,
        experiment_path=Path(args.experiment).resolve() if args.experiment else None,
        prerequisites_path=(Path(args.prerequisites).resolve() if args.prerequisites else None),
        workload_path=Path(args.workload).resolve() if args.workload else None,
    )
    if args.operation_warmup != contract.operation_warmup:
        raise BenchmarkError(
            "operation warm-up override differs from the tracked measurement contract"
        )
    runtime = resolve_runtime_paths(args, contract)
    report = preflight_report(contract, runtime)
    if args.dry_run:
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return 0

    require_runtime_ready(report)
    if runtime.output is None:
        raise BenchmarkError("output path is required")
    probes = probe_runtime_tools(contract, runtime)
    source_state = capture_git_source_state(repo)
    require_configured_source_revision(contract, source_state)
    if source_state.dirty:
        print("WARNING: gorti worktree is dirty; this fact will be recorded.", file=sys.stderr)

    payloads: dict[str, str]
    with transient_workspace(repo) as transient:
        verifier = prepare_verifier(
            contract,
            runtime,
            transient,
            compiler_version=probes["java_compiler"],
        )
        comparison = invoke_comparator(args, contract, runtime, verifier.jar, transient)
        final_source_state = capture_git_source_state(repo)
        if final_source_state != source_state:
            raise BenchmarkError("Git-visible source state changed during the experiment")
        transport_override_build = verify_runtime_evidence(
            contract, runtime, verifier, comparison, source_state, probes
        )
        results = adapt_results(comparison, contract)
        if str(comparison["metadata"]["docker_server"]) != probes["docker_server"]:
            raise BenchmarkError("Docker server version changed during the experiment")
        if str(comparison["metadata"]["docker_client"]) != probes["docker_client"]:
            raise BenchmarkError("Docker client version changed during the experiment")
        if str(comparison["metadata"]["go_version"]) != probes["go"]:
            raise BenchmarkError("Go version changed during the experiment")
        versions = _runtime_versions(comparison, probes)
        payloads = build_artifact_payloads(
            results,
            contract,
            comparison,
            {
                "fom_path": str(runtime.fom.relative_to(repo)).replace("\\", "/"),
                "versions": versions,
                "worktree_dirty": source_state.dirty,
                "source_tree_sha256": source_state.source_tree_sha256,
                "java_verifier_build": verifier_build_runtime_evidence(verifier),
                "transport_override_build": transport_override_build,
            },
            bootstrap_resamples=args.bootstrap_resamples,
        )
    publish_artifacts(runtime.output, payloads, repo)
    print(f"PASS: structured benchmark artifacts written to {runtime.output}")
    return 0


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=("Run the 5-warm-up, 30+30 Portico/gorti DEVStone-HLA paired OM projection.")
    )
    parser.add_argument("--output", help="new output directory outside the repository")
    parser.add_argument("--portico-home", help="Portico 2.1.4 distribution inside the repo")
    parser.add_argument("--fom", help="FOM path; defaults to the tracked verifier FOM")
    parser.add_argument("--verifier-jar", help="prebuilt Java verifier jar inside the repo")
    parser.add_argument("--experiment", help="experiment contract JSON")
    parser.add_argument("--prerequisites", help="runtime prerequisite pins JSON")
    parser.add_argument("--workload", help="DEVStone-HLA workload JSON")
    parser.add_argument("--operation-warmup", type=int, default=DEFAULT_OPERATION_WARMUP)
    parser.add_argument("--timeout-ms", type=int, default=DEFAULT_TIMEOUT_MS)
    parser.add_argument("--bootstrap-resamples", type=int, default=10000)
    parser.add_argument("--docker", default="docker")
    parser.add_argument("--go", default="go")
    parser.add_argument("--javac", default="javac")
    parser.add_argument("--jar", default="jar")
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="validate structure and report missing runtime artifacts without failing",
    )
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = _parser()
    args = parser.parse_args(argv)
    if args.operation_warmup < 1:
        parser.error("--operation-warmup must be at least 1")
    if args.timeout_ms < 1000:
        parser.error("--timeout-ms must be at least 1000")
    if args.bootstrap_resamples < 100:
        parser.error("--bootstrap-resamples must be at least 100")
    try:
        return execute(args)
    except (BenchmarkError, OSError, ValueError) as exc:
        parser.error(str(exc))
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
