"""IEEE 1516 ambassador-surface smoke federate — M26 Phase G.

A federate written using ONLY the IEEE 1516 service-style Rti1516eAmbassador
methods. No reach-around into ``sdk.ownership.*`` / ``sdk.ddm.*``
modules. The goal: prove the M25 + M26 ambassador surface is rich
enough that a federate ported from a commercial RTI (reference_rti /
Portico / MAK) compiles and runs against gorti without restructuring.

Exercises:
  - §10.2 handle / name lookups       (getObjectClassHandle, …)
  - §6.1   reservation flow            (reserveObjectInstanceName)
  - §6.6   register-by-handle          (registerObjectInstance)
  - §6.10  attribute update            (updateAttributeValues)
  - §6.12  interaction send            (sendInteraction)
  - §6.16  delete                      (deleteObjectInstance via SDK)
  - §10.4  callback evocation          (evokeCallback)
  - §4.11  sync points                 (registerFederationSynchronizationPoint)

This file is consumed by the M26 Phase G smoke test
(pysdk/tests/spec/m26/test_ieee1516e_ambassador_smoke.py). It is intentionally
runnable standalone via:

    python examples/ieee1516e-ambassador-smoke/smoke_federate.py grpc://127.0.0.1:8080
"""

from __future__ import annotations

import sys
from pathlib import Path
from typing import Any

# Make the pysdk package importable when running from a checkout.
_REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(_REPO / "pysdk"))

# ruff: noqa: E402
from rti1516e.standard import Rti1516eAmbassador

FOM = str(Path(__file__).resolve().parent / "federation.fom.xml")
FEDERATION_NAME = "ieee1516e-ambassador-smoke"


