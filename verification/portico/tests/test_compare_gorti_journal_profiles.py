from __future__ import annotations

import argparse
import importlib.util
import sys
from pathlib import Path
from types import SimpleNamespace
from typing import Any

import pytest

MODULE_PATH = Path(__file__).resolve().parents[1] / "compare_gorti_journal_profiles.py"
sys.path.insert(0, str(MODULE_PATH.parent))
SPEC = importlib.util.spec_from_file_location("gorti_journal_profiles", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


def test_schedule_has_disjoint_seeds_and_balanced_measured_ab_ba() -> None:
    schedule = MODULE.build_profile_schedule(1516, warmup_pairs=5, measured_pairs=30)
    warmup = [item for item in schedule if item["phase"] == "warmup"]
    measured = [item for item in schedule if item["phase"] == "measured"]

    assert [item["seed"] for item in measured] == list(range(1516, 1546))
    assert [item["seed"] for item in warmup] == list(range(1546, 1551))
    assert {item["seed"] for item in warmup}.isdisjoint(
        item["seed"] for item in measured
    )
    assert sum(item["order"][0] == MODULE.AUDIT_PROFILE_ID for item in measured) == 15
    assert sum(item["order"][0] == MODULE.CORE_PROFILE_ID for item in measured) == 15
    assert all(
        item["order"]
        == (
            (MODULE.AUDIT_PROFILE_ID, MODULE.CORE_PROFILE_ID)
            if index % 2 == 0
            else (MODULE.CORE_PROFILE_ID, MODULE.AUDIT_PROFILE_ID)
        )
        for index, item in enumerate(measured)
    )


def test_profile_pair_rejects_any_evidence_mismatch() -> None:
    matching = {
        MODULE.AUDIT_PROFILE_ID: {"evidence": {"trace": "same", "callbacks": 81200}},
        MODULE.CORE_PROFILE_ID: {"evidence": {"trace": "same", "callbacks": 81200}},
    }
    MODULE.assert_profile_pair_evidence("measured", 1, matching)

    mismatched = {
        **matching,
        MODULE.CORE_PROFILE_ID: {
            "evidence": {"trace": "different", "callbacks": 81200}
        },
    }
    with pytest.raises(ValueError, match="profile semantic evidence differs"):
        MODULE.assert_profile_pair_evidence("measured", 1, mismatched)


def test_paired_ratio_and_bootstrap_ci_are_deterministic() -> None:
    pairs = [
        {
            "pair": index,
            "seed": 1515 + index,
            "order": list(
                (MODULE.AUDIT_PROFILE_ID, MODULE.CORE_PROFILE_ID)
                if index % 2
                else (MODULE.CORE_PROFILE_ID, MODULE.AUDIT_PROFILE_ID)
            ),
            "batch_ns": {
                MODULE.AUDIT_PROFILE_ID: 100,
                MODULE.CORE_PROFILE_ID: off,
            },
        }
        for index, off in enumerate((90, 80, 100, 70), start=1)
    ]

    first = MODULE.analyze_measured_pairs(
        pairs,
        bootstrap_seed=1516,
        bootstrap_resamples=512,
    )
    second = MODULE.analyze_measured_pairs(
        pairs,
        bootstrap_seed=1516,
        bootstrap_resamples=512,
    )

    assert first == second
    paired = first["paired_core_over_audit"]
    assert paired["n_pairs"] == 4
    assert paired["median"] == pytest.approx(0.85)
    assert [row["core_over_audit_ratio"] for row in paired["pairs"]] == pytest.approx(
        [0.9, 0.8, 1.0, 0.7]
    )
    assert paired["bootstrap_ci95"]["low"] <= paired["median"]
    assert paired["bootstrap_ci95"]["high"] >= paired["median"]
    assert paired["bootstrap"]["seed"] == 1516
    assert paired["bootstrap"]["resampling_unit"] == "paired-seed"
    assert paired["bootstrap"]["stratification"] == "first-profile"
    assert [stratum["n_pairs"] for stratum in paired["order_strata"]] == [2, 2]


def test_runtime_profile_metadata_is_explicit_and_isolates_audit_plugin() -> None:
    definitions = {
        item["profile_id"]: item["runtime_profile"]
        for item in MODULE.profile_definitions(protobuf_validation=True)
    }
    audit = definitions[MODULE.AUDIT_PROFILE_ID]
    core = definitions[MODULE.CORE_PROFILE_ID]

    assert audit["schema"] == MODULE.receive.RUNTIME_PROFILE_SCHEMA
    assert audit["diagnostic_logging_level"] == core["diagnostic_logging_level"] == "error"
    assert audit["audit_replay_plugin"] == "event-journal"
    assert audit["event_journal_sink"] == "file"
    assert audit["event_journal_failure_boundary"] is False
    assert audit["protobuf_initialization_validation"] is True
    assert core["audit_replay_plugin"] == "none"
    assert core["event_journal_sink"] == "none"
    assert core["event_journal_failure_boundary"] is False
    assert core["protobuf_initialization_validation"] == "not_executed"
    assert audit["semantic_instrumentation"] == core["semantic_instrumentation"]
    assert audit["process_transcript_capture"] == "memory"
    assert audit["process_transcript_retention"] == "none"


def test_actual_runtime_profile_must_match_requested_profile() -> None:
    audit = MODULE.runtime_profile(MODULE.AUDIT_PROFILE_ID)
    MODULE.require_actual_runtime_profile(
        {"profile_id": MODULE.AUDIT_PROFILE_ID, "runtime_profile": audit},
        MODULE.AUDIT_PROFILE_ID,
        audit,
    )

    with pytest.raises(ValueError, match="profile_id"):
        MODULE.require_actual_runtime_profile(
            {"profile_id": MODULE.CORE_PROFILE_ID, "runtime_profile": audit},
            MODULE.AUDIT_PROFILE_ID,
            audit,
        )
    with pytest.raises(ValueError, match="runtime profile differs"):
        MODULE.require_actual_runtime_profile(
            {
                "profile_id": MODULE.AUDIT_PROFILE_ID,
                "runtime_profile": MODULE.runtime_profile(MODULE.CORE_PROFILE_ID),
            },
            MODULE.AUDIT_PROFILE_ID,
            audit,
        )


def test_cli_defaults_to_fixed_core_audit_profiles_and_existing_gorti_controls() -> None:
    parser = MODULE.build_parser()
    args = parser.parse_args(
        [
            "--repo",
            "repo",
            "--fom",
            "fom.xml",
            "--workload",
            "workload.json",
            "--output",
            "output",
        ]
    )

    assert args.seed == 1516
    assert args.operation_warmup == 128
    assert args.warmup == 5
    assert args.measured == 30
    assert args.max_pair_attempts == 3
    assert args.timeout_ms == 300000
    assert args.gorti_transport == "confirmed"
    assert args.local_lrc_queue == 1024
    assert args.local_lrc_ack_every == 32
    assert args.local_lrc_batch_size == 32
    assert args.gorti_callback_representation == "handles"
    assert args.gorti_outbox_event_capacity == 8192
    assert args.gorti_outbox_batch_size == 32
    assert args.gorti_outbox_flush_interval_ms == 1
    assert args.gorti_event_log_protobuf_validation is True
    assert not hasattr(args, "gorti_event_journal_mode")

    with pytest.raises(argparse.ArgumentTypeError):
        MODULE.receive.positive_int("0")
    with pytest.raises(SystemExit):
        parser.parse_args(
            [
                "--repo",
                "repo",
                "--fom",
                "fom.xml",
                "--workload",
                "workload.json",
                "--output",
                "output",
                "--measured",
                "5",
            ]
        )


def test_profile_retry_replays_the_complete_pair_and_cleans_each_network() -> None:
    created: list[str] = []
    removed: list[str] = []
    calls: list[tuple[int, str]] = []
    retry_ledger: list[dict[str, Any]] = []

    def create_network(attempt: int) -> str:
        network = f"network-{attempt}"
        created.append(network)
        return network

    def remove_network(network: str) -> None:
        removed.append(network)

    def run_profile(
        profile_id: str,
        _network: str,
        _position: int,
        attempt: int,
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        calls.append((attempt, profile_id))
        if attempt == 1 and profile_id == MODULE.CORE_PROFILE_ID:
            raise MODULE.receive.RetryableParticipantError("transient participant exit")
        result = {
            "profile_id": profile_id,
            "evidence": {"callback_trace": "same"},
        }
        return result, {"profile_id": profile_id, "pair_attempt": attempt}

    records, results, discarded = MODULE.run_profile_pair_with_retries(
        phase="measured",
        pair_index=1,
        order=(MODULE.AUDIT_PROFILE_ID, MODULE.CORE_PROFILE_ID),
        max_pair_attempts=2,
        create_network_for_attempt=create_network,
        remove_network_for_attempt=remove_network,
        run_profile=run_profile,
        retry_ledger=retry_ledger,
    )

    assert discarded == 1
    assert created == ["network-1", "network-2"]
    assert removed == created
    assert calls == [
        (1, MODULE.AUDIT_PROFILE_ID),
        (1, MODULE.CORE_PROFILE_ID),
        (2, MODULE.AUDIT_PROFILE_ID),
        (2, MODULE.CORE_PROFILE_ID),
    ]
    assert [record["pair_attempt"] for record in records] == [2, 2]
    assert set(results) == set(MODULE.PROFILE_IDS)
    assert retry_ledger == [
        {
            "phase": "measured",
            "pair": 1,
            "pair_attempt": 1,
            "implementation": MODULE.CORE_PROFILE_ID,
            "order": [MODULE.AUDIT_PROFILE_ID, MODULE.CORE_PROFILE_ID],
            "classification": "participant-exit",
        }
    ]


def test_prepare_binaries_invokes_only_two_go_builds(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    commands: list[list[str]] = []

    def fake_run_checked(
        command: list[str],
        *,
        cwd: Path,
        env: dict[str, str] | None = None,
    ) -> str:
        assert cwd == tmp_path
        assert env is not None
        commands.append(command)
        return ""

    monkeypatch.setattr(MODULE.receive, "run_checked", fake_run_checked)
    rtid, client = MODULE.prepare_binaries(
        SimpleNamespace(go="go"),
        tmp_path,
        tmp_path / "build",
    )

    assert rtid.name == "rtid-linux-amd64"
    assert client.name == "gorti-go-fair-linux-amd64"
    assert len(commands) == 2
    assert commands[0][-1] == "./rti/cmd/rtid"
    assert commands[1][-1] == "./verification/gorti-go-fair"
    assert all("portico" not in " ".join(command).lower() for command in commands)
