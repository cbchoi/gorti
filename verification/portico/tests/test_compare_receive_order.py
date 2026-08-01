from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import sys
import tarfile
from pathlib import Path, PurePosixPath
from types import SimpleNamespace
from typing import Any

import pytest

MODULE_PATH = Path(__file__).resolve().parents[1] / "compare_receive_order.py"
REPO = MODULE_PATH.parents[2]
sys.path.insert(0, str(MODULE_PATH.parent))
SPEC = importlib.util.spec_from_file_location("portico_comparison", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)

EMPTY_SHA = hashlib.sha256(b"").hexdigest()
PLAN_SHA = hashlib.sha256(b"plan").hexdigest()
TOPOLOGY_SHA = hashlib.sha256(b"topology").hexdigest()
ATTRIBUTE_SHA = hashlib.sha256(b"attributes").hexdigest()
INTERACTION_SHA = hashlib.sha256(b"interactions").hexdigest()
TRACE_SHA = hashlib.sha256(b"callback-trace").hexdigest()
REFERENCE_DIGEST = "sha256:" + "6" * 64
PLATFORM_IMAGE_ID = "sha256:" + "d" * 64


def pair_plan(tmp_path: Path, *, count: int = 3, seed: int = 1516) -> Any:
    path = tmp_path / "pair.dvshla"
    path.write_bytes(b"plan")
    return MODULE.PairPlan(path, seed, count, PLAN_SHA, TOPOLOGY_SHA)


def java_summary(role: str, plan: Any) -> dict[str, Any]:
    subscriber = role == "subscriber"
    accepted_attributes = plan.count if subscriber else 0
    accepted_interactions = plan.count if subscriber else 0
    accepted_total = accepted_attributes + accepted_interactions
    measurements = (
        {"completed_receive_order_batch_ns": 600}
        if subscriber
        else {
            "update_attribute_values_median_ns": 101,
            "send_interaction_median_ns": 202,
        }
    )
    return {
        "schema": MODULE.PARTICIPANT_SUMMARY_SCHEMA,
        "role": role,
        "seed": plan.seed,
        "count": plan.count,
        "plan_sha256": plan.sha256,
        "topology_sha256": plan.topology_sha256,
        "status": "ok",
        "sync": {"ready": True, "start": True, "measure": True, "done": True},
        "callback_accounting": {
            "accepted_attributes": accepted_attributes,
            "accepted_interactions": accepted_interactions,
            "accepted_total": accepted_total,
            "dropped_attributes": 0,
            "dropped_interactions": 0,
            "dropped_total": 0,
            "duplicate_callbacks": 0,
            "expected_attributes": accepted_attributes,
            "expected_interactions": accepted_interactions,
            "expected_total": accepted_total,
            "invalid_callbacks": 0,
        },
        "callback_trace_sha256": TRACE_SHA if subscriber else EMPTY_SHA,
        "callback_order_sha256": {
            "attribute_sha256": ATTRIBUTE_SHA if subscriber else EMPTY_SHA,
            "interaction_sha256": INTERACTION_SHA if subscriber else EMPTY_SHA,
        },
        "measurements": measurements,
    }


def go_summary(role: str, plan: Any) -> dict[str, Any]:
    subscriber = role == "subscriber"
    delivered = 2 * plan.count if subscriber else 0
    summary: dict[str, Any] = {
        "schema": MODULE.PARTICIPANT_SUMMARY_SCHEMA,
        "role": role,
        "seed": plan.seed,
        "count": plan.count,
        "plan_sha256": plan.sha256,
        "topology_sha256": plan.topology_sha256,
        "status": "accepted",
        "ready": True,
        "start": True,
        "measure": True,
        "done": True,
        "callback_accounting": {
            "expected": delivered,
            "delivered": delivered,
            "attribute_delivered": plan.count if subscriber else 0,
            "interaction_delivered": plan.count if subscriber else 0,
            "rejected": 0,
            "dropped": 0,
            "unexpected": 0,
            "duplicates": 0,
            "invalid": 0,
        },
        "attribute_arrival_order_sha256": ATTRIBUTE_SHA if subscriber else EMPTY_SHA,
        "interaction_arrival_order_sha256": INTERACTION_SHA if subscriber else EMPTY_SHA,
        "callback_trace_sha256": TRACE_SHA if subscriber else EMPTY_SHA,
    }
    if subscriber:
        summary["completed_receive_order_batch_ns"] = 600
    else:
        summary["update_attribute_values_median_ns"] = 101
        summary["send_interaction_median_ns"] = 202
    return summary


def write_summary_pair(output: Path, plan: Any, style: str) -> None:
    output.mkdir()
    factory = java_summary if style == "java" else go_summary
    for role in ("publisher", "subscriber"):
        (output / f"{role}-summary.json").write_text(
            json.dumps(factory(role, plan)), encoding="utf-8"
        )


def retry_pair_result(
    implementation: str, pair_attempt: int, *, evidence: str = "same"
) -> tuple[dict[str, Any], dict[str, Any]]:
    result = {"implementation": implementation, "evidence": {"id": evidence}}
    return result, {
        "implementation": implementation,
        "pair_attempt": pair_attempt,
    }


def test_seed_parser_accepts_only_nonnegative_integers() -> None:
    assert MODULE.nonnegative_int("0") == 0
    assert MODULE.nonnegative_int("1516") == 1516
    with pytest.raises(argparse.ArgumentTypeError, match="nonnegative"):
        MODULE.nonnegative_int("-1")
    with pytest.raises(argparse.ArgumentTypeError, match="integer"):
        MODULE.nonnegative_int("not-a-number")


def test_parser_requires_workload_and_defaults_to_five_plus_thirty() -> None:
    parser = MODULE.build_parser()
    args = parser.parse_args(
        [
            "--repo",
            "repo",
            "--portico-home",
            "portico",
            "--verifier-jar",
            "verifier.jar",
            "--fom",
            "fom.xml",
            "--workload",
            "workload.json",
            "--output",
            "output",
        ]
    )
    assert args.workload == Path("workload.json")
    assert args.warmup == 5
    assert args.measured == 30
    assert args.max_pair_attempts == 3
    assert args.gorti_transport == "local-lrc"
    assert args.local_lrc_queue == 1024
    assert args.local_lrc_ack_every == 32
    assert args.local_lrc_batch_size == 32
    assert args.gorti_callback_representation == "handles"
    assert args.gorti_outbox_event_capacity == 8192
    assert args.gorti_outbox_batch_size == 32
    assert args.gorti_outbox_flush_interval_ms == 1
    assert args.gorti_event_log_protobuf_validation is True
    assert args.gorti_audit_replay_plugin == "none"
    assert not hasattr(args, "count")


def test_parser_accepts_audit_replay_plugin_and_rejects_unknown_plugin() -> None:
    parser = MODULE.build_parser()
    required = [
        "--repo",
        "repo",
        "--portico-home",
        "portico",
        "--verifier-jar",
        "verifier.jar",
        "--fom",
        "fom.xml",
        "--workload",
        "workload.json",
        "--output",
        "output",
    ]

    args = parser.parse_args([*required, "--gorti-audit-replay-plugin", "event-journal"])
    assert args.gorti_audit_replay_plugin == "event-journal"

    with pytest.raises(SystemExit):
        parser.parse_args([*required, "--gorti-audit-replay-plugin", "buffered"])


