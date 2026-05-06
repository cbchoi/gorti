"""Unit tests for the bridge's OBJECT-CLASS extension to ``HLAFederate``.

Covers the ``ObjectClassFederateProtocol`` opt-in surface introduced
to extend the bridge beyond interactions. Each test pins one
behavioral guarantee:

* publish/subscribe RPCs fire exactly once on first ``step_once`` /
  ``run`` for each declared class,
* ``register_instances`` translates to ``register_object_instance``
  RPCs and the resulting handles are stored under the local name,
* ``attribute_update_handler`` per cycle drives ``update_attributes``
  RPCs in deterministic instance + attribute order,
* unknown instance local-names are silently dropped,
* ``DiscoverObjectInstance`` events dispatch to ``discover_handler``
  only for subscribed classes,
* ``ReflectAttributeValues`` events dispatch to ``reflect_handler``
  with the full attribute dict,
* a model that implements NO object-class methods stays
  interaction-only (back-compat),
* the runtime ``register_instance`` hook works for late registrations.

Run with::

    python3 -m pytest pysdk/tests/test_object_class_bridge.py -v
"""

from __future__ import annotations

from collections import deque
from dataclasses import dataclass, field
from typing import Any

import pytest

from pyjevsim_bridge import HLAFederate, PortMapping
from rti1516e.connection import FederationSpec
from rti1516e.events import DiscoverObjectInstance, ReflectAttributeValues
from tests.spec.m4._fakes import FakeRtiServer


@dataclass
class _ObjectModel:
    """Bare-minimum object-class model wired ad-hoc per test.

    Each test instantiates one and pre-loads ``ta`` / ``outputs`` /
    ``updates`` so the bridge runs exactly the right number of
    cycles without lookups in test bodies.
    """

    ta_schedule: deque[float] = field(default_factory=deque)
    output_schedule: deque[dict[str, Any]] = field(default_factory=deque)
    update_schedule: deque[dict[str, dict[str, Any]]] = field(default_factory=deque)
    publications: dict[str, list[str]] = field(default_factory=dict)
    subscriptions: dict[str, list[str]] = field(default_factory=dict)
    instances: dict[str, str] = field(default_factory=dict)
    discovered: list[tuple[int, str, str]] = field(default_factory=list)
    reflects: list[tuple[int, dict[str, Any]]] = field(default_factory=list)
    internal_count: int = 0
    external_calls: list[tuple[str, Any]] = field(default_factory=list)

    def time_advance(self) -> float:
        if not self.ta_schedule:
            return 1.0
        return self.ta_schedule.popleft()

    def output_handler(self) -> dict[str, Any]:
        if not self.output_schedule:
            return {}
        return self.output_schedule.popleft()

    def internal_transition(self) -> None:
        self.internal_count += 1

    def external_transition(self, port: str, payload: Any) -> None:
        self.external_calls.append((port, payload))

    # --- ObjectClassFederateProtocol opt-ins ---

    def object_class_publications(self) -> dict[str, list[str]]:
        return dict(self.publications)

    def object_class_subscriptions(self) -> dict[str, list[str]]:
        return dict(self.subscriptions)

    def register_instances(self) -> dict[str, str]:
        return dict(self.instances)

    def attribute_update_handler(self) -> dict[str, dict[str, Any]]:
        if not self.update_schedule:
            return {}
        return self.update_schedule.popleft()

    def discover_handler(
        self, instance_handle: int, class_name: str, instance_name: str
    ) -> None:
        self.discovered.append((instance_handle, class_name, instance_name))

    def reflect_handler(
        self, instance_handle: int, attrs: dict[str, Any]
    ) -> None:
        self.reflects.append((instance_handle, dict(attrs)))


def _make_federate(
    model: Any,
    mapping: dict[str, str] | None = None,
    *,
    name: str = "alice",
) -> HLAFederate:
    return HLAFederate(
        coupled_model=model,
        federation=FederationSpec(name="demo"),
        federate_name=name,
        port_mapping=PortMapping.from_dict(mapping or {}),
        url="memory://fake-rti",
    )


