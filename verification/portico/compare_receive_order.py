"""Run a paired DEVStone-HLA receive-order comparison for Portico and gorti."""

# Subprocess argument vectors are assembled without a shell from validated paths.
# ruff: noqa: S603

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import math
import os
import platform
import re
import shutil
import socket
import statistics
import subprocess
import sys
import tarfile
import tempfile
import time
import types
import uuid
from collections import defaultdict
from collections.abc import Callable, Iterable, Mapping, Sequence
from dataclasses import dataclass, field
from pathlib import Path, PurePosixPath
from types import ModuleType
from typing import Any

from build_transport_override import build_override

IMAGE = "ubuntu:24.04"
CONTAINER_PLATFORM = "linux/amd64"
PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS = 5000
PORTICO_PUBLISHER_READY_FILENAME = ".portico-publisher-startup.ready"
PORTICO_TEARDOWN_READY_FILENAME = ".portico-teardown.ready"
PORTICO_PUBLISHER_RESIGNED_READY_FILENAME = ".portico-publisher-resigned.ready"
PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME = ".portico-subscriber-disconnected.ready"
VERIFIER_CLASS = "gorti.verification.commercialrti.CommercialRtiVerifier"
PARTICIPANT_SUMMARY_SCHEMA = "gorti.devstone.participant-summary/v1"
RUNTIME_PROFILE_SCHEMA = "gorti.benchmark-runtime-profile/v1"
GORTI_TRANSPORT_MODES = ("local-lrc", "confirmed")
TRANSPORT_PROFILE_SCHEMA = "gorti.receive-order-transport/v1"
TRANSPORT_PROFILE_KEYS = frozenset(
    {
        "schema_version",
        "receive_order_transport",
        "local_lrc_queue_capacity",
        "local_lrc_ack_every",
        "local_lrc_batch_size",
        "callback_representation",
    }
)
PLAN_FORMAT = "DVSHLA1"
OPERATIONS = ("updateAttributeValues", "sendInteraction", "completedReceiveOrderBatch")
PROCESS_OUTPUT_TAIL_CHARS = 1000
DOCKER_PING_RETRY_ATTEMPTS = 4
BENCH_ROOT = PurePosixPath("/bench")
BENCH_RUNS_ROOT = BENCH_ROOT / "runs"
BENCH_SERVER_ROOT = BENCH_ROOT / "server-state"
SUMMARY_FILES = ("publisher-summary.json", "subscriber-summary.json")
EVIDENCE_KEYS = (
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
SHA256_LENGTH = 64
_PLAN_MODULES: dict[Path, ModuleType] = {}


class RetryableParticipantError(RuntimeError):
    """A participant exited or timed out after its containers were cleaned up."""


@dataclass
class ProcessCapture:
    returncode: int
    stdout: str
    stderr: str


@dataclass
class ContainerProcess:
    name: str
    role: str
    process: subprocess.Popen[str]
    capture: ProcessCapture | None = field(default=None)


@dataclass(frozen=True)
class PairPlan:
    path: Path
    seed: int
    count: int
    sha256: str
    topology_sha256: str


def nonnegative_int(value: str) -> int:
    try:
        parsed = int(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError("must be an integer") from error
    if parsed < 0:
        raise argparse.ArgumentTypeError("must be nonnegative")
    return parsed


def positive_int(value: str) -> int:
    parsed = nonnegative_int(value)
    if parsed < 1:
        raise argparse.ArgumentTypeError("must be positive")
    return parsed


def build_pair_schedule(
    base_seed: int, warmup_pairs: int, measured_pairs: int
) -> list[dict[str, Any]]:
    """Return phase-local AB/BA orders with disjoint deterministic seeds."""
    if base_seed < 0 or warmup_pairs < 0 or measured_pairs < 0:
        raise ValueError("seed and repetition counts must be nonnegative")

    schedule: list[dict[str, Any]] = []
    for phase, count, seed_offset in (
        ("warmup", warmup_pairs, measured_pairs),
        ("measured", measured_pairs, 0),
    ):
        for index in range(count):
            schedule.append(
                {
                    "phase": phase,
                    "pair": index + 1,
                    "seed": base_seed + seed_offset + index,
                    "order": ("portico", "gorti") if index % 2 == 0 else ("gorti", "portico"),
                }
            )
    return schedule


def process_output_tail(capture: ProcessCapture) -> str:
    transcript = capture.stderr.strip() or capture.stdout.strip()
    return transcript[-PROCESS_OUTPUT_TAIL_CHARS:]


def run_checked(command: list[str], *, cwd: Path, env: dict[str, str] | None = None) -> str:
    return run_captured_checked(command, cwd=cwd, env=env).stdout.strip()


def run_captured_checked(
    command: list[str], *, cwd: Path, env: dict[str, str] | None = None
) -> ProcessCapture:
    for attempt in range(1, DOCKER_PING_RETRY_ATTEMPTS + 1):
        completed = subprocess.run(
            command,
            cwd=cwd,
            env=env,
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            check=False,
        )
        capture = ProcessCapture(completed.returncode, completed.stdout, completed.stderr)
        if completed.returncode == 0:
            return capture
        detail = process_output_tail(capture)
        if not _is_retryable_docker_ping_failure(command, detail) or (
            attempt == DOCKER_PING_RETRY_ATTEMPTS
        ):
            raise RuntimeError(
                f"command failed ({completed.returncode}): {' '.join(command)}\n{detail}"
            )
        time.sleep(0.25 * attempt)
    raise AssertionError("Docker ping retry loop completed without a result")


def _is_retryable_docker_ping_failure(command: Sequence[str], detail: str) -> bool:
    if not command:
        return False
    executable = Path(command[0]).name.lower()
    return (
        executable in {"docker", "docker.exe"}
        and "500 internal server error" in detail.lower()
        and "_ping" in detail.lower()
    )


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def sha256_json(value: object) -> str:
    payload = json.dumps(
        value,
        allow_nan=False,
        ensure_ascii=True,
        sort_keys=True,
        separators=(",", ":"),
    )
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def read_ndjson(path: Path) -> list[dict[str, Any]]:
    """Compatibility helper for the separate legacy three-way harness."""
    return [
        json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()
    ]


def write_json(path: Path, value: object) -> None:
    path.write_text(
        json.dumps(
            value,
            allow_nan=False,
            ensure_ascii=True,
            indent=2,
            sort_keys=True,
        )
        + "\n",
        encoding="utf-8",
    )


def _load_module(name: str, path: Path) -> ModuleType:
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load module from {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def load_devstone_plan_module(repo: Path) -> ModuleType:
    """Load the repository plan module without relying on the current directory."""
    repo = repo.resolve()
    cached = _PLAN_MODULES.get(repo)
    if cached is not None:
        return cached

    workload_directory = repo / "benchmark" / "devstone" / "workload"
    plan_path = workload_directory / "plan.py"
    model_path = workload_directory / "model.py"
    if not plan_path.is_file() or not model_path.is_file():
        raise FileNotFoundError("DEVStone workload plan.py and model.py are required")

    suffix = hashlib.sha256(str(repo).encode("utf-8")).hexdigest()[:12]
    package_name = f"_gorti_devstone_workload_{suffix}"
    package = types.ModuleType(package_name)
    package.__path__ = [str(workload_directory)]  # type: ignore[attr-defined]
    package.__package__ = package_name
    sys.modules[package_name] = package
    _load_module(f"{package_name}.model", model_path)
    module = _load_module(f"{package_name}.plan", plan_path)
    for required in (
        "load_workload",
        "materialize_plan",
        "validate_plan",
        "file_sha256",
        "write_plan",
    ):
        if not callable(getattr(module, required, None)):
            raise ImportError(f"DEVStone plan module does not provide {required}()")
    _PLAN_MODULES[repo] = module
    return module


def materialize_pair_plan(
    plan_module: ModuleType,
    workload: Mapping[str, Any],
    seed: int,
    destination: Path,
) -> PairPlan:
    if destination.suffix != ".dvshla":
        raise ValueError("DEVStone-HLA plan files must use the .dvshla suffix")
    plan = plan_module.materialize_plan(workload, seed)
    plan_module.validate_plan(plan, workload, expected_seed=seed)
    plan_module.write_plan(destination, plan)
    validated = plan_module.validate_plan(destination, workload, expected_seed=seed)
    return PairPlan(
        path=destination.resolve(),
        seed=int(validated.seed),
        count=int(validated.record_count),
        sha256=str(plan_module.file_sha256(destination)),
        topology_sha256=bytes(validated.topology_identity).hex(),
    )


def container_path(path: Path, repo: Path) -> str:
    relative = path.resolve().relative_to(repo.resolve())
    return str(PurePosixPath("/work", *relative.parts))


def staged_path(path: Path, repo: Path) -> str:
    relative = path.resolve().relative_to(repo.resolve())
    return str(BENCH_ROOT.joinpath(*relative.parts))


def volume_run_path(run_id: str) -> str:
    if not run_id or PurePosixPath(run_id).name != run_id:
        raise ValueError(f"invalid run id: {run_id!r}")
    return str(BENCH_RUNS_ROOT / run_id)


def volume_server_path(run_id: str) -> str:
    if not run_id or PurePosixPath(run_id).name != run_id:
        raise ValueError(f"invalid run id: {run_id!r}")
    return str(BENCH_SERVER_ROOT / run_id)


def portico_publisher_ready_path(run_output: str) -> str:
    run_root = _validate_volume_child(run_output)
    if run_root.parent != BENCH_RUNS_ROOT:
        raise ValueError(f"Portico run output is outside the run root: {run_output}")
    return str(run_root / PORTICO_PUBLISHER_READY_FILENAME)


def portico_teardown_ready_path(run_output: str) -> str:
    run_root = _validate_volume_child(run_output)
    if run_root.parent != BENCH_RUNS_ROOT:
        raise ValueError(f"Portico run output is outside the run root: {run_output}")
    return str(run_root / PORTICO_TEARDOWN_READY_FILENAME)


def portico_teardown_ready_paths(run_output: str) -> tuple[str, str, str]:
    base = PurePosixPath(portico_teardown_ready_path(run_output))
    return (
        str(base),
        str(base.with_name(PORTICO_PUBLISHER_RESIGNED_READY_FILENAME)),
        str(base.with_name(PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME)),
    )


def _validate_volume_child(path: str) -> PurePosixPath:
    candidate = PurePosixPath(path)
    if not candidate.is_absolute() or candidate == BENCH_ROOT:
        raise ValueError(f"refusing unsafe benchmark volume path: {path}")
    try:
        candidate.relative_to(BENCH_ROOT)
    except ValueError as error:
        raise ValueError(f"path is outside the benchmark volume: {path}") from error
    if ".." in candidate.parts:
        raise ValueError(f"path traversal is not allowed: {path}")
    return candidate


def docker_run_prefix(docker: str) -> list[str]:
    return [docker, "run", "--platform", CONTAINER_PLATFORM]


def image_platform_command(docker: str) -> list[str]:
    return [
        docker,
        "image",
        "inspect",
        "--platform",
        CONTAINER_PLATFORM,
        "--format",
        "{{.Os}}/{{.Architecture}}",
        IMAGE,
    ]


def image_reference_digest_command(docker: str) -> list[str]:
    return [
        docker,
        "image",
        "inspect",
        "--format",
        "{{.Id}}",
        IMAGE,
    ]


def platform_image_id_command(docker: str) -> list[str]:
    return [
        docker,
        "image",
        "inspect",
        "--platform",
        CONTAINER_PLATFORM,
        "--format",
        "{{.Id}}",
        IMAGE,
    ]


def portico_java_version_command(docker: str, volume: str, staged_java: str) -> list[str]:
    _validate_volume_child(staged_java)
    return [
        *docker_run_prefix(docker),
        "--rm",
        "--volume",
        f"{volume}:{BENCH_ROOT}:ro",
        "--workdir",
        str(BENCH_ROOT),
        IMAGE,
        staged_java,
        "-version",
    ]


def capture_portico_java_version(docker: str, repo: Path, volume: str, staged_java: str) -> str:
    capture = run_captured_checked(
        portico_java_version_command(docker, volume, staged_java), cwd=repo
    )
    transcript = "\n".join(
        part.strip() for part in (capture.stdout, capture.stderr) if part.strip()
    )
    match = re.search(r"\bbuild\s+([0-9][0-9A-Za-z._+-]*)", transcript)
    if match is None:
        raise ValueError(f"cannot normalize Portico bundled Java version: {transcript}")
    return match.group(1)


def volume_create_command(docker: str, volume: str) -> list[str]:
    return [docker, "volume", "create", volume]


def volume_remove_command(docker: str, volume: str) -> list[str]:
    return [docker, "volume", "rm", volume]


def volume_path_cleanup_command(docker: str, volume: str, path: str) -> list[str]:
    candidate = _validate_volume_child(path)
    return [
        *docker_run_prefix(docker),
        "--rm",
        "--volume",
        f"{volume}:{BENCH_ROOT}",
        IMAGE,
        "rm",
        "--recursive",
        "--force",
        str(candidate),
    ]


def volume_path_exists_command(docker: str, volume: str, path: str) -> list[str]:
    candidate = _validate_volume_child(path)
    return [
        *docker_run_prefix(docker),
        "--rm",
        "--volume",
        f"{volume}:{BENCH_ROOT}:ro",
        IMAGE,
        "test",
        "-e",
        str(candidate),
    ]


def stage_artifacts_command(
    docker: str,
    repo: Path,
    volume: str,
    artifacts: Iterable[Path],
) -> list[str]:
    relative_paths = [
        str(PurePosixPath(*path.resolve().relative_to(repo.resolve()).parts)) for path in artifacts
    ]
    if not relative_paths:
        raise ValueError("at least one artifact is required for staging")
    return [
        *docker_run_prefix(docker),
        "--rm",
        "--volume",
        f"{repo}:/work:ro",
        "--volume",
        f"{volume}:{BENCH_ROOT}",
        "--workdir",
        "/work",
        IMAGE,
        "cp",
        "--archive",
        "--parents",
        *relative_paths,
        f"{BENCH_ROOT}/",
    ]


def create_staging_archive(repo: Path, artifacts: Iterable[Path], destination: Path) -> Path:
    """Pack static inputs once so Docker Desktop performs one sequential read."""
    repo = repo.resolve()
    destination = destination.resolve()
    destination.relative_to(repo)
    if destination.name.endswith(".tar.gz") is False:
        raise ValueError("staging archive must use the .tar.gz suffix")
    sources = tuple(path.resolve() for path in artifacts)
    if not sources:
        raise ValueError("at least one static artifact is required")
    for source in sources:
        source.relative_to(repo)
        if not source.exists():
            raise FileNotFoundError(source)
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.exists():
        raise FileExistsError(destination)

    executable_files = {
        source.relative_to(repo).as_posix()
        for source in sources
        if source.is_file() and source.name in {"rtid-linux-amd64", "gorti-go-fair-linux-amd64"}
    }

    def normalize_mode(info: tarfile.TarInfo) -> tarfile.TarInfo:
        if info.isfile() and ("/jre/bin/" in f"/{info.name}" or info.name in executable_files):
            info.mode = 0o755
        return info

    with tarfile.open(
        destination,
        mode="w:gz",
        format=tarfile.PAX_FORMAT,
        compresslevel=1,
    ) as archive:
        archive.dereference = True
        for source in sources:
            relative = source.relative_to(repo)
            archive.add(
                source,
                arcname=PurePosixPath(*relative.parts).as_posix(),
                recursive=True,
                filter=normalize_mode,
            )
    return destination


def stage_archive_command(docker: str, repo: Path, volume: str, archive: Path) -> list[str]:
    archive_in_worktree = container_path(archive, repo)
    return [
        *docker_run_prefix(docker),
        "--rm",
        "--volume",
        f"{repo}:/work:ro",
        "--volume",
        f"{volume}:{BENCH_ROOT}",
        IMAGE,
        "tar",
        "--extract",
        "--gzip",
        "--file",
        archive_in_worktree,
        "--directory",
        str(BENCH_ROOT),
    ]


def volume_files_command(docker: str, volume: str, path: str) -> list[str]:
    candidate = _validate_volume_child(path)
    return [
        *docker_run_prefix(docker),
        "--rm",
        "--volume",
        f"{volume}:{BENCH_ROOT}:ro",
        IMAGE,
        "find",
        str(candidate),
        "-type",
        "f",
        "-printf",
        "%P\n",
    ]


def copy_summary_file_command(
    docker: str,
    volume: str,
    source: str,
    host_output: Path,
    target_name: str,
) -> list[str]:
    _validate_volume_child(source)
    if target_name not in SUMMARY_FILES:
        raise ValueError(f"invalid summary target: {target_name}")
    return [
        *docker_run_prefix(docker),
        "--rm",
        "--volume",
        f"{volume}:{BENCH_ROOT}:ro",
        "--volume",
        f"{host_output}:/host-output",
        IMAGE,
        "cp",
        "--archive",
        source,
        f"/host-output/{target_name}",
    ]


def _resource_names(docker: str, repo: Path, kind: str, name: str) -> list[str]:
    if kind == "container":
        command = [
            docker,
            "container",
            "ls",
            "--all",
            "--filter",
            f"name={name}",
            "--format",
            "{{.Names}}",
        ]
    elif kind == "network":
        command = [
            docker,
            "network",
            "ls",
            "--filter",
            f"name={name}",
            "--format",
            "{{.Name}}",
        ]
    elif kind == "volume":
        command = [
            docker,
            "volume",
            "ls",
            "--filter",
            f"name={name}",
            "--format",
            "{{.Name}}",
        ]
    else:
        raise ValueError(f"unsupported Docker resource kind: {kind}")
    return [line for line in run_checked(command, cwd=repo).splitlines() if line]


def _resource_exists(docker: str, repo: Path, kind: str, name: str) -> bool:
    return name in _resource_names(docker, repo, kind, name)


def _wait_resource_absent(
    docker: str, repo: Path, kind: str, name: str, timeout_seconds: float = 5.0
) -> None:
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if not _resource_exists(docker, repo, kind, name):
            return
        time.sleep(0.05)
    raise RuntimeError(f"Docker {kind} was not removed: {name}")


def create_benchmark_volume(docker: str, repo: Path) -> str:
    volume = f"gorti-portico-bench-{uuid.uuid4().hex[:12]}"
    try:
        created = run_checked(volume_create_command(docker, volume), cwd=repo)
        if created.splitlines()[-1:] != [volume] or not _resource_exists(
            docker, repo, "volume", volume
        ):
            raise RuntimeError(f"Docker did not create benchmark volume {volume}")
        return volume
    except Exception:
        if _resource_exists(docker, repo, "volume", volume):
            remove_benchmark_volume(docker, repo, volume)
        raise


def remove_benchmark_volume(docker: str, repo: Path, volume: str) -> None:
    if _resource_exists(docker, repo, "volume", volume):
        run_checked(volume_remove_command(docker, volume), cwd=repo)
    _wait_resource_absent(docker, repo, "volume", volume)


def create_pair_network(docker: str, repo: Path, pair_identity: str) -> str:
    network = f"gorti-devstone-{pair_identity}-{uuid.uuid4().hex[:8]}"
    try:
        run_checked([docker, "network", "create", "--driver", "bridge", network], cwd=repo)
        if not _resource_exists(docker, repo, "network", network):
            raise RuntimeError(f"Docker did not create pair network {network}")
        return network
    except Exception:
        if _resource_exists(docker, repo, "network", network):
            remove_pair_network(docker, repo, network)
        raise


def assert_network_empty(docker: str, repo: Path, network: str) -> None:
    attached = run_checked(
        [
            docker,
            "network",
            "inspect",
            "--format",
            "{{len .Containers}}",
            network,
        ],
        cwd=repo,
    )
    if attached != "0":
        raise RuntimeError(f"pair network {network} still has {attached} containers")


def remove_pair_network(docker: str, repo: Path, network: str) -> None:
    if _resource_exists(docker, repo, "network", network):
        assert_network_empty(docker, repo, network)
        run_checked([docker, "network", "rm", network], cwd=repo)
    _wait_resource_absent(docker, repo, "network", network)


def stage_artifacts(
    docker: str,
    repo: Path,
    volume: str,
    artifacts: Iterable[Path],
) -> None:
    run_checked(stage_artifacts_command(docker, repo, volume, artifacts), cwd=repo)


def stage_archive(docker: str, repo: Path, volume: str, archive: Path) -> None:
    run_checked(stage_archive_command(docker, repo, volume, archive), cwd=repo)


def volume_path_exists(docker: str, repo: Path, volume: str, path: str) -> bool:
    command = volume_path_exists_command(docker, volume, path)
    completed = subprocess.run(
        command,
        cwd=repo,
        text=True,
        encoding="utf-8",
        errors="replace",
        capture_output=True,
        check=False,
    )
    if completed.returncode == 0:
        return True
    if completed.returncode == 1:
        return False
    detail = process_output_tail(
        ProcessCapture(completed.returncode, completed.stdout, completed.stderr)
    )
    raise RuntimeError(f"could not inspect benchmark volume path {path}: {detail}")


def remove_volume_path(docker: str, repo: Path, volume: str, path: str) -> None:
    run_checked(volume_path_cleanup_command(docker, volume, path), cwd=repo)
    if volume_path_exists(docker, repo, volume, path):
        raise RuntimeError(f"benchmark volume path was not removed: {path}")


def remove_volume_paths(docker: str, repo: Path, volume: str, paths: Iterable[str]) -> None:
    failures: list[str] = []
    for path in paths:
        try:
            remove_volume_path(docker, repo, volume, path)
        except Exception as error:
            failures.append(f"{path}: {error}")
    if failures:
        raise RuntimeError("benchmark volume path cleanup failed: " + " | ".join(failures))


def require_volume_paths_absent(docker: str, repo: Path, volume: str, paths: Iterable[str]) -> None:
    retained = [path for path in paths if volume_path_exists(docker, repo, volume, path)]
    if retained:
        names = ", ".join(PurePosixPath(path).name for path in retained)
        raise RuntimeError(
            "Portico ordered teardown markers were not consumed after successful "
            f"participant exits: {names}"
        )


def _volume_files(docker: str, repo: Path, volume: str, path: str) -> set[str]:
    output = run_checked(volume_files_command(docker, volume, path), cwd=repo)
    return {line.strip() for line in output.splitlines() if line.strip()}


def copy_participant_summaries(
    docker: str,
    repo: Path,
    volume: str,
    run_output: str,
    host_output: Path,
) -> None:
    files = _volume_files(docker, repo, volume, run_output)
    sources: dict[str, str] = {}
    expected_sources: set[str] = set()
    for role in ("publisher", "subscriber"):
        candidates = (
            f"{role}/{role}-summary.json",
            f"{role}/role-summary.json",
        )
        matches = [candidate for candidate in candidates if candidate in files]
        if len(matches) != 1:
            raise ValueError(
                f"{role}: expected exactly one compact participant summary, found {matches}"
            )
        sources[role] = str(PurePosixPath(run_output) / matches[0])
        expected_sources.add(matches[0])
    if files != expected_sources:
        raise ValueError(
            "compact run output contains files other than the two participant summaries: "
            f"{sorted(files - expected_sources)}"
        )

    host_output.mkdir(parents=True, exist_ok=False)
    try:
        for role in ("publisher", "subscriber"):
            target = f"{role}-summary.json"
            run_checked(
                copy_summary_file_command(docker, volume, sources[role], host_output, target),
                cwd=repo,
            )
        if {path.name for path in host_output.iterdir()} != set(SUMMARY_FILES):
            raise RuntimeError("host compact output is not exactly two summary files")
    except Exception as error:
        if host_output.exists():
            shutil.rmtree(host_output)
        if host_output.exists():
            raise RuntimeError(f"failed to remove partial compact output: {host_output}") from error
        raise


def start_container(
    docker: str,
    repo: Path,
    network: str,
    volume: str,
    name: str,
    role: str,
    command: list[str],
    *,
    publish_ports: Iterable[int] = (),
) -> ContainerProcess:
    argv = [
        *docker_run_prefix(docker),
        "--rm",
        "--name",
        name,
        "--network",
        network,
    ]
    for port in publish_ports:
        if port < 1 or port > 65535:
            raise ValueError(f"invalid container port: {port}")
        argv.extend(["--publish", f"127.0.0.1::{port}"])
    argv.extend(
        [
            "--volume",
            f"{volume}:{BENCH_ROOT}",
            "--workdir",
            str(BENCH_ROOT),
            IMAGE,
            *command,
        ]
    )
    process = subprocess.Popen(
        argv,
        cwd=repo,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return ContainerProcess(name=name, role=role, process=process)


def remove_container(docker: str, repo: Path, name: str) -> None:
    if _resource_exists(docker, repo, "container", name):
        completed = subprocess.run(
            [docker, "container", "rm", "--force", name],
            cwd=repo,
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            check=False,
        )
        if completed.returncode and _resource_exists(docker, repo, "container", name):
            detail = process_output_tail(
                ProcessCapture(completed.returncode, completed.stdout, completed.stderr)
            )
            try:
                _wait_resource_absent(docker, repo, "container", name, timeout_seconds=5.0)
            except RuntimeError as error:
                raise RuntimeError(f"could not remove Docker container {name}: {detail}") from error
    _wait_resource_absent(docker, repo, "container", name)


def capture_process(container: ContainerProcess) -> ProcessCapture:
    if container.capture is None:
        stdout, stderr = container.process.communicate(timeout=10)
        container.capture = ProcessCapture(
            returncode=int(container.process.returncode or 0),
            stdout=stdout,
            stderr=stderr,
        )
    return container.capture


def cleanup_container_processes(
    docker: str, repo: Path, containers: Iterable[ContainerProcess]
) -> None:
    failures: list[str] = []
    for item in containers:
        try:
            if item.process.poll() is None or _resource_exists(
                docker, repo, "container", item.name
            ):
                remove_container(docker, repo, item.name)
            capture_process(item)
            _wait_resource_absent(docker, repo, "container", item.name)
        except Exception as error:  # Continue so one failure cannot strand its peer.
            failures.append(f"{item.name}: {error}")
    if failures:
        raise RuntimeError("container cleanup failed: " + " | ".join(failures))


def wait_federates(
    docker: str,
    repo: Path,
    containers: list[ContainerProcess],
    timeout_seconds: float,
) -> dict[str, ProcessCapture]:
    deadline = time.monotonic() + timeout_seconds
    failure: str | None = None
    while any(item.process.poll() is None for item in containers):
        failed = next(
            (item for item in containers if item.process.poll() not in (None, 0)),
            None,
        )
        if failed is not None:
            failure = f"{failed.role} exited with code {failed.process.returncode}"
            break
        if time.monotonic() >= deadline:
            failure = f"federates exceeded {timeout_seconds:.1f} seconds"
            break
        time.sleep(0.05)
    cleanup_failures: list[str] = []
    if failure:
        for item in containers:
            if item.process.poll() is None:
                try:
                    remove_container(docker, repo, item.name)
                except Exception as error:
                    cleanup_failures.append(f"{item.name}: {error}")
    captures = {item.role: capture_process(item) for item in containers}
    for item in containers:
        try:
            _wait_resource_absent(docker, repo, "container", item.name)
        except Exception as error:
            cleanup_failures.append(f"{item.name}: {error}")
    participant_failed = failure is not None or any(
        item.returncode != 0 for item in captures.values()
    )
    if participant_failed or cleanup_failures:
        details = " | ".join(
            f"{role}={capture.returncode}: {process_output_tail(capture)}"
            for role, capture in captures.items()
        )
        cleanup_detail = " | cleanup=" + ", ".join(cleanup_failures) if cleanup_failures else ""
        message = f"{failure or 'federate failure'}; {details}{cleanup_detail}"
        if cleanup_failures:
            raise RuntimeError(message)
        raise RetryableParticipantError(message)
    return captures


def wait_for_portico_publisher_ready(
    docker: str,
    repo: Path,
    volume: str,
    publisher: ContainerProcess,
    ready_file: str,
    timeout_seconds: float,
) -> None:
    """Wait for publisher join/registration evidence without hiding participant exits."""
    _validate_volume_child(ready_file)
    deadline = time.monotonic() + timeout_seconds

    def reject_exited_publisher() -> None:
        if publisher.process.poll() is None:
            return
        capture = capture_process(publisher)
        raise RetryableParticipantError(
            f"{publisher.role} exited with code {capture.returncode} before startup readiness: "
            f"{process_output_tail(capture)}"
        )

    while True:
        reject_exited_publisher()
        if volume_path_exists(docker, repo, volume, ready_file):
            reject_exited_publisher()
            return
        if time.monotonic() >= deadline:
            raise RetryableParticipantError(
                f"{publisher.role} exceeded {timeout_seconds:.1f} seconds before startup readiness"
            )
        time.sleep(0.05)


def wait_for_container_port(
    docker: str,
    repo: Path,
    container: ContainerProcess,
    container_port: int,
    timeout_seconds: float,
) -> None:
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if container.process.poll() is not None:
            capture = capture_process(container)
            detail = process_output_tail(capture)
            if (
                _resource_exists(docker, repo, "container", container.name)
                and "unable to upgrade to tcp" in detail.lower()
            ):
                raise RetryableParticipantError(
                    f"{container.role} Docker attach failed before port "
                    f"{container_port} became ready: {detail}"
                )
            raise RuntimeError(
                f"{container.role} exited before opening port {container_port}: "
                f"{detail}"
            )
        completed = subprocess.run(
            [docker, "port", container.name, f"{container_port}/tcp"],
            cwd=repo,
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            check=False,
        )
        if completed.returncode == 0 and completed.stdout.strip():
            host_port = completed.stdout.strip().rsplit(":", 1)[-1]
            try:
                with socket.create_connection(("127.0.0.1", int(host_port)), timeout=0.2):
                    return
            except (OSError, ValueError):
                pass
        time.sleep(0.05)
    raise TimeoutError(f"timed out waiting for {container.role} port {container_port}")


def wait_for_rtid_port(
    docker: str, repo: Path, server: ContainerProcess, timeout_seconds: float
) -> None:
    wait_for_container_port(docker, repo, server, 8442, timeout_seconds)


def stop_server(docker: str, repo: Path, server: ContainerProcess) -> dict[str, Any]:
    process_alive = server.process.poll() is None
    container_alive = _resource_exists(docker, repo, "container", server.name)
    if not process_alive or not container_alive:
        if container_alive:
            remove_container(docker, repo, server.name)
        capture = capture_process(server)
        _wait_resource_absent(docker, repo, "container", server.name)
        raise RuntimeError(
            "RTI server was not alive before intentional shutdown "
            f"(process_alive={process_alive}, container_alive={container_alive}, "
            f"exit_code={capture.returncode})"
        )

    shutdown_error: Exception | None = None
    try:
        run_checked([docker, "container", "stop", "--time", "2", server.name], cwd=repo)
    except Exception as error:
        shutdown_error = error
    if server.process.poll() is None:
        remove_container(docker, repo, server.name)
    capture = capture_process(server)
    _wait_resource_absent(docker, repo, "container", server.name)
    if shutdown_error is not None:
        raise RuntimeError("intentional RTI server shutdown failed") from shutdown_error
    return {
        "alive_before_shutdown": True,
        "shutdown_requested": True,
        "exit_code_after_shutdown": capture.returncode,
        "cleanup_verified": True,
    }


def _require_mapping(value: object, label: str) -> Mapping[str, Any]:
    if not isinstance(value, Mapping):
        raise ValueError(f"{label} must be an object")
    return value


def _require_bool(value: object, label: str) -> bool:
    if not isinstance(value, bool):
        raise ValueError(f"{label} must be boolean")
    return value


def _require_int(value: object, label: str, *, minimum: int = 0) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < minimum:
        raise ValueError(f"{label} must be an integer >= {minimum}")
    return value


def _require_sha256(value: object, label: str) -> str:
    if (
        not isinstance(value, str)
        or len(value) != SHA256_LENGTH
        or any(character not in "0123456789abcdef" for character in value)
    ):
        raise ValueError(f"{label} must be a lowercase SHA-256")
    return value


def inspect_container_image_identity(docker: str, repo: Path) -> dict[str, str]:
    reference_digest = run_checked(image_reference_digest_command(docker), cwd=repo)
    image_platform = run_checked(image_platform_command(docker), cwd=repo)
    platform_image_id = run_checked(platform_image_id_command(docker), cwd=repo)
    if image_platform != CONTAINER_PLATFORM:
        raise RuntimeError(
            f"container image platform is {image_platform}, expected {CONTAINER_PLATFORM}"
        )
    for value, label in (
        (reference_digest, "container image reference digest"),
        (platform_image_id, "container platform image id"),
    ):
        if not value.startswith("sha256:"):
            raise RuntimeError(f"{label} is unresolved: {value}")
        _require_sha256(value.removeprefix("sha256:"), label)
    return {
        "container_image_reference": IMAGE,
        "container_image_reference_digest": reference_digest,
        "container_platform": image_platform,
        "container_platform_image_id": platform_image_id,
    }


def _optional_metric(container: Mapping[str, Any], name: str, label: str) -> int | None:
    value = container.get(name)
    if value is None:
        return None
    return _require_int(value, label, minimum=1)


def _normalize_accounting(raw: Mapping[str, Any], label: str) -> dict[str, int]:
    if "accepted_total" in raw:
        expected = _require_int(raw.get("expected_total"), f"{label}.expected_total")
        delivered = _require_int(raw.get("accepted_total"), f"{label}.accepted_total")
        attributes = _require_int(raw.get("accepted_attributes"), f"{label}.accepted_attributes")
        interactions = _require_int(
            raw.get("accepted_interactions"), f"{label}.accepted_interactions"
        )
        dropped = _require_int(raw.get("dropped_total"), f"{label}.dropped_total")
        duplicates = _require_int(raw.get("duplicate_callbacks"), f"{label}.duplicate_callbacks")
        invalid = _require_int(raw.get("invalid_callbacks"), f"{label}.invalid_callbacks")
        # The Java verifier rejects duplicate/invalid callbacks before inserting them.
        rejected = _require_int(
            raw.get("rejected_callbacks", duplicates + invalid),
            f"{label}.rejected_callbacks",
        )
        unexpected = _require_int(
            raw.get("unexpected_callbacks", invalid),
            f"{label}.unexpected_callbacks",
        )
    elif "expected" in raw:
        expected = _require_int(raw.get("expected"), f"{label}.expected")
        delivered = _require_int(raw.get("delivered"), f"{label}.delivered")
        attributes = _require_int(raw.get("attribute_delivered"), f"{label}.attribute_delivered")
        interactions = _require_int(
            raw.get("interaction_delivered"), f"{label}.interaction_delivered"
        )
        rejected = _require_int(raw.get("rejected"), f"{label}.rejected")
        dropped = _require_int(raw.get("dropped"), f"{label}.dropped")
        unexpected = _require_int(raw.get("unexpected"), f"{label}.unexpected")
        duplicates = _require_int(raw.get("duplicates"), f"{label}.duplicates")
        invalid = _require_int(raw.get("invalid"), f"{label}.invalid")
    else:
        expected = _require_int(raw.get("expected_callbacks"), f"{label}.expected_callbacks")
        delivered = _require_int(raw.get("delivered_callbacks"), f"{label}.delivered_callbacks")
        attributes = _require_int(raw.get("attribute_callbacks"), f"{label}.attribute_callbacks")
        interactions = _require_int(
            raw.get("interaction_callbacks"), f"{label}.interaction_callbacks"
        )
        rejected = _require_int(raw.get("rejected_callbacks"), f"{label}.rejected_callbacks")
        dropped = _require_int(raw.get("dropped_callbacks"), f"{label}.dropped_callbacks")
        unexpected = _require_int(raw.get("unexpected_callbacks"), f"{label}.unexpected_callbacks")
        duplicates = _require_int(raw.get("duplicate_callbacks"), f"{label}.duplicate_callbacks")
        invalid = _require_int(raw.get("invalid_callbacks"), f"{label}.invalid_callbacks")
    return {
        "expected": expected,
        "delivered": delivered,
        "attribute_delivered": attributes,
        "interaction_delivered": interactions,
        "rejected": rejected,
        "dropped": dropped,
        "unexpected": unexpected,
        "duplicates": duplicates,
        "invalid": invalid,
    }


def normalize_participant_summary(raw: Mapping[str, Any], expected_role: str) -> dict[str, Any]:
    if raw.get("schema") != PARTICIPANT_SUMMARY_SCHEMA:
        raise ValueError(f"{expected_role}: unsupported participant summary schema")
    if raw.get("role") != expected_role:
        raise ValueError(f"{expected_role}: participant summary role differs")
    if raw.get("status") not in {"accepted", "ok", "pass"}:
        raise ValueError(f"{expected_role}: participant did not reach an accepted state")

    sync_source = raw.get("sync", raw)
    sync = _require_mapping(sync_source, f"{expected_role}.sync")
    synchronization = {
        name: _require_bool(sync.get(name), f"{expected_role}.sync.{name}")
        for name in ("ready", "start", "measure", "done")
    }

    accounting = _normalize_accounting(
        _require_mapping(raw.get("callback_accounting"), "callback_accounting"),
        f"{expected_role}.callback_accounting",
    )
    callback_order = raw.get("callback_order_sha256")
    if isinstance(callback_order, Mapping):
        attribute_sha256 = _require_sha256(
            callback_order.get("attribute_sha256"),
            f"{expected_role}.callback_order_sha256.attribute_sha256",
        )
        interaction_sha256 = _require_sha256(
            callback_order.get("interaction_sha256"),
            f"{expected_role}.callback_order_sha256.interaction_sha256",
        )
    else:
        attribute_sha256 = _require_sha256(
            raw.get("attribute_arrival_order_sha256"),
            f"{expected_role}.attribute_arrival_order_sha256",
        )
        interaction_sha256 = _require_sha256(
            raw.get("interaction_arrival_order_sha256"),
            f"{expected_role}.interaction_arrival_order_sha256",
        )

    measurement_source = raw.get("measurements", raw)
    measurements = _require_mapping(measurement_source, f"{expected_role}.measurements")
    return {
        "role": expected_role,
        "seed": _require_int(raw.get("seed"), f"{expected_role}.seed"),
        "count": _require_int(raw.get("count"), f"{expected_role}.count", minimum=1),
        "plan_sha256": _require_sha256(raw.get("plan_sha256"), f"{expected_role}.plan_sha256"),
        "topology_sha256": _require_sha256(
            raw.get("topology_sha256"), f"{expected_role}.topology_sha256"
        ),
        "terminal_state": "accepted",
        "synchronization": synchronization,
        "callback_accounting": accounting,
        "attribute_callback_sha256": attribute_sha256,
        "interaction_callback_sha256": interaction_sha256,
        "callback_trace_sha256": _require_sha256(
            raw.get("callback_trace_sha256"),
            f"{expected_role}.callback_trace_sha256",
        ),
        "measurements": {
            "updateAttributeValues": _optional_metric(
                measurements,
                "update_attribute_values_median_ns",
                f"{expected_role}.update_attribute_values_median_ns",
            ),
            "sendInteraction": _optional_metric(
                measurements,
                "send_interaction_median_ns",
                f"{expected_role}.send_interaction_median_ns",
            ),
            "completedReceiveOrderBatch": _optional_metric(
                measurements,
                "completed_receive_order_batch_ns",
                f"{expected_role}.completed_receive_order_batch_ns",
            ),
        },
    }


def _read_summary(path: Path) -> Mapping[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ValueError(f"cannot read participant summary {path}: {error}") from error
    return _require_mapping(value, str(path))


def validate_run(
    output: Path,
    implementation: str,
    plan: PairPlan,
) -> dict[str, Any]:
    files = {path.name for path in output.iterdir() if path.is_file()}
    if files != set(SUMMARY_FILES):
        raise ValueError(f"{implementation}: compact output must contain exactly {SUMMARY_FILES}")
    participants = {
        role: normalize_participant_summary(_read_summary(output / f"{role}-summary.json"), role)
        for role in ("publisher", "subscriber")
    }
    for role, summary in participants.items():
        if summary["seed"] != plan.seed or summary["count"] != plan.count:
            raise ValueError(f"{implementation} {role}: plan seed/count differs")
        if summary["plan_sha256"] != plan.sha256:
            raise ValueError(f"{implementation} {role}: plan SHA-256 differs")
        if summary["topology_sha256"] != plan.topology_sha256:
            raise ValueError(f"{implementation} {role}: topology SHA-256 differs")
        if not all(summary["synchronization"].values()):
            raise ValueError(f"{implementation} {role}: synchronization is incomplete")

    publisher = participants["publisher"]
    subscriber = participants["subscriber"]
    publisher_accounting = publisher["callback_accounting"]
    subscriber_accounting = subscriber["callback_accounting"]
    if any(publisher_accounting.values()):
        raise ValueError(f"{implementation}: publisher accepted delivery callbacks")
    if (
        subscriber_accounting["expected"] != 2 * plan.count
        or subscriber_accounting["delivered"] != subscriber_accounting["expected"]
        or subscriber_accounting["attribute_delivered"] != plan.count
        or subscriber_accounting["interaction_delivered"] != plan.count
    ):
        raise ValueError(f"{implementation}: subscriber callback accounting is incomplete")
    for error_name in ("rejected", "dropped", "unexpected", "duplicates", "invalid"):
        if subscriber_accounting[error_name] != 0:
            raise ValueError(
                f"{implementation}: subscriber {error_name} callbacks="
                f"{subscriber_accounting[error_name]}"
            )

    publisher_metrics = publisher["measurements"]
    subscriber_metrics = subscriber["measurements"]
    if (
        publisher_metrics["updateAttributeValues"] is None
        or publisher_metrics["sendInteraction"] is None
        or publisher_metrics["completedReceiveOrderBatch"] is not None
    ):
        raise ValueError(f"{implementation}: publisher latency summary is incomplete")
    if (
        subscriber_metrics["completedReceiveOrderBatch"] is None
        or subscriber_metrics["updateAttributeValues"] is not None
        or subscriber_metrics["sendInteraction"] is not None
    ):
        raise ValueError(f"{implementation}: subscriber batch summary is incomplete")

    terminal_state = {
        "schema": PARTICIPANT_SUMMARY_SCHEMA,
        "seed": plan.seed,
        "count": plan.count,
        "plan_sha256": plan.sha256,
        "topology_sha256": plan.topology_sha256,
        "participants": {
            role: {
                "terminal_state": summary["terminal_state"],
                "synchronization": summary["synchronization"],
                "callback_accounting": summary["callback_accounting"],
                "attribute_callback_sha256": summary["attribute_callback_sha256"],
                "interaction_callback_sha256": summary["interaction_callback_sha256"],
                "callback_trace_sha256": summary["callback_trace_sha256"],
            }
            for role, summary in participants.items()
        },
    }
    synchronization = {
        name: all(
            participants[role]["synchronization"][name] for role in ("publisher", "subscriber")
        )
        for name in ("ready", "start", "measure", "done")
    }
    batch_ns = int(subscriber_metrics["completedReceiveOrderBatch"])
    delivered_callbacks = int(subscriber_accounting["delivered"])
    evidence = {
        "workload_instance_sha256": plan.sha256,
        "expected_callbacks": int(subscriber_accounting["expected"]),
        "delivered_callbacks": delivered_callbacks,
        "rejected_callbacks": int(subscriber_accounting["rejected"]),
        "dropped_callbacks": int(subscriber_accounting["dropped"]),
        "unexpected_callbacks": int(subscriber_accounting["unexpected"]),
        "duplicate_callbacks": int(subscriber_accounting["duplicates"]),
        "invalid_callbacks": int(subscriber_accounting["invalid"]),
        "ready_synchronized": synchronization["ready"],
        "start_synchronized": synchronization["start"],
        "measure_synchronized": synchronization["measure"],
        "done_synchronized": synchronization["done"],
        "attribute_callback_sha256": subscriber["attribute_callback_sha256"],
        "interaction_callback_sha256": subscriber["interaction_callback_sha256"],
        "callback_trace_sha256": subscriber["callback_trace_sha256"],
        "terminal_state_sha256": sha256_json(terminal_state),
    }
    if tuple(evidence) != EVIDENCE_KEYS:
        raise AssertionError("comparison evidence field order/contract drifted")
    return {
        "implementation": implementation,
        "operation_medians_ns": {
            "updateAttributeValues": int(publisher_metrics["updateAttributeValues"]),
            "sendInteraction": int(publisher_metrics["sendInteraction"]),
        },
        "batch_ns": batch_ns,
        "throughput_deliveries_per_second": (delivered_callbacks * 1_000_000_000.0 / batch_ns),
        "evidence": evidence,
    }


def assert_pair_semantics(
    phase: str,
    pair_index: int,
    pair_results: Mapping[str, Mapping[str, Any]],
) -> None:
    if set(pair_results) != {"portico", "gorti"}:
        raise ValueError(f"{phase} pair {pair_index}: both implementations are required")
    if pair_results["portico"].get("evidence") != pair_results["gorti"].get("evidence"):
        raise ValueError(f"{phase} pair {pair_index}: semantic evidence differs")


def run_pair_with_retries(
    *,
    phase: str,
    pair_index: int,
    order: tuple[str, ...],
    max_pair_attempts: int,
    create_network_for_attempt: Callable[[int], str],
    remove_network_for_attempt: Callable[[str], None],
    run_implementation: Callable[[str, str, int, int], tuple[dict[str, Any], dict[str, Any]]],
    retry_ledger: list[dict[str, Any]] | None = None,
) -> tuple[list[dict[str, Any]], dict[str, dict[str, Any]], int]:
    """Run an atomic pair, replaying only cleaned participant failures."""
    if max_pair_attempts < 1:
        raise ValueError("max_pair_attempts must be positive")

    discarded_attempts = 0
    for pair_attempt in range(1, max_pair_attempts + 1):
        network = create_network_for_attempt(pair_attempt)
        retryable_error: RetryableParticipantError | None = None
        attempt_records: list[dict[str, Any]] = []
        pair_results: dict[str, dict[str, Any]] = {}
        failed_implementation: str | None = None
        try:
            for position, implementation in enumerate(order, start=1):
                failed_implementation = implementation
                result, record = run_implementation(implementation, network, position, pair_attempt)
                pair_results[implementation] = result
                attempt_records.append(record)
                failed_implementation = None
            assert_pair_semantics(phase, pair_index, pair_results)
        except RetryableParticipantError as error:
            retryable_error = error
            if retry_ledger is not None:
                retry_ledger.append(
                    {
                        "phase": phase,
                        "pair": pair_index,
                        "pair_attempt": pair_attempt,
                        "implementation": failed_implementation,
                        "order": list(order),
                        "classification": classify_retryable_error(error),
                    }
                )
        finally:
            # A cleanup exception is deliberately fatal and supersedes retryability.
            remove_network_for_attempt(network)

        if retryable_error is not None:
            discarded_attempts += 1
            if pair_attempt == max_pair_attempts:
                raise retryable_error
            continue
        return attempt_records, pair_results, discarded_attempts

    raise AssertionError("pair retry loop completed without a result")


def classify_retryable_error(error: RetryableParticipantError) -> str:
    detail = str(error).lower()
    if "docker attach failed" in detail:
        return "docker-attach"
    if "timed out" in detail or "timeout" in detail or "exceeded" in detail:
        return "participant-timeout"
    if "exit" in detail:
        return "participant-exit"
    return "retryable-participant-error"


def portico_command(
    role: str,
    publisher_name: str,
    portico_home: Path,
    verifier_jar: Path,
    override_jar: Path,
    fom: Path,
    run_output: str,
    repo: Path,
    federation: str,
    plan: PairPlan,
    operation_warmup: int,
    timeout_ms: int,
    *,
    join_ready_file: str | None = None,
    startup_ready_file: str | None = None,
    startup_release_file: str | None = None,
    teardown_ready_file: str | None = None,
) -> list[str]:
    java = staged_path(portico_home / "jre" / "bin" / "java", repo)
    classpath = ":".join(
        staged_path(path, repo)
        for path in (override_jar, verifier_jar, portico_home / "lib" / "portico.jar")
    )
    command = [
        java,
        "-Dportico.loglevel=OFF",
        (f"-Dportico.jgroups.responseTimeout={PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS}"),
        "-Dportico.jgroups.tcp.bindAddress=NON_LOOPBACK",
        f"-Dportico.jgroups.tcp.initialHosts={publisher_name}[7800]",
        "-cp",
        classpath,
        VERIFIER_CLASS,
        "--role",
        role,
        "--seed",
        str(plan.seed),
        "--count",
        str(plan.count),
        "--operation-warmup",
        str(operation_warmup),
        "--all-federates-sync",
        "true",
        "--receive-order",
        "true",
        "--workload-plan",
        staged_path(plan.path, repo),
        "--workload-plan-sha256",
        plan.sha256,
        "--compact-summary",
        "true",
        "--federation",
        federation,
        "--fom",
        staged_path(fom, repo),
        "--output",
        str(PurePosixPath(run_output) / role),
        "--timeout-ms",
        str(timeout_ms),
    ]
    run_root = _validate_volume_child(run_output)
    if teardown_ready_file is None:
        raise ValueError("Portico compact DEVStone-HLA runs require a teardown-ready file")
    teardown_marker = _validate_volume_child(teardown_ready_file)
    if teardown_marker.parent != run_root:
        raise ValueError("Portico teardown-ready file must be directly under the run root")
    command.extend(["--teardown-ready-file", str(teardown_marker)])

    if join_ready_file is not None:
        marker = _validate_volume_child(join_ready_file)
        if marker.parent != run_root:
            raise ValueError("Portico join-ready file must be directly under the run root")
        command.extend(["--join-ready-file", str(marker)])
    if role == "publisher" and startup_ready_file is None:
        raise ValueError("Portico publisher requires a startup-ready file")
    if startup_ready_file is not None:
        marker = _validate_volume_child(startup_ready_file)
        if marker.parent != run_root:
            raise ValueError("Portico startup-ready file must be directly under the run root")
        command.extend(["--startup-ready-file", str(marker)])
    if startup_release_file is not None:
        if role != "publisher" or startup_ready_file is None:
            raise ValueError(
                "Portico startup release requires publisher startup readiness"
            )
        marker = _validate_volume_child(startup_release_file)
        if marker.parent != run_root:
            raise ValueError("Portico startup release file must be directly under the run root")
        command.extend(["--startup-release-file", str(marker)])
    return command


def gorti_client_command(
    role: str,
    server_name: str,
    client: Path,
    fom: Path,
    run_output: str,
    repo: Path,
    federation: str,
    plan: PairPlan,
    operation_warmup: int,
    timeout_ms: int,
    transport_mode: str = "local-lrc",
    local_lrc_queue: int = 1024,
    local_lrc_ack_every: int = 32,
    local_lrc_batch_size: int = 32,
    callback_representation: str = "handles",
) -> list[str]:
    if transport_mode not in GORTI_TRANSPORT_MODES:
        raise ValueError(f"unsupported gorti receive-order transport: {transport_mode}")
    if callback_representation not in {"names", "handles"}:
        raise ValueError(f"unsupported gorti callback representation: {callback_representation}")
    command = [
        staged_path(client, repo),
        f"--address={server_name}:8442",
        f"--role={role}",
        f"--federation={federation}",
        f"--fom={staged_path(fom, repo)}",
        f"--seed={plan.seed}",
        f"--count={plan.count}",
        f"--operation-warmup={operation_warmup}",
        f"--workload-plan={staged_path(plan.path, repo)}",
        f"--workload-plan-sha256={plan.sha256}",
        "--compact-summary=true",
        f"--output={PurePosixPath(run_output) / role}",
        f"--timeout={timeout_ms}ms",
        "--receive-order=true",
        f"--callback-representation={callback_representation}",
    ]
    if transport_mode == "local-lrc":
        command.extend(
            [
                "--local-lrc=true",
                f"--local-lrc-queue={local_lrc_queue}",
                f"--local-lrc-ack-every={local_lrc_ack_every}",
                f"--local-lrc-batch-size={local_lrc_batch_size}",
            ]
        )
    else:
        command.append("--confirmed=true")
    return command


def _remove_host_output(path: Path) -> None:
    if path.exists():
        shutil.rmtree(path)
    if path.exists():
        raise RuntimeError(f"transient host output was not removed: {path}")


def run_portico(
    docker: str,
    repo: Path,
    network: str,
    volume: str,
    portico_home: Path,
    verifier_jar: Path,
    override_jar: Path,
    fom: Path,
    output: Path,
    run_id: str,
    plan: PairPlan,
    operation_warmup: int,
    timeout_ms: int,
) -> dict[str, Any]:
    suffix = uuid.uuid4().hex[:8]
    publisher_name = f"portico-pub-{suffix}"
    federation = f"PF-{suffix}"
    run_output = volume_run_path(run_id)
    publisher_ready_file = portico_publisher_ready_path(run_output)
    teardown_ready_files = portico_teardown_ready_paths(run_output)
    teardown_ready_file = teardown_ready_files[0]
    containers: list[ContainerProcess] = []
    result: dict[str, Any] | None = None
    participant_captures: dict[str, ProcessCapture] | None = None
    startup_marker_cleanup_verified = False
    portico_run_verified = False
    try:
        # These shared control-plane markers must never survive or predate a run.
        remove_volume_paths(docker, repo, volume, teardown_ready_files)
        # The publisher is Portico's initial host. Its transient gate proves that it has
        # joined and registered the object before the subscriber starts.
        publisher = start_container(
            docker,
            repo,
            network,
            volume,
            publisher_name,
            "publisher",
            portico_command(
                "publisher",
                publisher_name,
                portico_home,
                verifier_jar,
                override_jar,
                fom,
                run_output,
                repo,
                federation,
                plan,
                operation_warmup,
                timeout_ms,
                startup_ready_file=publisher_ready_file,
                teardown_ready_file=teardown_ready_file,
            ),
        )
        containers.append(publisher)
        wait_for_portico_publisher_ready(
            docker,
            repo,
            volume,
            publisher,
            publisher_ready_file,
            timeout_ms / 1000,
        )
        remove_volume_path(docker, repo, volume, publisher_ready_file)
        startup_marker_cleanup_verified = True
        containers.append(
            start_container(
                docker,
                repo,
                network,
                volume,
                f"portico-sub-{suffix}",
                "subscriber",
                portico_command(
                    "subscriber",
                    publisher_name,
                    portico_home,
                    verifier_jar,
                    override_jar,
                    fom,
                    run_output,
                    repo,
                    federation,
                    plan,
                    operation_warmup,
                    timeout_ms,
                    teardown_ready_file=teardown_ready_file,
                ),
            )
        )
        participant_captures = wait_federates(docker, repo, containers, timeout_ms / 1000 * 3)
        require_volume_paths_absent(docker, repo, volume, teardown_ready_files)
        copy_participant_summaries(docker, repo, volume, run_output, output)
        result = validate_run(output, "portico", plan)
        portico_run_verified = True
    finally:
        try:
            cleanup_container_processes(docker, repo, containers)
        finally:
            try:
                if not startup_marker_cleanup_verified:
                    remove_volume_path(docker, repo, volume, publisher_ready_file)
            finally:
                try:
                    if not portico_run_verified:
                        remove_volume_paths(docker, repo, volume, teardown_ready_files)
                finally:
                    try:
                        remove_volume_path(docker, repo, volume, run_output)
                    finally:
                        _remove_host_output(output)
    if result is None:
        raise AssertionError("Portico run completed without a validated result")
    if participant_captures is None:
        raise AssertionError("Portico participants completed without process captures")
    result["status"] = "ok"
    result["participant_exit_codes"] = {
        role: participant_captures[role].returncode for role in ("publisher", "subscriber")
    }
    runtime_profile = portico_runtime_profile()
    result["profile_id"] = runtime_profile["profile_id"]
    result["runtime_profile"] = runtime_profile
    result["logging_disabled"] = True
    result["cleanup_verified"] = True
    return result


def run_gorti(
    docker: str,
    repo: Path,
    network: str,
    volume: str,
    rtid: Path,
    client: Path,
    fom: Path,
    output: Path,
    run_id: str,
    plan: PairPlan,
    operation_warmup: int,
    timeout_ms: int,
    transport_mode: str = "local-lrc",
    local_lrc_queue: int = 1024,
    local_lrc_ack_every: int = 32,
    local_lrc_batch_size: int = 32,
    callback_representation: str = "handles",
    outbox_event_capacity: int = 8192,
    outbox_batch_size: int = 32,
    outbox_flush_interval_ms: int = 1,
    event_log_protobuf_validation: bool = True,
    audit_replay_plugin: str = "none",
) -> dict[str, Any]:
    suffix = uuid.uuid4().hex[:8]
    server_name = f"gorti-rtid-{suffix}"
    federation = f"GF-{suffix}"
    run_output = volume_run_path(run_id)
    server_output = volume_server_path(run_id)
    server: ContainerProcess | None = None
    clients: list[ContainerProcess] = []
    result: dict[str, Any] | None = None
    participant_captures: dict[str, ProcessCapture] | None = None
    server_lifecycle: dict[str, Any] | None = None
    try:
        server = start_container(
            docker,
            repo,
            network,
            volume,
            server_name,
            "rtid",
            [
                staged_path(rtid, repo),
                "--listen=0.0.0.0:8442",
                "--metrics-listen=127.0.0.1:0",
                "--admin-listen=",
                f"--save-dir={server_output}",
                (
                    f"--log-dir={server_output}"
                    if audit_replay_plugin == "event-journal"
                    else "--log-dir="
                ),
                "--log-format=text",
                "--log-level=error",
                f"--outbox-event-capacity={outbox_event_capacity}",
                f"--outbox-batch-size={outbox_batch_size}",
                f"--outbox-flush-interval={outbox_flush_interval_ms}ms",
                (f"--event-log-protobuf-validation={str(event_log_protobuf_validation).lower()}"),
                f"--audit-replay-plugin={audit_replay_plugin}",
            ],
            publish_ports=(8442,),
        )
        try:
            wait_for_rtid_port(docker, repo, server, 20)
        except RetryableParticipantError:
            cleanup_container_processes(docker, repo, (server,))
            server = None
            raise
        for role in ("subscriber", "publisher"):
            clients.append(
                start_container(
                    docker,
                    repo,
                    network,
                    volume,
                    f"gorti-{role[:3]}-{suffix}",
                    role,
                    gorti_client_command(
                        role,
                        server_name,
                        client,
                        fom,
                        run_output,
                        repo,
                        federation,
                        plan,
                        operation_warmup,
                        timeout_ms,
                        transport_mode,
                        local_lrc_queue,
                        local_lrc_ack_every,
                        local_lrc_batch_size,
                        callback_representation,
                    ),
                )
            )
        participant_captures = wait_federates(docker, repo, clients, timeout_ms / 1000 * 3)
        copy_participant_summaries(docker, repo, volume, run_output, output)
        result = validate_run(output, "gorti", plan)
    finally:
        try:
            try:
                cleanup_container_processes(docker, repo, clients)
            finally:
                if server is not None:
                    server_lifecycle = stop_server(docker, repo, server)
        finally:
            try:
                remove_volume_path(docker, repo, volume, run_output)
                remove_volume_path(docker, repo, volume, server_output)
            finally:
                _remove_host_output(output)
    if result is None:
        raise AssertionError("gorti run completed without a validated result")
    if participant_captures is None:
        raise AssertionError("gorti participants completed without process captures")
    if server_lifecycle is None:
        raise AssertionError("gorti completed without RTI server lifecycle evidence")
    result["status"] = "ok"
    result["participant_exit_codes"] = {
        role: participant_captures[role].returncode for role in ("publisher", "subscriber")
    }
    runtime_profile = gorti_runtime_profile(
        audit_replay_plugin,
        event_log_protobuf_validation,
    )
    result["profile_id"] = runtime_profile["profile_id"]
    result["runtime_profile"] = runtime_profile
    result["logging_disabled"] = True
    result["cleanup_verified"] = True
    result["server_lifecycle"] = server_lifecycle
    return result


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


def aggregate(measured: list[dict[str, Any]]) -> dict[str, Any]:
    operations: dict[str, dict[str, list[int]]] = {
        implementation: defaultdict(list) for implementation in ("portico", "gorti")
    }
    throughput: dict[str, list[float]] = {"portico": [], "gorti": []}
    batch: dict[str, list[int]] = {"portico": [], "gorti": []}
    for result in measured:
        implementation = result["implementation"]
        for operation, duration in result["operation_medians_ns"].items():
            operations[implementation][operation].append(int(duration))
        throughput[implementation].append(float(result["throughput_deliveries_per_second"]))
        batch[implementation].append(int(result["batch_ns"]))

    output: dict[str, Any] = {"operations_ns": {}, "delivery": {}}
    for operation in OPERATIONS[:2]:
        portico = summarize(operations["portico"][operation])
        gorti = summarize(operations["gorti"][operation])
        output["operations_ns"][operation] = {
            "portico": portico,
            "gorti": gorti,
            "gorti_speedup": portico["median"] / gorti["median"],
        }
    portico_batch = summarize(batch["portico"])
    gorti_batch = summarize(batch["gorti"])
    portico_throughput = summarize(throughput["portico"])
    gorti_throughput = summarize(throughput["gorti"])
    output["delivery"] = {
        "completed_batch_ns": {
            "portico": portico_batch,
            "gorti": gorti_batch,
            "gorti_speedup": portico_batch["median"] / gorti_batch["median"],
        },
        "throughput_deliveries_per_second": {
            "portico": portico_throughput,
            "gorti": gorti_throughput,
            "gorti_ratio": gorti_throughput["median"] / portico_throughput["median"],
        },
    }
    return output


def prepare_binaries(
    args: argparse.Namespace, repo: Path, build_root: Path
) -> tuple[Path, Path, Path]:
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
    run_checked(
        [args.go, "build", "-trimpath", "-o", str(rtid), "./rti/cmd/rtid"],
        cwd=repo,
        env=go_env,
    )
    run_checked(
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
    override = binary_dir / "portico-jgroups-tcp-override.jar"
    build_override(args.portico_home / "lib" / "portico.jar", override)
    return rtid, client, override


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--transport-config",
        "--config",
        dest="transport_config",
        type=Path,
        help="JSON receive-order transport profile; explicit CLI options take precedence",
    )
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--portico-home", type=Path, required=True)
    parser.add_argument("--verifier-jar", type=Path, required=True)
    parser.add_argument("--fom", type=Path, required=True)
    parser.add_argument("--workload", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--seed", type=nonnegative_int, default=1516)
    parser.add_argument("--operation-warmup", type=positive_int, default=128)
    parser.add_argument("--warmup", type=nonnegative_int, default=5)
    parser.add_argument("--measured", type=positive_int, default=30)
    parser.add_argument("--max-pair-attempts", type=positive_int, default=3)
    parser.add_argument("--timeout-ms", type=positive_int, default=300000)
    parser.add_argument(
        "--gorti-transport",
        choices=GORTI_TRANSPORT_MODES,
        default="local-lrc",
        help="gorti receive-order completion mode",
    )
    parser.add_argument("--local-lrc-queue", type=positive_int, default=1024)
    parser.add_argument("--local-lrc-ack-every", type=positive_int, default=32)
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
    parser.add_argument("--gorti-outbox-event-capacity", type=positive_int, default=8192)
    parser.add_argument("--gorti-outbox-batch-size", type=positive_int, default=32)
    parser.add_argument("--gorti-outbox-flush-interval-ms", type=positive_int, default=1)
    parser.add_argument(
        "--gorti-event-log-protobuf-validation",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="check required Protobuf fields while marshaling gorti event-log records",
    )
    parser.add_argument(
        "--gorti-audit-replay-plugin",
        choices=("none", "event-journal"),
        default="none",
        help="optional gorti audit/replay runtime plugin",
    )
    parser.add_argument("--docker", default="docker")
    parser.add_argument("--go", default="go")
    return parser


def load_transport_profile(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"unable to read transport config {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise ValueError("transport config must be a JSON object")
    unknown = set(value) - TRANSPORT_PROFILE_KEYS
    if unknown:
        raise ValueError(f"transport config has unknown fields: {', '.join(sorted(unknown))}")
    missing = {"schema_version", "receive_order_transport"} - set(value)
    if missing:
        raise ValueError(f"transport config is missing fields: {', '.join(sorted(missing))}")
    if value.get("schema_version") != TRANSPORT_PROFILE_SCHEMA:
        raise ValueError(f"transport config schema_version must be {TRANSPORT_PROFILE_SCHEMA!r}")

    defaults: dict[str, Any] = {
        "gorti_transport": "local-lrc",
        "local_lrc_queue": 1024,
        "local_lrc_ack_every": 32,
        "local_lrc_batch_size": 32,
        "gorti_callback_representation": "handles",
    }
    mapping = {
        "receive_order_transport": "gorti_transport",
        "local_lrc_queue_capacity": "local_lrc_queue",
        "local_lrc_ack_every": "local_lrc_ack_every",
        "local_lrc_batch_size": "local_lrc_batch_size",
        "callback_representation": "gorti_callback_representation",
    }
    for source, destination in mapping.items():
        if source in value:
            defaults[destination] = value[source]

    if defaults["gorti_transport"] not in GORTI_TRANSPORT_MODES:
        raise ValueError("receive_order_transport must be local-lrc or confirmed")
    if defaults["gorti_callback_representation"] not in {"names", "handles"}:
        raise ValueError("callback_representation must be names or handles")
    for key in ("local_lrc_queue", "local_lrc_ack_every"):
        if isinstance(defaults[key], bool) or not isinstance(defaults[key], int) or defaults[key] < 1:
            raise ValueError(f"{key} must be a positive integer")
    if defaults["local_lrc_batch_size"] not in {32, 64, 128, 256}:
        raise ValueError("local_lrc_batch_size must be 32, 64, 128, or 256")
    return defaults


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    probe = argparse.ArgumentParser(add_help=False)
    probe.add_argument("--transport-config", "--config", dest="transport_config", type=Path)
    known, _ = probe.parse_known_args(argv)
    parser = build_parser()
    if known.transport_config is not None:
        parser.set_defaults(**load_transport_profile(known.transport_config))
    return parser.parse_args(argv)


def _portico_control_protocol() -> dict[str, bool]:
    return {
        "portico_publisher_ready_gate": True,
        "portico_ordered_teardown_gate": True,
    }

def portico_runtime_profile() -> dict[str, Any]:
    return {
        "schema": RUNTIME_PROFILE_SCHEMA,
        "profile_id": "portico-2.1.4-diagnostics-off-auditor-off",
        "diagnostic_logging_level": "off",
        "traffic_auditing": "off",
        "event_journal_mode": "not_identified",
        "event_journal_sink": "not_identified",
        "event_journal_persistence": False,
        "event_journal_replay_available": False,
        "event_journal_write_ahead_boundary": "not_identified",
        "protobuf_initialization_validation": "not_applicable",
        "semantic_instrumentation": "compact_counters_and_sha256",
        "process_transcript_capture": "memory",
        "process_transcript_retention": "none",
    }


def gorti_runtime_profile(
    audit_replay_plugin: str,
    event_log_protobuf_validation: bool,
) -> dict[str, Any]:
    if audit_replay_plugin not in {"none", "event-journal"}:
        raise ValueError(f"unknown gorti audit/replay plugin: {audit_replay_plugin}")
    enabled = audit_replay_plugin == "event-journal"
    return {
        "schema": RUNTIME_PROFILE_SCHEMA,
        "profile_id": "gorti-audit-replay" if enabled else "gorti-hla-core",
        "diagnostic_logging_level": "error",
        "traffic_auditing": "not_applicable",
        "audit_replay_plugin": audit_replay_plugin,
        "event_journal_sink": "file" if enabled else "none",
        "event_journal_persistence": enabled,
        "event_journal_replay_available": enabled,
        "event_journal_failure_boundary": False,
        "protobuf_initialization_validation": (
            event_log_protobuf_validation if enabled else "not_executed"
        ),
        "semantic_instrumentation": "compact_counters_and_sha256",
        "process_transcript_capture": "memory",
        "process_transcript_retention": "none",
    }


def _workload_metadata(
    plan_module: ModuleType, workload_path: Path, workload: Mapping[str, Any]
) -> dict[str, Any]:
    topology = _require_mapping(workload.get("topology_identity"), "topology_identity")
    topology_sha256 = _require_sha256(topology.get("digest"), "topology_identity.digest")
    hla_mapping = _require_mapping(workload.get("hla_mapping"), "hla_mapping")
    if hla_mapping.get("delivery_plan_format") != PLAN_FORMAT:
        raise ValueError(f"workload delivery plan format must be {PLAN_FORMAT}")
    expected_counts = _require_mapping(workload.get("expected_counts"), "expected_counts")
    total = _require_mapping(expected_counts.get("total"), "expected_counts.total")
    count = _require_int(
        total.get("atomic_event_deliveries"),
        "expected_counts.total.atomic_event_deliveries",
        minimum=1,
    )
    return {
        "configuration_sha256": str(plan_module.file_sha256(workload_path)),
        "topology_identity_sha256": topology_sha256,
        "plan_format": PLAN_FORMAT,
        "count": count,
    }


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    repo = args.repo.resolve()
    args.portico_home = args.portico_home.resolve()
    args.verifier_jar = args.verifier_jar.resolve()
    args.fom = args.fom.resolve()
    args.workload = args.workload.resolve()
    if args.transport_config is not None:
        args.transport_config = args.transport_config.resolve()
    output = args.output.resolve()
    if output.exists():
        raise FileExistsError(f"comparison output already exists: {output}")
    if args.timeout_ms < 1000:
        raise ValueError("--timeout-ms must be at least 1000")

    portico_jar = args.portico_home / "lib" / "portico.jar"
    java = args.portico_home / "jre" / "bin" / "java"
    for path in (portico_jar, java, args.verifier_jar, args.fom, args.workload):
        if not path.is_file():
            raise FileNotFoundError(path)
    for path in (args.portico_home, args.verifier_jar, args.fom, args.workload):
        path.resolve().relative_to(repo)
    if shutil.which(args.docker) is None or shutil.which(args.go) is None:
        raise FileNotFoundError("docker and go must be available on PATH")

    plan_module = load_devstone_plan_module(repo)
    workload = plan_module.load_workload(args.workload)
    workload_metadata = _workload_metadata(plan_module, args.workload, workload)
    scratch_parent = repo / ".tmp"
    scratch_parent.mkdir(parents=True, exist_ok=True)
    comparison: dict[str, Any]
    with tempfile.TemporaryDirectory(
        prefix="devstone-hla-comparison-", dir=scratch_parent
    ) as temporary:
        scratch = Path(temporary)
        rtid, client, override = prepare_binaries(args, repo, scratch)
        container_identity = inspect_container_image_identity(args.docker, repo)
        volume = create_benchmark_volume(args.docker, repo)
        measured_results: list[dict[str, Any]] = []
        run_records: list[dict[str, Any]] = []
        retry_ledger: list[dict[str, Any]] = []
        discarded_pair_attempts = 0
        schedule = build_pair_schedule(args.seed, args.warmup, args.measured)
        try:
            static_archive = create_staging_archive(
                repo,
                (
                    args.portico_home / "jre",
                    portico_jar,
                    args.verifier_jar,
                    override,
                    args.fom,
                    rtid,
                    client,
                ),
                scratch / "static-artifacts.tar.gz",
            )
            stage_archive(
                args.docker,
                repo,
                volume,
                static_archive,
            )
            portico_bundled_java_version = capture_portico_java_version(
                args.docker,
                repo,
                volume,
                staged_path(args.portico_home / "jre" / "bin" / "java", repo),
            )
            for schedule_position, pair_spec in enumerate(schedule, start=1):
                phase = str(pair_spec["phase"])
                pair_index = int(pair_spec["pair"])
                pair_seed = int(pair_spec["seed"])
                order = tuple(pair_spec["order"])
                pair_identity = f"{phase}-{pair_index:02d}-{pair_seed}"
                plan_path = scratch / "plans" / f"{pair_identity}.dvshla"
                pair_plan = materialize_pair_plan(plan_module, workload, pair_seed, plan_path)
                if (
                    pair_plan.count != workload_metadata["count"]
                    or pair_plan.topology_sha256 != workload_metadata["topology_identity_sha256"]
                ):
                    raise ValueError(f"{pair_identity}: plan differs from workload metadata")
                stage_artifacts(args.docker, repo, volume, (pair_plan.path,))
                staged_plan_path = staged_path(pair_plan.path, repo)

                def create_attempt_network(
                    pair_attempt: int,
                    *,
                    schedule_position: int = schedule_position,
                    phase: str = phase,
                    pair_index: int = pair_index,
                    pair_seed: int = pair_seed,
                    order: tuple[str, ...] = order,
                    pair_identity: str = pair_identity,
                ) -> str:
                    print(
                        f"[{schedule_position}/{len(schedule)}] {phase} "
                        f"{pair_index} seed={pair_seed}: {' -> '.join(order)} "
                        f"(attempt {pair_attempt}/{args.max_pair_attempts})",
                        flush=True,
                    )
                    return create_pair_network(
                        args.docker,
                        repo,
                        f"{pair_identity}-attempt-{pair_attempt}",
                    )

                def remove_attempt_network(network: str) -> None:
                    remove_pair_network(args.docker, repo, network)

                def run_implementation(
                    implementation: str,
                    network: str,
                    position: int,
                    pair_attempt: int,
                    *,
                    pair_identity: str = pair_identity,
                    pair_plan: PairPlan = pair_plan,
                    phase: str = phase,
                    pair_index: int = pair_index,
                    pair_seed: int = pair_seed,
                    order: tuple[str, ...] = order,
                ) -> tuple[dict[str, Any], dict[str, Any]]:
                    assert_network_empty(args.docker, repo, network)
                    run_id = f"{pair_identity}-attempt-{pair_attempt}-{implementation}"
                    transient_output = scratch / "summaries" / run_id
                    started = time.time()
                    if implementation == "portico":
                        result = run_portico(
                            args.docker,
                            repo,
                            network,
                            volume,
                            args.portico_home,
                            args.verifier_jar,
                            override,
                            args.fom,
                            transient_output,
                            run_id,
                            pair_plan,
                            args.operation_warmup,
                            args.timeout_ms,
                        )
                    else:
                        result = run_gorti(
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
                            args.gorti_audit_replay_plugin,
                        )
                    assert_network_empty(args.docker, repo, network)
                    record = dict(result)
                    record.update(
                        {
                            "phase": phase,
                            "pair": pair_index,
                            "position": position,
                            "pair_attempt": pair_attempt,
                            "seed": pair_seed,
                            "order": list(order),
                            "wall_seconds": time.time() - started,
                            "artifacts_retained": False,
                        }
                    )
                    return result, record

                try:
                    pair_records, pair_results, discarded = run_pair_with_retries(
                        phase=phase,
                        pair_index=pair_index,
                        order=order,
                        max_pair_attempts=args.max_pair_attempts,
                        create_network_for_attempt=create_attempt_network,
                        remove_network_for_attempt=remove_attempt_network,
                        run_implementation=run_implementation,
                        retry_ledger=retry_ledger,
                    )
                    discarded_pair_attempts += discarded
                    run_records.extend(pair_records)
                    if phase == "measured":
                        measured_results.extend(
                            pair_results[implementation] for implementation in order
                        )
                finally:
                    remove_volume_path(args.docker, repo, volume, staged_plan_path)
        finally:
            remove_benchmark_volume(args.docker, repo, volume)

        commit = run_checked(["git", "rev-parse", "HEAD"], cwd=repo)
        worktree_dirty = bool(
            run_checked(
                ["git", "-c", "core.excludesFile=", "status", "--porcelain"],
                cwd=repo,
            )
        )
        portico_version = args.portico_home.name.removeprefix("portico-")
        comparison = {
            "schema": "gorti.portico-receive-order-comparison/v2",
            "metadata": {
                "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "host": platform.node(),
                "host_os": platform.platform(),
                **container_identity,
                "docker_client": run_checked(
                    [args.docker, "version", "--format", "{{.Client.Version}}"],
                    cwd=repo,
                ),
                "docker_server": run_checked(
                    [args.docker, "version", "--format", "{{.Server.Version}}"],
                    cwd=repo,
                ),
                "go_version": run_checked([args.go, "version"], cwd=repo),
                "gorti_commit": commit,
                "gorti_worktree_dirty": worktree_dirty,
                "gorti_rtid_sha256": sha256_file(rtid),
                "gorti_client_sha256": sha256_file(client),
                "comparison_harness_sha256": sha256_file(Path(__file__).resolve()),
                "java_verifier_source_sha256": sha256_file(
                    repo
                    / "verification"
                    / "commercial-rti"
                    / "src"
                    / "gorti"
                    / "verification"
                    / "commercialrti"
                    / "CommercialRtiVerifier.java"
                ),
                "portico_version": portico_version,
                "portico_bundled_java_version": portico_bundled_java_version,
                "portico_jar_sha256": sha256_file(portico_jar),
                "verifier_jar_sha256": sha256_file(args.verifier_jar),
                "transport_override_sha256": sha256_file(override),
                "gorti_receive_order_transport": args.gorti_transport,
                "gorti_transport_config_schema": (
                    TRANSPORT_PROFILE_SCHEMA if args.transport_config is not None else None
                ),
                "gorti_transport_config_sha256": (
                    sha256_file(args.transport_config)
                    if args.transport_config is not None
                    else None
                ),
                "gorti_local_lrc_queue_capacity": args.local_lrc_queue,
                "gorti_local_lrc_ack_every": args.local_lrc_ack_every,
                "gorti_local_lrc_batch_size": args.local_lrc_batch_size,
                "gorti_callback_representation": args.gorti_callback_representation,
                "gorti_outbox_event_capacity": args.gorti_outbox_event_capacity,
                "gorti_outbox_batch_size": args.gorti_outbox_batch_size,
                "gorti_outbox_flush_interval_ms": args.gorti_outbox_flush_interval_ms,
                "gorti_event_log_protobuf_validation": (args.gorti_event_log_protobuf_validation),
                "gorti_audit_replay_plugin": args.gorti_audit_replay_plugin,
            },
            "workload": {
                **workload_metadata,
                "seed": args.seed,
                "operation_warmup": args.operation_warmup,
                "fom_sha256": sha256_file(args.fom),
                "fom_bytes": args.fom.stat().st_size,
                "choreography": (
                    "sequential_local_admission_update_then_interaction_then_flush"
                    if args.gorti_transport == "local-lrc"
                    else "sequential_receive_order_update_then_interaction"
                ),
                "callback_model": "HLA_IMMEDIATE",
                "delivery_boundary": (
                    "subscriber_timer_armed_before_VERIFY_START_release_to_final_callback_arrival"
                ),
                "process_transcripts": "not_written",
                "participant_artifacts": "compact_summaries_validated_then_discarded",
                "time_management": "excluded",
                "primary_metric_boundary": "subscriber final callback arrival",
                "caller_latency_comparable": False,
                "caller_latency_boundaries": {
                    "portico": "Portico LRC service return",
                    "gorti": (
                        "gorti LocalLRC queue admission"
                        if args.gorti_transport == "local-lrc"
                        else "gorti server-confirmed service return"
                    ),
                },
            },
            "protocol": {
                "warmup_pairs": args.warmup,
                "measured_pairs": args.measured,
                "max_pair_attempts": args.max_pair_attempts,
                "discarded_pair_attempts": discarded_pair_attempts,
                "discarded_pair_attempt_ledger": retry_ledger,
                "replacement_policy": (
                    "Only participant process exit or timeout after verified cleanup "
                    "replays the entire pair with the same seed, materialized plan, "
                    "and AB/BA order on a fresh network; semantic, summary, plan, "
                    "evidence, and cleanup failures abort without replacement"
                ),
                "ordering": "phase-local AB/BA alternating; Portico first in pair 1",
                "portico_jgroups_response_timeout_ms": (PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS),
                **_portico_control_protocol(),
                "seed_schedule": (
                    "measured pair i uses base_seed+i-1; warm-up seeds start after "
                    "the measured seed range"
                ),
                "retain_run_artifacts": False,
                "logging_disabled": True,
                "logging_control": (
                    "Portico diagnostics OFF; gorti diagnostics ERROR-only; compact "
                    "participant evidence retained in memory; gorti event-journal "
                    "processing is identified separately by runtime_profile"
                ),
                "cleanup_verified": True,
                "cleanup_verified_all_runs": True,
                "cleanup_control": (
                    "run containers, run/server volume paths, pair networks, and "
                    "comparison volume proven absent"
                ),
                "fresh_processes_per_run": True,
                "independent_federate_processes": 2,
                "container_environment": (
                    "same pinned linux/amd64 image; fresh bridge network per pair"
                ),
                "plan_materialization": "one deterministic DVSHLA1 file per pair",
            },
            "aggregate": aggregate(measured_results),
            "runs": run_records,
        }

    output.mkdir(parents=True)
    write_json(output / "comparison.json", comparison)
    if {path.name for path in output.iterdir()} != {"comparison.json"}:
        raise RuntimeError("comparison workspace contains unexpected files")
    print(f"PASS: {output / 'comparison.json'}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
