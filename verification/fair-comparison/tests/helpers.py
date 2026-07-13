from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from fair_comparison.contract import (
    MANIFEST_SCHEMA,
    RESULT_SCHEMA,
    WORKLOAD_SCHEMA,
    canonical_json,
    sha256_file,
    sha256_json,
)


def workload(*, count: int = 2, fom_sha256: str = "ab" * 32) -> dict[str, Any]:
    return {
        "schema": WORKLOAD_SCHEMA,
        "fom_sha256": fom_sha256,
        "seed": 1516,
        "count": count,
        "two_process": True,
        "choreography": "sequential_update_send_then_tar",
        "delivery_boundary": "subscriber_pre_tar_to_both_callbacks",
        "callback": "immediate",
        "server_event_log": "off",
    }


def projection(*, suffix: str = "") -> list[dict[str, Any]]:
    return [
        {"service": service, "record": {"event": f"{service.lower()}-pass{suffix}"}}
        for service in ("FM", "DM", "OM", "TM")
    ]


def result(
    implementation: str,
    run_id: str,
    shared_workload: dict[str, Any],
    *,
    samples: list[int] | None = None,
    projection_suffix: str = "",
) -> dict[str, Any]:
    canonical_projection = projection(suffix=projection_suffix)
    expected = 2 * int(shared_workload["count"])
    return {
        "schema": RESULT_SCHEMA,
        "run_id": run_id,
        "implementation": implementation,
        "workload": shared_workload,
        "semantics": {
            "normalization": "gorti.fm-dm-om-tm-projection/v1",
            "canonical_projection": canonical_projection,
            "projection_sha256": sha256_json(canonical_projection),
            "status": "pass",
        },
        "provenance": {
            "commit": "test-commit",
            "binary_sha256": "cd" * 32,
            "runtime_versions": {"test": "1"},
            "build_flags": [],
            "exact_argv": ["fake"],
            "environment": {"GOMAXPROCS": ""},
        },
        "metrics": [
            {
                "name": "completed_delivery_batch_latency",
                "unit": "ns",
                "direction": "lower",
                "sample_scope": "subscriber_pre_tar_to_both_callbacks",
                "dimensions": {"service": "OM"},
                "samples": samples or [10, 20, 30],
            }
        ],
        "accounting": {
            "expected_fanout": expected,
            "delivered": expected,
            "explicitly_rejected": 0,
            "dropped": 0,
            "duplicates": 0,
            "invalid": 0,
        },
    }


def add_server_evidence(artifact: dict[str, Any], root: Path) -> None:
    provenance = artifact["provenance"]
    provenance["server_process"] = {
        "lifecycle": "persistent_session",
        "pid": 1516,
        "started_at": "2026-07-13T00:00:00Z",
        "executable": str((root / "rtid.exe").resolve()),
        "executable_sha256": "ef" * 32,
        "argv": [str((root / "rtid.exe").resolve()), "--listen=127.0.0.1:1516"],
    }
    provenance["server_logs"] = {
        "stdout": str((root / "rtid.stdout.log").resolve()),
        "stderr": str((root / "rtid.stderr.log").resolve()),
    }


def add_go_event_log(
    artifact: dict[str, Any],
    root: Path,
    *,
    generation: int = 7,
    header_federation: str | None = None,
) -> Path:
    run_id = str(artifact["run_id"])
    federation = f"GortiGoFair-{run_id}"
    encoded_federation = (header_federation or federation).encode("utf-8")
    if len(encoded_federation) > 32:
        raise ValueError("test federation exceeds the kdrti header field")
    header = bytearray(64)
    header[:8] = b"KDRTI\x00\x01\x00"
    header[8:12] = (2).to_bytes(4, "little")
    header[12 : 12 + len(encoded_federation)] = encoded_federation
    header[44:52] = generation.to_bytes(8, "little")
    event_log = root / federation.encode("utf-8").hex() / f"{generation:016x}.log"
    event_log.parent.mkdir(parents=True, exist_ok=True)
    event_log.write_bytes(bytes(header) + b"sealed-event-record")
    artifact["provenance"]["event_log"] = {
        "path": str(event_log.resolve()),
        "header": {
            "format": "kdrti/v2",
            "federation": federation,
            "generation": generation,
        },
        "bytes": event_log.stat().st_size,
        "sha256": sha256_file(event_log),
    }
    return event_log


def write_json(path: Path, value: object) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(canonical_json(value) + "\n", encoding="utf-8", newline="\n")
    return path


def session(
    root: Path,
    *,
    semantic_mismatch: bool = False,
    workload_mismatch: bool = False,
) -> Path:
    shared_workload = workload()
    pairs = [(1, "AB"), (2, "BA")]
    runs: list[dict[str, Any]] = []
    global_index = 0
    for pair_index, order in pairs:
        implementations = ("pitch", "go") if order == "AB" else ("go", "pitch")
        for slot, implementation in enumerate(implementations, start=1):
            global_index += 1
            run_id = f"measured-{pair_index:02d}-s{slot}-{implementation}"
            artifact_workload = dict(shared_workload)
            if workload_mismatch and pair_index == 2 and implementation == "go":
                artifact_workload["count"] = 3
            artifact = result(
                implementation,
                run_id,
                artifact_workload,
                samples=(
                    [20, 40, 60]
                    if pair_index == 1 and implementation == "go"
                    else [20, 40, 60]
                    if pair_index == 2
                    else [10, 20, 30]
                ),
                projection_suffix=(
                    "-different"
                    if semantic_mismatch and pair_index == 2 and implementation == "go"
                    else ""
                ),
            )
            relative = Path("measured") / f"pair-{pair_index:02d}" / f"{implementation}.json"
            artifact_path = write_json(root / relative, artifact)
            runs.append(
                {
                    "global_index": global_index,
                    "phase": "measured",
                    "pair_index": pair_index,
                    "slot": slot,
                    "order": order,
                    "implementation": implementation,
                    "run_id": run_id,
                    "output_directory": str(relative.parent).replace("\\", "/"),
                    "result_path": str(relative).replace("\\", "/"),
                    "command": {
                        "executable": "fake",
                        "executable_sha256": "unavailable",
                        "argv": ["fake"],
                        "working_directory": str(root),
                        "environment": {},
                    },
                    "started_at": "2026-07-12T00:00:00Z",
                    "finished_at": "2026-07-12T00:00:01Z",
                    "duration_ns": 1,
                    "exit_code": 0,
                    "status": "success",
                    "result_sha256": sha256_file(artifact_path),
                }
            )
    manifest = {
        "schema": MANIFEST_SCHEMA,
        "session_id": "test-session",
        "state": "complete",
        "created_at": "2026-07-12T00:00:00Z",
        "finished_at": "2026-07-12T00:00:04Z",
        "workload": shared_workload,
        "schedule": {
            "warmup_pairs": 0,
            "measured_pairs": 2,
            "order_seed": 1516,
            "pairs": [
                {"phase": "measured", "pair_index": pair_index, "order": order}
                for pair_index, order in pairs
            ],
        },
        "orchestrator_provenance": {"test": True},
        "runs": runs,
        "analysis_path": "analysis.json",
    }
    return write_json(root / "manifest.json", manifest)


def dump(value: object) -> str:
    return json.dumps(value, sort_keys=True)