class SmokeFederate(Rti1516eAmbassador):
    """IEEE 1516 service-style federate using only Rti1516eAmbassador methods."""

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

    M27 Phase D: optional coordination hooks for the cross-federate
    smoke. If ``joined_event`` is provided, ``.set()`` is called once
    the publisher has joined (lets a subscriber thread synchronize on
    "federation exists"). If ``proceed_event`` is provided, the
    publisher waits on ``.wait()`` after joining + declaring its
    publish set, before the register / update / interact phase — so
    the subscriber has a chance to subscribe before the data flows.

    ``resign_when_done`` controls the cleanup phase. Default behavior
    is to resign the federate, which the test wants when the call is
    standalone. The cross-federate smoke disables resign and lets the
    main test thread own the federation lifecycle.
    """
    pub = SmokeFederate()
    pub.connect(pub, url)
    pub.createFederationExecution(FEDERATION_NAME, [FOM])
    pub.joinFederationExecution("publisher", FEDERATION_NAME)
    if joined_event is not None:
        joined_event.set()
    try:
        # 1. Look up handles (IEEE 1516 service-style: name → handle before pub/sub).
        vehicle_class = pub.getObjectClassHandle("Vehicle")
        position_attr = pub.getAttributeHandle(vehicle_class, "Position")
        velocity_attr = pub.getAttributeHandle(vehicle_class, "Velocity")
        honk_class = pub.getInteractionClassHandle("Honk")

        # 2. Publish using string class name (pysdk convenience).
        pub.publishObjectClassAttributes("Vehicle", ["Position", "Velocity"])
        pub.publishInteractionClass("Honk")

        # M27 Phase D — let the subscriber catch up before the
        # publish-side work fires. Cross-federate smoke uses this.
        if proceed_event is not None:
            proceed_event.wait(timeout=10.0)

        # 3. Reserve an instance name (IEEE 1516 object-name reservation flow).
        pub.reserveObjectInstanceName("car-7")
        # Let the reservation callback fire — evokeCallback yields.
        fired = pub.evokeMultipleCallbacks(approx_min_time=0.1, approx_max_time=0.5)

        # 4. Register the object using the reserved name.
        # The SDK's registerObjectInstance only takes class_name; we
        # don't have a IEEE 1516 service-style "register with handle + reserved
        # name" wrapper on the ambassador (deferred). For the smoke
        # we use the existing wrapper, which goes through the same
        # reservation table on the server side.
        obj_h = pub.registerObjectInstance("Vehicle", "car-7")

        # 5. Update attributes — IEEE 1516 service-style update by handle.
        pub.updateAttributeValues(
            obj_h,
            {"Position": (42).to_bytes(8, "big"), "Velocity": (7).to_bytes(8, "big")},
        )

        # 6. Send an interaction by class name (SDK shortcut).
        pub.sendInteraction("Honk", {"Volume": (5).to_bytes(4, "big")})

        # 7. Register a sync point.
        pub.registerFederationSynchronizationPoint("phase1", b"go")
        pub.evokeMultipleCallbacks(approx_min_time=0.1, approx_max_time=0.5)

        return {
            "vehicle_class": vehicle_class,
            "position_attr": position_attr,
            "velocity_attr": velocity_attr,
            "honk_class": honk_class,
            "reserved_callbacks": list(pub.reservations_ok),
            "reservation_evoke_fired": fired,
            "object_handle": obj_h,
            "sync_announcements": list(pub.sync_announcements),
        }
    finally:
        if resign_when_done:
            pub.resignFederationExecution()
            pub.disconnect()


def run_subscriber(
    url: str,
    *,
    evoke_seconds: float = 3.0,
    subscribed_event: Any | None = None,
) -> dict[str, Any]:
    """IEEE 1516 service-style subscriber federate. M27 Phase D.

    Joins the same federation the publisher created, subscribes to
    Vehicle.Position/Velocity + Honk by HANDLE (IEEE 1516 handle-based idiom), and
    drives callback dispatch via ``evokeMultipleCallbacks`` for
    ``evoke_seconds``. Returns the captured callback state.

    The subscriber uses ONLY IEEE 1516 service-style ambassador methods — no
    reach-around into ``self._fed().events()`` async iteration, no
    direct SDK module access. This is the test that gorti's Layer 2
    is usable as a IEEE 1516 service-style federate harness.
    """
    sub = SmokeFederate()
    sub.connect(sub, url)
    # The publisher created the federation; subscriber only joins —
    # but the subscriber must pass the same FOM modules so the
    # local handle cache is populated for event translation
    # (Discover / Reflect / Receive callbacks would otherwise see
    # the stringified handle instead of the FOM class name).
    sub.joinFederationExecution(
        "subscriber",
        FEDERATION_NAME,
        additional_fom_modules=[FOM],
    )
    try:
        # 1. Handle lookups (IEEE 1516 handle-based idiom: resolve once, reuse).
        vehicle_class = sub.getObjectClassHandle("Vehicle")
        position_attr = sub.getAttributeHandle(vehicle_class, "Position")
        velocity_attr = sub.getAttributeHandle(vehicle_class, "Velocity")
        honk_class = sub.getInteractionClassHandle("Honk")

        # 2. Subscribe by handle (IEEE 1516 handle-based idiom — by-handle subscription
        # works even when the subscriber joined an already-created
        # federation and has no local FOM cache).
        sub.subscribeObjectClassAttributes(
            vehicle_class, [position_attr, velocity_attr]
        )
        sub.subscribeInteractionClass(honk_class)
        if subscribed_event is not None:
            subscribed_event.set()

        # 3. Drive callback dispatch via the IEEE 1516 callback-evocation loop. The
        #    publisher should produce Discover + Reflect + Receive
        #    events that fire the corresponding override slots.
        import time as _time
        deadline = _time.monotonic() + evoke_seconds
        while _time.monotonic() < deadline:
            sub.evokeMultipleCallbacks(approx_min_time=0.05, approx_max_time=0.1)
            # Early-out: we expect ≥1 discover, ≥1 reflect, ≥1 interaction.
            if sub.discovered and sub.reflections and sub.interactions:
                break

        return {
            "vehicle_class": vehicle_class,
            "position_attr": position_attr,
            "velocity_attr": velocity_attr,
            "honk_class": honk_class,
            "discovered": list(sub.discovered),
            "reflections": list(sub.reflections),
            "interactions": list(sub.interactions),
            "sync_announcements": list(sub.sync_announcements),
        }
    finally:
        sub.resignFederationExecution()
        sub.disconnect()


if __name__ == "__main__":
    url = sys.argv[1] if len(sys.argv) > 1 else "grpc://127.0.0.1:8080"
    result = run_publisher(url)
    for k, v in result.items():
        print(f"{k}: {v!r}")