def test_transport_config_loads_defaults_and_cli_wins(tmp_path: Path) -> None:
    profile = tmp_path / "transport.json"
    profile.write_text(
        json.dumps(
            {
                "schema_version": MODULE.TRANSPORT_PROFILE_SCHEMA,
                "receive_order_transport": "confirmed",
                "local_lrc_queue_capacity": 2048,
                "local_lrc_ack_every": 64,
                "local_lrc_batch_size": 128,
                "callback_representation": "names",
            }
        ),
        encoding="utf-8",
    )
    required = [
        "--repo", "repo",
        "--portico-home", "portico",
        "--verifier-jar", "verifier.jar",
        "--fom", "fom.xml",
        "--workload", "workload.json",
        "--output", "output",
        "--config", str(profile),
    ]
    args = MODULE.parse_args(required)
    assert args.gorti_transport == "confirmed"
    assert args.local_lrc_queue == 2048
    assert args.local_lrc_ack_every == 64
    assert args.local_lrc_batch_size == 128
    assert args.gorti_callback_representation == "names"

    args = MODULE.parse_args([*required, "--gorti-transport", "local-lrc", "--local-lrc-queue", "4096"])
    assert args.gorti_transport == "local-lrc"
    assert args.local_lrc_queue == 4096


def test_transport_config_rejects_unknown_fields(tmp_path: Path) -> None:
    profile = tmp_path / "transport.json"
    profile.write_text(
        json.dumps(
            {
                "schema_version": MODULE.TRANSPORT_PROFILE_SCHEMA,
                "receive_order_transport": "local-lrc",
                "unknown": True,
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(ValueError, match="unknown fields"):
        MODULE.parse_args(["--transport-config", str(profile)])


def test_runtime_profiles_distinguish_core_and_audit_plugin() -> None:
    audit = MODULE.gorti_runtime_profile("event-journal", True)
    core = MODULE.gorti_runtime_profile("none", True)
    portico = MODULE.portico_runtime_profile()

    assert audit["profile_id"] == "gorti-audit-replay"
    assert audit["event_journal_sink"] == "file"
    assert audit["event_journal_failure_boundary"] is False
    assert core["profile_id"] == "gorti-hla-core"
    assert core["event_journal_sink"] == "none"
    assert core["event_journal_failure_boundary"] is False
    assert core["protobuf_initialization_validation"] == "not_executed"
    assert portico["diagnostic_logging_level"] == "off"
    assert portico["event_journal_mode"] == "not_identified"

    with pytest.raises(ValueError, match="unknown"):
        MODULE.gorti_runtime_profile("buffered", True)


def test_checked_command_retries_only_docker_ping_500(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    responses = iter(
        (
            SimpleNamespace(
                returncode=125,
                stdout="",
                stderr="500 Internal Server Error for /_ping",
            ),
            SimpleNamespace(returncode=0, stdout="ok\n", stderr=""),
        )
    )
    calls: list[list[str]] = []
    sleeps: list[float] = []

    def fake_run(command: list[str], **kwargs: object) -> SimpleNamespace:
        del kwargs
        calls.append(command)
        return next(responses)

    monkeypatch.setattr(MODULE.subprocess, "run", fake_run)
    monkeypatch.setattr(MODULE.time, "sleep", sleeps.append)

    result = MODULE.run_captured_checked(["docker", "version"], cwd=tmp_path)

    assert result.stdout == "ok\n"
    assert calls == [["docker", "version"], ["docker", "version"]]
    assert sleeps == [0.25]


def test_powershell_launcher_uses_workload_contract() -> None:
    script = (MODULE_PATH.parent / "RunComparison.ps1").read_text(encoding="utf-8")
    assert "benchmark\\devstone\\workload\\workload.json" in script
    assert "'--workload', $Workload" in script
    assert "'--gorti-audit-replay-plugin', $GortiAuditReplayPlugin" in script
    assert "'--transport-config', $TransportConfig" in script
    assert "--count" not in script
    assert "'comparison.json'" in script


def test_pair_schedule_uses_unique_disjoint_seeds_and_balanced_order() -> None:
    schedule = MODULE.build_pair_schedule(1516, warmup_pairs=5, measured_pairs=30)
    warmup = [item for item in schedule if item["phase"] == "warmup"]
    measured = [item for item in schedule if item["phase"] == "measured"]

    assert [item["seed"] for item in measured] == list(range(1516, 1546))
    assert [item["seed"] for item in warmup] == list(range(1546, 1551))
    assert {item["seed"] for item in warmup}.isdisjoint(item["seed"] for item in measured)
    assert sum(item["order"][0] == "portico" for item in measured) == 15
    assert sum(item["order"][0] == "gorti" for item in measured) == 15


def test_plan_module_is_loaded_from_the_repository() -> None:
    module = MODULE.load_devstone_plan_module(REPO)
    assert (
        Path(module.__file__).resolve()
        == (REPO / "benchmark" / "devstone" / "workload" / "plan.py").resolve()
    )
    assert all(
        callable(getattr(module, name))
        for name in (
            "load_workload",
            "materialize_plan",
            "validate_plan",
            "file_sha256",
        )
    )


def test_repository_workload_materializes_a_valid_pair_plan(tmp_path: Path) -> None:
    module = MODULE.load_devstone_plan_module(REPO)
    workload = module.load_workload(REPO / "benchmark" / "devstone" / "workload" / "workload.json")

    result = MODULE.materialize_pair_plan(
        module, workload, 1516, tmp_path / "repository-vector.dvshla"
    )

    assert result.count == workload["expected_counts"]["total"]["atomic_event_deliveries"]
    assert result.sha256 == module.file_sha256(result.path)
    assert result.topology_sha256 == workload["topology_identity"]["digest"]


def test_pair_plan_is_materialized_once_and_validated_from_file(tmp_path: Path) -> None:
    calls: list[object] = []
    generated = SimpleNamespace(
        seed=77,
        record_count=2,
        topology_identity=bytes.fromhex(TOPOLOGY_SHA),
    )

    class FakePlanModule:
        @staticmethod
        def materialize_plan(workload: object, seed: int) -> object:
            calls.append(("materialize", workload, seed))
            return generated

        @staticmethod
        def validate_plan(source: object, workload: object, *, expected_seed: int) -> object:
            calls.append(("validate", source, workload, expected_seed))
            return generated

        @staticmethod
        def write_plan(path: Path, plan: object) -> None:
            calls.append(("write", path, plan))
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b"plan")

        @staticmethod
        def file_sha256(path: Path) -> str:
            calls.append(("sha256", path))
            return PLAN_SHA

    destination = tmp_path / "measured-01.dvshla"
    result = MODULE.materialize_pair_plan(FakePlanModule, {"workload": 1}, 77, destination)

    assert sum(call[0] == "materialize" for call in calls) == 1
    assert sum(call[0] == "validate" for call in calls) == 2
    assert result == MODULE.PairPlan(destination.resolve(), 77, 2, PLAN_SHA, TOPOLOGY_SHA)


def test_pair_plan_rejects_non_dvshla_suffix(tmp_path: Path) -> None:
    with pytest.raises(ValueError, match=".dvshla"):
        MODULE.materialize_pair_plan(object(), {}, 1, tmp_path / "plan.bin")


@pytest.mark.parametrize("style", ["java", "go"])
def test_compact_summary_drives_metrics_and_common_evidence(tmp_path: Path, style: str) -> None:
    plan = pair_plan(tmp_path)
    output = tmp_path / style
    write_summary_pair(output, plan, style)

    result = MODULE.validate_run(output, style, plan)

    assert result["operation_medians_ns"] == {
        "updateAttributeValues": 101,
        "sendInteraction": 202,
    }
    assert result["batch_ns"] == 600
    assert result["throughput_deliveries_per_second"] == 10_000_000.0
    assert tuple(result["evidence"]) == MODULE.EVIDENCE_KEYS
    assert result["evidence"] == {
        "workload_instance_sha256": PLAN_SHA,
        "expected_callbacks": 6,
        "delivered_callbacks": 6,
        "rejected_callbacks": 0,
        "dropped_callbacks": 0,
        "unexpected_callbacks": 0,
        "duplicate_callbacks": 0,
        "invalid_callbacks": 0,
        "ready_synchronized": True,
        "start_synchronized": True,
        "measure_synchronized": True,
        "done_synchronized": True,
        "attribute_callback_sha256": ATTRIBUTE_SHA,
        "interaction_callback_sha256": INTERACTION_SHA,
        "callback_trace_sha256": TRACE_SHA,
        "terminal_state_sha256": result["evidence"]["terminal_state_sha256"],
    }


def test_java_and_go_summary_shapes_normalize_to_identical_semantics(
    tmp_path: Path,
) -> None:
    plan = pair_plan(tmp_path)
    java_output = tmp_path / "java"
    go_output = tmp_path / "go"
    write_summary_pair(java_output, plan, "java")
    write_summary_pair(go_output, plan, "go")

    java_result = MODULE.validate_run(java_output, "portico", plan)
    go_result = MODULE.validate_run(go_output, "gorti", plan)

    assert java_result["evidence"] == go_result["evidence"]
    MODULE.assert_pair_semantics("measured", 1, {"portico": java_result, "gorti": go_result})


def test_summary_validation_rejects_incomplete_start_boundary(tmp_path: Path) -> None:
    plan = pair_plan(tmp_path)
    output = tmp_path / "run"
    write_summary_pair(output, plan, "go")
    subscriber_path = output / "subscriber-summary.json"
    subscriber = json.loads(subscriber_path.read_text(encoding="utf-8"))
    subscriber["start"] = False
    subscriber_path.write_text(json.dumps(subscriber), encoding="utf-8")

    with pytest.raises(ValueError, match="synchronization is incomplete"):
        MODULE.validate_run(output, "gorti", plan)


def test_summary_validation_rejects_plan_identity_mismatch(tmp_path: Path) -> None:
    plan = pair_plan(tmp_path)
    output = tmp_path / "run"
    write_summary_pair(output, plan, "java")
    publisher_path = output / "publisher-summary.json"
    publisher = json.loads(publisher_path.read_text(encoding="utf-8"))
    publisher["plan_sha256"] = "0" * 64
    publisher_path.write_text(json.dumps(publisher), encoding="utf-8")

    with pytest.raises(ValueError, match="plan SHA-256 differs"):
        MODULE.validate_run(output, "portico", plan)


def test_summary_validation_rejects_any_extra_file(tmp_path: Path) -> None:
    plan = pair_plan(tmp_path)
    output = tmp_path / "run"
    write_summary_pair(output, plan, "go")
    (output / "publisher-semantic.ndjson").write_text("{}\n", encoding="utf-8")

    with pytest.raises(ValueError, match="exactly"):
        MODULE.validate_run(output, "gorti", plan)


def test_pair_comparison_rejects_any_evidence_difference() -> None:
    left = {"evidence": {"workload_instance_sha256": PLAN_SHA}}
    right = {"evidence": {"workload_instance_sha256": "0" * 64}}
    with pytest.raises(ValueError, match="semantic evidence differs"):
        MODULE.assert_pair_semantics("measured", 1, {"portico": left, "gorti": right})


def test_first_transient_attempt_retries_with_fresh_network() -> None:
    networks: list[str] = []
    cleaned: list[str] = []
    calls: list[tuple[int, str]] = []

    def create_network(attempt: int) -> str:
        network = f"network-{attempt}"
        networks.append(network)
        return network

    def run(
        implementation: str, network: str, position: int, attempt: int
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        del network, position
        calls.append((attempt, implementation))
        if attempt == 1:
            raise MODULE.RetryableParticipantError("transient participant exit")
        return retry_pair_result(implementation, attempt)

    records, results, discarded = MODULE.run_pair_with_retries(
        phase="measured",
        pair_index=1,
        order=("portico", "gorti"),
        max_pair_attempts=3,
        create_network_for_attempt=create_network,
        remove_network_for_attempt=cleaned.append,
        run_implementation=run,
    )

    assert networks == ["network-1", "network-2"]
    assert cleaned == networks
    assert calls == [(1, "portico"), (2, "portico"), (2, "gorti")]
    assert [record["pair_attempt"] for record in records] == [2, 2]
    assert set(results) == {"portico", "gorti"}
    assert discarded == 1


def test_semantic_failure_is_fatal_without_retry() -> None:
    attempts: list[int] = []
    cleaned: list[str] = []

    def create_network(attempt: int) -> str:
        attempts.append(attempt)
        return f"network-{attempt}"

    def run(
        implementation: str, network: str, position: int, attempt: int
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        del network, position
        return retry_pair_result(
            implementation,
            attempt,
            evidence="portico" if implementation == "portico" else "gorti",
        )

    with pytest.raises(ValueError, match="semantic evidence differs"):
        MODULE.run_pair_with_retries(
            phase="measured",
            pair_index=1,
            order=("portico", "gorti"),
            max_pair_attempts=3,
            create_network_for_attempt=create_network,
            remove_network_for_attempt=cleaned.append,
            run_implementation=run,
        )

    assert attempts == [1]
    assert cleaned == ["network-1"]


def test_cleanup_failure_is_fatal_without_retry() -> None:
    attempts: list[int] = []

    def create_network(attempt: int) -> str:
        attempts.append(attempt)
        return f"network-{attempt}"

    def run(*args: object) -> tuple[dict[str, Any], dict[str, Any]]:
        raise MODULE.RetryableParticipantError("transient participant exit")

    def fail_cleanup(network: str) -> None:
        raise RuntimeError(f"cleanup failed for {network}")

    with pytest.raises(RuntimeError, match="cleanup failed"):
        MODULE.run_pair_with_retries(
            phase="measured",
            pair_index=1,
            order=("portico", "gorti"),
            max_pair_attempts=3,
            create_network_for_attempt=create_network,
            remove_network_for_attempt=fail_cleanup,
            run_implementation=run,
        )

    assert attempts == [1]


def test_second_implementation_failure_replays_the_full_pair() -> None:
    calls: list[tuple[int, str]] = []

    def run(
        implementation: str, network: str, position: int, attempt: int
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        del network, position
        calls.append((attempt, implementation))
        if attempt == 1 and implementation == "gorti":
            raise MODULE.RetryableParticipantError("transient participant exit")
        return retry_pair_result(implementation, attempt)

    records, _, discarded = MODULE.run_pair_with_retries(
        phase="measured",
        pair_index=1,
        order=("portico", "gorti"),
        max_pair_attempts=3,
        create_network_for_attempt=lambda attempt: f"network-{attempt}",
        remove_network_for_attempt=lambda network: None,
        run_implementation=run,
    )

    assert calls == [
        (1, "portico"),
        (1, "gorti"),
        (2, "portico"),
        (2, "gorti"),
    ]
    assert [record["pair_attempt"] for record in records] == [2, 2]
    assert discarded == 1


def test_failed_pair_exposes_no_partial_records() -> None:
    committed_records: list[dict[str, Any]] = []
    created_records: list[dict[str, Any]] = []

    def run(
        implementation: str, network: str, position: int, attempt: int
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        del network, position
        if implementation == "gorti":
            raise MODULE.RetryableParticipantError("transient participant exit")
        result, record = retry_pair_result(implementation, attempt)
        created_records.append(record)
        return result, record

    with pytest.raises(MODULE.RetryableParticipantError):
        records, _, _ = MODULE.run_pair_with_retries(
            phase="measured",
            pair_index=1,
            order=("portico", "gorti"),
            max_pair_attempts=1,
            create_network_for_attempt=lambda attempt: f"network-{attempt}",
            remove_network_for_attempt=lambda network: None,
            run_implementation=run,
        )
        committed_records.extend(records)

    assert len(created_records) == 1
    assert committed_records == []


def test_stage_and_cleanup_docker_runs_pin_linux_amd64(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    artifact = repo / "out" / "client"
    commands = (
        MODULE.stage_artifacts_command("docker", repo, "volume", (artifact,)),
        MODULE.volume_path_cleanup_command("docker", "volume", "/bench/runs/measured-01"),
        MODULE.volume_path_exists_command("docker", "volume", "/bench/runs/measured-01"),
        MODULE.volume_files_command("docker", "volume", "/bench/runs/measured-01"),
        MODULE.copy_summary_file_command(
            "docker",
            "volume",
            "/bench/runs/measured-01/publisher/publisher-summary.json",
            tmp_path / "host",
            "publisher-summary.json",
        ),
    )
    for command in commands:
        assert command[:4] == ["docker", "run", "--platform", "linux/amd64"]
    assert MODULE.image_platform_command("docker")[:5] == [
        "docker",
        "image",
        "inspect",
        "--platform",
        "linux/amd64",
    ]
    assert "--platform" not in MODULE.image_reference_digest_command("docker")
    assert MODULE.platform_image_id_command("docker")[:5] == [
        "docker",
        "image",
        "inspect",
        "--platform",
        "linux/amd64",
    ]


def test_container_identity_distinguishes_reference_digest_and_platform_image_id(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    commands: list[list[str]] = []

    def fake_run(command: list[str], **kwargs: object) -> str:
        commands.append(command)
        if command == MODULE.image_reference_digest_command("docker"):
            return REFERENCE_DIGEST
        if command == MODULE.image_platform_command("docker"):
            return "linux/amd64"
        if command == MODULE.platform_image_id_command("docker"):
            return PLATFORM_IMAGE_ID
        raise AssertionError(command)

    monkeypatch.setattr(MODULE, "run_checked", fake_run)

    identity = MODULE.inspect_container_image_identity("docker", tmp_path)

    assert identity == {
        "container_image_reference": "ubuntu:24.04",
        "container_image_reference_digest": REFERENCE_DIGEST,
        "container_platform": "linux/amd64",
        "container_platform_image_id": PLATFORM_IMAGE_ID,
    }
    assert "--platform" not in commands[0]
    assert "--platform" in commands[1]
    assert "--platform" in commands[2]


@pytest.mark.parametrize(
    ("reference_digest", "platform_image_id", "message"),
    [
        ("ubuntu:24.04", PLATFORM_IMAGE_ID, "reference digest is unresolved"),
        (REFERENCE_DIGEST, "ubuntu:24.04", "platform image id is unresolved"),
    ],
)
def test_container_identity_rejects_mutable_or_unresolved_values(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
    reference_digest: str,
    platform_image_id: str,
    message: str,
) -> None:
    responses = iter((reference_digest, "linux/amd64", platform_image_id))
    monkeypatch.setattr(MODULE, "run_checked", lambda *args, **kwargs: next(responses))

    with pytest.raises(RuntimeError, match=message):
        MODULE.inspect_container_image_identity("docker", tmp_path)


def test_portico_bundled_java_version_is_captured_in_memory(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    observed: dict[str, object] = {}

    def fake_capture(command: list[str], **kwargs: object) -> Any:
        observed["command"] = command
        observed["kwargs"] = kwargs
        return MODULE.ProcessCapture(
            0,
            "",
            'openjdk version "11.0.14"\nOpenJDK Runtime Environment (build 11.0.14+9)',
        )

    monkeypatch.setattr(MODULE, "run_captured_checked", fake_capture)

    version = MODULE.capture_portico_java_version(
        "docker", tmp_path, "volume", "/bench/portico/jre/bin/java"
    )

    assert version == "11.0.14+9"
    command = observed["command"]
    assert isinstance(command, list)
    assert command[:4] == ["docker", "run", "--platform", "linux/amd64"]
    assert command[-2:] == ["/bench/portico/jre/bin/java", "-version"]


def test_stage_command_copies_plan_and_artifacts_without_a_shell(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    artifacts = (
        repo / ".tools" / "portico-2.1.4",
        repo / ".tmp" / "plans" / "measured-01.dvshla",
    )

    command = MODULE.stage_artifacts_command("docker", repo, "volume", artifacts)

    assert "cp" in command
    assert "--parents" in command
    assert ".tmp/plans/measured-01.dvshla" in command
    assert "sh" not in command
    assert "-c" not in command


def test_static_staging_archive_preserves_runtime_layout_and_executable_mode(
    tmp_path: Path,
) -> None:
    repo = tmp_path / "repo"
    java = repo / "portico" / "jre" / "bin" / "java"
    portico_jar = repo / "portico" / "lib" / "portico.jar"
    client = repo / "build" / "gorti-go-fair-linux-amd64"
    for path in (java, portico_jar, client):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(path.name.encode("ascii"))

    archive_path = MODULE.create_staging_archive(
        repo,
        (java.parent.parent, portico_jar, client),
        repo / ".tmp" / "static-artifacts.tar.gz",
    )
    command = MODULE.stage_archive_command("docker", repo, "comparison-volume", archive_path)

    with tarfile.open(archive_path, "r:gz") as archive:
        names = set(archive.getnames())
        java_info = archive.getmember("portico/jre/bin/java")
        client_info = archive.getmember("build/gorti-go-fair-linux-amd64")
    assert "portico/lib/portico.jar" in names
    assert java_info.mode & 0o111
    assert client_info.mode & 0o111
    assert command[:4] == ["docker", "run", "--platform", "linux/amd64"]
    assert command[-7:] == [
        "tar",
        "--extract",
        "--gzip",
        "--file",
        "/work/.tmp/static-artifacts.tar.gz",
        "--directory",
        "/bench",
    ]


def test_commands_pass_one_staged_plan_and_compact_summary(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    portico_home = repo / ".tools" / "portico-2.1.4"
    verifier = repo / "verification" / "verifier.jar"
    override = repo / "out" / "override.jar"
    fom = repo / "verification" / "fom.xml"
    client = repo / "out" / "client"
    plan = MODULE.PairPlan(
        repo / ".tmp" / "plans" / "pair.dvshla",
        1516,
        10,
        PLAN_SHA,
        TOPOLOGY_SHA,
    )
    run_output = "/bench/runs/measured-01"
    ready_file = f"{run_output}/{MODULE.PORTICO_PUBLISHER_READY_FILENAME}"
    teardown_file = f"{run_output}/{MODULE.PORTICO_TEARDOWN_READY_FILENAME}"

    portico = MODULE.portico_command(
        "publisher",
        "portico-pub-test",
        portico_home,
        verifier,
        override,
        fom,
        run_output,
        repo,
        "federation",
        plan,
        2,
        30000,
        startup_ready_file=ready_file,
        teardown_ready_file=teardown_file,
    )
    portico_subscriber = MODULE.portico_command(
        "subscriber",
        "portico-pub-test",
        portico_home,
        verifier,
        override,
        fom,
        run_output,
        repo,
        "federation",
        plan,
        2,
        30000,
        teardown_ready_file=teardown_file,
    )
    gorti = MODULE.gorti_client_command(
        "publisher",
        "rtid",
        client,
        fom,
        run_output,
        repo,
        "federation",
        plan,
        2,
        30000,
        "confirmed",
    )

    staged_plan = "/bench/.tmp/plans/pair.dvshla"
    assert portico[portico.index("--workload-plan") + 1] == staged_plan
    assert portico[portico.index("--workload-plan-sha256") + 1] == PLAN_SHA
    assert portico[portico.index("--compact-summary") + 1] == "true"
    assert portico[portico.index("--count") + 1] == "10"
    assert f"--workload-plan={staged_plan}" in gorti
    assert f"--workload-plan-sha256={PLAN_SHA}" in gorti
    assert "--compact-summary=true" in gorti
    assert "--count=10" in gorti
    assert "--local-lrc=true" not in gorti
    assert "--confirmed=true" in gorti
    assert (
        f"-Dportico.jgroups.responseTimeout={MODULE.PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS}"
    ) in portico
    assert portico[portico.index("--output") + 1].endswith("/publisher")
    assert portico[portico.index("--startup-ready-file") + 1] == ready_file
    assert "--startup-ready-file" not in portico_subscriber
    assert portico[portico.index("--teardown-ready-file") + 1] == teardown_file
    assert (
        portico_subscriber[portico_subscriber.index("--teardown-ready-file") + 1] == teardown_file
    )
    assert PurePosixPath(teardown_file).parent == PurePosixPath(run_output)
    assert PurePosixPath(teardown_file).parent.name not in {"publisher", "subscriber"}
    assert MODULE.portico_teardown_ready_paths(run_output) == (
        teardown_file,
        f"{run_output}/{MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME}",
        f"{run_output}/{MODULE.PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME}",
    )
    for command in (portico, portico_subscriber):
        assert command.count("--teardown-ready-file") == 1
        assert MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME not in command
        assert MODULE.PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME not in command

    local_lrc = MODULE.gorti_client_command(
        "publisher",
        "rtid",
        client,
        fom,
        run_output,
        repo,
        "federation",
        plan,
        2,
        30000,
        "local-lrc",
        2048,
        64,
        128,
    )
    assert "--local-lrc=true" in local_lrc
    assert "--local-lrc-queue=2048" in local_lrc
    assert "--local-lrc-ack-every=64" in local_lrc
    assert "--local-lrc-batch-size=128" in local_lrc
    assert "--confirmed=true" not in local_lrc
    assert "--callback-representation=handles" in local_lrc
    assert any(argument.endswith("/publisher") for argument in gorti)

    subscriber_with_ready = MODULE.portico_command(
        "subscriber",
        "portico-pub-test",
        portico_home,
        verifier,
        override,
        fom,
        run_output,
        repo,
        "federation",
        plan,
        2,
        30000,
        startup_ready_file=ready_file,
        teardown_ready_file=teardown_file,
    )
    assert (
        subscriber_with_ready[
            subscriber_with_ready.index("--startup-ready-file") + 1
        ]
        == ready_file
    )

    with pytest.raises(ValueError, match="teardown-ready"):
        MODULE.portico_command(
            "publisher",
            "portico-pub-test",
            portico_home,
            verifier,
            override,
            fom,
            run_output,
            repo,
            "federation",
            plan,
            2,
            30000,
            startup_ready_file=ready_file,
        )


def test_start_container_pins_platform_and_uses_only_staging_volume(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    observed: dict[str, object] = {}

    class Process:
        returncode = 0

    def fake_popen(argv: list[str], **kwargs: object) -> Process:
        observed["argv"] = argv
        observed["kwargs"] = kwargs
        return Process()

    monkeypatch.setattr(MODULE.subprocess, "Popen", fake_popen)
    repo = tmp_path / "repo"
    MODULE.start_container(
        "docker",
        repo,
        "network",
        "volume",
        "container-name",
        "publisher",
        ["/bench/client"],
    )

    argv = observed["argv"]
    assert isinstance(argv, list)
    assert argv[:4] == ["docker", "run", "--platform", "linux/amd64"]
    assert "volume:/bench" in argv
    assert not any(str(repo) in argument for argument in argv)


def test_wait_federates_classifies_clean_participant_exit_as_retryable(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    process = SimpleNamespace(poll=lambda: 1, returncode=1)
    participant = SimpleNamespace(name="publisher", role="publisher", process=process)
    monkeypatch.setattr(
        MODULE,
        "capture_process",
        lambda item: MODULE.ProcessCapture(1, "", "participant failed"),
    )
    monkeypatch.setattr(MODULE, "_wait_resource_absent", lambda *args: None)

    with pytest.raises(MODULE.RetryableParticipantError):
        MODULE.wait_federates("docker", tmp_path, [participant], 1.0)


def test_wait_federates_cleanup_failure_is_fatal_not_retryable(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    process = SimpleNamespace(poll=lambda: 1, returncode=1)
    participant = SimpleNamespace(name="publisher", role="publisher", process=process)
    monkeypatch.setattr(
        MODULE,
        "capture_process",
        lambda item: MODULE.ProcessCapture(1, "", "participant failed"),
    )
    monkeypatch.setattr(
        MODULE,
        "_wait_resource_absent",
        lambda *args: (_ for _ in ()).throw(RuntimeError("cleanup failed")),
    )

    with pytest.raises(RuntimeError) as captured:
        MODULE.wait_federates("docker", tmp_path, [participant], 1.0)

    assert type(captured.value) is RuntimeError
    assert "cleanup" in str(captured.value)


def test_wait_for_container_port_retries_docker_attach_transport_failure(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    process = SimpleNamespace(poll=lambda: 1, returncode=1)
    server = MODULE.ContainerProcess("rtid", "rtid", process)
    monkeypatch.setattr(
        MODULE,
        "capture_process",
        lambda item: MODULE.ProcessCapture(
            1,
            "",
            "unable to upgrade to tcp, received 500",
        ),
    )
    monkeypatch.setattr(MODULE, "_resource_exists", lambda *args: True)

    with pytest.raises(MODULE.RetryableParticipantError, match="Docker attach failed"):
        MODULE.wait_for_container_port("docker", tmp_path, server, 8442, 1.0)


def test_cleanup_removes_live_container_after_docker_client_exits(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    process = SimpleNamespace(poll=lambda: 1, returncode=1)
    server = MODULE.ContainerProcess("rtid", "rtid", process)
    removed: list[str] = []
    monkeypatch.setattr(MODULE, "_resource_exists", lambda *args: True)
    monkeypatch.setattr(
        MODULE,
        "remove_container",
        lambda _docker, _repo, name: removed.append(name),
    )
    monkeypatch.setattr(
        MODULE,
        "capture_process",
        lambda item: MODULE.ProcessCapture(1, "", "attach failed"),
    )
    monkeypatch.setattr(MODULE, "_wait_resource_absent", lambda *args: None)

    MODULE.cleanup_container_processes("docker", tmp_path, (server,))

    assert removed == ["rtid"]


def test_wait_for_portico_publisher_ready_polls_actual_volume_path(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    checks = iter((False, True))
    observed: list[tuple[str, Path, str, str]] = []
    process = SimpleNamespace(poll=lambda: None)
    publisher = SimpleNamespace(name="publisher", role="publisher", process=process)
    ready_file = "/bench/runs/measured-01/.portico-publisher-startup.ready"

    def exists(docker: str, repo: Path, volume: str, path: str) -> bool:
        observed.append((docker, repo, volume, path))
        return next(checks)

    monkeypatch.setattr(MODULE, "volume_path_exists", exists)
    monkeypatch.setattr(MODULE.time, "sleep", lambda _seconds: None)

    MODULE.wait_for_portico_publisher_ready(
        "docker", tmp_path, "volume", publisher, ready_file, 1.0
    )

    assert observed == [
        ("docker", tmp_path, "volume", ready_file),
        ("docker", tmp_path, "volume", ready_file),
    ]


@pytest.mark.parametrize("returncode", [0, 1])
def test_wait_for_portico_publisher_ready_keeps_exit_retryable(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path, returncode: int
) -> None:
    class ExitedProcess:
        def poll(self) -> int:
            return returncode

        def communicate(self, timeout: int) -> tuple[str, str]:
            del timeout
            return "", "publisher stopped"

        @property
        def returncode(self) -> int:
            return returncode

    publisher = MODULE.ContainerProcess("publisher", "publisher", ExitedProcess())
    monkeypatch.setattr(
        MODULE,
        "volume_path_exists",
        lambda *args: (_ for _ in ()).throw(AssertionError("marker checked after exit")),
    )

    with pytest.raises(MODULE.RetryableParticipantError, match=f"code {returncode}"):
        MODULE.wait_for_portico_publisher_ready(
            "docker",
            tmp_path,
            "volume",
            publisher,
            "/bench/runs/measured-01/.portico-publisher-startup.ready",
            1.0,
        )


def test_wait_for_portico_publisher_ready_rechecks_exit_after_marker(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    polls = iter((None, 0))

    class ExitingProcess:
        returncode = 0

        def poll(self) -> int | None:
            return next(polls)

        def communicate(self, timeout: int) -> tuple[str, str]:
            del timeout
            return "publisher done", ""

    publisher = MODULE.ContainerProcess("publisher", "publisher", ExitingProcess())
    monkeypatch.setattr(MODULE, "volume_path_exists", lambda *args: True)

    with pytest.raises(MODULE.RetryableParticipantError, match="code 0"):
        MODULE.wait_for_portico_publisher_ready(
            "docker",
            tmp_path,
            "volume",
            publisher,
            "/bench/runs/measured-01/.portico-publisher-startup.ready",
            1.0,
        )


def test_run_facts_are_added_only_after_verified_cleanup(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    events: list[str] = []
    repo = tmp_path / "repo"
    plan = MODULE.PairPlan(repo / ".tmp" / "pair.dvshla", 1516, 2, PLAN_SHA, TOPOLOGY_SHA)

    def fake_start(*args: object, **kwargs: object) -> object:
        role = str(args[5])
        events.append(f"start:{role}")
        return SimpleNamespace(role=role)

    monkeypatch.setattr(MODULE, "start_container", fake_start)
    monkeypatch.setattr(
        MODULE,
        "wait_federates",
        lambda *args, **kwargs: (
            events.append("wait")
            or {
                "publisher": MODULE.ProcessCapture(0, "publisher done", ""),
                "subscriber": MODULE.ProcessCapture(0, "subscriber done", ""),
            }
        ),
    )
    monkeypatch.setattr(
        MODULE,
        "wait_for_portico_publisher_ready",
        lambda *args, **kwargs: events.append("ready-gate"),
    )
    monkeypatch.setattr(
        MODULE,
        "copy_participant_summaries",
        lambda *args, **kwargs: events.append("copy"),
    )
    monkeypatch.setattr(
        MODULE,
        "validate_run",
        lambda *args, **kwargs: (
            events.append("validate") or {"implementation": "portico", "evidence": {}}
        ),
    )
    monkeypatch.setattr(
        MODULE,
        "cleanup_container_processes",
        lambda *args, **kwargs: events.append("containers-clean"),
    )

    def remove_path(_docker: str, _repo: Path, _volume: str, path: str) -> None:
        events.append(f"remove:{PurePosixPath(path).name}")

    monkeypatch.setattr(MODULE, "remove_volume_path", remove_path)
    monkeypatch.setattr(
        MODULE,
        "volume_path_exists",
        lambda *args, **kwargs: events.append("teardown-absent") or False,
    )
    monkeypatch.setattr(
        MODULE,
        "_remove_host_output",
        lambda *args, **kwargs: events.append("host-clean"),
    )

    result = MODULE.run_portico(
        "docker",
        repo,
        "network",
        "volume",
        repo / "portico",
        repo / "verifier.jar",
        repo / "override.jar",
        repo / "fom.xml",
        repo / "transient",
        "measured-01-portico",
        plan,
        1,
        30000,
    )

    teardown_marker_names = [
        MODULE.PORTICO_TEARDOWN_READY_FILENAME,
        MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME,
        MODULE.PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME,
    ]
    assert events[:7] == [
        f"remove:{MODULE.PORTICO_TEARDOWN_READY_FILENAME}",
        f"remove:{MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME}",
        f"remove:{MODULE.PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME}",
        "start:publisher",
        "ready-gate",
        f"remove:{MODULE.PORTICO_PUBLISHER_READY_FILENAME}",
        "start:subscriber",
    ]
    assert events.index("wait") < events.index("teardown-absent") < events.index("copy")
    assert events.count("teardown-absent") == 3
    for marker_name in teardown_marker_names:
        assert events.count(f"remove:{marker_name}") == 1
    assert events[-3:] == [
        "containers-clean",
        "remove:measured-01-portico",
        "host-clean",
    ]
    assert result["status"] == "ok"
    assert result["participant_exit_codes"] == {"publisher": 0, "subscriber": 0}
    assert result["logging_disabled"] is True
    assert result["cleanup_verified"] is True


def test_portico_ready_wait_failure_cleans_marker_and_run_root(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    events: list[str] = []
    repo = tmp_path / "repo"
    run_id = "measured-01-portico"
    plan = MODULE.PairPlan(repo / ".tmp" / "pair.dvshla", 1516, 2, PLAN_SHA, TOPOLOGY_SHA)

    def fake_start(*args: object, **kwargs: object) -> object:
        del kwargs
        role = str(args[5])
        events.append(f"start:{role}")
        return SimpleNamespace(role=role)

    def fail_ready(*args: object, **kwargs: object) -> None:
        del args, kwargs
        events.append("ready-gate")
        raise MODULE.RetryableParticipantError("publisher exited")

    def remove_path(_docker: str, _repo: Path, _volume: str, path: str) -> None:
        events.append(f"remove:{PurePosixPath(path).name}")

    monkeypatch.setattr(MODULE, "start_container", fake_start)
    monkeypatch.setattr(MODULE, "wait_for_portico_publisher_ready", fail_ready)
    monkeypatch.setattr(
        MODULE,
        "cleanup_container_processes",
        lambda *args: events.append("containers-clean"),
    )
    monkeypatch.setattr(MODULE, "remove_volume_path", remove_path)
    monkeypatch.setattr(
        MODULE,
        "_remove_host_output",
        lambda *args: events.append("host-clean"),
    )

    with pytest.raises(MODULE.RetryableParticipantError, match="publisher exited"):
        MODULE.run_portico(
            "docker",
            repo,
            "network",
            "volume",
            repo / "portico",
            repo / "verifier.jar",
            repo / "override.jar",
            repo / "fom.xml",
            repo / "transient",
            run_id,
            plan,
            1,
            30000,
        )

    assert events == [
        f"remove:{MODULE.PORTICO_TEARDOWN_READY_FILENAME}",
        f"remove:{MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME}",
        f"remove:{MODULE.PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME}",
        "start:publisher",
        "ready-gate",
        "containers-clean",
        f"remove:{MODULE.PORTICO_PUBLISHER_READY_FILENAME}",
        f"remove:{MODULE.PORTICO_TEARDOWN_READY_FILENAME}",
        f"remove:{MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME}",
        f"remove:{MODULE.PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME}",
        f"remove:{run_id}",
        "host-clean",
    ]


def test_portico_marker_cleanup_failure_is_fatal_and_run_root_is_removed(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    events: list[str] = []
    repo = tmp_path / "repo"
    run_id = "measured-01-portico"
    plan = MODULE.PairPlan(repo / ".tmp" / "pair.dvshla", 1516, 2, PLAN_SHA, TOPOLOGY_SHA)

    def fake_start(*args: object, **kwargs: object) -> object:
        del kwargs
        role = str(args[5])
        events.append(f"start:{role}")
        return SimpleNamespace(role=role)

    def remove_path(_docker: str, _repo: Path, _volume: str, path: str) -> None:
        name = PurePosixPath(path).name
        events.append(f"remove:{name}")
        if name == MODULE.PORTICO_PUBLISHER_READY_FILENAME:
            raise RuntimeError("marker cleanup failed")

    monkeypatch.setattr(MODULE, "start_container", fake_start)
    monkeypatch.setattr(MODULE, "wait_for_portico_publisher_ready", lambda *args: None)
    monkeypatch.setattr(
        MODULE,
        "cleanup_container_processes",
        lambda *args: events.append("containers-clean"),
    )
    monkeypatch.setattr(MODULE, "remove_volume_path", remove_path)
    monkeypatch.setattr(
        MODULE,
        "_remove_host_output",
        lambda *args: events.append("host-clean"),
    )

    with pytest.raises(RuntimeError, match="marker cleanup failed") as captured:
        MODULE.run_portico(
            "docker",
            repo,
            "network",
            "volume",
            repo / "portico",
            repo / "verifier.jar",
            repo / "override.jar",
            repo / "fom.xml",
            repo / "transient",
            run_id,
            plan,
            1,
            30000,
        )

    assert type(captured.value) is RuntimeError
    assert "start:subscriber" not in events
    assert events.count(f"remove:{MODULE.PORTICO_TEARDOWN_READY_FILENAME}") == 2
    assert events.count(f"remove:{MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME}") == 2
    assert events.count(f"remove:{MODULE.PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME}") == 2
    assert events[-2:] == [f"remove:{run_id}", "host-clean"]


def test_retained_portico_teardown_marker_is_fatal_and_cleaned(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    events: list[str] = []
    repo = tmp_path / "repo"
    run_id = "measured-01-portico"
    plan = MODULE.PairPlan(repo / ".tmp" / "pair.dvshla", 1516, 2, PLAN_SHA, TOPOLOGY_SHA)
    retained_name = MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME

    def fake_start(*args: object, **kwargs: object) -> object:
        del kwargs
        role = str(args[5])
        events.append(f"start:{role}")
        return SimpleNamespace(role=role)

    captures = {
        "publisher": MODULE.ProcessCapture(0, "publisher done", ""),
        "subscriber": MODULE.ProcessCapture(0, "subscriber done", ""),
    }

    def remove_path(_docker: str, _repo: Path, _volume: str, path: str) -> None:
        events.append(f"remove:{PurePosixPath(path).name}")

    def marker_exists(_docker: str, _repo: Path, _volume: str, path: str) -> bool:
        name = PurePosixPath(path).name
        events.append(f"check:{name}")
        return name == retained_name

    monkeypatch.setattr(MODULE, "start_container", fake_start)
    monkeypatch.setattr(MODULE, "wait_for_portico_publisher_ready", lambda *args: None)
    monkeypatch.setattr(MODULE, "wait_federates", lambda *args, **kwargs: captures)
    monkeypatch.setattr(MODULE, "remove_volume_path", remove_path)
    monkeypatch.setattr(MODULE, "volume_path_exists", marker_exists)
    monkeypatch.setattr(
        MODULE,
        "copy_participant_summaries",
        lambda *args: (_ for _ in ()).throw(AssertionError("summaries copied after failure")),
    )
    monkeypatch.setattr(
        MODULE,
        "cleanup_container_processes",
        lambda *args: events.append("containers-clean"),
    )
    monkeypatch.setattr(MODULE, "_remove_host_output", lambda *args: events.append("host-clean"))

    with pytest.raises(RuntimeError, match=retained_name):
        MODULE.run_portico(
            "docker",
            repo,
            "network",
            "volume",
            repo / "portico",
            repo / "verifier.jar",
            repo / "override.jar",
            repo / "fom.xml",
            repo / "transient",
            run_id,
            plan,
            1,
            30000,
        )

    assert [event for event in events if event.startswith("check:")] == [
        f"check:{MODULE.PORTICO_TEARDOWN_READY_FILENAME}",
        f"check:{MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME}",
        f"check:{MODULE.PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME}",
    ]
    for marker_name in (
        MODULE.PORTICO_TEARDOWN_READY_FILENAME,
        MODULE.PORTICO_PUBLISHER_RESIGNED_READY_FILENAME,
        MODULE.PORTICO_SUBSCRIBER_DISCONNECTED_READY_FILENAME,
    ):
        assert events.count(f"remove:{marker_name}") == 2
    assert events.index("containers-clean") < events.index(
        f"remove:{MODULE.PORTICO_TEARDOWN_READY_FILENAME}", 3
    )
    assert events[-2:] == [f"remove:{run_id}", "host-clean"]


def test_success_protocol_records_portico_ordered_teardown_gate() -> None:
    assert MODULE._portico_control_protocol() == {
        "portico_publisher_ready_gate": True,
        "portico_ordered_teardown_gate": True,
    }


def test_gorti_run_records_captured_participant_exit_codes(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    events: list[str] = []
    commands: dict[str, list[str]] = {}
    repo = tmp_path / "repo"
    plan = MODULE.PairPlan(repo / ".tmp" / "pair.dvshla", 1516, 2, PLAN_SHA, TOPOLOGY_SHA)

    def fake_start(*args: object, **kwargs: object) -> object:
        role = str(args[5])
        events.append(f"start:{role}")
        commands[role] = list(args[6])  # type: ignore[arg-type]
        return SimpleNamespace(role=role)

    captures = {
        "publisher": MODULE.ProcessCapture(0, "publisher done", ""),
        "subscriber": MODULE.ProcessCapture(0, "subscriber done", ""),
    }
    monkeypatch.setattr(MODULE, "start_container", fake_start)
    monkeypatch.setattr(MODULE, "wait_for_rtid_port", lambda *args: None)
    monkeypatch.setattr(MODULE, "wait_federates", lambda *args: captures)
    monkeypatch.setattr(MODULE, "copy_participant_summaries", lambda *args: None)
    monkeypatch.setattr(
        MODULE,
        "validate_run",
        lambda *args: {"implementation": "gorti", "evidence": {}},
    )
    monkeypatch.setattr(MODULE, "cleanup_container_processes", lambda *args: None)
    lifecycle = {
        "alive_before_shutdown": True,
        "shutdown_requested": True,
        "exit_code_after_shutdown": 143,
        "cleanup_verified": True,
    }
    monkeypatch.setattr(MODULE, "stop_server", lambda *args: lifecycle)
    monkeypatch.setattr(MODULE, "remove_volume_path", lambda *args: None)
    monkeypatch.setattr(MODULE, "_remove_host_output", lambda *args: None)

    result = MODULE.run_gorti(
        "docker",
        repo,
        "network",
        "volume",
        repo / "rtid",
        repo / "client",
        repo / "fom.xml",
        repo / "transient",
        "measured-01-gorti",
        plan,
        1,
        30000,
        event_log_protobuf_validation=False,
        audit_replay_plugin="none",
    )

    assert events == ["start:rtid", "start:subscriber", "start:publisher"]
    assert result["status"] == "ok"
    assert result["participant_exit_codes"] == {"publisher": 0, "subscriber": 0}
    assert result["logging_disabled"] is True
    assert result["cleanup_verified"] is True
    assert result["server_lifecycle"] == lifecycle
    assert result["profile_id"] == "gorti-hla-core"
    assert result["runtime_profile"]["event_journal_sink"] == "none"
    assert "--event-log-protobuf-validation=false" in commands["rtid"]
    assert "--audit-replay-plugin=none" in commands["rtid"]
    assert "--log-level=error" in commands["rtid"]


def test_stop_server_attests_health_and_accepts_expected_stop_code(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    class Process:
        returncode: int | None = None

        def poll(self) -> int | None:
            return self.returncode

        def communicate(self, timeout: int) -> tuple[str, str]:
            del timeout
            return "server output is not evidence", ""

    process = Process()
    server = MODULE.ContainerProcess("rtid", "rtid", process)
    monkeypatch.setattr(MODULE, "_resource_exists", lambda *args: True)

    def stop(command: list[str], **kwargs: object) -> str:
        del command, kwargs
        process.returncode = 143
        return ""

    monkeypatch.setattr(MODULE, "run_checked", stop)
    monkeypatch.setattr(MODULE, "_wait_resource_absent", lambda *args: None)

    lifecycle = MODULE.stop_server("docker", tmp_path, server)

    assert lifecycle == {
        "alive_before_shutdown": True,
        "shutdown_requested": True,
        "exit_code_after_shutdown": 143,
        "cleanup_verified": True,
    }
    assert "output" not in lifecycle
    assert "stderr" not in lifecycle


def test_stop_server_rejects_server_that_exited_before_shutdown(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    class Process:
        returncode = 2

        def poll(self) -> int:
            return self.returncode

        def communicate(self, timeout: int) -> tuple[str, str]:
            del timeout
            return "", "server failed"

    server = MODULE.ContainerProcess("rtid", "rtid", Process())
    monkeypatch.setattr(MODULE, "_resource_exists", lambda *args: False)
    monkeypatch.setattr(MODULE, "_wait_resource_absent", lambda *args: None)
    stop_calls: list[list[str]] = []
    monkeypatch.setattr(
        MODULE,
        "run_checked",
        lambda command, **kwargs: stop_calls.append(command) or "",
    )

    with pytest.raises(RuntimeError, match="not alive before intentional shutdown"):
        MODULE.stop_server("docker", tmp_path, server)

    assert stop_calls == []


def test_compact_copy_rejects_any_non_summary_volume_file(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    monkeypatch.setattr(
        MODULE,
        "_volume_files",
        lambda *args: {
            "publisher/publisher-summary.json",
            "subscriber/subscriber-summary.json",
            "publisher/events.ndjson",
        },
    )
    with pytest.raises(ValueError, match="other than the two"):
        MODULE.copy_participant_summaries(
            "docker",
            tmp_path,
            "volume",
            "/bench/runs/run-1",
            tmp_path / "host",
        )


def test_volume_cleanup_is_scoped_and_verified(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    observed: list[list[str]] = []
    monkeypatch.setattr(
        MODULE,
        "run_checked",
        lambda command, **kwargs: observed.append(command) or "",
    )
    monkeypatch.setattr(MODULE, "volume_path_exists", lambda *args: False)

    MODULE.remove_volume_path("docker", tmp_path, "volume", "/bench/runs/measured-01")

    assert observed[0][:4] == ["docker", "run", "--platform", "linux/amd64"]
    assert observed[0][-1] == "/bench/runs/measured-01"
    with pytest.raises(ValueError, match="unsafe"):
        MODULE.volume_path_cleanup_command("docker", "volume", "/bench")


def test_resource_creation_failure_reclaims_created_volume_and_network(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    removed: list[tuple[str, str]] = []
    monkeypatch.setattr(
        MODULE,
        "run_checked",
        lambda *args, **kwargs: (_ for _ in ()).throw(RuntimeError("response lost")),
    )
    monkeypatch.setattr(MODULE, "_resource_exists", lambda *args: True)
    monkeypatch.setattr(
        MODULE,
        "remove_benchmark_volume",
        lambda _docker, _repo, name: removed.append(("volume", name)),
    )
    monkeypatch.setattr(
        MODULE,
        "remove_pair_network",
        lambda _docker, _repo, name: removed.append(("network", name)),
    )

    with pytest.raises(RuntimeError, match="response lost"):
        MODULE.create_benchmark_volume("docker", tmp_path)
    with pytest.raises(RuntimeError, match="response lost"):
        MODULE.create_pair_network("docker", tmp_path, "measured-01")

    assert [kind for kind, _name in removed] == ["volume", "network"]
    assert removed[0][1].startswith("gorti-portico-bench-")
    assert removed[1][1].startswith("gorti-devstone-measured-01-")


def test_aggregate_uses_compact_run_medians() -> None:
    measured = []
    for implementation, update, interaction, batch in (
        ("portico", 200, 300, 2000),
        ("gorti", 100, 150, 1000),
    ):
        measured.append(
            {
                "implementation": implementation,
                "operation_medians_ns": {
                    "updateAttributeValues": update,
                    "sendInteraction": interaction,
                },
                "batch_ns": batch,
                "throughput_deliveries_per_second": 2_000_000_000 / batch,
            }
        )
    result = MODULE.aggregate(measured)
    assert result["operations_ns"]["updateAttributeValues"]["gorti_speedup"] == 2.0
    assert result["operations_ns"]["sendInteraction"]["gorti_speedup"] == 2.0
    assert result["delivery"]["throughput_deliveries_per_second"]["gorti_ratio"] == 2.0


def test_workload_metadata_uses_raw_file_and_topology_sha(
    tmp_path: Path,
) -> None:
    workload_path = tmp_path / "workload.json"
    workload_path.write_text("{}", encoding="utf-8")
    workload = {
        "topology_identity": {"digest": TOPOLOGY_SHA},
        "hla_mapping": {"delivery_plan_format": "DVSHLA1"},
        "expected_counts": {"total": {"atomic_event_deliveries": 9}},
    }
    module = SimpleNamespace(file_sha256=lambda path: hashlib.sha256(b"{}").hexdigest())

    metadata = MODULE._workload_metadata(module, workload_path, workload)

    assert metadata == {
        "configuration_sha256": hashlib.sha256(b"{}").hexdigest(),
        "topology_identity_sha256": TOPOLOGY_SHA,
        "plan_format": "DVSHLA1",
        "count": 9,
    }
