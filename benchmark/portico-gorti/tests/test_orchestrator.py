from __future__ import annotations

# The subprocess test invokes only the checked-in orchestrator with sys.executable.
# ruff: noqa: S603
import argparse
import copy
import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
import zipfile
from collections import Counter
from dataclasses import replace
from pathlib import Path
from unittest import mock

MODULE_PATH = Path(__file__).resolve().parents[1] / "orchestrator.py"
SPEC = importlib.util.spec_from_file_location("portico_gorti_orchestrator", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
orchestrator = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = orchestrator
SPEC.loader.exec_module(orchestrator)
REPO = Path(__file__).resolve().parents[3]
JAVA_SOURCE = REPO / "verification" / "commercial-rti" / "src" / orchestrator.VERIFIER_MAIN_SOURCE
PINNED_REFERENCE_DIGEST = json.loads(
    (REPO / "benchmark" / "environment" / "prerequisites.json").read_text(encoding="utf-8")
)["container_pin"]["digest"]
PLATFORM_IMAGE_ID = "sha256:" + "d" * 64


def fake_verifier_build_evidence() -> dict:
    return {
        "schema": orchestrator.VERIFIER_BUILD_INPUT_SCHEMA,
        "build_input_sha256": "b" * 64,
        "jar_sha256": "6" * 64,
        "main_source_sha256": orchestrator.sha256_file(JAVA_SOURCE),
        "sources": [
            {
                "path": orchestrator.VERIFIER_MAIN_SOURCE.as_posix(),
                "sha256": orchestrator.sha256_file(JAVA_SOURCE),
            }
        ],
        "portico_api_jar_sha256": "1" * 64,
        "compiler_version": "javac 11.0.31",
        "compiler_args": [
            *orchestrator.VERIFIER_COMPILER_FLAGS,
            "-cp",
            "${PORTICO_API_JAR}",
            "-d",
            "${CLASSES_DIR}",
            orchestrator.VERIFIER_MAIN_SOURCE.as_posix(),
        ],
    }


def fake_transport_override_build_evidence() -> dict:
    return {
        "schema": "gorti.portico-transport-override-build-input/v1",
        "build_input_sha256": "c" * 64,
        "builder_path": orchestrator.TRANSPORT_OVERRIDE_BUILDER.as_posix(),
        "builder_sha256": "d" * 64,
        "portico_jar_sha256": "1" * 64,
        "source_resource": orchestrator.TRANSPORT_OVERRIDE_RESOURCE,
        "source_resource_sha256": "e" * 64,
    }


def fake_runtime_probes() -> dict[str, str]:
    return {
        "docker_client": "test-docker",
        "docker_server": "test-docker",
        "container_image_reference_digest": PINNED_REFERENCE_DIGEST,
        "container_platform": "linux/amd64",
        "container_platform_image_id": PLATFORM_IMAGE_ID,
        "go": "go version go1.26.5 test/amd64",
        "java_compiler": "javac 11.0.31",
        "jar_tool": "jar 11.0.31",
    }


def fake_run_profile(implementation: str) -> dict:
    if implementation == "portico":
        profile = {
            "schema": orchestrator.RUNTIME_PROFILE_SCHEMA,
            "profile_id": orchestrator.PORTICO_PROFILE_ID,
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
    else:
        profile = {
            "schema": orchestrator.RUNTIME_PROFILE_SCHEMA,
            "profile_id": orchestrator.GORTI_PROFILE_ID,
            "diagnostic_logging_level": "error",
            "traffic_auditing": "not_applicable",
            "audit_replay_plugin": orchestrator.GORTI_AUDIT_REPLAY_PLUGIN,
            "event_journal_sink": "none",
            "event_journal_persistence": False,
            "event_journal_replay_available": False,
            "event_journal_failure_boundary": False,
            "protobuf_initialization_validation": "not_executed",
            "semantic_instrumentation": "compact_counters_and_sha256",
            "process_transcript_capture": "memory",
            "process_transcript_retention": "none",
        }
    return {"profile_id": profile["profile_id"], "runtime_profile": profile}


def fake_comparison(*, first_schedule: list[str] | None = None) -> dict:
    first_schedule = first_schedule or ["portico" if pair % 2 else "gorti" for pair in range(1, 31)]
    records = []
    feature_summary = {
        "FM": {
            "result": "pass",
            "federates": 2,
            "synchronization_points": [
                "VERIFY_READY",
                "VERIFY_MEASURE",
                "VERIFY_DONE",
            ],
        },
        "DM": {"publish_subscribe": "pass"},
        "OM": {
            "receive_order": "pass",
            "updates": 40600,
            "interactions": 40600,
            "object_lifecycle": "pass",
        },
        "TM": {"status": "excluded", "reason": "not measured"},
    }

    def evidence(pair: int, phase: str) -> dict:
        digest_index = pair + (1000 if phase == "warmup" else 0)
        return {
            "workload_instance_sha256": f"{digest_index:064x}",
            "expected_callbacks": 81200,
            "delivered_callbacks": 81200,
            "rejected_callbacks": 0,
            "dropped_callbacks": 0,
            "unexpected_callbacks": 0,
            "duplicate_callbacks": 0,
            "invalid_callbacks": 0,
            "ready_synchronized": True,
            "start_synchronized": True,
            "measure_synchronized": True,
            "done_synchronized": True,
            "attribute_callback_sha256": f"{digest_index + 100:064x}",
            "interaction_callback_sha256": f"{digest_index + 200:064x}",
            "callback_trace_sha256": f"{digest_index + 300:064x}",
            "terminal_state_sha256": f"{digest_index + 400:064x}",
        }

    for pair in range(1, 6):
        order = ["portico", "gorti"] if pair % 2 else ["gorti", "portico"]
        for position, implementation in enumerate(order, 1):
            records.append(
                {
                    "implementation": implementation,
                    "phase": "warmup",
                    "status": "ok",
                    "participant_exit_codes": {
                        "publisher": 0,
                        "subscriber": 0,
                    },
                    "pair": pair,
                    "pair_attempt": 1,
                    "order": order,
                    "position": position,
                    "seed": 1516 + 30 + pair - 1,
                    "operation_medians_ns": {
                        "updateAttributeValues": 10,
                        "sendInteraction": 20,
                    },
                    "batch_ns": 1000,
                    "throughput_deliveries_per_second": 100.0,
                    "feature_summary": feature_summary,
                    "evidence": evidence(pair, "warmup"),
                    **fake_run_profile(implementation),
                    "logging_disabled": True,
                    "cleanup_verified": True,
                    "behavior_projection_sha256": f"{pair:064x}",
                    "payload_projection_sha256": f"{pair + 100:064x}",
                    "artifacts_retained": False,
                    **(
                        {
                            "server_lifecycle": {
                                "alive_before_shutdown": True,
                                "shutdown_requested": True,
                                "exit_code_after_shutdown": 143,
                                "cleanup_verified": True,
                            }
                        }
                        if implementation == "gorti"
                        else {}
                    ),
                }
            )
    for pair, first in enumerate(first_schedule, 1):
        second = "gorti" if first == "portico" else "portico"
        order = [first, second]
        for position, implementation in enumerate(order, 1):
            offset = 100 if implementation == "gorti" else 0
            records.append(
                {
                    "implementation": implementation,
                    "phase": "measured",
                    "status": "ok",
                    "participant_exit_codes": {
                        "publisher": 0,
                        "subscriber": 0,
                    },
                    "pair": pair,
                    "pair_attempt": 1,
                    "order": order,
                    "position": position,
                    "seed": 1516 + pair - 1,
                    "operation_medians_ns": {
                        "updateAttributeValues": 1000 + pair + offset,
                        "sendInteraction": 2000 + pair + offset,
                    },
                    "batch_ns": 1_000_000 + pair + offset,
                    "throughput_deliveries_per_second": 80_000.0 - offset,
                    "feature_summary": feature_summary,
                    "evidence": evidence(pair, "measured"),
                    **fake_run_profile(implementation),
                    "logging_disabled": True,
                    "cleanup_verified": True,
                    "behavior_projection_sha256": f"{pair:064x}",
                    "payload_projection_sha256": f"{pair + 100:064x}",
                    "artifacts_retained": False,
                    **(
                        {
                            "server_lifecycle": {
                                "alive_before_shutdown": True,
                                "shutdown_requested": True,
                                "exit_code_after_shutdown": 143,
                                "cleanup_verified": True,
                            }
                        }
                        if implementation == "gorti"
                        else {}
                    ),
                }
            )
    hashes = {
        "portico_jar_sha256": "1" * 64,
        "gorti_rtid_sha256": "2" * 64,
        "gorti_client_sha256": "3" * 64,
        "comparison_harness_sha256": "4" * 64,
        "java_verifier_source_sha256": orchestrator.sha256_file(JAVA_SOURCE),
        "verifier_jar_sha256": "6" * 64,
        "transport_override_sha256": "7" * 64,
    }
    return {
        "schema": "gorti.portico-receive-order-comparison/v2",
        "metadata": {
            "generated_at": "2026-07-22T00:00:00Z",
            "host": "test-host",
            "host_os": "test-os",
            "container_image_reference": "ubuntu:24.04",
            "container_image_reference_digest": PINNED_REFERENCE_DIGEST,
            "container_platform": "linux/amd64",
            "container_platform_image_id": PLATFORM_IMAGE_ID,
            "docker_server": "test-docker",
            "go_version": "go version go1.26.5 test/amd64",
            "gorti_commit": "a" * 40,
            "gorti_worktree_dirty": True,
            "gorti_receive_order_transport": "confirmed",
            "gorti_local_lrc_queue_capacity": 1024,
            "gorti_local_lrc_ack_every": 32,
            "gorti_local_lrc_batch_size": 32,
            "gorti_callback_representation": "handles",
            "gorti_outbox_event_capacity": 8192,
            "gorti_outbox_batch_size": 32,
            "gorti_outbox_flush_interval_ms": 1,
            "gorti_audit_replay_plugin": orchestrator.GORTI_AUDIT_REPLAY_PLUGIN,
            "portico_version": "2.1.4",
            "portico_bundled_java_version": "11.0.14+9",
            **hashes,
        },
        "workload": {
            "seed": "1516",
            "count": 40600,
            "configuration_sha256": orchestrator.sha256_file(
                REPO / "benchmark" / "devstone" / "workload" / "workload.json"
            ),
            "topology_identity_sha256": orchestrator.load_contract(REPO).workload_document[
                "topology_identity"
            ]["digest"],
            "plan_format": "DVSHLA1",
            "operation_warmup": 128,
            "fom_sha256": "8" * 64,
            "choreography": orchestrator.CHOREOGRAPHY,
            "callback_model": orchestrator.CALLBACK_MODEL,
            "time_management": orchestrator.TIME_MANAGEMENT,
            "delivery_boundary": orchestrator.TIMER_BOUNDARY_ID,
            "process_transcripts": "not_written",
            "primary_metric_boundary": "subscriber final callback arrival",
            "caller_latency_comparable": False,
            "caller_latency_boundaries": {
                "portico": "Portico LRC service return",
                "gorti": "gorti server-confirmed service return",
            },
        },
        "protocol": {
            "warmup_pairs": 5,
            "measured_pairs": 30,
            "max_pair_attempts": orchestrator.MAX_PAIR_ATTEMPTS,
            "discarded_pair_attempts": 0,
            "replacement_policy": orchestrator.PAIR_REPLACEMENT_POLICY,
            "portico_jgroups_response_timeout_ms": (
                orchestrator.PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS
            ),
            "portico_publisher_ready_gate": True,
            "portico_ordered_teardown_gate": True,
            "retain_run_artifacts": False,
            "fresh_processes_per_run": True,
            "logging_disabled": True,
            "cleanup_verified_all_runs": True,
        },
        "runs": records,
    }


class OrchestratorTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.contract = orchestrator.load_contract(REPO)
        cls.local_lrc_contract = orchestrator.load_contract(
            REPO,
            experiment_path=REPO / "benchmark" / "environment" / "experiment-local-lrc.json",
        )

    def test_workload_identity_and_paired_projection(self) -> None:
        self.assertEqual(self.contract.count_per_channel, 40600)
        self.assertEqual(
            self.contract.workload_identity_sha256,
            orchestrator._canonical_workload_identity(self.contract.workload_document),
        )
        self.assertRegex(self.contract.workload_identity_sha256, r"^[0-9a-f]{64}$")
        self.assertEqual(self.contract.max_pair_attempts, 3)
        self.assertEqual(self.contract.replacement_policy, orchestrator.PAIR_REPLACEMENT_POLICY)
        self.assertEqual(self.contract.operation_warmup, 128)
        self.assertEqual(self.contract.choreography, orchestrator.CHOREOGRAPHY)
        self.assertEqual(self.contract.callback_model, orchestrator.CALLBACK_MODEL)
        self.assertEqual(self.contract.time_management, orchestrator.TIME_MANAGEMENT)
        self.assertEqual(self.contract.timer_boundary_id, orchestrator.TIMER_BOUNDARY_ID)
        self.assertEqual(self.contract.callback_representation, "handles")
        self.assertEqual(self.contract.outbox_event_capacity, 8192)
        self.assertEqual(self.contract.outbox_batch_size, 32)
        self.assertEqual(self.contract.outbox_flush_interval_ms, 1)

    def test_local_lrc_contract_publishes_only_common_completion_metrics(self) -> None:
        contract = self.local_lrc_contract
        self.assertEqual(contract.gorti_transport, "local-lrc")
        self.assertEqual(contract.choreography, orchestrator.LOCAL_LRC_CHOREOGRAPHY)
        self.assertEqual(set(contract.metric_ids), orchestrator.PRIMARY_METRIC_IDS)

        comparison = fake_comparison()
        comparison["metadata"]["gorti_receive_order_transport"] = "local-lrc"
        comparison["workload"]["choreography"] = orchestrator.LOCAL_LRC_CHOREOGRAPHY
        comparison["workload"]["caller_latency_boundaries"]["gorti"] = (
            "gorti LocalLRC queue admission"
        )
        results = orchestrator.adapt_results(comparison, contract)
        self.assertEqual(
            {metric["id"] for metric in results["metrics"]},
            orchestrator.PRIMARY_METRIC_IDS,
        )
        self.assertTrue(
            all(
                set(run["measurements"]) == orchestrator.PRIMARY_METRIC_IDS
                for run in results["runs"]
            )
        )
        implementations = {item["id"]: item for item in results["implementations"]}
        self.assertEqual(
            implementations["gorti"]["label"],
            "gorti LocalLRC (queue 1024, ACK 32, batch 32)",
        )
        self.assertEqual(
            implementations["gorti"]["runtime"],
            "go version go1.26.5 build; ubuntu:24.04; linux/amd64 execution",
        )
        self.assertEqual(
            implementations["portico"]["runtime"],
            "Portico LRC; ubuntu:24.04; linux/amd64",
        )

    def test_load_contract_rejects_measurement_or_replacement_drift(self) -> None:
        mutations = {
            "operation_warmup_iterations_per_channel": 127,
            "choreography": "parallel",
            "callback_model": "queued",
            "time_management": "included",
            "timer_boundary_id": "different-boundary",
        }
        for key, value in mutations.items():
            with self.subTest(key=key), tempfile.TemporaryDirectory() as directory:
                document = copy.deepcopy(self.contract.experiment_document)
                document["experiment"]["measurement"][key] = value
                path = Path(directory) / "experiment.json"
                path.write_text(json.dumps(document), encoding="utf-8")
                with self.assertRaisesRegex(orchestrator.BenchmarkError, key):
                    orchestrator.load_contract(REPO, experiment_path=path)

        for key, value in (
            ("max_pair_attempts", 4),
            ("replacement_policy", "retry-anything"),
        ):
            with self.subTest(key=key), tempfile.TemporaryDirectory() as directory:
                document = copy.deepcopy(self.contract.experiment_document)
                document["experiment"]["design"][key] = value
                path = Path(directory) / "experiment.json"
                path.write_text(json.dumps(document), encoding="utf-8")
                with self.assertRaises(orchestrator.BenchmarkError):
                    orchestrator.load_contract(REPO, experiment_path=path)

    def test_fake_comparison_adapts_to_exact_common_schema(self) -> None:
        comparison = fake_comparison()
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "comparison.json"
            source.write_text(json.dumps(comparison), encoding="utf-8")
            loaded = json.loads(source.read_text(encoding="utf-8"))
        results = orchestrator.adapt_results(loaded, self.contract)
        validate, _, _ = orchestrator._load_common(REPO)
        validate(results)
        counts = Counter(run["implementation"] for run in results["runs"])
        self.assertEqual(counts, Counter({"portico": 30, "gorti": 30}))
        first = Counter(run["implementation"] for run in results["runs"] if run["position"] == 1)
        self.assertEqual(first, Counter({"portico": 15, "gorti": 15}))
        paired_seeds = {
            pair: {run["seed"] for run in results["runs"] if run["pair_index"] == pair}
            for pair in range(1, 31)
        }
        self.assertTrue(all(len(seeds) == 1 for seeds in paired_seeds.values()))
        self.assertEqual(len({next(iter(seeds)) for seeds in paired_seeds.values()}), 30)

    def test_rejects_missing_measured_run(self) -> None:
        comparison = fake_comparison()
        comparison["runs"].pop()
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "60 measured"):
            orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_duplicate_paired_seed(self) -> None:
        comparison = fake_comparison()
        for record in comparison["runs"]:
            if record["phase"] == "measured" and record["pair"] == 2:
                record["seed"] = 1516
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "expected"):
            orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_unbalanced_order(self) -> None:
        comparison = fake_comparison(first_schedule=["portico"] * 30)
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "alternating AB/BA"):
            orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_invalid_warmup_contract(self) -> None:
        comparison = fake_comparison()
        comparison["runs"][0]["implementation"] = "gorti"
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "warmup"):
            orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_failed_status_in_every_phase(self) -> None:
        for phase in ("warmup", "measured"):
            with self.subTest(phase=phase):
                comparison = fake_comparison()
                record = next(item for item in comparison["runs"] if item["phase"] == phase)
                record["status"] = "failed"
                with self.assertRaisesRegex(
                    orchestrator.BenchmarkError, rf"{phase} run .*status must be 'ok'"
                ):
                    orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_nonzero_participant_exit_code_in_every_phase(self) -> None:
        for phase in ("warmup", "measured"):
            with self.subTest(phase=phase):
                comparison = fake_comparison()
                record = next(item for item in comparison["runs"] if item["phase"] == phase)
                record["participant_exit_codes"]["subscriber"] = 1
                with self.assertRaisesRegex(
                    orchestrator.BenchmarkError,
                    rf"{phase} run .*participant_exit_codes.subscriber must be zero",
                ):
                    orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_unverified_logging_or_cleanup(self) -> None:
        for field in ("logging_disabled", "cleanup_verified"):
            with self.subTest(field=field):
                comparison = fake_comparison()
                comparison["runs"][0][field] = False
                with self.assertRaisesRegex(orchestrator.BenchmarkError, field):
                    orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_missing_profile_metadata(self) -> None:
        for value in (None, "event-journal"):
            with self.subTest(metadata_audit_replay_plugin=value):
                comparison = fake_comparison()
                if value is None:
                    comparison["metadata"].pop("gorti_audit_replay_plugin")
                else:
                    comparison["metadata"]["gorti_audit_replay_plugin"] = value
                with self.assertRaisesRegex(
                    orchestrator.BenchmarkError, "gorti_audit_replay_plugin"
                ):
                    orchestrator.adapt_results(comparison, self.contract)

        for implementation in ("portico", "gorti"):
            for field in ("profile_id", "runtime_profile"):
                with self.subTest(implementation=implementation, field=field):
                    comparison = fake_comparison()
                    record = next(
                        item
                        for item in comparison["runs"]
                        if item["implementation"] == implementation
                    )
                    record.pop(field)
                    with self.assertRaisesRegex(orchestrator.BenchmarkError, field):
                        orchestrator.adapt_results(comparison, self.contract)

        required_runtime_fields = {
            "portico": ("diagnostic_logging_level", "traffic_auditing"),
            "gorti": (
                "audit_replay_plugin",
                "event_journal_sink",
                "event_journal_persistence",
                "event_journal_replay_available",
                "event_journal_failure_boundary",
            ),
        }
        for implementation, fields in required_runtime_fields.items():
            for field in fields:
                with self.subTest(implementation=implementation, runtime_field=field):
                    comparison = fake_comparison()
                    record = next(
                        item
                        for item in comparison["runs"]
                        if item["implementation"] == implementation
                    )
                    record["runtime_profile"].pop(field)
                    with self.assertRaisesRegex(orchestrator.BenchmarkError, field):
                        orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_audit_profile_label_for_core_comparison(self) -> None:
        for location in ("run", "runtime_profile"):
            with self.subTest(location=location):
                comparison = fake_comparison()
                gorti = next(
                    item
                    for item in comparison["runs"]
                    if item["implementation"] == "gorti"
                )
                if location == "run":
                    gorti["profile_id"] = "gorti-audit-replay"
                else:
                    gorti["runtime_profile"]["profile_id"] = "gorti-audit-replay"
                with self.assertRaisesRegex(orchestrator.BenchmarkError, "profile_id"):
                    orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_runtime_profile_drift(self) -> None:
        gorti_mutations = {
            "audit_replay_plugin": "event-journal",
            "event_journal_sink": "file",
            "event_journal_persistence": True,
            "event_journal_replay_available": True,
            "event_journal_failure_boundary": True,
        }
        for field, value in gorti_mutations.items():
            with self.subTest(implementation="gorti", field=field):
                comparison = fake_comparison()
                gorti = next(
                    item
                    for item in comparison["runs"]
                    if item["implementation"] == "gorti"
                )
                gorti["runtime_profile"][field] = value
                with self.assertRaisesRegex(orchestrator.BenchmarkError, field):
                    orchestrator.adapt_results(comparison, self.contract)

        for field, value in (
            ("event_journal_persistence", 0),
            ("event_journal_failure_boundary", 1),
        ):
            with self.subTest(implementation="gorti", field=field, value=value):
                comparison = fake_comparison()
                gorti = next(
                    item
                    for item in comparison["runs"]
                    if item["implementation"] == "gorti"
                )
                gorti["runtime_profile"][field] = value
                with self.assertRaisesRegex(orchestrator.BenchmarkError, field):
                    orchestrator.adapt_results(comparison, self.contract)

        comparison = fake_comparison()
        portico = next(
            item
            for item in comparison["runs"]
            if item["implementation"] == "portico"
        )
        portico["runtime_profile"]["diagnostic_logging_level"] = "warn"
        with self.assertRaisesRegex(
            orchestrator.BenchmarkError, "diagnostic_logging_level"
        ):
            orchestrator.adapt_results(comparison, self.contract)

    def test_accepts_bounded_whole_pair_replacements(self) -> None:
        comparison = fake_comparison()
        for record in comparison["runs"]:
            if record["phase"] == "warmup" and record["pair"] == 2:
                record["pair_attempt"] = 2
            if record["phase"] == "measured" and record["pair"] == 3:
                record["pair_attempt"] = 3
        comparison["protocol"]["discarded_pair_attempts"] = 3
        results = orchestrator.adapt_results(comparison, self.contract)
        pair_three = [run for run in results["runs"] if run["pair_index"] == 3]
        self.assertTrue(all("attempt-3" in run["run_id"] for run in pair_three))

    def test_rejects_mismatched_or_out_of_range_pair_attempts(self) -> None:
        comparison = fake_comparison()
        pair = [
            record
            for record in comparison["runs"]
            if record["phase"] == "measured" and record["pair"] == 1
        ]
        pair[0]["pair_attempt"] = 2
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "pair attempts differ"):
            orchestrator.adapt_results(comparison, self.contract)

        for value in (0, 4, True):
            with self.subTest(value=value):
                comparison = fake_comparison()
                comparison["runs"][0]["pair_attempt"] = value
                with self.assertRaisesRegex(orchestrator.BenchmarkError, "pair_attempt"):
                    orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_retry_policy_or_discard_count_mismatch(self) -> None:
        comparison = fake_comparison()
        comparison["protocol"]["discarded_pair_attempts"] = 1
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "accepted pair-attempt count"):
            orchestrator.adapt_results(comparison, self.contract)

        for key, value in (
            ("max_pair_attempts", 2),
            ("replacement_policy", "different"),
        ):
            with self.subTest(key=key):
                comparison = fake_comparison()
                comparison["protocol"][key] = value
                with self.assertRaises(orchestrator.BenchmarkError):
                    orchestrator.adapt_results(comparison, self.contract)

        comparison = fake_comparison()
        comparison["protocol"]["portico_jgroups_response_timeout_ms"] = 1000
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "response timeout"):
            orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_missing_or_non_boolean_portico_publisher_ready_gate(self) -> None:
        for value in (None, False, 1):
            with self.subTest(value=value):
                comparison = fake_comparison()
                if value is None:
                    comparison["protocol"].pop("portico_publisher_ready_gate")
                else:
                    comparison["protocol"]["portico_publisher_ready_gate"] = value
                with self.assertRaisesRegex(
                    orchestrator.BenchmarkError, "portico_publisher_ready_gate"
                ):
                    orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_missing_or_non_boolean_portico_ordered_teardown_gate(self) -> None:
        for value in (None, False, 1):
            with self.subTest(value=value):
                comparison = fake_comparison()
                if value is None:
                    comparison["protocol"].pop("portico_ordered_teardown_gate")
                else:
                    comparison["protocol"]["portico_ordered_teardown_gate"] = value
                with self.assertRaisesRegex(
                    orchestrator.BenchmarkError, "portico_ordered_teardown_gate"
                ):
                    orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_measurement_contract_drift_in_comparator_output(self) -> None:
        mutations = {
            "operation_warmup": 127,
            "choreography": "parallel",
            "callback_model": "queued",
            "time_management": "included",
            "delivery_boundary": "different-boundary",
        }
        for key, value in mutations.items():
            with self.subTest(key=key):
                comparison = fake_comparison()
                comparison["workload"][key] = value
                with self.assertRaisesRegex(orchestrator.BenchmarkError, key):
                    orchestrator.adapt_results(comparison, self.contract)

    def test_requires_exact_gorti_server_lifecycle_evidence(self) -> None:
        comparison = fake_comparison()
        gorti = next(record for record in comparison["runs"] if record["implementation"] == "gorti")
        del gorti["server_lifecycle"]
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "server lifecycle"):
            orchestrator.adapt_results(comparison, self.contract)

        comparison = fake_comparison()
        gorti = next(record for record in comparison["runs"] if record["implementation"] == "gorti")
        gorti["server_lifecycle"]["cleanup_verified"] = False
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "cleanup_verified"):
            orchestrator.adapt_results(comparison, self.contract)

    def test_rejects_invalid_transport_override_digest(self) -> None:
        comparison = fake_comparison()
        comparison["metadata"]["transport_override_sha256"] = "not-a-digest"
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "transport override hash"):
            orchestrator.adapt_results(comparison, self.contract)

    def test_configured_source_revision_is_checked_directly(self) -> None:
        expected = next(
            item["source_revision"]
            for item in self.contract.experiment["implementations"]
            if item["id"] == "gorti"
        )
        orchestrator.require_configured_source_revision(
            self.contract,
            orchestrator.GitSourceState(expected, True, "a" * 64),
        )
        with self.assertRaisesRegex(orchestrator.BenchmarkError, "source_revision"):
            orchestrator.require_configured_source_revision(
                self.contract,
                orchestrator.GitSourceState("0" * 40, True, "a" * 64),
            )

    def test_transient_workspace_is_always_removed(self) -> None:
        transient = None
        with (
            self.assertRaisesRegex(RuntimeError, "stop"),
            orchestrator.transient_workspace(REPO) as path,
        ):
            transient = path
            (path / "transient.log").write_text("discard", encoding="utf-8")
            raise RuntimeError("stop")
        assert transient is not None
        self.assertFalse(transient.exists())

    def test_prebuilt_verifier_overrides_are_rejected_for_real_runs(self) -> None:
        stale = REPO / "verification" / "commercial-rti" / "build" / "stale.jar"
        cases = (
            (
                "command line",
                orchestrator._parser().parse_args(["--dry-run", "--verifier-jar", str(stale)]),
                {"PATH": ""},
            ),
            (
                "environment",
                orchestrator._parser().parse_args(["--dry-run"]),
                {"PATH": "", "GORTI_VERIFIER_JAR": str(stale)},
            ),
        )
        for label, args, environ in cases:
            with self.subTest(source=label):
                runtime = orchestrator.resolve_runtime_paths(
                    args,
                    self.contract,
                    environ=environ,
                    which=lambda _name: None,
                )
                report = orchestrator.preflight_report(self.contract, runtime)
                check = next(
                    item
                    for item in report["checks"]
                    if item["name"] == "prebuilt Java verifier override absent"
                )
                self.assertFalse(check["ok"])
                self.assertFalse(report["runtime_ready"])
                with self.assertRaisesRegex(
                    orchestrator.BenchmarkError,
                    "prebuilt Java verifier override absent",
                ):
                    orchestrator.require_runtime_ready(report)

    def test_prepare_verifier_builds_and_attests_current_inputs(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source_root = root / "src"
            source = source_root / orchestrator.VERIFIER_MAIN_SOURCE
            source.parent.mkdir(parents=True)
            original_source = b"package test; public class CurrentVerifier {}\n"
            source.write_bytes(original_source)
            api_jar = root / "portico.jar"
            with zipfile.ZipFile(api_jar, "w") as archive:
                archive.writestr(orchestrator.TRANSPORT_OVERRIDE_RESOURCE, b"<config />")
            runtime = orchestrator.RuntimePaths(
                output=None,
                portico_home=root,
                portico_jar=api_jar,
                portico_java=root / "java",
                fom=root / "fom.xml",
                verifier_jar=None,
                verifier_source_root=source_root,
                comparator=root / "compare.py",
                docker="docker",
                go="go",
                javac="javac",
                jar="jar",
            )
            contract = replace(self.contract, repo=root)
            calls: list[list[str]] = []

            def fake_run(command, *, cwd):
                self.assertEqual(cwd, root)
                calls.append(list(command))
                if command[0] == "jar":
                    Path(command[2]).write_bytes(b"deterministic-test-verifier")

            with mock.patch.object(orchestrator, "_run_inherited", fake_run):
                verifier = orchestrator.prepare_verifier(
                    contract,
                    runtime,
                    root / "transient",
                    compiler_version="javac 11.0.31",
                )

            self.assertEqual(len(calls), 2)
            compile_command = calls[0]
            self.assertEqual(compile_command[compile_command.index("-cp") + 1], str(api_jar))
            self.assertIn(str(source), compile_command)
            self.assertEqual(
                verifier.inputs.main_source_sha256,
                orchestrator.sha256_file(source),
            )
            self.assertEqual(
                verifier.inputs.portico_api_jar_sha256,
                orchestrator.sha256_file(api_jar),
            )
            self.assertRegex(verifier.inputs.build_input_sha256, r"^[0-9a-f]{64}$")
            self.assertEqual(
                verifier.inputs,
                orchestrator._verifier_build_inputs(runtime, "javac 11.0.31"),
            )

            source.write_bytes(original_source + b"// changed\n")
            changed_source = orchestrator._verifier_build_inputs(runtime, "javac 11.0.31")
            self.assertNotEqual(
                changed_source.build_input_sha256,
                verifier.inputs.build_input_sha256,
            )
            source.write_bytes(original_source)
            changed_compiler = orchestrator._verifier_build_inputs(runtime, "javac 11.0.32")
            self.assertNotEqual(
                changed_compiler.build_input_sha256,
                verifier.inputs.build_input_sha256,
            )
            api_jar.write_bytes(b"different-portico-api")
            changed_api = orchestrator._verifier_build_inputs(runtime, "javac 11.0.31")
            self.assertNotEqual(
                changed_api.build_input_sha256,
                verifier.inputs.build_input_sha256,
            )

    def test_prepare_verifier_rejects_prebuilt_artifact_directly(self) -> None:
        missing = REPO / ".missing-benchmark-runtime"
        runtime = orchestrator.RuntimePaths(
            output=None,
            portico_home=missing,
            portico_jar=missing / "portico.jar",
            portico_java=missing / "java",
            fom=missing / "fom.xml",
            verifier_jar=missing / "stale.jar",
            verifier_source_root=missing / "src",
            comparator=missing / "compare.py",
            docker="docker",
            go="go",
            javac="javac",
            jar="jar",
        )
        with self.assertRaisesRegex(
            orchestrator.BenchmarkError, "prebuilt Java verifier overrides are prohibited"
        ):
            orchestrator.prepare_verifier(
                self.contract,
                runtime,
                missing / "transient",
                compiler_version="javac 11.0.31",
            )

    def test_comparator_source_hash_must_equal_built_current_source(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source_root = root / "src"
            source = source_root / orchestrator.VERIFIER_MAIN_SOURCE
            source.parent.mkdir(parents=True)
            source.write_bytes(b"package test; public class CurrentVerifier {}\n")
            api_jar = root / "portico.jar"
            with zipfile.ZipFile(api_jar, "w") as archive:
                archive.writestr(orchestrator.TRANSPORT_OVERRIDE_RESOURCE, b"<config />")
            fom = root / "fom.xml"
            fom.write_bytes(b"<fom />")
            comparator = root / "compare.py"
            comparator.write_bytes(b"# comparator\n")
            runtime = orchestrator.RuntimePaths(
                output=None,
                portico_home=root,
                portico_jar=api_jar,
                portico_java=root / "java",
                fom=fom,
                verifier_jar=None,
                verifier_source_root=source_root,
                comparator=comparator,
                docker="docker",
                go="go",
                javac="javac",
                jar="jar",
            )

            def fake_run(command, *, cwd):
                if command[0] == "jar":
                    Path(command[2]).write_bytes(b"current-verifier")

            with mock.patch.object(orchestrator, "_run_inherited", fake_run):
                verifier = orchestrator.prepare_verifier(
                    self.contract,
                    runtime,
                    root / "transient",
                    compiler_version="javac 11.0.31",
                )

            comparison = fake_comparison()
            metadata = comparison["metadata"]
            metadata.update(
                {
                    "portico_jar_sha256": orchestrator.sha256_file(api_jar),
                    "verifier_jar_sha256": verifier.jar_sha256,
                    "comparison_harness_sha256": orchestrator.sha256_file(comparator),
                    "java_verifier_source_sha256": verifier.inputs.main_source_sha256,
                }
            )
            comparison["workload"]["fom_sha256"] = orchestrator.sha256_file(fom)
            revision = next(
                item["source_revision"]
                for item in self.contract.experiment["implementations"]
                if item["id"] == "gorti"
            )
            metadata["gorti_commit"] = revision
            source_state = orchestrator.GitSourceState(
                commit=revision,
                dirty=True,
                source_tree_sha256="a" * 64,
            )
            orchestrator.verify_runtime_evidence(
                self.contract,
                runtime,
                verifier,
                comparison,
                source_state,
                fake_runtime_probes(),
            )

            metadata["java_verifier_source_sha256"] = "0" * 64
            with self.assertRaisesRegex(orchestrator.BenchmarkError, "Java verifier source hash"):
                orchestrator.verify_runtime_evidence(
                    self.contract,
                    runtime,
                    verifier,
                    comparison,
                    source_state,
                    fake_runtime_probes(),
                )

    def test_publish_allows_only_four_atomic_artifacts(self) -> None:
        payloads = {name: f"{name}\n" for name in orchestrator.RESULT_NAMES}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo = root / "repo"
            repo.mkdir()
            output = root / "result"
            orchestrator.publish_artifacts(output, payloads, repo)
            self.assertEqual({path.name for path in output.iterdir()}, set(payloads))
            self.assertFalse(any(path.suffix == ".log" for path in output.iterdir()))
            with self.assertRaisesRegex(orchestrator.BenchmarkError, "exactly"):
                orchestrator.publish_artifacts(
                    root / "bad", {**payloads, "stdout.log": "bad"}, repo
                )

    def test_publish_rejects_output_inside_repository(self) -> None:
        payloads = {name: f"{name}\n" for name in orchestrator.RESULT_NAMES}
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory) / "repo"
            repo.mkdir()
            with self.assertRaisesRegex(orchestrator.BenchmarkError, "outside"):
                orchestrator.publish_artifacts(repo / "result", payloads, repo)

    def test_comparator_is_invoked_once_with_fixed_schedule(self) -> None:
        args = argparse.Namespace(operation_warmup=128, timeout_ms=30000)
        with tempfile.TemporaryDirectory() as directory:
            transient = Path(directory)
            runtime = orchestrator.RuntimePaths(
                output=None,
                portico_home=REPO / ".tools" / "portico",
                portico_jar=REPO / ".tools" / "portico" / "lib" / "portico.jar",
                portico_java=REPO / ".tools" / "portico" / "jre" / "bin" / "java",
                fom=REPO / "verification" / "commercial-rti" / "fom" / "CommercialRtiVerifier.xml",
                verifier_jar=None,
                verifier_source_root=REPO / "verification" / "commercial-rti" / "src",
                comparator=REPO / "verification" / "portico" / "compare_receive_order.py",
                docker="docker",
                go="go",
                javac="javac",
                jar="jar",
            )
            calls = []

            def fake_run(command, *, cwd):
                calls.append((command, cwd))
                output = Path(command[command.index("--output") + 1])
                output.mkdir(parents=True)
                (output / "comparison.json").write_text(
                    json.dumps(fake_comparison()), encoding="utf-8"
                )

            with mock.patch.object(orchestrator, "_run_inherited", fake_run):
                loaded = orchestrator.invoke_comparator(
                    args,
                    self.contract,
                    runtime,
                    transient / "verifier.jar",
                    transient,
                )
            self.assertEqual(len(calls), 1)
            command = calls[0][0]
            self.assertEqual(command[command.index("--warmup") + 1], "5")
            self.assertEqual(command[command.index("--measured") + 1], "30")
            self.assertEqual(command[command.index("--max-pair-attempts") + 1], "3")
            self.assertEqual(command[command.index("--operation-warmup") + 1], "128")
            self.assertEqual(
                command[command.index("--gorti-audit-replay-plugin") + 1],
                "none",
            )
            self.assertEqual(
                Path(command[command.index("--workload") + 1]),
                self.contract.workload_path,
            )
            self.assertEqual(loaded["schema"], "gorti.portico-receive-order-comparison/v2")

            args.operation_warmup = 127
            with self.assertRaisesRegex(orchestrator.BenchmarkError, "warm-up override"):
                orchestrator.invoke_comparator(
                    args,
                    self.contract,
                    runtime,
                    transient / "verifier.jar",
                    transient,
                )

    def test_analysis_and_latex_are_built_from_common_apis(self) -> None:
        comparison = fake_comparison()
        results = orchestrator.adapt_results(comparison, self.contract)
        comparison["protocol"]["failure_transcript"] = "must-not-be-published"
        payloads = orchestrator.build_artifact_payloads(
            results,
            self.contract,
            comparison,
            {
                "fom_path": "verification/commercial-rti/fom/CommercialRtiVerifier.xml",
                "versions": {
                    "host_os": "test-os",
                    "python": "test-python",
                    "go": "test-go",
                    "java_compiler": "test-javac",
                    "docker_server": "test-docker",
                    "container_image_reference": "ubuntu:24.04",
                    "container_image_reference_digest": PINNED_REFERENCE_DIGEST,
                    "container_platform": "linux/amd64",
                    "container_platform_image_id": PLATFORM_IMAGE_ID,
                },
                "worktree_dirty": True,
                "source_tree_sha256": "a" * 64,
                "java_verifier_build": fake_verifier_build_evidence(),
                "transport_override_build": fake_transport_override_build_evidence(),
            },
            bootstrap_resamples=100,
        )
        self.assertEqual(set(payloads), set(orchestrator.RESULT_NAMES))
        self.assertIn("gorti.benchmark.analysis/v1", payloads["analysis.json"])
        self.assertIn("\\begin{table*}", payloads["comparison.tex"])
        self.assertNotIn("__REQUIRED_", payloads["manifest.json"])
        manifest = json.loads(payloads["manifest.json"])
        self.assertEqual(
            manifest["resolved_inputs"]["java_verifier"]["build_input_sha256"],
            "b" * 64,
        )
        self.assertEqual(
            manifest["runtime"]["java_verifier_build"],
            fake_verifier_build_evidence(),
        )
        self.assertEqual(manifest["design"]["max_pair_attempts"], 3)
        self.assertEqual(manifest["design"]["warmup_pairs"], 5)
        self.assertEqual(manifest["design"]["measured_pairs"], 30)
        self.assertEqual(manifest["design"]["runs_per_implementation"], 30)
        self.assertEqual(manifest["design"]["order"], "paired-balanced-ab-ba-15-15")
        self.assertEqual(manifest["design"]["discarded_pair_attempts"], 0)
        self.assertEqual(
            manifest["design"]["replacement_policy"],
            orchestrator.PAIR_REPLACEMENT_POLICY,
        )
        self.assertEqual(
            manifest["design"]["measured_pair_seed_rule"],
            "base_seed + pair_index - 1",
        )
        self.assertEqual(
            manifest["design"]["warmup_pair_seed_rule"],
            "base_seed + measured_pair_count + pair_index - 1",
        )
        self.assertEqual(
            manifest["startup_controls"],
            {
                "portico_publisher_ready_gate": True,
                "portico_jgroups_response_timeout_ms": 5000,
                "measurement_boundary": "outside",
                "ready_marker_lifecycle": "removed-before-subscriber-launch",
                "ready_marker_retained": False,
                "ready_marker_is_log": False,
            },
        )
        self.assertEqual(
            manifest["teardown_controls"],
            {
                "portico_ordered_teardown_gate": True,
                "portico_jgroups_response_timeout_ms": 5000,
                "measurement_boundary": "outside",
                "handshake_kind": "three-phase-transient",
                "handshake_phases": [
                    "subscriber_resigned",
                    "publisher_resigned",
                    "subscriber_disconnected",
                ],
                "handshake_evidence_disposition": "discarded",
                "handshake_evidence_retained": False,
                "handshake_evidence_is_log": False,
            },
        )
        transport = manifest["resolved_inputs"]["portico_transport_override"]
        self.assertEqual(transport["sha256"], "7" * 64)
        self.assertEqual(transport["build_input"], fake_transport_override_build_evidence())
        self.assertNotIn("must-not-be-published", "".join(payloads.values()))

    def test_portico_teardown_contract_is_fixed(self) -> None:
        self.assertIs(orchestrator.PORTICO_ORDERED_TEARDOWN_GATE, True)
        self.assertEqual(orchestrator.PORTICO_JGROUPS_RESPONSE_TIMEOUT_MS, 5000)
        self.assertEqual(
            orchestrator.PORTICO_ORDERED_TEARDOWN_PHASES,
            (
                "subscriber_resigned",
                "publisher_resigned",
                "subscriber_disconnected",
            ),
        )

    def test_dry_run_succeeds_with_missing_tools_and_placeholders(self) -> None:
        completed = subprocess.run(
            [sys.executable, str(MODULE_PATH), "--dry-run"],
            cwd=REPO,
            text=True,
            encoding="utf-8",
            capture_output=True,
            check=False,
            env={"PATH": "", "SYSTEMROOT": str(Path("C:/Windows"))},
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)
        report = json.loads(completed.stdout)
        self.assertEqual(report["projection"]["count_per_channel"], 40600)
        self.assertFalse(report["runtime_ready"])

    def test_dry_run_rejects_operation_warmup_override(self) -> None:
        completed = subprocess.run(
            [
                sys.executable,
                str(MODULE_PATH),
                "--dry-run",
                "--operation-warmup",
                "127",
            ],
            cwd=REPO,
            text=True,
            encoding="utf-8",
            capture_output=True,
            check=False,
            env={"PATH": "", "SYSTEMROOT": str(Path("C:/Windows"))},
        )
        self.assertEqual(completed.returncode, 2)
        self.assertIn("tracked measurement contract", completed.stderr)

    def test_container_identity_and_platform_pins_are_runtime_required(self) -> None:
        prerequisites = copy.deepcopy(self.contract.prerequisites_document)
        prerequisites["container_pin"].update(
            {
                "image": "ubuntu:wrong",
                "platform": "windows/amd64",
                "digest": "not-a-digest",
            }
        )
        contract = replace(self.contract, prerequisites_document=prerequisites)
        missing = REPO / ".missing-benchmark-runtime"
        runtime = orchestrator.RuntimePaths(
            output=None,
            portico_home=missing,
            portico_jar=missing / "portico.jar",
            portico_java=missing / "java",
            fom=missing / "fom.xml",
            verifier_jar=None,
            verifier_source_root=missing / "src",
            comparator=missing / "compare.py",
            docker=None,
            go=None,
            javac=None,
            jar=None,
        )
        report = orchestrator.preflight_report(contract, runtime)
        checks = {item["name"]: item for item in report["checks"]}
        for name in (
            "container image reference",
            "container platform",
            "container image digest pin",
        ):
            with self.subTest(name=name):
                self.assertTrue(checks[name]["runtime_required"])
                self.assertFalse(checks[name]["ok"])
        with self.assertRaisesRegex(
            orchestrator.BenchmarkError,
            "container image reference:.*container platform:.*container image digest pin:",
        ):
            orchestrator.require_runtime_ready(report)


if __name__ == "__main__":
    unittest.main()