@pytest.mark.asyncio
async def test_object_class_publications_emit_once_at_startup() -> None:
    fake = FakeRtiServer()
    model = _ObjectModel(
        ta_schedule=deque([1.0, 1.0]),
        publications={"Vehicle": ["pos", "vel"]},
    )
    fed = _make_federate(model)
    await fed.step_once()
    await fed.step_once()
    pubs = fake.calls_for("publish_object_class")
    assert len(pubs) == 1
    assert pubs[0].args["class_name"] == "Vehicle"
    assert pubs[0].args["attributes"] == ["pos", "vel"]
    await fed.aclose()


@pytest.mark.asyncio
async def test_object_class_subscriptions_emit_once_at_startup() -> None:
    fake = FakeRtiServer()
    model = _ObjectModel(
        ta_schedule=deque([1.0, 1.0]),
        subscriptions={"Vehicle": ["pos"], "Sensor": ["reading"]},
    )
    fed = _make_federate(model)
    await fed.step_once()
    await fed.step_once()
    subs = fake.calls_for("subscribe_object_class")
    # Sorted-name iteration: "Sensor" comes before "Vehicle".
    assert [c.args["class_name"] for c in subs] == ["Sensor", "Vehicle"]
    await fed.aclose()


@pytest.mark.asyncio
async def test_register_instances_records_handles() -> None:
    fake = FakeRtiServer()
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        instances={"sensor-1": "Sensor", "sensor-2": "Sensor"},
    )
    fed = _make_federate(model)
    await fed.step_once()
    regs = fake.calls_for("register_object_instance")
    # Sorted-name iteration.
    assert [c.args["instance_name"] for c in regs] == ["sensor-1", "sensor-2"]
    # Internal map captured the allocator's monotonic handles.
    assert set(fed._instance_handles) == {"sensor-1", "sensor-2"}  # noqa: SLF001
    assert all(isinstance(h, int) for h in fed._instance_handles.values())  # noqa: SLF001
    await fed.aclose()


@pytest.mark.asyncio
async def test_attribute_update_handler_drives_update_attributes() -> None:
    fake = FakeRtiServer()
    model = _ObjectModel(
        ta_schedule=deque([1.0, 1.0]),
        instances={"sensor-1": "Sensor"},
        update_schedule=deque(
            [
                {"sensor-1": {"value": b"\x00\x00\x00\x07"}},
                {"sensor-1": {"value": b"\x00\x00\x00\x08"}},
            ]
        ),
    )
    fed = _make_federate(model)
    await fed.step_once()
    await fed.step_once()
    updates = fake.calls_for("update_attributes")
    assert len(updates) == 2
    handle = fed._instance_handles["sensor-1"]  # noqa: SLF001
    for u in updates:
        assert u.args["object_handle"] == handle
    assert updates[0].args["values"] == {"value": b"\x00\x00\x00\x07"}
    assert updates[1].args["values"] == {"value": b"\x00\x00\x00\x08"}
    await fed.aclose()


@pytest.mark.asyncio
async def test_attribute_update_unknown_instance_is_dropped() -> None:
    fake = FakeRtiServer()
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        instances={"sensor-1": "Sensor"},
        update_schedule=deque([{"unknown": {"v": b"x"}, "sensor-1": {"v": b"y"}}]),
    )
    fed = _make_federate(model)
    await fed.step_once()
    updates = fake.calls_for("update_attributes")
    assert len(updates) == 1
    assert updates[0].args["values"] == {"v": b"y"}
    await fed.aclose()


