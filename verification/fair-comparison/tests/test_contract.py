from __future__ import annotations

import copy
import json
from pathlib import Path
from typing import Any

import pytest
from fair_comparison.contract import CONFIG_SCHEMA, ContractError, validate_config, validate_result

from tests.helpers import add_go_event_log, add_server_evidence, projection, result, workload


def valid_config() -> dict[str, object]:
    arguments = [
        "--fom={fom}",
        "--seed={seed}",
        "--count={count}",
        "--event-log={server_event_log}",
        "--output={output}",
        "--run={run_id}",
        "--workload={workload_file}",
    ]
    launcher = {"executable": "fake", "arguments": arguments, "result_file": "result.json"}
    return {
        "schema": CONFIG_SCHEMA,
        "launchers": {"reference_rti": copy.deepcopy(launcher), "go": copy.deepcopy(launcher)},
    }


def test_config_requires_every_shared_token_in_both_arms() -> None:
    validate_config(valid_config())
    invalid = valid_config()
    invalid["launchers"]["go"]["arguments"] = [  # type: ignore[index]
        argument
        for argument in invalid["launchers"]["go"]["arguments"]  # type: ignore[index]
        if "{fom}" not in argument
    ]
    with pytest.raises(ContractError, match=r"missing tokens: \{fom\}"):
        validate_config(invalid)


@pytest.mark.parametrize(
    ("field", "value", "message"),
    [
        ("seed", 1517, "seed must be 1516"),
        ("two_process", False, "two_process must be true"),
        ("choreography", "parallel", "choreography must be"),
        ("delivery_boundary", "producer_send", "delivery_boundary must be"),
        ("callback", "evoked", "callback must be"),
        ("server_event_log", "discard", "server_event_log must be"),
    ],
)
def test_result_rejects_any_fixed_workload_difference(
    field: str, value: object, message: str
) -> None:
    shared = workload()
    artifact = result("reference_rti", "run", shared)
    artifact["workload"] = {**shared, field: value}
    with pytest.raises(ContractError, match=message):
        validate_result(artifact)


def test_result_recomputes_projection_hash_and_requires_four_services() -> None:
    artifact = result("reference_rti", "run", workload())
    artifact["semantics"]["canonical_projection"][2]["record"]["event"] = "tampered"
    with pytest.raises(ContractError, match="projection SHA-256"):
        validate_result(artifact)

    artifact = result("reference_rti", "run", workload())
    artifact["semantics"]["canonical_projection"] = projection()[:3]
    with pytest.raises(ContractError, match="four records"):
        validate_result(artifact)


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("expected_fanout", 3),
        ("delivered", 3),
        ("explicitly_rejected", 1),
        ("dropped", 1),
        ("duplicates", 1),
        ("invalid", 1),
    ],
)
def test_result_requires_exact_no_loss_two_times_count_accounting(field: str, value: int) -> None:
    artifact = result("go", "run", workload(count=2))
    artifact["accounting"][field] = value
    with pytest.raises(ContractError, match="accounting|reported"):
        validate_result(artifact)


def test_result_must_echo_orchestrator_workload_exactly() -> None:
    expected = workload(fom_sha256="ab" * 32)
    artifact = result("go", "run", workload(fom_sha256="cd" * 32))
    with pytest.raises(ContractError, match="does not match"):
        validate_result(artifact, expected_workload=expected)


def file_workload() -> dict[str, Any]:
    shared = workload()
    shared["server_event_log"] = "file"
    return shared


def claim_grade_go_result(tmp_path: Path) -> tuple[dict[str, Any], Path]:
    artifact = result("go", "measured-01-s1-go", file_workload())
    add_server_evidence(artifact, tmp_path)
    event_log = add_go_event_log(artifact, tmp_path)
    return artifact, event_log


def test_smoke_remains_backward_compatible_but_claim_grade_requires_evidence() -> None:
    artifact = result("go", "smoke", workload())

    validate_result(artifact)
    with pytest.raises(ContractError, match="missing claim-grade evidence"):
        validate_result(artifact, claim_grade=True)


def test_claim_grade_go_file_mode_accepts_a_sealed_generation_log(tmp_path: Path) -> None:
    artifact, _ = claim_grade_go_result(tmp_path)

    validate_result(artifact, claim_grade=True)


def test_claim_grade_go_file_mode_requires_event_log_descriptor(tmp_path: Path) -> None:
    artifact = result("go", "measured-01-s1-go", file_workload())
    add_server_evidence(artifact, tmp_path)

    with pytest.raises(ContractError, match="event_log"):
        validate_result(artifact, claim_grade=True)


def test_event_log_rejects_a_stale_generation_path(tmp_path: Path) -> None:
    artifact, event_log = claim_grade_go_result(tmp_path)
    stale_path = event_log.with_name("0000000000000006.log")
    event_log.rename(stale_path)
    artifact["provenance"]["event_log"]["path"] = str(stale_path.resolve())

    with pytest.raises(ContractError, match="exact generation-qualified path"):
        validate_result(artifact)


def test_event_log_rejects_wrong_header_federation(tmp_path: Path) -> None:
    artifact = result("go", "measured-01-s1-go", file_workload())
    add_go_event_log(artifact, tmp_path, header_federation="wrong-federation")

    with pytest.raises(ContractError, match="federation does not match the event-log header"):
        validate_result(artifact)


def test_event_log_rejects_byte_count_and_hash_mismatch(tmp_path: Path) -> None:
    artifact, event_log = claim_grade_go_result(tmp_path)
    artifact["provenance"]["event_log"]["bytes"] += 1
    with pytest.raises(ContractError, match=r"event_log\.bytes"):
        validate_result(artifact)

    artifact, event_log = claim_grade_go_result(tmp_path / "hash")
    contents = bytearray(event_log.read_bytes())
    contents[-1] ^= 0xFF
    event_log.write_bytes(contents)
    with pytest.raises(ContractError, match=r"event_log\.sha256"):
        validate_result(artifact)


def test_launcher_result_schema_keeps_new_provenance_evidence_optional() -> None:
    schema_path = Path(__file__).parents[1] / "schemas" / "launcher-result.schema.json"
    schema = json.loads(schema_path.read_text(encoding="utf-8"))
    provenance = schema["properties"]["provenance"]

    assert {"event_log", "server_process", "server_logs"} <= provenance["properties"].keys()
    assert not {"event_log", "server_process", "server_logs"} & set(provenance["required"])
