"""Two-federate gorti implementation of the Pitch Evolved Chat contract."""

from __future__ import annotations

import sys
import threading
import time
import uuid
from collections.abc import Callable
from contextlib import suppress
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

_REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(_REPO / "pysdk"))

from rti1516e.encoding.string_codec import HLAunicodeString  # noqa: E402
from rti1516e.standard import Rti1516eAmbassador  # noqa: E402

FOM = str(Path(__file__).resolve().parent / "federation.fom.xml")
_STRING = HLAunicodeString()


@dataclass
class TraceRecorder:
    """Thread-safe, process-local trace of actions and callbacks."""

    _events: list[dict[str, Any]] = field(default_factory=list)
    _lock: threading.Lock = field(default_factory=threading.Lock)

    def record(self, event: str, actor: str, **details: Any) -> None:
        with self._lock:
            self._events.append(
                {
                    "sequence": len(self._events),
                    "event": event,
                    "actor": actor,
                    **details,
                }
            )

    def snapshot(self) -> list[dict[str, Any]]:
        with self._lock:
            return [dict(item) for item in self._events]


class ChatFederate(Rti1516eAmbassador):
    def __init__(self, actor: str, trace: TraceRecorder) -> None:
        super().__init__()
        self.actor = actor
        self.trace = trace
        self.name_reserved = threading.Event()
        self.message_received = threading.Event()
        self.participant_removed = threading.Event()
        self.participant_handle: int | None = None
        self.name_attribute_handle: int | None = None
        self.communication_handle: int | None = None
        self.message_parameter_handle: int | None = None
        self.sender_parameter_handle: int | None = None

    def objectInstanceNameReservationSucceeded(self, object_name: str) -> None:  # noqa: N802
        self.trace.record("NAME_RESERVED", self.actor, instance_name=object_name)
        self.name_reserved.set()

    def objectInstanceNameReservationFailed(self, object_name: str) -> None:  # noqa: N802
        self.trace.record("NAME_RESERVATION_FAILED", self.actor, instance_name=object_name)

    def discoverObjectInstance(  # noqa: N802
        self,
        object_handle: int,
        class_name: str,
        instance_name: str,
        object_class: Any | None = None,
    ) -> None:
        if object_class is not None and int(object_class) == self.participant_handle:
            class_name = "Participant"
        self.trace.record(
            "PARTICIPANT_DISCOVERED",
            self.actor,
            object_handle=int(object_handle),
            class_name=class_name,
            instance_name=instance_name,
        )

    def reflectAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None,
        attribute_values: dict[Any, bytes] | None = None,
    ) -> None:
        encoded_name = values.get("Name")
        if encoded_name is None and attribute_values is not None:
            encoded_name = attribute_values.get(self.name_attribute_handle)
        if encoded_name is None:
            return
        name, _ = _STRING.decode(bytes(encoded_name))
        self.trace.record(
            "PARTICIPANT_NAME_REFLECTED",
            self.actor,
            object_handle=int(object_handle),
            name=name,
            timestamp=timestamp,
        )

    def receiveInteraction(  # noqa: N802
        self,
        class_name: str,
        parameters: dict[str, Any],
        timestamp: float | None,
    ) -> None:
        if class_name not in {"Communication", str(self.communication_handle)}:
            return
        message_value = parameters.get("Message")
        if message_value is None:
            message_value = parameters[str(self.message_parameter_handle)]
        sender_value = parameters.get("Sender")
        if sender_value is None:
            sender_value = parameters[str(self.sender_parameter_handle)]
        message, _ = _STRING.decode(bytes(message_value))
        sender, _ = _STRING.decode(bytes(sender_value))
        self.trace.record(
            "MESSAGE_RECEIVED",
            self.actor,
            class_name=class_name,
            message=message,
            sender=sender,
            timestamp=timestamp,
        )
        self.message_received.set()

    def removeObjectInstance(  # noqa: N802
        self, object_handle: int, tag: bytes, timestamp: float | None
    ) -> None:
        self.trace.record(
            "PARTICIPANT_REMOVED",
            self.actor,
            object_handle=int(object_handle),
            tag=tag.hex(),
            timestamp=timestamp,
        )
        self.participant_removed.set()


def _wait(event: threading.Event, description: str, timeout: float) -> None:
    if not event.wait(timeout=timeout):
        raise TimeoutError(f"timed out waiting for {description}")


def _evoke_until(
    ambassador: ChatFederate,
    predicate: Callable[[], bool],
    description: str,
    timeout: float,
) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        ambassador.evokeMultipleCallbacks(0.02, 0.1)
        if predicate():
            return
    raise TimeoutError(f"timed out waiting for {description}")


