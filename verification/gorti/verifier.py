"""Deterministic two-federate verifier for gorti's Python HLA surface.

The semantic log deliberately excludes wall-clock values and runtime handles so
it can be byte-compared with a reference_rti run. Timing measurements are emitted to a
separate NDJSON stream with the same metric schema used by the reference_rti verifier.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import statistics
import sys
import threading
import time
from collections import defaultdict
from collections.abc import Callable, Mapping, Sequence
from concurrent.futures import Future, ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[2]
PYSDK = REPO_ROOT / "pysdk"
if str(PYSDK) not in sys.path:
    sys.path.insert(0, str(PYSDK))
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from rti1516e.encoding.integer import HLAinteger32BE  # noqa: E402, I001
from rti1516e.encoding.string_codec import HLAASCIIstring  # noqa: E402, I001
from rti1516e.standard import Rti1516eAmbassador  # noqa: E402, I001
from verification.common.perf_contract import (  # noqa: E402, I001
    BenchmarkRun,
    DeliveryAccounting,
    OperationSample,
    RunMetadata,
    dumps_benchmark,
)


FOM_PATH = Path(__file__).with_name("federation.fom.xml")
DEFAULT_FEDERATION = "GortiCommercialRtiVerifier-20260712"
DEFAULT_SEED = 20260712
DEFAULT_ITERATIONS = 100
DEFAULT_TIMEOUT = 15.0
DEFAULT_OBJECT_CLASS = "VerifierEntity"
DEFAULT_INTERACTION_CLASS = "VerifierMessage"
DEFAULT_OBJECT_NAME = "CommercialRtiVerifierEntity"
LOOKAHEAD = 1.0
READY_SYNC = "VERIFY_READY"
DONE_SYNC = "VERIFY_DONE"
PHASES = ("plan", "do", "review", "reflect")
SERVICES = ("FM", "DM", "OM", "TM")
INTEGER_CODEC = HLAinteger32BE()
STRING_CODEC = HLAASCIIstring()


def deterministic_payload(seed: int, channel: str, index: int) -> str:
    """Return the shared reference RTI/gorti fixed-seed payload representation."""

    source = f"{seed}:{channel}:{index}".encode()
    return hashlib.sha256(source).hexdigest()[:16]


def _json_line(record: Mapping[str, Any]) -> str:
    return json.dumps(
        record,
        ensure_ascii=True,
        allow_nan=False,
        separators=(",", ":"),
        sort_keys=True,
    )


def write_ndjson(path: Path, records: Sequence[Mapping[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    content = "".join(f"{_json_line(record)}\n" for record in records)
    path.write_text(content, encoding="utf-8", newline="\n")


class SemanticLog:
    """Canonical semantic event collector."""

    def __init__(self) -> None:
        self.records: list[dict[str, Any]] = []

    def emit(
        self,
        service: str,
        event: str,
        actor: str,
        data: Mapping[str, Any],
    ) -> None:
        if service not in SERVICES:
            raise ValueError(f"unknown service {service!r}")
        phase = data.get("phase")
        if phase not in PHASES:
            raise ValueError(f"unknown phase {phase!r}")
        self.records.append(
            {
                "kind": "semantic",
                "seq": len(self.records),
                "service": service,
                "event": event,
                "actor": actor,
                "data": dict(data),
            }
        )


class PerformanceLog:
    """Collect durations and render reference_rti-compatible aggregate metrics."""

    def __init__(self) -> None:
        self._samples: dict[tuple[str, str], list[float]] = defaultdict(list)
        self._raw_samples: list[OperationSample] = []
        self._direct: list[dict[str, Any]] = []

    def measure(self, service: str, metric: str, function: Callable[[], Any]) -> Any:
        started = time.perf_counter_ns()
        try:
            return function()
        finally:
            elapsed_ns = time.perf_counter_ns() - started
            self.add_nanoseconds(service, metric, elapsed_ns)

    def add_nanoseconds(self, service: str, metric: str, value: int) -> None:
        self._samples[(service, metric)].append(value / 1_000_000.0)
        self._record_raw(service, metric, value, "call")

    def add_milliseconds(self, service: str, metric: str, value: float) -> None:
        self._samples[(service, metric)].append(value)
        self._record_raw(service, metric, round(value * 1_000_000.0), "delivery")

    def _record_raw(self, service: str, metric: str, duration_ns: int, sample_kind: str) -> None:
        self._raw_samples.append(
            OperationSample(
                sequence=len(self._raw_samples),
                operation=metric,
                duration_ns=duration_ns,
                dimensions={"service": service, "sample_kind": sample_kind},
            )
        )

    def raw_samples(self) -> tuple[OperationSample, ...]:
        return tuple(self._raw_samples)

    def add_metric(self, service: str, metric: str, unit: str, value: float) -> None:
        self._direct.append(
            {
                "kind": "metric",
                "service": service,
                "metric": metric,
                "unit": unit,
                "value": round(value, 6),
            }
        )

    def records(self) -> list[dict[str, Any]]:
        records: list[dict[str, Any]] = []
        service_order = {service: index for index, service in enumerate(SERVICES)}
        ordered = sorted(
            self._samples.items(),
            key=lambda item: (service_order[item[0][0]], item[0][1]),
        )
        for (service, name), samples in ordered:
            values = sorted(samples)
            count = len(values)
            total = sum(values)
            aggregates = (
                (f"{name}.count", "count", count),
                (f"{name}.min", "milliseconds", values[0]),
                (f"{name}.mean", "milliseconds", statistics.fmean(values)),
                (f"{name}.p50", "milliseconds", _percentile(values, 50)),
                (f"{name}.p95", "milliseconds", _percentile(values, 95)),
                (f"{name}.max", "milliseconds", values[-1]),
                (
                    f"{name}.throughput",
                    "operations/second",
                    0.0 if total == 0 else count * 1000.0 / total,
                ),
            )
            for metric, unit, value in aggregates:
                rendered = value if isinstance(value, int) else round(value, 6)
                records.append(
                    {
                        "kind": "metric",
                        "service": service,
                        "metric": metric,
                        "unit": unit,
                        "value": rendered,
                    }
                )
        return records + list(self._direct)


def _percentile(sorted_values: Sequence[float], percentile: int) -> float:
    if not sorted_values:
        raise ValueError("percentile requires at least one value")
    rank = max(0, math.ceil(percentile / 100.0 * len(sorted_values)) - 1)
    return sorted_values[rank]


@dataclass(frozen=True)
class Observation:
    index: int
    payload: str
    logical_time: float | None
    received_ns: int


@dataclass(frozen=True)
class _TimedCallOutcome:
    elapsed_ns: int
    error: BaseException | None


@dataclass
class _PendingAsyncCall:
    event: str
    data: dict[str, Any]
    future: Future[Any]
    completed: threading.Event
    started_ns: int
    elapsed_ns: int = 0

    def mark_completed(self) -> None:
        self.elapsed_ns = time.perf_counter_ns() - self.started_ns
        self.completed.set()


class ProducerFederate(Rti1516eAmbassador):
    def __init__(self, callback_signal: threading.Event | None = None) -> None:
        super().__init__()
        self._lock = threading.Lock()
        self._callback_signal = callback_signal or threading.Event()
        self.grants: list[float] = []
        self.sync_announced: set[str] = set()
        self.sync_completed: set[str] = set()
        self.reservations_ok: set[str] = set()
        self.reservations_failed: set[str] = set()

    def timeAdvanceGrant(self, logical_time: float) -> None:  # noqa: N802
        with self._lock:
            self.grants.append(logical_time)
        self._callback_signal.set()

    def grant_snapshot(self) -> list[float]:
        with self._lock:
            return list(self.grants)

    def announceSynchronizationPoint(self, label: str, tag: bytes) -> None:  # noqa: N802
        del tag
        with self._lock:
            self.sync_announced.add(label)
        self._callback_signal.set()

    def federationSynchronized(  # noqa: N802
        self, label: str, failed_to_sync: tuple[int, ...] = ()
    ) -> None:
        if failed_to_sync:
            return
        with self._lock:
            self.sync_completed.add(label)
        self._callback_signal.set()

    def objectInstanceNameReservationSucceeded(self, object_name: str) -> None:  # noqa: N802
        with self._lock:
            self.reservations_ok.add(object_name)
        self._callback_signal.set()

    def objectInstanceNameReservationFailed(self, object_name: str) -> None:  # noqa: N802
        with self._lock:
            self.reservations_failed.add(object_name)
        self._callback_signal.set()

    def state_snapshot(
        self,
    ) -> tuple[list[float], set[str], set[str], set[str], set[str]]:
        with self._lock:
            return (
                list(self.grants),
                set(self.sync_announced),
                set(self.sync_completed),
                set(self.reservations_ok),
                set(self.reservations_failed),
            )


class ConsumerFederate(Rti1516eAmbassador):
    def __init__(self, callback_signal: threading.Event | None = None) -> None:
        super().__init__()
        self._lock = threading.Lock()
        self._callback_signal = callback_signal or threading.Event()
        self.sequence_attribute = 0
        self.payload_attribute = 0
        self.sequence_parameter = 0
        self.payload_parameter = 0
        self.expected_object_class = 0
        self.discoveries: list[tuple[int, int | None, str]] = []
        self.reflections: list[Observation] = []
        self.interactions: list[Observation] = []
        self.grants: list[float] = []
        self.removals: list[tuple[int, float | None]] = []
        self.sync_announced: set[str] = set()
        self.sync_completed: set[str] = set()

    def configure_handles(
        self,
        *,
        object_class: int,
        sequence_attribute: int,
        payload_attribute: int,
        sequence_parameter: int,
        payload_parameter: int,
    ) -> None:
        self.expected_object_class = int(object_class)
        self.sequence_attribute = int(sequence_attribute)
        self.payload_attribute = int(payload_attribute)
        self.sequence_parameter = int(sequence_parameter)
        self.payload_parameter = int(payload_parameter)

    def discoverObjectInstance(  # noqa: N802
        self,
        object_handle: int,
        class_name: str,
        instance_name: str,
        object_class: int | None = None,
    ) -> None:
        del class_name
        class_handle = None if object_class is None else int(object_class)
        with self._lock:
            self.discoveries.append((object_handle, class_handle, instance_name))
        self._callback_signal.set()

    def reflectAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None,
        attribute_values: Mapping[Any, Any] | None = None,
    ) -> None:
        del object_handle
        source = attribute_values if attribute_values is not None else values
        sequence = _lookup_value(source, self.sequence_attribute, "Sequence")
        payload = _lookup_value(source, self.payload_attribute, "Payload")
        observation = Observation(
            index=int(INTEGER_CODEC.decode(bytes(sequence))[0]),
            payload=str(STRING_CODEC.decode(bytes(payload))[0]),
            logical_time=timestamp,
            received_ns=time.perf_counter_ns(),
        )
        with self._lock:
            self.reflections.append(observation)
        self._callback_signal.set()

    def receiveInteraction(  # noqa: N802
        self,
        class_name: str,
        parameters: dict[str, Any],
        timestamp: float | None,
    ) -> None:
        del class_name
        sequence = _lookup_value(parameters, self.sequence_parameter, "Sequence")
        payload = _lookup_value(parameters, self.payload_parameter, "Payload")
        observation = Observation(
            index=int(INTEGER_CODEC.decode(bytes(sequence))[0]),
            payload=str(STRING_CODEC.decode(bytes(payload))[0]),
            logical_time=timestamp,
            received_ns=time.perf_counter_ns(),
        )
        with self._lock:
            self.interactions.append(observation)
        self._callback_signal.set()

    def timeAdvanceGrant(self, logical_time: float) -> None:  # noqa: N802
        with self._lock:
            self.grants.append(logical_time)
        self._callback_signal.set()

    def announceSynchronizationPoint(self, label: str, tag: bytes) -> None:  # noqa: N802
        del tag
        with self._lock:
            self.sync_announced.add(label)
        self._callback_signal.set()

    def federationSynchronized(  # noqa: N802
        self, label: str, failed_to_sync: tuple[int, ...] = ()
    ) -> None:
        if failed_to_sync:
            return
        with self._lock:
            self.sync_completed.add(label)
        self._callback_signal.set()

    def removeObjectInstance(  # noqa: N802
        self, object_handle: int, tag: bytes, timestamp: float | None
    ) -> None:
        del tag
        with self._lock:
            self.removals.append((object_handle, timestamp))
        self._callback_signal.set()

    def snapshot(
        self,
    ) -> tuple[
        list[tuple[int, int | None, str]],
        list[Observation],
        list[Observation],
        list[float],
        list[tuple[int, float | None]],
        set[str],
        set[str],
    ]:
        with self._lock:
            return (
                list(self.discoveries),
                list(self.reflections),
                list(self.interactions),
                list(self.grants),
                list(self.removals),
                set(self.sync_announced),
                set(self.sync_completed),
            )


def _lookup_value(values: Mapping[Any, Any], handle: int, name: str) -> Any:
    candidates: tuple[Any, ...] = (handle, str(handle), name)
    for candidate in candidates:
        if candidate in values:
            return values[candidate]
    for key, value in values.items():
        if str(key).rsplit(".", 1)[-1] == name:
            return value
    raise KeyError(f"callback omitted {name}")


class VerificationRunner:
    def __init__(
        self,
        *,
        url: str,
        federation: str,
        seed: int,
        iterations: int,
        timeout: float,
        tar_transport: str = "threaded",
        callback_transport: str = "queue",
        object_class: str = DEFAULT_OBJECT_CLASS,
        interaction_class: str = DEFAULT_INTERACTION_CLASS,
        object_name: str = DEFAULT_OBJECT_NAME,
    ) -> None:
        self.url = url
        self.federation = federation
        self.seed = seed
        self.iterations = iterations
        self.timeout = timeout
        self.object_class = object_class
        self.interaction_class = interaction_class
        self.object_name = object_name
        self.tar_transport = tar_transport
        self.callback_transport = callback_transport
        self.semantic = SemanticLog()
        self.performance = PerformanceLog()
        self._callback_signal = threading.Event()
        self.producer = ProducerFederate(self._callback_signal)
        self.consumer = ConsumerFederate(self._callback_signal)
        direct_callbacks = callback_transport == "direct"
        self.producer.setDirectCallbackDelivery(direct_callbacks)
        self.consumer.setDirectCallbackDelivery(direct_callbacks)
        self._tar_executor = (
            ThreadPoolExecutor(max_workers=2, thread_name_prefix="gorti-tar")
            if tar_transport == "threaded"
            else None
        )
        self._connected = {"producer": False, "consumer": False}
        self._joined = {"producer": False, "consumer": False}
        self._created = False
        self._errors: list[str] = []
        self._diagnostics: list[str] = []
        self._attribute_sent_ns: dict[int, int] = {}
        self._interaction_sent_ns: dict[int, int] = {}
        self._delivery_batch_started_ns: dict[int, int] = {}

    def run(self) -> bool:
        self._emit_plan()
        try:
            self._execute()
        except Exception as exc:  # The transcript must survive verification failures.
            self._errors.append(type(exc).__name__)
            self._diagnostics.append(f"{type(exc).__name__}: {exc}")
            self.semantic.emit(
                "FM",
                "verification_error",
                "verifier",
                {
                    "phase": "do",
                    "status": "error",
                    "error_type": type(exc).__name__,
                },
            )
        finally:
            self._cleanup()
        self._review()
        self._reflect()
        return not self._errors

    def _emit_plan(self) -> None:
        self.semantic.emit(
            "FM",
            "federation_plan",
            "verifier",
            {
                "phase": "plan",
                "federates": ["producer", "consumer"],
                "synchronization_points": [READY_SYNC, DONE_SYNC],
            },
        )
        self.semantic.emit(
            "DM",
            "declaration_plan",
            "verifier",
            {
                "phase": "plan",
                "object_class": self.object_class,
                "attributes": ["Sequence", "Payload"],
                "interaction_class": self.interaction_class,
                "parameters": ["Sequence", "Payload"],
            },
        )
        self.semantic.emit(
            "OM",
            "workload_plan",
            "verifier",
            {
                "phase": "plan",
                "iterations": self.iterations,
                "seed": self.seed,
                "payload_channels": ["attribute", "interaction"],
                "reserved_name": self.object_name,
            },
        )
        self.semantic.emit(
            "TM",
            "time_plan",
            "verifier",
            {
                "phase": "plan",
                "lookahead": LOOKAHEAD,
                "first_time": 1,
                "last_time": self.iterations + 1,
            },
        )

    def _execute(self) -> None:
        self._call(
            "FM",
            "connect",
            "producer",
            lambda: self.producer.connect(self.producer, self.url),
        )
        self._connected["producer"] = True
        self._call(
            "FM",
            "createFederationExecution",
            "producer",
            lambda: self.producer.createFederationExecution(self.federation, [str(FOM_PATH)]),
        )
        self._created = True
        self._call(
            "FM",
            "joinFederationExecution",
            "producer",
            lambda: self.producer.joinFederationExecution("producer", self.federation),
        )
        self._joined["producer"] = True

        self._call(
            "FM",
            "connect",
            "consumer",
            lambda: self.consumer.connect(self.consumer, self.url),
        )
        self._connected["consumer"] = True
        self._call(
            "FM",
            "joinFederationExecution",
            "consumer",
            lambda: self.consumer.joinFederationExecution(
                "consumer",
                self.federation,
                additional_fom_modules=[str(FOM_PATH)],
            ),
        )
        self._joined["consumer"] = True

        producer_handles = self._handles(self.producer)
        consumer_handles = self._handles(self.consumer)
        self.consumer.configure_handles(
            object_class=consumer_handles["object_class"],
            sequence_attribute=consumer_handles["sequence_attribute"],
            payload_attribute=consumer_handles["payload_attribute"],
            sequence_parameter=consumer_handles["sequence_parameter"],
            payload_parameter=consumer_handles["payload_parameter"],
        )

        self._call(
            "DM",
            "publishObjectClassAttributes",
            "producer",
            lambda: self.producer.publishObjectClassAttributes(
                producer_handles["object_class"],
                [
                    producer_handles["sequence_attribute"],
                    producer_handles["payload_attribute"],
                ],
            ),
            {"class": self.object_class, "attributes": ["Sequence", "Payload"]},
        )
        self._call(
            "DM",
            "publishInteractionClass",
            "producer",
            lambda: self.producer.publishInteractionClass(producer_handles["interaction_class"]),
            {"class": self.interaction_class},
        )
        self._call(
            "DM",
            "subscribeObjectClassAttributes",
            "consumer",
            lambda: self.consumer.subscribeObjectClassAttributes(
                consumer_handles["object_class"],
                [
                    consumer_handles["sequence_attribute"],
                    consumer_handles["payload_attribute"],
                ],
            ),
            {"class": self.object_class, "attributes": ["Sequence", "Payload"]},
        )
        self._call(
            "DM",
            "subscribeInteractionClass",
            "consumer",
            lambda: self.consumer.subscribeInteractionClass(consumer_handles["interaction_class"]),
            {"class": self.interaction_class},
        )

        for actor, federate in (
            ("producer", self.producer),
            ("consumer", self.consumer),
        ):
            self._call(
                "TM",
                "enableTimeRegulation",
                actor,
                lambda federate=federate: federate.enableTimeRegulation(LOOKAHEAD),
                {"lookahead": LOOKAHEAD},
            )
            self._call(
                "TM",
                "enableTimeConstrained",
                actor,
                federate.enableTimeConstrained,
            )

        self._synchronize(READY_SYNC)
        self._call(
            "OM",
            "reserveObjectInstanceName",
            "producer",
            lambda: self.producer.reserveObjectInstanceName(self.object_name),
            {"name": self.object_name},
        )
        self._wait_until(
            lambda: (
                self.object_name in self.producer.state_snapshot()[3]
                or self.object_name in self.producer.state_snapshot()[4]
            ),
            "object name reservation",
        )
        if self.object_name not in self.producer.state_snapshot()[3]:
            raise AssertionError("object name reservation failed")

        instance = self._call(
            "OM",
            "registerObjectInstance",
            "producer",
            lambda: self.producer.registerObjectInstance(
                producer_handles["object_class"], self.object_name
            ),
            {"class": self.object_class, "name": self.object_name},
        )
        self._wait_until(lambda: bool(self.consumer.snapshot()[0]), "object discovery")

        for index in range(self.iterations):
            logical_time = float(index + 1)
            sequence = INTEGER_CODEC.encode(index)
            attribute_payload = deterministic_payload(self.seed, "attribute", index)
            interaction_payload = deterministic_payload(self.seed, "interaction", index)
            attribute_values = {
                producer_handles["sequence_attribute"]: sequence,
                producer_handles["payload_attribute"]: STRING_CODEC.encode(
                    attribute_payload
                ),
            }
            interaction_parameters = {
                producer_handles["sequence_parameter"]: sequence,
                producer_handles["payload_parameter"]: STRING_CODEC.encode(
                    interaction_payload
                ),
            }

            attribute_data = {
                "index": index,
                "logical_time": int(logical_time),
                "payload": attribute_payload,
            }
            interaction_data = {
                "index": index,
                "logical_time": int(logical_time),
                "payload": interaction_payload,
            }
            self._attribute_sent_ns[index] = time.perf_counter_ns()
            attribute_call = self._submit_async_call(
                "updateAttributeValues",
                lambda attribute_values=attribute_values,
                logical_time=logical_time: self.producer.updateAttributeValuesAsync(
                    instance, attribute_values, timestamp=logical_time
                ),
                attribute_data,
            )
            self._interaction_sent_ns[index] = time.perf_counter_ns()
            interaction_call = self._submit_async_call(
                "sendInteraction",
                lambda interaction_parameters=interaction_parameters,
                logical_time=logical_time: self.producer.sendInteractionAsync(
                    producer_handles["interaction_class"],
                    interaction_parameters,
                    timestamp=logical_time,
                ),
                interaction_data,
            )
            self._finish_async_om_calls((attribute_call, interaction_call))
            self._delivery_batch_started_ns[index] = time.perf_counter_ns()
            self._advance_both(logical_time)
            self._wait_until(
                lambda index=index: (
                    any(item.index == index for item in self.consumer.snapshot()[1])
                    and any(item.index == index for item in self.consumer.snapshot()[2])
                ),
                f"TSO callbacks at logical time {int(logical_time)}",
            )

        removal_time = float(self.iterations + 1)
        self._call(
            "OM",
            "deleteObjectInstance",
            "producer",
            lambda: self.producer.deleteObjectInstance(
                instance, b"verification-complete", timestamp=removal_time
            ),
            {"name": self.object_name, "logical_time": int(removal_time)},
        )
        self._advance_both(removal_time)
        discovered_handle = self.consumer.snapshot()[0][0][0]
        self._wait_until(
            lambda: any(
                handle == discovered_handle and timestamp == removal_time
                for handle, timestamp in self.consumer.snapshot()[4]
            ),
            "timestamped object removal",
        )
        self._synchronize(DONE_SYNC)

        producer_time = self._call(
            "TM", "queryLogicalTime", "producer", self.producer.queryLogicalTime
        )
        consumer_time = self._call(
            "TM", "queryLogicalTime", "consumer", self.consumer.queryLogicalTime
        )
        if producer_time != removal_time or consumer_time != removal_time:
            raise AssertionError("final logical time did not reach removal time")

    def _synchronize(self, label: str) -> None:
        self._call(
            "FM",
            "registerFederationSynchronizationPoint",
            "producer",
            lambda: self.producer.registerFederationSynchronizationPoint(label, b"gorti-verifier"),
            {"label": label},
        )
        self._wait_until(
            lambda: (
                label in self.producer.state_snapshot()[1] and label in self.consumer.snapshot()[5]
            ),
            f"synchronization announcement {label}",
        )
        for actor in ("producer", "consumer"):
            self.semantic.emit(
                "FM",
                "announceSynchronizationPoint",
                actor,
                {"phase": "do", "label": label, "status": "ok"},
            )
        self._call(
            "FM",
            "synchronizationPointAchieved",
            "producer",
            lambda: self.producer.synchronizationPointAchieved(label),
            {"label": label},
        )
        self._call(
            "FM",
            "synchronizationPointAchieved",
            "consumer",
            lambda: self.consumer.synchronizationPointAchieved(label),
            {"label": label},
        )
        self._wait_until(
            lambda: (
                label in self.producer.state_snapshot()[2] and label in self.consumer.snapshot()[6]
            ),
            f"federation synchronized {label}",
        )
        for actor in ("producer", "consumer"):
            self.semantic.emit(
                "FM",
                "federationSynchronized",
                actor,
                {"phase": "do", "label": label, "status": "ok"},
            )

    def _advance_both(self, logical_time: float) -> None:
        if self.tar_transport == "threaded":
            outcomes = self._advance_both_threaded(logical_time)
        else:
            outcomes = self._advance_both_async(logical_time)
        calls = (
            ("producer", self.producer),
            ("consumer", self.consumer),
        )

        stable_data = {
            "phase": "do",
            "status": "ok",
            "logical_time": int(logical_time),
        }
        for (actor, _), outcome in zip(calls, outcomes, strict=True):
            self.performance.add_nanoseconds("TM", "timeAdvanceRequest", outcome.elapsed_ns)
            if outcome.error is None:
                self.semantic.emit("TM", "timeAdvanceRequest", actor, stable_data)

        for outcome in outcomes:
            if outcome.error is not None:
                raise outcome.error

        self._wait_until(
            lambda: (
                logical_time in self.producer.grant_snapshot()
                and logical_time in self.consumer.snapshot()[3]
            ),
            f"time advance grants {int(logical_time)}",
        )
        for actor in ("producer", "consumer"):
            self.semantic.emit(
                "TM",
                "timeAdvanceGrant",
                actor,
                {
                    "phase": "do",
                    "logical_time": int(logical_time),
                    "status": "ok",
                },
            )

    def _advance_both_threaded(self, logical_time: float) -> list[_TimedCallOutcome]:
        if self._tar_executor is None:
            raise RuntimeError("threaded TAR executor is not configured")
        calls = (self.producer, self.consumer)
        start = threading.Barrier(len(calls))
        futures = [
            self._tar_executor.submit(
                self._timed_time_advance_request, start, federate, logical_time
            )
            for federate in calls
        ]
        return [future.result() for future in futures]

    def _advance_both_async(self, logical_time: float) -> list[_TimedCallOutcome]:
        calls = (self.producer, self.consumer)
        pending: list[tuple[Rti1516eAmbassador, _PendingAsyncCall]] = []
        for federate in calls:
            pending.append(
                (
                    federate,
                    self._submit_async_call(
                        "timeAdvanceRequest",
                        lambda federate=federate: federate.timeAdvanceRequestAsync(
                            logical_time
                        ),
                        {"logical_time": int(logical_time)},
                    ),
                )
            )

        outcomes: list[_TimedCallOutcome] = []
        for federate, call in pending:
            if not call.completed.wait(timeout=self.timeout):
                outcomes.append(
                    _TimedCallOutcome(
                        call.elapsed_ns,
                        TimeoutError("timed out waiting for async timeAdvanceRequest"),
                    )
                )
                continue
            error: BaseException | None = None
            try:
                call.future.result()
            except BaseException as exc:
                error = exc
            try:
                federate.flushAsyncOperations()
            except BaseException as exc:
                if error is None:
                    error = exc
            outcomes.append(_TimedCallOutcome(call.elapsed_ns, error))
        return outcomes

    @staticmethod
    def _timed_time_advance_request(
        start: threading.Barrier,
        federate: Rti1516eAmbassador,
        logical_time: float,
    ) -> _TimedCallOutcome:
        start.wait()
        started = time.perf_counter_ns()
        try:
            federate.timeAdvanceRequest(logical_time)
        except BaseException as error:
            return _TimedCallOutcome(time.perf_counter_ns() - started, error)
        return _TimedCallOutcome(time.perf_counter_ns() - started, None)

    def _handles(self, federate: Rti1516eAmbassador) -> dict[str, int]:
        object_class = federate.getObjectClassHandle(self.object_class)
        interaction_class = federate.getInteractionClassHandle(self.interaction_class)
        return {
            "object_class": int(object_class),
            "sequence_attribute": int(federate.getAttributeHandle(object_class, "Sequence")),
            "payload_attribute": int(federate.getAttributeHandle(object_class, "Payload")),
            "interaction_class": int(interaction_class),
            "sequence_parameter": int(federate.getParameterHandle(interaction_class, "Sequence")),
            "payload_parameter": int(federate.getParameterHandle(interaction_class, "Payload")),
        }

    def _call(
        self,
        service: str,
        event: str,
        actor: str,
        function: Callable[[], Any],
        data: Mapping[str, Any] | None = None,
    ) -> Any:
        result = self.performance.measure(service, event, function)
        stable_data = {"phase": "do", "status": "ok"}
        if data:
            stable_data.update(data)
        self.semantic.emit(service, event, actor, stable_data)
        return result

    def _submit_async_call(
        self,
        event: str,
        submitter: Callable[[], Future[Any]],
        data: Mapping[str, Any],
    ) -> _PendingAsyncCall:
        started_ns = time.perf_counter_ns()
        future = submitter()
        pending = _PendingAsyncCall(
            event=event,
            data=dict(data),
            future=future,
            completed=threading.Event(),
            started_ns=started_ns,
        )
        future.add_done_callback(lambda _future: pending.mark_completed())
        return pending

    def _finish_async_om_calls(
        self, pending_calls: Sequence[_PendingAsyncCall]
    ) -> None:
        flush_error: BaseException | None = None
        try:
            self.producer.flushAsyncOperations()
        except BaseException as exc:
            flush_error = exc

        first_error: BaseException | None = None
        for pending in pending_calls:
            if not pending.completed.wait(timeout=self.timeout):
                raise TimeoutError(f"timed out waiting for async {pending.event}")
            self.performance.add_nanoseconds("OM", pending.event, pending.elapsed_ns)
            try:
                pending.future.result()
            except BaseException as exc:
                if first_error is None:
                    first_error = exc
                continue
            stable_data = {"phase": "do", "status": "ok", **pending.data}
            self.semantic.emit("OM", pending.event, "producer", stable_data)

        if first_error is not None:
            raise first_error
        if flush_error is not None:
            raise flush_error

    def _wait_until(self, predicate: Callable[[], bool], description: str) -> None:
        deadline = time.monotonic() + self.timeout
        while True:
            self._callback_signal.clear()
            if predicate():
                return
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError(f"timed out waiting for {description}")
            self._callback_signal.wait(remaining)

    def _cleanup(self) -> None:
        if self._joined["consumer"]:
            self._cleanup_call(
                "resignFederationExecution",
                "consumer",
                lambda: self.consumer.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST"),
            )
            self._joined["consumer"] = False
        if self._joined["producer"]:
            self._cleanup_call(
                "resignFederationExecution",
                "producer",
                lambda: self.producer.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST"),
            )
            self._joined["producer"] = False
        if self._created and self._connected["producer"]:
            self._cleanup_call(
                "destroyFederationExecution",
                "producer",
                lambda: self.producer.destroyFederationExecution(self.federation),
            )
            self._created = False
        for actor, federate in (
            ("consumer", self.consumer),
            ("producer", self.producer),
        ):
            if self._connected[actor]:
                self._cleanup_call("disconnect", actor, federate.disconnect)
                self._connected[actor] = False
        if self._tar_executor is not None:
            self._tar_executor.shutdown(wait=True)

    def _cleanup_call(
        self,
        event: str,
        actor: str,
        function: Callable[[], Any],
        data: Mapping[str, Any] | None = None,
    ) -> None:
        try:
            self._call("FM", event, actor, function, data)
        except Exception as exc:
            self._errors.append(f"cleanup:{event}:{type(exc).__name__}")
            self.semantic.emit(
                "FM",
                event,
                actor,
                {
                    "phase": "do",
                    "status": "error",
                    "error_type": type(exc).__name__,
                },
            )

    def _review(self) -> None:
        (
            discoveries,
            reflections,
            interactions,
            consumer_grants,
            removals,
            consumer_announced,
            consumer_synchronized,
        ) = self.consumer.snapshot()
        (
            producer_grants,
            producer_announced,
            producer_synchronized,
            reservations_ok,
            reservations_failed,
        ) = self.producer.state_snapshot()

        discovered_handle = next(
            (
                handle
                for handle, class_handle, name in discoveries
                if class_handle == self.consumer.expected_object_class and name == self.object_name
            ),
            None,
        )
        discovery_ok = discovered_handle is not None
        self._check(discovery_ok, "object discovery mismatch")
        self.semantic.emit(
            "OM",
            "discoverObjectInstance",
            "consumer",
            {
                "phase": "review",
                "class": self.object_class,
                "name": self.object_name,
                "status": "pass" if discovery_ok else "fail",
            },
        )

        reservation_ok = (
            self.object_name in reservations_ok and self.object_name not in reservations_failed
        )
        self._check(reservation_ok, "object name reservation mismatch")
        self.semantic.emit(
            "OM",
            "objectInstanceNameReservationSucceeded",
            "producer",
            {
                "phase": "review",
                "name": self.object_name,
                "status": "pass" if reservation_ok else "fail",
            },
        )

        reflection_map = {item.index: item for item in reflections}
        interaction_map = {item.index: item for item in interactions}
        first_sent_ns: int | None = None
        last_received_ns: int | None = None
        for index in range(self.iterations):
            logical_time = index + 1
            expected = deterministic_payload(self.seed, "attribute", index)
            actual = reflection_map.get(index)
            passed = (
                actual is not None
                and actual.payload == expected
                and actual.logical_time == logical_time
            )
            self._check(passed, f"object callback mismatch at index {index}")
            self.semantic.emit(
                "OM",
                "reflectAttributeValues",
                "consumer",
                {
                    "phase": "review",
                    "index": index,
                    "logical_time": logical_time,
                    "payload": None if actual is None else actual.payload,
                    "status": "pass" if passed else "fail",
                },
            )
            if actual is not None and index in self._attribute_sent_ns:
                self.performance.add_milliseconds(
                    "OM",
                    "reflectAttributeValues.latency",
                    (actual.received_ns - self._attribute_sent_ns[index]) / 1_000_000.0,
                )

            expected = deterministic_payload(self.seed, "interaction", index)
            actual = interaction_map.get(index)
            passed = (
                actual is not None
                and actual.payload == expected
                and actual.logical_time == logical_time
            )
            self._check(passed, f"interaction callback mismatch at index {index}")
            self.semantic.emit(
                "OM",
                "receiveInteraction",
                "consumer",
                {
                    "phase": "review",
                    "index": index,
                    "logical_time": logical_time,
                    "payload": None if actual is None else actual.payload,
                    "status": "pass" if passed else "fail",
                },
            )
            if actual is not None and index in self._interaction_sent_ns:
                self.performance.add_milliseconds(
                    "OM",
                    "receiveInteraction.latency",
                    (actual.received_ns - self._interaction_sent_ns[index]) / 1_000_000.0,
                )

            reflected = reflection_map.get(index)
            interacted = interaction_map.get(index)
            if reflected is not None and interacted is not None:
                send_started = min(
                    self._attribute_sent_ns[index], self._interaction_sent_ns[index]
                )
                batch_started = self._delivery_batch_started_ns[index]
                batch_completed = max(reflected.received_ns, interacted.received_ns)
                self.performance.add_milliseconds(
                    "OM",
                    "completed_delivery_batch_latency",
                    (batch_completed - batch_started) / 1_000_000.0,
                )
                self.performance.add_milliseconds(
                    "OM",
                    "send_to_delivery_batch_latency",
                    (batch_completed - send_started) / 1_000_000.0,
                )
                first_sent_ns = (
                    batch_started if first_sent_ns is None else min(first_sent_ns, batch_started)
                )
                last_received_ns = (
                    batch_completed
                    if last_received_ns is None
                    else max(last_received_ns, batch_completed)
                )

        if first_sent_ns is not None and last_received_ns is not None:
            elapsed_seconds = max(1, last_received_ns - first_sent_ns) / 1_000_000_000.0
            self.performance.add_metric(
                "OM",
                "sustained_throughput",
                "deliveries_per_second",
                self.iterations * 2.0 / elapsed_seconds,
            )

        removal_time = self.iterations + 1
        removal_ok = discovered_handle is not None and any(
            handle == discovered_handle and timestamp == removal_time
            for handle, timestamp in removals
        )
        self._check(removal_ok, "object removal mismatch")
        self.semantic.emit(
            "OM",
            "removeObjectInstance",
            "consumer",
            {
                "phase": "review",
                "class": self.object_class,
                "name": self.object_name,
                "logical_time": removal_time,
                "status": "pass" if removal_ok else "fail",
            },
        )

        expected_grants = {float(value) for value in range(1, removal_time + 1)}
        grants_ok = expected_grants <= set(producer_grants) and expected_grants <= set(
            consumer_grants
        )
        self._check(grants_ok, "time advance grant mismatch")
        for actor, grants in (
            ("producer", producer_grants),
            ("consumer", consumer_grants),
        ):
            self.semantic.emit(
                "TM",
                "timeAdvanceGrant",
                actor,
                {
                    "phase": "review",
                    "logical_time": removal_time,
                    "status": "pass" if expected_grants <= set(grants) else "fail",
                },
            )

        sync_ok = all(
            label in producer_announced
            and label in consumer_announced
            and label in producer_synchronized
            and label in consumer_synchronized
            for label in (READY_SYNC, DONE_SYNC)
        )
        self._check(sync_ok, "federation synchronization mismatch")
        self.semantic.emit(
            "FM",
            "synchronization_review",
            "verifier",
            {
                "phase": "review",
                "labels": [READY_SYNC, DONE_SYNC],
                "status": "pass" if sync_ok else "fail",
            },
        )

        self.semantic.emit(
            "FM",
            "lifecycle_review",
            "verifier",
            {
                "phase": "review",
                "status": "pass" if not self._errors else "fail",
            },
        )
        self.semantic.emit(
            "DM",
            "declaration_review",
            "verifier",
            {
                "phase": "review",
                "status": "pass" if not self._errors else "fail",
            },
        )

    def _check(self, condition: bool, message: str) -> None:
        if not condition:
            self._errors.append(message)

    def _reflect(self) -> None:
        status = "pass" if not self._errors else "fail"
        for service in SERVICES:
            self.semantic.emit(
                service,
                "service_result",
                "verifier",
                {"phase": "reflect", "status": status},
            )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="grpc://127.0.0.1:8442")
    parser.add_argument("--federation", default=DEFAULT_FEDERATION)
    parser.add_argument("--seed", type=int, default=DEFAULT_SEED)
    parser.add_argument(
        "--count", "--iterations", dest="iterations", type=int, default=DEFAULT_ITERATIONS
    )
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT)
    parser.add_argument(
        "--tar-transport", choices=("threaded", "async"), default="threaded"
    )
    parser.add_argument(
        "--callback-transport", choices=("queue", "direct"), default="queue"
    )
    parser.add_argument(
        "--object-class", default=DEFAULT_OBJECT_CLASS, help="federation object class under test"
    )
    parser.add_argument(
        "--interaction-class",
        default=DEFAULT_INTERACTION_CLASS,
        help="federation interaction class under test",
    )
    parser.add_argument(
        "--object-name",
        default=DEFAULT_OBJECT_NAME,
        help="object instance name reserved and registered by producer",
    )
    parser.add_argument("--semantic-log", type=Path, default=Path("canonical.ndjson"))
    parser.add_argument("--performance-log", type=Path, default=Path("metrics.ndjson"))
    parser.add_argument("--provenance-log", type=Path)
    parser.add_argument("--benchmark-log", type=Path)
    return parser


def _build_benchmark_run(runner: VerificationRunner, provenance: Mapping[str, Any]) -> BenchmarkRun:
    source = provenance["source"]
    server = provenance["server"]
    client = provenance["client"]
    benchmark = provenance["benchmark"]
    host = provenance["host"]
    _, reflections, interactions, *_ = runner.consumer.snapshot()
    expected = runner.iterations * 2
    delivered = len(reflections) + len(interactions)
    dropped = 0 if delivered > expected else expected - delivered
    metadata = RunMetadata(
        run_id=f"gorti-{runner.seed}-{provenance['captured_at_utc']}",
        benchmark="gorti-tso-lockstep",
        started_at=str(provenance["captured_at_utc"]),
        commit=str(source["commit"]),
        binary_sha256=str(server["sha256"]),
        runtime_versions={
            "rtid": str(server["version"]),
            "python": str(client["python_version"]),
        },
        build_flags=tuple(str(item) for item in server["arguments"]),
        environment={
            "host": dict(host),
            "branch": str(source["branch"]),
            "dirty": bool(source["dirty"]),
            "logging_mode": str(benchmark["logging_mode"]),
        },
        workload=dict(benchmark),
    )
    return BenchmarkRun(
        metadata=metadata,
        samples=runner.performance.raw_samples(),
        delivery_accounting=DeliveryAccounting(
            expected_fanout=expected,
            delivered=delivered,
            explicitly_rejected=0,
            dropped=dropped,
        ),
    )


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.iterations < 1:
        raise SystemExit("--count must be at least 1")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be positive")

    runner = VerificationRunner(
        url=args.url,
        federation=args.federation,
        seed=args.seed,
        iterations=args.iterations,
        timeout=args.timeout,
        tar_transport=args.tar_transport,
        callback_transport=args.callback_transport,
        object_class=args.object_class,
        interaction_class=args.interaction_class,
        object_name=args.object_name,
    )
    passed = runner.run()
    write_ndjson(args.semantic_log, runner.semantic.records)
    write_ndjson(args.performance_log, runner.performance.records())
    if args.benchmark_log is not None:
        if args.provenance_log is None:
            raise SystemExit("--benchmark-log requires --provenance-log")
        provenance = json.loads(args.provenance_log.read_text(encoding="utf-8"))
        benchmark_run = _build_benchmark_run(runner, provenance)
        args.benchmark_log.parent.mkdir(parents=True, exist_ok=True)
        args.benchmark_log.write_text(
            dumps_benchmark(benchmark_run), encoding="utf-8", newline="\n"
        )
    print(
        f"gorti verifier: {'PASS' if passed else 'FAIL'}; "
        f"semantic={args.semantic_log}; performance={args.performance_log}",
        flush=True,
    )
    if not passed:
        print("failures: " + ", ".join(runner._errors), file=sys.stderr, flush=True)
        for diagnostic in runner._diagnostics:
            print("detail: " + diagnostic, file=sys.stderr, flush=True)
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
