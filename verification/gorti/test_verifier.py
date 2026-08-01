# This is a trusted, checked-in XML fixture and pytest assertions are intended.
# ruff: noqa: S101, S314

from __future__ import annotations

import inspect
import json
import threading
from pathlib import Path
from unittest.mock import Mock, patch
from xml.etree import ElementTree

import pytest

from verification.gorti.verifier import (
    INTEGER_CODEC,
    STRING_CODEC,
    ConsumerFederate,
    Observation,
    PerformanceLog,
    SemanticLog,
    VerificationRunner,
    _build_benchmark_run,
    deterministic_payload,
    write_ndjson,
)


def test_deterministic_payload_matches_shared_contract() -> None:
    assert deterministic_payload(20260712, "object", 0) == "436d115983667c79"
    assert deterministic_payload(20260712, "interaction", 7) == "cbb9ab761e57b35a"
    assert deterministic_payload(20260712, "object", 0) == deterministic_payload(
        20260712, "object", 0
    )
    assert deterministic_payload(20260712, "object", 0) != deterministic_payload(
        20260712, "interaction", 0
    )


def test_semantic_log_has_exact_stable_envelope(tmp_path: Path) -> None:
    log = SemanticLog()
    log.emit("OM", "sendInteraction", "producer", {"phase": "do", "index": 1})
    assert log.records == [
        {
            "kind": "semantic",
            "seq": 0,
            "service": "OM",
            "event": "sendInteraction",
            "actor": "producer",
            "data": {"phase": "do", "index": 1},
        }
    ]
    output = tmp_path / "semantic.ndjson"
    write_ndjson(output, log.records)
    line = output.read_text(encoding="utf-8")
    assert line.endswith("\n")
    assert json.loads(line) == log.records[0]
    assert "timestamp" not in line
    assert "handle" not in line


def test_performance_log_has_reference_rti_compatible_metric_envelope() -> None:
    performance = PerformanceLog()
    performance.add_milliseconds("OM", "sendInteraction", 2.0)
    performance.add_milliseconds("OM", "sendInteraction", 4.0)
    records = performance.records()
    assert records
    assert all(set(record) == {"kind", "service", "metric", "unit", "value"} for record in records)
    assert all(record["kind"] == "metric" for record in records)
    mean = next(record for record in records if record["metric"].endswith(".mean"))
    assert mean["value"] == 3.0


def test_performance_log_preserves_integer_nanosecond_samples() -> None:
    performance = PerformanceLog()
    with patch("verification.gorti.verifier.time.perf_counter_ns", side_effect=[100, 350]):
        performance.measure("OM", "sendInteraction", lambda: None)
    performance.add_milliseconds("OM", "receiveInteraction.latency", 1.25)
    performance.add_nanoseconds("TM", "timeAdvanceRequest", 375)

    assert [sample.to_dict() for sample in performance.raw_samples()] == [
        {
            "sequence": 0,
            "operation": "sendInteraction",
            "duration_ns": 250,
            "dimensions": {"service": "OM", "sample_kind": "call"},
        },
        {
            "sequence": 1,
            "operation": "receiveInteraction.latency",
            "duration_ns": 1_250_000,
            "dimensions": {"service": "OM", "sample_kind": "delivery"},
        },
        {
            "sequence": 2,
            "operation": "timeAdvanceRequest",
            "duration_ns": 375,
            "dimensions": {"service": "TM", "sample_kind": "call"},
        },
    ]


