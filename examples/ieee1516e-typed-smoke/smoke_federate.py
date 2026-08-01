"""IEEE 1516 typed-handle smoke federate — M28 W4.

A federate written using ONLY the IEEE 1516 service-style Rti1516eAmbassador
methods AND the M28 IEEE 1516 service-style typed handles + typed collections.
No bare ints in user code (except numeric scalars like ``time``);
no bare ``list`` / ``dict`` literals passed to the ambassador
declaration / update / send paths.

This is the sibling of ``examples/ieee1516e-ambassador-smoke/smoke_federate.py``
that proves IEEE 1516 API portability at the TYPE level: a federate ported
from a commercial RTI (reference_rti HLA Evolved, Portico, MAK RTI) compiles
unchanged against gorti because the typed handle classes
(``ObjectClassHandle``, ``AttributeHandle``, …) and typed collection
classes (``AttributeHandleSet``, ``AttributeHandleValueMap``, …) live
in the public ``rti1516e`` namespace with the exact IEEE 1516 names.

Exercises:
  - §10.2 handle / name lookups       (getObjectClassHandle, …)
  - §6.1   reservation flow            (reserveObjectInstanceName)
  - §6.6   register-by-handle          (registerObjectInstance)
  - §6.10  attribute update            (updateAttributeValues)
  - §6.12  interaction send            (sendInteraction)
  - §10.4  callback evocation          (evokeCallback)
  - §4.11  sync points                 (registerFederationSynchronizationPoint)
  - §10.6  typed handle factories + typed collections (M28)

Runnable standalone via:

    python examples/ieee1516e-typed-smoke/smoke_federate.py grpc://127.0.0.1:8080
"""

from __future__ import annotations

import sys
from pathlib import Path
from typing import Any

# Make the pysdk package importable when running from a checkout.
_REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(_REPO / "pysdk"))

# ruff: noqa: E402
from rti1516e import (
    AttributeHandle,
    AttributeHandleSet,
    AttributeHandleValueMap,
    InteractionClassHandle,
    ObjectClassHandle,
    ObjectInstanceHandle,
    ParameterHandle,
    ParameterHandleValueMap,
)
from rti1516e.standard import Rti1516eAmbassador

FOM = str(Path(__file__).resolve().parent / "federation.fom.xml")
FEDERATION_NAME = "ieee1516e-typed-smoke"


class TypedSmokeFederate(Rti1516eAmbassador):
    """IEEE 1516 typed-handle federate — uses only typed handles + typed collections."""

    def __init__(self) -> None:
        super().__init__()
        # Captured callback payloads — the smoke test asserts on these.
        self.discovered: list[tuple[int, str, str]] = []
        self.reflections: list[tuple[int, dict[str, Any], float | None]] = []
        self.interactions: list[tuple[str, dict[str, Any], float | None]] = []
        self.reservations_ok: list[str] = []
        self.reservations_fail: list[str] = []
        self.sync_announcements: list[tuple[str, bytes]] = []
        self.removes: list[int] = []

    # --- callback overrides ---

    def discoverObjectInstance(  # noqa: N802
        self, object_handle: int, class_name: str, instance_name: str
    ) -> None:
        self.discovered.append((object_handle, class_name, instance_name))

    def reflectAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None,
    ) -> None:
        self.reflections.append((object_handle, dict(values), timestamp))

    def receiveInteraction(  # noqa: N802
        self,
        class_name: str,
        parameters: dict[str, Any],
        timestamp: float | None,
    ) -> None:
        self.interactions.append((class_name, dict(parameters), timestamp))

    def objectInstanceNameReservationSucceeded(self, object_name: str) -> None:  # noqa: N802
        self.reservations_ok.append(object_name)

    def objectInstanceNameReservationFailed(self, object_name: str) -> None:  # noqa: N802
        self.reservations_fail.append(object_name)

    def announceSynchronizationPoint(self, label: str, tag: bytes) -> None:  # noqa: N802
        self.sync_announcements.append((label, tag))

    def removeObjectInstance(  # noqa: N802
        self, object_handle: int, tag: bytes, timestamp: float | None
    ) -> None:
        self.removes.append(object_handle)


