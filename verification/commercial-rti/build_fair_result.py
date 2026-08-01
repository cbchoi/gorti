"""Build the shared fair-comparison result from one verified reference_rti run."""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from collections import defaultdict
from pathlib import Path
from typing import Any

OPERATIONS = {
    "updateAttributeValues": 1,
    "sendInteraction": 1,
    "timeAdvanceRequest": 2,
    "completed_delivery_batch_latency": 1,
}


def _load(path: Path) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path}: expected a JSON object")
    return value


def _canonical(value: object) -> str:
    return json.dumps(
        value, ensure_ascii=True, allow_nan=False, sort_keys=True, separators=(",", ":")
    )


def _sha256_json(value: object) -> str:
    return hashlib.sha256(_canonical(value).encode("utf-8")).hexdigest()


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _read_ndjson(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line]


def _artifact(value: object, name: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(f"{name}: artifact descriptor is absent")
    path_value = value.get("path")
    size = value.get("bytes")
    sha256 = value.get("sha256")
    if not isinstance(path_value, str) or not path_value:
        raise ValueError(f"{name}: artifact path is absent")
    path = Path(path_value)
    if not path.is_absolute() or not path.is_file():
        raise ValueError(f"{name}: captured artifact is missing: {path}")
    if isinstance(size, bool) or not isinstance(size, int) or size != path.stat().st_size:
        raise ValueError(f"{name}: byte count does not match the captured artifact")
    if not isinstance(sha256, str) or sha256 != _sha256_file(path):
        raise ValueError(f"{name}: SHA-256 does not match the captured artifact")
    return value


def _process(
    value: object,
    name: str,
    *,
    required_arguments: tuple[str, ...],
    allowed_lifecycles: tuple[str, ...] = ("per_arm",),
) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(f"{name}: process evidence is absent")
    executable_value = value.get("executable")
    argv = value.get("argv")
    if (
        value.get("lifecycle") not in allowed_lifecycles
        or isinstance(value.get("pid"), bool)
        or not isinstance(value.get("pid"), int)
        or value["pid"] < 1
        or not isinstance(value.get("started_at"), str)
        or not value["started_at"]
        or not isinstance(executable_value, str)
        or not isinstance(argv, list)
        or not all(isinstance(argument, str) for argument in argv)
    ):
        raise ValueError(f"{name}: process evidence is incomplete")
    executable = Path(executable_value)
    if not executable.is_absolute() or not executable.is_file():
        raise ValueError(f"{name}: executable is missing: {executable}")
    if value.get("executable_sha256") != _sha256_file(executable):
        raise ValueError(f"{name}: executable SHA-256 is absent or stale")
    if not argv or argv[0].casefold() != str(executable).casefold():
        raise ValueError(f"{name}: argv does not begin with its executable")
    for argument in required_arguments:
        if argument not in argv:
            raise ValueError(f"{name}: argv is missing required argument {argument!r}")
    return value


def _argument_value(process: dict[str, Any], option: str, name: str) -> str:
    argv = process["argv"]
    indexes = [index for index, argument in enumerate(argv) if argument == option]
    if len(indexes) != 1 or indexes[0] + 1 >= len(argv):
        raise ValueError(f"{name}: argv must contain exactly one {option} value")
    return argv[indexes[0] + 1]


def _attestation(evidence_path: Path, benchmark: dict[str, Any]) -> dict[str, Any]:
    evidence = _load(evidence_path)
    if evidence.get("schema") != "gorti.commercial-rti/run-evidence-v1":
        raise ValueError("reference_rti launch evidence has an unknown schema")
    if evidence.get("status") != "attested":
        reason = evidence.get("reason") or "required launch evidence is absent"
        raise ValueError(f"reference_rti launch is unattested: {reason}")
    recorded_hash = benchmark["metadata"]["provenance"].get("run_evidence_sha256")
    if recorded_hash != _sha256_file(evidence_path):
        raise ValueError("reference_rti run-evidence SHA-256 differs from the benchmark attestation")

    runtime = evidence.get("runtime_artifacts")
    if not isinstance(runtime, dict):
        raise ValueError("reference_rti runtime artifact evidence is absent")
    server_artifact = _artifact(
        runtime.get("server_artifact"), "reference_rti server artifact"
    )
    api_jar = _artifact(runtime.get("ieee1516e_api_jar"), "IEEE 1516e API JAR")
    verifier_jar = _artifact(runtime.get("verifier_jar"), "reference_rti verifier JAR")
    server_value = evidence.get("server_process")
    server_lifecycle = server_value.get("lifecycle") if isinstance(server_value, dict) else None
    server = _process(
        server_value,
        "reference_rti server",
        required_arguments=(),
        allowed_lifecycles=("per_arm", "persistent_session"),
    )
    selected_server_paths = {
        str(Path(value).resolve()).casefold() for value in server["argv"] if value
    }
    executable_path = str(Path(server["executable"]).resolve()).casefold()
    artifact_path = str(Path(server_artifact["path"]).resolve()).casefold()
    if artifact_path != executable_path and artifact_path not in selected_server_paths:
        raise ValueError(
            "reference_rti server: argv does not select the attested server artifact"
        )
    clients = evidence.get("client_processes")
    if not isinstance(clients, dict):
        raise ValueError("reference_rti verifier process evidence is absent")
    classpath = f"{verifier_jar['path']};{api_jar['path']}"
    publisher = _process(
        clients.get("publisher"),
        "reference_rti publisher verifier",
        required_arguments=(
            "-cp",
            classpath,
            "gorti.verification.commercialrti.CommercialRtiVerifier",
            "--role",
            "publisher",
        ),
    )
    subscriber = _process(
        clients.get("subscriber"),
        "reference_rti subscriber verifier",
        required_arguments=(
            "-cp",
            classpath,
            "gorti.verification.commercialrti.CommercialRtiVerifier",
            "--role",
            "subscriber",
        ),
    )
    if _argument_value(publisher, "-cp", "reference_rti publisher verifier") != classpath:
        raise ValueError("reference_rti publisher verifier: -cp differs from attested JARs")
    if _argument_value(subscriber, "-cp", "reference_rti subscriber verifier") != classpath:
        raise ValueError("reference_rti subscriber verifier: -cp differs from attested JARs")
    if _argument_value(publisher, "--role", "reference_rti publisher verifier") != "publisher":
        raise ValueError("reference_rti publisher verifier: role argument is wrong")
    if _argument_value(subscriber, "--role", "reference_rti subscriber verifier") != "subscriber":
        raise ValueError("reference_rti subscriber verifier: role argument is wrong")
    if publisher["executable"].casefold() != subscriber["executable"].casefold():
        raise ValueError("reference_rti verifier roles used different Java executables")

    server_logs = evidence.get("server_logs")
    client_logs = evidence.get("client_logs")
    if not isinstance(server_logs, dict) or not isinstance(client_logs, dict):
        raise ValueError("reference_rti process log evidence is absent")
    for stream in ("stdout", "stderr"):
        _artifact(server_logs.get(stream), f"reference_rti server {stream}")
        for role in ("publisher", "subscriber"):
            role_logs = client_logs.get(role)
            if not isinstance(role_logs, dict):
                raise ValueError(f"reference_rti {role} log evidence is absent")
            _artifact(role_logs.get(stream), f"reference_rti {role} {stream}")
    return evidence


def build(args: argparse.Namespace) -> dict[str, Any]:
    benchmark = _load(args.benchmark)
    workload = _load(args.workload)
    metadata = benchmark["metadata"]
    if _canonical(metadata["workload"]) != _canonical(workload):
        raise ValueError("reference_rti benchmark workload differs from the orchestrator workload")

    repo = Path(__file__).resolve().parents[2]
    sys.path.insert(0, str(repo))
    from verification.common.project_semantics import project

    rows = project(_read_ndjson(args.canonical), "reference_rti", str(workload["seed"]), workload["count"])
    projection = [{"service": row["service"], "record": row} for row in rows]

    groups: dict[tuple[str, str], list[int]] = defaultdict(list)
    for sample in benchmark["samples"]:
        operation = sample["operation"]
        if operation not in OPERATIONS:
            raise ValueError(f"unsupported reference_rti sample operation: {operation}")
        dimensions = sample["dimensions"]
        groups[(operation, _canonical(dimensions))].append(sample["duration_ns"])

    count = int(workload["count"])
    observed = dict.fromkeys(OPERATIONS, 0)
    metrics: list[dict[str, Any]] = []
    for (operation, encoded_dimensions), samples in sorted(groups.items()):
        invalid_duration = any(
            isinstance(value, bool) or not isinstance(value, int) or value < 0 for value in samples
        )
        if invalid_duration:
            raise ValueError(f"{operation}: durations must be non-negative integer nanoseconds")
        observed[operation] += len(samples)
        metrics.append(
            {
                "name": operation,
                "unit": "ns",
                "direction": "lower",
                "sample_scope": "raw_per_operation",
                "dimensions": json.loads(encoded_dimensions),
                "samples": samples,
            }
        )
    expected = {operation: multiplier * count for operation, multiplier in OPERATIONS.items()}
    if observed != expected:
        raise ValueError(f"reference_rti sample counts differ: {observed} != {expected}")

    accounting = benchmark["delivery_accounting"]
    fair_accounting = {
        name: int(accounting[name])
        for name in (
            "expected_fanout",
            "delivered",
            "explicitly_rejected",
            "dropped",
            "duplicates",
            "invalid",
        )
    }
    provenance = metadata["provenance"]
    evidence_path = getattr(args, "evidence", None)
    evidence = _attestation(evidence_path, benchmark) if evidence_path else None
    environment = dict(metadata["environment"])
    evidence_fields: dict[str, Any] = {}
    if evidence is None:
        environment["launch_attestation"] = "unattested"
        notes = "Smoke-compatible result: process and log evidence was not supplied."
    else:
        environment.update(
            {
                "launch_attestation": "attested",
                "run_evidence_path": str(evidence_path.resolve()),
                "run_evidence_sha256": _sha256_file(evidence_path),
                "runtime_artifacts": evidence["runtime_artifacts"],
                "client_processes": evidence["client_processes"],
                "client_logs": evidence["client_logs"],
                "server_log_artifacts": evidence["server_logs"],
            }
        )
        evidence_fields = {
            "server_process": evidence["server_process"],
            "server_logs": {
                stream: evidence["server_logs"][stream]["path"] for stream in ("stdout", "stderr")
            },
        }
        notes = "Reference RTI server and verifier identities and captured logs are attested."
    return {
        "schema": "gorti.fair-comparison/launcher-result-v1",
        "run_id": args.run_id,
        "implementation": "reference_rti",
        "workload": workload,
        "semantics": {
            "normalization": "gorti.fm-dm-om-tm-projection/v1",
            "canonical_projection": projection,
            "projection_sha256": _sha256_json(projection),
            "status": "pass",
        },
        "provenance": {
            "commit": provenance["commit"],
            "binary_sha256": provenance["binary_sha256"],
            "runtime_versions": provenance["runtime_versions"],
            "build_flags": provenance["build_flags"],
            "exact_argv": _load(args.argv)["argv"],
            "environment": environment,
            "notes": notes,
            **evidence_fields,
        },
        "metrics": metrics,
        "accounting": fair_accounting,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--canonical", type=Path, required=True)
    parser.add_argument("--benchmark", type=Path, required=True)
    parser.add_argument("--evidence", type=Path)
    parser.add_argument("--workload", type=Path, required=True)
    parser.add_argument("--argv", type=Path, required=True)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    result = build(args)
    args.output.write_text(json.dumps(result, ensure_ascii=True, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