def test_benchmark_run_has_complete_delivery_accounting_and_provenance() -> None:
    runner = _runner()
    runner.iterations = 1
    observation = Observation(0, "payload", 1.0, 123)
    runner.consumer.reflections.append(observation)
    runner.consumer.interactions.append(observation)
    runner.performance.add_milliseconds("OM", "delivery", 1.0)
    provenance = {
        "captured_at_utc": "2026-07-12T00:00:00+00:00",
        "source": {"commit": "abc123", "branch": "perf/improvement-p0", "dirty": True},
        "server": {
            "sha256": "ab" * 32,
            "version": "rtid dev",
            "arguments": ["--listen=127.0.0.1:8442"],
        },
        "client": {"python_version": "Python 3.11"},
        "host": {"platform": "test", "logical_cpu_count": 1},
        "benchmark": {"seed": 1516, "count": 1, "logging_mode": "file"},
    }

    run = _build_benchmark_run(runner, provenance)

    assert run.delivery_accounting.to_dict() == {
        "expected_fanout": 2,
        "delivered": 2,
        "explicitly_rejected": 0,
        "dropped": 0,
    }
    assert run.metadata.binary_sha256 == "ab" * 32
    assert run.summaries[0].p99_ns == 1_000_000


def test_fom_declares_shared_workload_classes() -> None:
    fom = Path(__file__).with_name("federation.fom.xml")
    root = ElementTree.parse(fom).getroot()
    names = {element.text for element in root.iter() if element.tag.rsplit("}", 1)[-1] == "name"}
    assert {"HLAobjectRoot", "VerifierEntity", "Sequence", "Payload"} <= names
    assert {"HLAinteractionRoot", "VerifierMessage"} <= names
    text = fom.read_text(encoding="utf-8")
    assert "HLAinteger32BE" in text
    assert "HLAASCIIstring" in text
    assert text.count("<order>TimeStamp</order>") == 3
    assert "<time>" not in text


def test_callbacks_preserve_and_decode_tso_timestamp() -> None:
    consumer = ConsumerFederate()
    consumer.configure_handles(
        object_class=10,
        sequence_attribute=11,
        payload_attribute=12,
        sequence_parameter=21,
        payload_parameter=22,
    )
    consumer.reflectAttributeValues(
        100,
        {},
        3.0,
        attribute_values={
            11: INTEGER_CODEC.encode(2),
            12: STRING_CODEC.encode("0123456789abcdef"),
        },
    )
    consumer.receiveInteraction(
        "VerifierMessage",
        {
            "21": INTEGER_CODEC.encode(2),
            "22": STRING_CODEC.encode("fedcba9876543210"),
        },
        3.0,
    )
    snapshot = consumer.snapshot()
    assert snapshot[1][0].logical_time == 3.0
    assert snapshot[1][0].payload == "0123456789abcdef"
    assert snapshot[2][0].logical_time == 3.0
    assert snapshot[2][0].payload == "fedcba9876543210"


def test_runner_uses_real_fm_dm_om_tm_ambassador_calls() -> None:
    source = inspect.getsource(VerificationRunner)
    required = {
        "createFederationExecution",
        "joinFederationExecution",
        "resignFederationExecution",
        "publishObjectClassAttributes",
        "subscribeObjectClassAttributes",
        "publishInteractionClass",
        "subscribeInteractionClass",
        "registerObjectInstance",
        "updateAttributeValues",
        "sendInteraction",
        "enableTimeRegulation",
        "enableTimeConstrained",
        "timeAdvanceRequest",
        "queryLogicalTime",
        "registerFederationSynchronizationPoint",
        "synchronizationPointAchieved",
        "reserveObjectInstanceName",
        "deleteObjectInstance",
    }
    assert not {name for name in required if name not in source}


def _runner(*, timeout: float = 1.0) -> VerificationRunner:
    return VerificationRunner(
        url="unused",
        federation="unused",
        seed=0,
        iterations=0,
        timeout=timeout,
    )


def test_wait_until_wakes_on_immediate_callback_without_evoking() -> None:
    runner = _runner()
    runner.producer.evokeMultipleCallbacks = Mock(side_effect=AssertionError)
    runner.consumer.evokeMultipleCallbacks = Mock(side_effect=AssertionError)

    class CallbackDuringWait:
        def clear(self) -> None:
            pass

        def set(self) -> None:
            pass

        def wait(self, timeout: float) -> bool:
            assert timeout > 0
            runner.consumer.timeAdvanceGrant(1.0)
            return True

    signal = CallbackDuringWait()
    runner._callback_signal = signal
    runner.consumer._callback_signal = signal

    runner._wait_until(
        lambda: 1.0 in runner.consumer.snapshot()[3],
        "consumer grant",
    )

    runner.producer.evokeMultipleCallbacks.assert_not_called()
    runner.consumer.evokeMultipleCallbacks.assert_not_called()