def run_publisher(
    url: str,
    *,
    joined_event: Any | None = None,
    proceed_event: Any | None = None,
    resign_when_done: bool = True,
) -> dict[str, Any]:
    """Federate that creates the federation + registers + publishes.

    All user-facing handle / collection variables are typed:
        - object / interaction / attribute / parameter handles are
          typed handle instances;
        - declaration uses ``AttributeHandleSet``;
        - update uses ``AttributeHandleValueMap``;
        - interaction send uses ``ParameterHandleValueMap`` obtained
          via the §10.6 factory accessor.
    """
    pub = TypedSmokeFederate()
    pub.connect(pub, url)
    pub.createFederationExecution(FEDERATION_NAME, [FOM])
    pub.joinFederationExecution("publisher", FEDERATION_NAME)
    if joined_event is not None:
        joined_event.set()
    try:
        # 1. Look up handles — return values are typed handle instances.
        vehicle_class: ObjectClassHandle = pub.getObjectClassHandle("Vehicle")
        position_attr: AttributeHandle = pub.getAttributeHandle(vehicle_class, "Position")
        velocity_attr: AttributeHandle = pub.getAttributeHandle(vehicle_class, "Velocity")
        honk_class: InteractionClassHandle = pub.getInteractionClassHandle("Honk")
        volume_param: ParameterHandle = pub.getParameterHandle(honk_class, "Volume")

        # 2. Publish using a typed ``AttributeHandleSet`` (IEEE 1516 handle-based idiom).
        #    The set is also constructible from the §10.6 factory.
        attr_set_via_factory = pub.getAttributeHandleSetFactory().create()
        attr_set_via_factory.add(position_attr)
        attr_set_via_factory.add(velocity_attr)
        pub.publishObjectClassAttributes(vehicle_class, attr_set_via_factory)
        pub.publishInteractionClass(honk_class)

        # M27 Phase D — let the subscriber catch up before the
        # publish-side work fires (kept for symmetry with the M26 smoke).
        if proceed_event is not None:
            proceed_event.wait(timeout=10.0)

        # 3. Reserve an instance name (IEEE 1516 object-name reservation flow).
        pub.reserveObjectInstanceName("car-7")
        fired = pub.evokeMultipleCallbacks(approx_min_time=0.1, approx_max_time=0.5)

        # 4. Register the object — return value is a typed
        #    ``ObjectInstanceHandle``.
        obj_handle: ObjectInstanceHandle = pub.registerObjectInstance(
            vehicle_class, "car-7"
        )

        # 5. Update attributes using a typed ``AttributeHandleValueMap``.
        attr_values = AttributeHandleValueMap()
        attr_values[position_attr] = (42).to_bytes(8, "big")
        attr_values[velocity_attr] = (7).to_bytes(8, "big")
        pub.updateAttributeValues(obj_handle, attr_values)

        # 6. Send an interaction using a typed ``ParameterHandleValueMap``
        #    obtained via the §10.6 factory accessor.
        param_values = pub.getParameterHandleValueMapFactory().create()
        param_values[volume_param] = (5).to_bytes(4, "big")
        pub.sendInteraction(honk_class, param_values)

        # 7. Register a sync point.
        pub.registerFederationSynchronizationPoint("phase1", b"go")
        pub.evokeMultipleCallbacks(approx_min_time=0.1, approx_max_time=0.5)

        return {
            "vehicle_class": vehicle_class,
            "position_attr": position_attr,
            "velocity_attr": velocity_attr,
            "honk_class": honk_class,
            "volume_param": volume_param,
            "reserved_callbacks": list(pub.reservations_ok),
            "reservation_evoke_fired": fired,
            "object_handle": obj_handle,
            "sync_announcements": list(pub.sync_announcements),
            # Typed-form witness fields — the test asserts isinstance().
            "vehicle_class_is_typed": isinstance(vehicle_class, ObjectClassHandle),
            "position_attr_is_typed": isinstance(position_attr, AttributeHandle),
            "honk_class_is_typed": isinstance(honk_class, InteractionClassHandle),
            "volume_param_is_typed": isinstance(volume_param, ParameterHandle),
            "object_handle_is_typed": isinstance(obj_handle, ObjectInstanceHandle),
            "attr_set_is_typed": isinstance(attr_set_via_factory, AttributeHandleSet),
            "attr_values_is_typed": isinstance(attr_values, AttributeHandleValueMap),
            "param_values_is_typed": isinstance(param_values, ParameterHandleValueMap),
        }
    finally:
        if resign_when_done:
            pub.resignFederationExecution()
            pub.disconnect()


if __name__ == "__main__":
    url = sys.argv[1] if len(sys.argv) > 1 else "grpc://127.0.0.1:8080"
    result = run_publisher(url)
    for k, v in result.items():
        print(f"{k}: {v!r}")