def run_chat_scenario(
    url: str,
    *,
    federation_name: str | None = None,
    timeout: float = 10.0,
) -> list[dict[str, Any]]:
    """Run publisher and subscriber and return a canonical semantic trace."""

    federation = federation_name or f"chat-{uuid.uuid4().hex[:16]}"
    trace = TraceRecorder()
    publisher_joined = threading.Event()
    subscriber_subscribed = threading.Event()
    subscriber_resigned = threading.Event()
    publisher = ChatFederate("publisher", trace)
    subscriber = ChatFederate("subscriber", trace)
    errors: list[BaseException] = []

    def run_publisher() -> None:
        connected = False
        joined = False
        try:
            publisher.connect(publisher, url)
            connected = True
            publisher.createFederationExecution(federation, [FOM])
            publisher.joinFederationExecution("alice", federation)
            joined = True
            trace.record("PUB_JOINED", "publisher", federate_name="alice")

            participant = publisher.getObjectClassHandle("Participant")
            name_attribute = publisher.getAttributeHandle(participant, "Name")
            communication = publisher.getInteractionClassHandle("Communication")
            message_parameter = publisher.getParameterHandle(communication, "Message")
            sender_parameter = publisher.getParameterHandle(communication, "Sender")
            publisher.participant_handle = int(participant)
            publisher.name_attribute_handle = int(name_attribute)
            publisher.communication_handle = int(communication)
            publisher.message_parameter_handle = int(message_parameter)
            publisher.sender_parameter_handle = int(sender_parameter)
            publisher.publishObjectClassAttributes(participant, [name_attribute])
            publisher.publishInteractionClass(communication)
            publisher_joined.set()

            _wait(subscriber_subscribed, "subscriber declaration", timeout)
            trace.record(
                "NAME_RESERVATION_REQUESTED", "publisher", instance_name="alice"
            )
            publisher.reserveObjectInstanceName("alice")
            _evoke_until(
                publisher,
                publisher.name_reserved.is_set,
                "name reservation callback",
                timeout,
            )

            object_handle = publisher.registerObjectInstance(participant, "alice")
            trace.record(
                "PARTICIPANT_REGISTERED",
                "publisher",
                object_handle=int(object_handle),
                instance_name="alice",
            )

            trace.record("PARTICIPANT_NAME_UPDATED", "publisher", name="alice")
            publisher.updateAttributeValues(
                object_handle, {name_attribute: _STRING.encode("alice")}
            )

            trace.record(
                "MESSAGE_SENT", "publisher", message="hello", sender="alice"
            )
            publisher.sendInteraction(
                communication,
                {
                    message_parameter: _STRING.encode("hello"),
                    sender_parameter: _STRING.encode("alice"),
                },
            )

            _wait(subscriber.message_received, "Communication callback", timeout)
            trace.record(
                "PUB_RESIGN_DELETE_REQUESTED",
                "publisher",
                object_handle=int(object_handle),
                action="CANCEL_THEN_DELETE_THEN_DIVEST",
            )
            publisher.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")
            joined = False
            trace.record("PUB_RESIGNED", "publisher")
            _wait(subscriber.participant_removed, "remove callback", timeout)
            _wait(subscriber_resigned, "subscriber resign", timeout)

            publisher.destroyFederationExecution(federation)
        except BaseException as exc:  # noqa: BLE001
            errors.append(exc)
            publisher_joined.set()
            subscriber_subscribed.set()
            subscriber_resigned.set()
        finally:
            if joined:
                with suppress(BaseException):
                    publisher.resignFederationExecution()
            if connected:
                publisher.disconnect()

    def run_subscriber() -> None:
        connected = False
        joined = False
        try:
            _wait(publisher_joined, "publisher join", timeout)
            subscriber.connect(subscriber, url)
            connected = True
            subscriber.joinFederationExecution(
                "observer", federation, additional_fom_modules=[FOM]
            )
            joined = True
            trace.record("SUB_JOINED", "subscriber", federate_name="observer")

            participant = subscriber.getObjectClassHandle("Participant")
            name_attribute = subscriber.getAttributeHandle(participant, "Name")
            communication = subscriber.getInteractionClassHandle("Communication")
            subscriber.participant_handle = int(participant)
            subscriber.name_attribute_handle = int(name_attribute)
            subscriber.communication_handle = int(communication)
            subscriber.message_parameter_handle = int(
                subscriber.getParameterHandle(communication, "Message")
            )
            subscriber.sender_parameter_handle = int(
                subscriber.getParameterHandle(communication, "Sender")
            )
            subscriber.subscribeObjectClassAttributes(participant, [name_attribute])
            subscriber.subscribeInteractionClass(communication)
            trace.record("SUBSCRIBED", "subscriber")
            subscriber_subscribed.set()

            _evoke_until(
                subscriber,
                subscriber.participant_removed.is_set,
                "discover, reflect, interaction, and remove callbacks",
                timeout,
            )
            subscriber.resignFederationExecution()
            joined = False
            trace.record("SUB_RESIGNED", "subscriber")
            subscriber_resigned.set()
        except BaseException as exc:  # noqa: BLE001
            errors.append(exc)
            subscriber_subscribed.set()
            subscriber_resigned.set()
        finally:
            if joined:
                with suppress(BaseException):
                    subscriber.resignFederationExecution()
            if connected:
                subscriber.disconnect()

    publisher_thread = threading.Thread(target=run_publisher, daemon=True)
    subscriber_thread = threading.Thread(target=run_subscriber, daemon=True)
    publisher_thread.start()
    subscriber_thread.start()
    publisher_thread.join(timeout=timeout * 4)
    subscriber_thread.join(timeout=timeout * 4)

    if publisher_thread.is_alive() or subscriber_thread.is_alive():
        raise TimeoutError("Pitch Chat parity scenario did not terminate")
    if errors:
        raise RuntimeError(
            f"Pitch Chat parity scenario failed; trace={trace.snapshot()!r}"
        ) from errors[0]
    return trace.snapshot()


if __name__ == "__main__":
    import json

    endpoint = sys.argv[1] if len(sys.argv) > 1 else "grpc://127.0.0.1:8080"
    print(json.dumps(run_chat_scenario(endpoint), indent=2))