def test_wait_until_does_not_lose_callback_during_clear() -> None:
    runner = _runner()

    class CallbackDuringClear:
        def clear(self) -> None:
            runner.producer.timeAdvanceGrant(2.0)

        def set(self) -> None:
            pass

        def wait(self, timeout: float) -> bool:
            raise AssertionError(f"unexpected wait for {timeout}")

    signal = CallbackDuringClear()
    runner._callback_signal = signal
    runner.producer._callback_signal = signal

    runner._wait_until(
        lambda: 2.0 in runner.producer.grant_snapshot(),
        "producer grant",
    )


def test_wait_until_uses_remaining_deadline_for_timeout() -> None:
    runner = _runner(timeout=1.0)
    signal = Mock()
    signal.wait.return_value = False
    runner._callback_signal = signal

    with (
        patch("verification.gorti.verifier.time.monotonic", side_effect=[10.0, 10.25, 11.0]),
        pytest.raises(TimeoutError, match="timed out waiting for missing callback"),
    ):
        runner._wait_until(lambda: False, "missing callback")

    signal.wait.assert_called_once_with(0.75)


def test_advance_both_submits_concurrently_with_deterministic_records() -> None:
    runner = _runner()
    both_submitted = threading.Barrier(2)

    def producer_request(logical_time: float) -> None:
        both_submitted.wait(timeout=1.0)
        runner.producer.timeAdvanceGrant(logical_time)

    def consumer_request(logical_time: float) -> None:
        both_submitted.wait(timeout=1.0)
        runner.consumer.timeAdvanceGrant(logical_time)

    runner.producer.timeAdvanceRequest = Mock(side_effect=producer_request)
    runner.consumer.timeAdvanceRequest = Mock(side_effect=consumer_request)

    runner._advance_both(3.0)

    tar_records = [
        record for record in runner.semantic.records if record["event"] == "timeAdvanceRequest"
    ]
    grant_records = [
        record for record in runner.semantic.records if record["event"] == "timeAdvanceGrant"
    ]
    assert [record["actor"] for record in tar_records] == ["producer", "consumer"]
    assert [record["actor"] for record in grant_records] == ["producer", "consumer"]
    assert [sample.operation for sample in runner.performance.raw_samples()] == [
        "timeAdvanceRequest",
        "timeAdvanceRequest",
    ]
    assert [sample.sequence for sample in runner.performance.raw_samples()] == [0, 1]
    assert all(sample.duration_ns >= 0 for sample in runner.performance.raw_samples())


def test_advance_both_records_both_timings_and_propagates_exception() -> None:
    runner = _runner()
    both_submitted = threading.Barrier(2)
    failure = RuntimeError("producer TAR failed")

    def producer_request(logical_time: float) -> None:
        both_submitted.wait(timeout=1.0)
        raise failure

    def consumer_request(logical_time: float) -> None:
        both_submitted.wait(timeout=1.0)

    runner.producer.timeAdvanceRequest = Mock(side_effect=producer_request)
    runner.consumer.timeAdvanceRequest = Mock(side_effect=consumer_request)

    with pytest.raises(RuntimeError, match="producer TAR failed") as raised:
        runner._advance_both(4.0)

    assert raised.value is failure
    runner.producer.timeAdvanceRequest.assert_called_once_with(4.0)
    runner.consumer.timeAdvanceRequest.assert_called_once_with(4.0)
    assert len(runner.performance.raw_samples()) == 2
    tar_records = [
        record for record in runner.semantic.records if record["event"] == "timeAdvanceRequest"
    ]
    assert [record["actor"] for record in tar_records] == ["consumer"]
    assert not any(record["event"] == "timeAdvanceGrant" for record in runner.semantic.records)