@pytest.mark.asyncio
async def test_attribute_update_attribute_keys_are_sorted() -> None:
    fake = FakeRtiServer()
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        instances={"sensor-1": "Sensor"},
        update_schedule=deque(
            [{"sensor-1": {"zeta": b"3", "alpha": b"1", "mu": b"2"}}]
        ),
    )
    fed = _make_federate(model)
    await fed.step_once()
    updates = fake.calls_for("update_attributes")
    assert len(updates) == 1
    # Dict iteration order is insertion order; the bridge inserts keys
    # in sorted order so the wire payload is deterministic.
    assert list(updates[0].args["values"]) == ["alpha", "mu", "zeta"]
    await fed.aclose()


@pytest.mark.asyncio
async def test_discover_event_routes_to_handler_for_subscribed_class() -> None:
    fake = FakeRtiServer()
    del fake  # constructor side-effect: register the memory:// driver.
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        subscriptions={"Vehicle": ["pos"]},
    )
    fed = _make_federate(model)
    await fed.deliver_object_event(
        DiscoverObjectInstance(
            object_handle=42, class_name="Vehicle", instance_name="v-1"
        )
    )
    await fed.step_once()
    assert model.discovered == [(42, "Vehicle", "v-1")]
    await fed.aclose()


@pytest.mark.asyncio
async def test_discover_event_for_unsubscribed_class_is_dropped() -> None:
    fake = FakeRtiServer()
    del fake
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        subscriptions={"Vehicle": ["pos"]},
    )
    fed = _make_federate(model)
    # Discover for a different class (not subscribed) — must drop.
    await fed.deliver_object_event(
        DiscoverObjectInstance(
            object_handle=99, class_name="Other", instance_name="o-1"
        )
    )
    await fed.step_once()
    assert model.discovered == []
    await fed.aclose()


@pytest.mark.asyncio
async def test_reflect_event_routes_to_handler() -> None:
    fake = FakeRtiServer()
    del fake
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        subscriptions={"Vehicle": ["pos"]},
    )
    fed = _make_federate(model)
    await fed.deliver_object_event(
        ReflectAttributeValues(
            object_handle=42, values={"pos": b"x"}, timestamp=1.5
        )
    )
    await fed.step_once()
    assert model.reflects == [(42, {"pos": b"x"})]
    await fed.aclose()


@pytest.mark.asyncio
async def test_object_event_short_circuits_internal_cycle() -> None:
    """Like the interaction-side external-first contract: a queued
    object-class event makes step_once drain externals and skip the
    internal phase."""
    fake = FakeRtiServer()
    del fake
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        subscriptions={"Vehicle": ["pos"]},
    )
    fed = _make_federate(model)
    await fed.deliver_object_event(
        DiscoverObjectInstance(
            object_handle=1, class_name="Vehicle", instance_name="v-1"
        )
    )
    await fed.step_once()
    assert model.discovered == [(1, "Vehicle", "v-1")]
    assert model.internal_count == 0
    # The next cycle should be a normal internal cycle.
    model.ta_schedule.append(1.0)
    await fed.step_once()
    assert model.internal_count == 1
    await fed.aclose()


@pytest.mark.asyncio
async def test_interaction_only_model_unaffected() -> None:
    """A model that doesn't implement any of the object-class methods
    must keep working exactly as before."""
    from tests.spec.m4._fakes import StubCoupledModel

    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.append(1.0)  # noqa: SLF001
    stub._outputs.append({})  # noqa: SLF001
    fed = HLAFederate(
        coupled_model=stub,
        federation=FederationSpec(name="demo"),
        federate_name="alice",
        port_mapping=PortMapping.from_dict({}),
        url="memory://fake-rti",
    )
    await fed.step_once()
    # No object-class RPCs fired.
    assert fake.calls_for("publish_object_class") == []
    assert fake.calls_for("subscribe_object_class") == []
    assert fake.calls_for("register_object_instance") == []
    assert fake.calls_for("update_attributes") == []
    assert stub.internal_transitions == 1
    await fed.aclose()


