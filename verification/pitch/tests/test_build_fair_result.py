from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import sys
import types
from pathlib import Path
from typing import Any

import pytest

MODULE_PATH = Path(__file__).parents[1] / "build_fair_result.py"
SPEC = importlib.util.spec_from_file_location("pitch_build_fair_result", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
FAIR_COMPARISON_ROOT = Path(__file__).parents[2] / "fair-comparison"
sys.path.insert(0, str(FAIR_COMPARISON_ROOT))
from fair_comparison.contract import validate_result  # noqa: E402


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def artifact(path: Path) -> dict[str, Any]:
    return {"path": str(path.resolve()), "bytes": path.stat().st_size, "sha256": sha256(path)}


def process(executable: Path, *arguments: str) -> dict[str, Any]:
    return {
        "lifecycle": "per_arm",
        "pid": 1516,
        "started_at": "2026-07-13T00:00:00Z",
        "executable": str(executable.resolve()),
        "executable_sha256": sha256(executable),
        "argv": [str(executable.resolve()), *arguments],
    }


def write_evidence(root: Path, *, status: str = "attested") -> tuple[Path, dict[str, Any]]:
    root.mkdir(parents=True, exist_ok=True)
    crc_java = root / "crc-java.exe"
    verifier_java = root / "verifier-java.exe"
    crc_jar = root / "prtifull.jar"
    api_jar = root / "prti1516e.jar"
    verifier_jar = root / "pitch-verifier.jar"
    for path, content in (
        (crc_java, b"crc-java"),
        (verifier_java, b"verifier-java"),
        (crc_jar, b"crc-jar"),
        (api_jar, b"api-jar"),
        (verifier_jar, b"verifier-jar"),
    ):
        path.write_bytes(content)
    logs: dict[str, Path] = {}
    for name in (
        "crc.stdout",
        "crc.stderr",
        "publisher.stdout",
        "publisher.stderr",
        "subscriber.stdout",
        "subscriber.stderr",
    ):
        logs[name] = root / f"{name}.log"
        logs[name].write_text(f"{name}\n", encoding="utf-8")

    classpath = f"{verifier_jar.resolve()};{api_jar.resolve()}"
    common = (
        "-cp",
        classpath,
        "gorti.verification.pitch.PitchVerifier",
        "--seed",
        "1516",
    )
    evidence = {
        "schema": "gorti.pitch/run-evidence-v1",
        "status": status,
        "reason": None if status == "attested" else "server process evidence is absent",
        "server_process": process(
            crc_java, "-jar", str(crc_jar.resolve()), "-nogui"
        ),
        "client_processes": {
            "publisher": process(verifier_java, *common, "--role", "publisher"),
            "subscriber": process(verifier_java, *common, "--role", "subscriber"),
        },
        "runtime_artifacts": {
            "crc_jar": artifact(crc_jar),
            "pitch_api_jar": artifact(api_jar),
            "verifier_jar": artifact(verifier_jar),
        },
        "server_logs": {
            "stdout": artifact(logs["crc.stdout"]),
            "stderr": artifact(logs["crc.stderr"]),
        },
        "client_logs": {
            role: {
                stream: artifact(logs[f"{role}.{stream}"])
                for stream in ("stdout", "stderr")
            }
            for role in ("publisher", "subscriber")
        },
    }
    evidence_path = root / "run-evidence.json"
    evidence_path.write_text(json.dumps(evidence), encoding="utf-8")
    benchmark = {
        "metadata": {"provenance": {"run_evidence_sha256": sha256(evidence_path)}}
    }
    return evidence_path, benchmark


def test_attestation_accepts_expected_java_commands_jars_and_logs(tmp_path: Path) -> None:
    evidence_path, benchmark = write_evidence(tmp_path)

    evidence = MODULE._attestation(evidence_path, benchmark)

    assert evidence["status"] == "attested"
    assert evidence["client_processes"]["publisher"]["argv"][-1] == "publisher"


def test_attestation_rejects_log_modified_after_capture(tmp_path: Path) -> None:
    evidence_path, benchmark = write_evidence(tmp_path)
    evidence = json.loads(evidence_path.read_text(encoding="utf-8"))
    Path(evidence["client_logs"]["subscriber"]["stderr"]["path"]).write_text(
        "modified", encoding="utf-8"
    )

    with pytest.raises(ValueError, match="subscriber stderr: byte count"):
        MODULE._attestation(evidence_path, benchmark)


def test_attestation_rejects_wrong_verifier_main_class(tmp_path: Path) -> None:
    evidence_path, benchmark = write_evidence(tmp_path)
    evidence = json.loads(evidence_path.read_text(encoding="utf-8"))
    argv = evidence["client_processes"]["publisher"]["argv"]
    main_index = argv.index("gorti.verification.pitch.PitchVerifier")
    argv[main_index] = "example.UnrelatedMain"
    evidence_path.write_text(json.dumps(evidence), encoding="utf-8")
    benchmark["metadata"]["provenance"]["run_evidence_sha256"] = sha256(evidence_path)

    with pytest.raises(ValueError, match="missing required argument"):
        MODULE._attestation(evidence_path, benchmark)


def test_attestation_rejects_explicitly_unattested_launch(tmp_path: Path) -> None:
    evidence_path, benchmark = write_evidence(tmp_path, status="unattested")

    with pytest.raises(ValueError, match="Pitch launch is unattested"):
        MODULE._attestation(evidence_path, benchmark)


def test_builder_keeps_unattested_smoke_result_compatible(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    workload = {
        "schema": "gorti.fair-comparison/workload-v1",
        "fom_sha256": "ab" * 32,
        "seed": 1516,
        "count": 1,
        "two_process": True,
        "choreography": "sequential_update_send_then_tar",
        "delivery_boundary": "subscriber_pre_tar_to_both_callbacks",
        "callback": "immediate",
        "server_event_log": "off",
    }
    samples = []
    sequence = 0
    for operation, multiplier in MODULE.OPERATIONS.items():
        for _ in range(multiplier):
            samples.append(
                {
                    "sequence": sequence,
                    "operation": operation,
                    "duration_ns": sequence + 1,
                    "dimensions": {"sample_kind": "call", "service": "OM"},
                }
            )
            sequence += 1
    benchmark = {
        "metadata": {
            "workload": workload,
            "environment": {"host": "smoke"},
            "provenance": {
                "commit": "test",
                "binary_sha256": "cd" * 32,
                "runtime_versions": {"java": "test"},
                "build_flags": [],
            },
        },
        "samples": samples,
        "delivery_accounting": {
            "expected_fanout": 2,
            "delivered": 2,
            "explicitly_rejected": 0,
            "dropped": 0,
            "duplicates": 0,
            "invalid": 0,
        },
    }
    paths = {
        "benchmark": tmp_path / "benchmark.json",
        "workload": tmp_path / "workload.json",
        "canonical": tmp_path / "canonical.ndjson",
        "argv": tmp_path / "argv.json",
    }
    paths["benchmark"].write_text(json.dumps(benchmark), encoding="utf-8")
    paths["workload"].write_text(json.dumps(workload), encoding="utf-8")
    paths["canonical"].write_text("", encoding="utf-8")
    paths["argv"].write_text(json.dumps({"argv": ["smoke-launcher"]}), encoding="utf-8")
    projection_module = types.ModuleType("verification.common.project_semantics")
    projection_module.project = lambda *_: [
        {"service": service, "record": {"status": "pass"}}
        for service in ("FM", "DM", "OM", "TM")
    ]
    monkeypatch.setitem(sys.modules, "verification.common.project_semantics", projection_module)
    args = argparse.Namespace(
        benchmark=paths["benchmark"],
        workload=paths["workload"],
        canonical=paths["canonical"],
        argv=paths["argv"],
        evidence=None,
        run_id="smoke",
    )

    result = MODULE.build(args)

    assert result["provenance"]["environment"]["launch_attestation"] == "unattested"
    assert "server_process" not in result["provenance"]
    assert "server_logs" not in result["provenance"]

    evidence_path, _ = write_evidence(tmp_path / "attested")
    benchmark["metadata"]["provenance"]["run_evidence_sha256"] = sha256(evidence_path)
    paths["benchmark"].write_text(json.dumps(benchmark), encoding="utf-8")
    args.evidence = evidence_path
    attested = MODULE.build(args)

    assert attested["provenance"]["server_process"]["argv"][-1] == "-nogui"
    assert attested["provenance"]["server_logs"]["stdout"].endswith("crc.stdout.log")
    assert (
        attested["provenance"]["environment"]["client_logs"]["publisher"]["stdout"][
            "sha256"
        ]
        == sha256(tmp_path / "attested" / "publisher.stdout.log")
    )
    validate_result(
        attested,
        expected_workload=workload,
        expected_implementation="pitch",
        expected_run_id="smoke",
        claim_grade=True,
    )
