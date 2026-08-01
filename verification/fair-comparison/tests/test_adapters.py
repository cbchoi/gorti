from __future__ import annotations

from pathlib import Path
from typing import Any

import pytest
from adapters.convert_result import convert_artifacts
from fair_comparison.contract import ContractError, canonical_json, load_json, sha256_file

from tests.helpers import add_go_event_log, add_server_evidence, workload, write_json


def canonical_records(count: int) -> list[dict[str, object]]:
    records: list[dict[str, object]] = []

    def add(actor: str, service: str, event: str, data: dict[str, object]) -> None:
        records.append(
            {
                "kind": "semantic",
                "seq": len(records),
                "service": service,
                "event": event,
                "actor": actor,
                "data": data,
            }
        )

    for actor in ("publisher", "subscriber"):
        add(actor, "FM", "federation_synchronized", {"label": "VERIFY_READY"})
        add(actor, "FM", "federation_synchronized", {"label": "VERIFY_DONE"})
    add("publisher", "DM", "object_published", {"class": "VerifierEntity"})
    add("publisher", "DM", "interaction_published", {"class": "VerifierMessage"})
    add("subscriber", "DM", "object_subscribed", {"class": "VerifierEntity"})
    add("subscriber", "DM", "interaction_subscribed", {"class": "VerifierMessage"})
    add("publisher", "OM", "object_registered", {"name": "CommercialRtiVerifierEntity"})
    add("subscriber", "OM", "object_discovered", {"name": "CommercialRtiVerifierEntity"})
    for index in range(count):
        common = {"index": index, "logical_time": index + 1}
        attribute = {**common, "payload": f"attribute-{index}"}
        interaction = {**common, "payload": f"interaction-{index}"}
        add("publisher", "OM", "attributes_updated", attribute)
        add("publisher", "OM", "interaction_sent", interaction)
        add("subscriber", "OM", "attributes_reflected", attribute)
        add("subscriber", "OM", "interaction_received", interaction)
    add("publisher", "OM", "object_deleted", {"logical_time": count + 1})
    add("subscriber", "OM", "object_removed", {"logical_time": count + 1})
    for actor in ("publisher", "subscriber"):
        for logical_time in range(1, count + 2):
            add(actor, "TM", "time_advance_granted", {"logical_time": logical_time})
    return records


def benchmark(shared: dict[str, object], implementation: str) -> dict[str, object]:
    actual_workload = {
        "fom_sha256": shared["fom_sha256"],
        "seed": 1516 if implementation == "reference_rti" else "1516",
        "count": shared["count"],
        "two_process": True,
        "choreography": "sequential_update_send_then_tar",
    }
    actual_workload.update(
        {
            "schema": "gorti.fair-comparison/workload-v1",
            "delivery_boundary": "subscriber_pre_tar_to_both_callbacks",
            "callback": "immediate",
            "server_event_log": "off",
        }
    )
    count = int(shared["count"])
    return {
        "schema": "gorti.production-benchmark/v1",
        "metadata": {"workload": actual_workload},
        "samples": [
            {
                "sequence": index,
                "operation": "completed_delivery_batch_latency",
                "duration_ns": 100 + index,
                "dimensions": {"sample_kind": "delivery", "service": "OM"},
            }
            for index in range(count)
        ],
        "delivery_accounting": {
            "expected_fanout": 2 * count,
            "delivered": 2 * count,
            "explicitly_rejected": 0,
            "dropped": 0,
        },
    }


def provenance() -> dict[str, object]:
    return {
        "commit": "test",
        "binary_sha256": "cd" * 32,
        "runtime_versions": {"test": "1"},
        "build_flags": [],
        "exact_argv": ["actual-launcher"],
        "environment": {"server_event_log": "off"},
    }


def adapter_inputs(root: Path, implementation: str) -> dict[str, Any]:
    fom = root / "CommercialRtiVerifier.xml"
    fom.write_bytes(b"canonical-fom-bytes")
    shared = workload(count=2, fom_sha256=sha256_file(fom))
    canonical = root / f"{implementation}-canonical.ndjson"
    canonical.write_text(
        "\n".join(canonical_json(record) for record in canonical_records(2)) + "\n",
        encoding="utf-8",
    )
    return {
        "benchmark_path": write_json(
            root / f"{implementation}-benchmark.json", benchmark(shared, implementation)
        ),
        "canonical_path": canonical,
        "workload_path": write_json(root / "workload.json", shared),
        "provenance_path": write_json(root / f"{implementation}-provenance.json", provenance()),
        "fom_path": fom,
        "implementation": implementation,
        "run_id": f"measured-01-{implementation}",
    }


def test_real_artifact_adapters_produce_identical_semantic_projection(tmp_path: Path) -> None:
    reference_rti = convert_artifacts(**adapter_inputs(tmp_path, "reference_rti"))
    go = convert_artifacts(**adapter_inputs(tmp_path, "go"))

    assert reference_rti["semantics"] == go["semantics"]
    assert reference_rti["workload"] == go["workload"]
    assert reference_rti["accounting"]["expected_fanout"] == 4
    assert reference_rti["accounting"]["delivered"] == 4
    assert reference_rti["metrics"][0]["sample_scope"] == "subscriber_pre_tar_to_both_callbacks"


def test_go_adapter_rejects_non_immediate_callback_metadata(tmp_path: Path) -> None:
    inputs = adapter_inputs(tmp_path, "go")
    artifact = inputs["benchmark_path"]
    value = load_json(artifact)
    value["metadata"]["workload"]["callback"] = "evoked"
    write_json(artifact, value)

    with pytest.raises(ContractError, match="workload callback differs"):
        convert_artifacts(**inputs)


def test_go_adapter_conversion_preserves_sealed_server_evidence(tmp_path: Path) -> None:
    inputs = adapter_inputs(tmp_path, "go")
    shared = load_json(inputs["workload_path"])
    shared["server_event_log"] = "file"
    write_json(inputs["workload_path"], shared)
    benchmark_value = load_json(inputs["benchmark_path"])
    benchmark_value["metadata"]["workload"]["server_event_log"] = "file"
    write_json(inputs["benchmark_path"], benchmark_value)

    evidence: dict[str, Any] = {
        "run_id": inputs["run_id"],
        "provenance": load_json(inputs["provenance_path"]),
    }
    add_server_evidence(evidence, tmp_path)
    add_go_event_log(evidence, tmp_path)
    write_json(inputs["provenance_path"], evidence["provenance"])

    converted = convert_artifacts(**inputs)

    assert converted["provenance"]["event_log"] == evidence["provenance"]["event_log"]
    assert converted["provenance"]["server_process"] == evidence["provenance"]["server_process"]