@pytest.mark.asyncio
async def test_runtime_register_instance_hook() -> None:
    fake = FakeRtiServer()
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        instances={},  # no startup registrations
    )
    fed = _make_federate(model)
    handle = await fed.register_instance("Sensor", "late-1")
    assert isinstance(handle, int)
    assert fed._instance_handles["late-1"] == handle  # noqa: SLF001
    regs = fake.calls_for("register_object_instance")
    assert len(regs) == 1
    assert regs[0].args["instance_name"] == "late-1"
    assert regs[0].args["class_name"] == "Sensor"
    await fed.aclose()


@pytest.mark.asyncio
async def test_partial_subscriber_only_implements_handlers() -> None:
    """A pure subscriber model exposes ``object_class_subscriptions``
    + ``reflect_handler`` only — no publication / registration /
    update path. The bridge dispatches reflects to the handler and
    issues no publish / register RPCs."""

    class _Subscriber:
        def __init__(self) -> None:
            self.reflects: list[tuple[int, dict[str, Any]]] = []
            self._ta = deque([1.0])

        def time_advance(self) -> float:
            return self._ta.popleft() if self._ta else 1.0

        def output_handler(self) -> dict[str, Any]:
            return {}

        def internal_transition(self) -> None:
            return None

        def external_transition(self, port: str, payload: Any) -> None:
            return None

        def object_class_subscriptions(self) -> dict[str, list[str]]:
            return {"Vehicle": ["pos"]}

        def reflect_handler(
            self, instance_handle: int, attrs: dict[str, Any]
        ) -> None:
            self.reflects.append((instance_handle, dict(attrs)))

    fake = FakeRtiServer()
    sub = _Subscriber()
    fed = _make_federate(sub)
    await fed.deliver_object_event(
        ReflectAttributeValues(
            object_handle=7, values={"pos": b"y"}, timestamp=None
        )
    )
    await fed.step_once()
    assert sub.reflects == [(7, {"pos": b"y"})]
    # Subscriber-only — no publish / register fired.
    assert fake.calls_for("publish_object_class") == []
    assert fake.calls_for("register_object_instance") == []
    # Sub did fire.
    assert len(fake.calls_for("subscribe_object_class")) == 1
    await fed.aclose()


@pytest.mark.asyncio
async def test_in_flight_object_event_buffered() -> None:
    """A DiscoverObjectInstance arriving via fed.events() before the
    grant must be buffered onto the object-event queue (not the
    interaction queue) and dispatched on the same cycle."""
    fake = FakeRtiServer()
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        subscriptions={"Vehicle": ["pos"]},
    )
    fed = _make_federate(model)
    federate = await fed._ensure_federate()  # noqa: SLF001
    fake.push_event(
        federate.handle,
        DiscoverObjectInstance(
            object_handle=11, class_name="Vehicle", instance_name="v-x"
        ),
    )
    await fed.step_once()
    assert model.discovered == [(11, "Vehicle", "v-x")]
    # Cycle was treated as external — internal_count not incremented.
    assert model.internal_count == 0
    await fed.aclose()


@pytest.mark.asyncio
async def test_object_class_and_interaction_paths_coexist() -> None:
    """A model can mix interactions + object classes. Both paths
    must fire in the same internal cycle."""
    fake = FakeRtiServer()
    model = _ObjectModel(
        ta_schedule=deque([1.0]),
        publications={"Vehicle": ["pos"]},
        instances={"v1": "Vehicle"},
        output_schedule=deque([{"out_msg": "PING"}]),
        update_schedule=deque([{"v1": {"pos": b"\x00"}}]),
    )
    fed = _make_federate(model, {"out_msg": "Tick"})
    await fed.step_once()
    sends = fake.calls_for("send_interaction")
    updates = fake.calls_for("update_attributes")
    assert len(sends) == 1
    assert sends[0].args["class_name"] == "Tick"
    assert len(updates) == 1
    assert updates[0].args["values"] == {"pos": b"\x00"}
    assert model.internal_count == 1
    await fed.aclose()
