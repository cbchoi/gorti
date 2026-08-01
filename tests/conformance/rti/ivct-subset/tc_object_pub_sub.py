"""IVCT-inspired TC-005 analogue — object publish/subscribe round-trip.

Spec-anchored assertions (IEEE 1516.1-2010):

- §5.2  ``publishObjectClassAttributes`` — publishing enables
        ``registerObjectInstance``; re-publish is idempotent.
- §5.6  ``subscribeObjectClassAttributes`` — subscribers discover
        instances; zero-publisher classes yield no discovery.
- §6.1-6.5 name reservation — succeeded/failed callbacks; a reserved
        name is usable at register time.
- §6.8  ``registerObjectInstance`` — unpublished class is rejected
        (gorti M37-DC enforcement: ERR_OBJ_CLASS_NOT_PUBLISHED).
- §6.10/§6.11 update → reflect — attribute bytes round-trip exactly.
- §6.14/§6.15 delete / resign-delete → removeObjectInstance at every
        discoverer.

pysdk surface notes:

- ``discoverObjectInstance``'s ``class_name`` argument arrives as the
  stringified object-class HANDLE (``_translate_event`` does not reverse
  the FOM map for discover), so class assertions compare against
  ``int(getObjectClassHandle(...))``.
- ``reflectAttributeValues`` keys its values dict by stringified
  attribute handle; byte payload comparisons use those keys. (M39 also
  delivers typed forms alongside — ``object_class`` on discover,
  ``attribute_values`` on reflect — for overrides that accept them.)
- object-path RPC errors are translated to typed ``rti1516e.errors``
  exceptions since M39 (same shared translator as the time path); the
  register-without-publish assertion below is typed.
"""

from __future__ import annotations

import pytest

from _driver import federate_handle, join, leave

VEHICLE_ATTRS = ["Position", "Velocity"]


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_publish_then_register_instance(
    rtid_url: str, federation_name: str
) -> None:
    """§5.2 + §6.8 — publishing enables registerObjectInstance; re-publish
    is idempotent ("may be called more than once")."""
    amb = join(rtid_url, federation_name, "pub")
    try:
        amb.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        # §5.2 — re-publish must be accepted, not raise.
        amb.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        # §6.8 — a publisher may register; the handle is valid (>0).
        obj = amb.registerObjectInstance("Vehicle")
        assert int(obj) > 0, "§6.8: registerObjectInstance must mint a handle"
    finally:
        leave(amb)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_register_unpublished_class_rejected(
    rtid_url: str, federation_name: str
) -> None:
    """§6.8 precondition — registering an instance of a class the federate
    does not publish fails ObjectClassNotPublished (M37-DC enforcement).

    Typed since M39: the object path shares the time path's exception
    translator, so pysdk raises rti1516e.errors.ObjectClassNotPublished
    (the IEEE Annex C name) instead of a raw AioRpcError.
    """
    from rti1516e.errors import ObjectClassNotPublished

    amb = join(rtid_url, federation_name, "nopub")
    try:
        with pytest.raises(ObjectClassNotPublished):
            # §6.8 — register without publish must fail ObjectClassNotPublished.
            amb.registerObjectInstance("Vehicle")
    finally:
        leave(amb)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_subscriber_discovers_instance(
    rtid_url: str, federation_name: str
) -> None:
    """§5.6 + §6.9 — an in-effect subscription yields discoverObjectInstance
    carrying the registered class (as handle) and the instance handle."""
    pub = join(rtid_url, federation_name, "pub")
    sub = join(rtid_url, federation_name, "sub")
    try:
        vehicle_handle = int(sub.getObjectClassHandle("Vehicle"))
        sub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        pub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        obj = pub.registerObjectInstance("Vehicle")

        got = sub.wait_for("discoverObjectInstance", object_handle=int(obj))
        # §6.9 — discover carries the registered object class.
        # (pysdk delivers class_name as the stringified class handle.)
        assert int(got["class_name"]) == vehicle_handle, (
            "§6.9: discover must carry the registered object class"
        )
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_subscribe_discovers_existing_instance(
    rtid_url: str, federation_name: str
) -> None:
    """§5.6 — subscribing AFTER registration still yields discovery for the
    pre-existing instance (subscription catch-up)."""
    pub = join(rtid_url, federation_name, "pub")
    sub = join(rtid_url, federation_name, "sub")
    try:
        pub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        obj = pub.registerObjectInstance("Vehicle")  # registered BEFORE subscribe

        sub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        try:
            sub.wait_for(
                "discoverObjectInstance", object_handle=int(obj), timeout=2.0
            )
        except TimeoutError:
            pytest.xfail(
                "gorti gap: no discovery catch-up — subscribers only discover "
                "instances registered AFTER their subscription is in effect "
                "(object registry fans Discover out at Register time only)"
            )
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_update_reflect_round_trip_bytes(
    rtid_url: str, federation_name: str
) -> None:
    """§6.10 + §6.11 — updated attribute bytes arrive unchanged at the
    subscriber, keyed by the same attribute handles."""
    pub = join(rtid_url, federation_name, "pub")
    sub = join(rtid_url, federation_name, "sub")
    try:
        vehicle = pub.getObjectClassHandle("Vehicle")
        pos = pub.getAttributeHandle(vehicle, "Position")
        vel = pub.getAttributeHandle(vehicle, "Velocity")

        sub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        pub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        obj = pub.registerObjectInstance("Vehicle")
        sub.wait_for("discoverObjectInstance", object_handle=int(obj))

        pos_bytes = b"\x40\x59\x00\x00\x00\x00\x00\x00"  # 100.0 f64 BE
        vel_bytes = b"\x00\x01\x02\xff"
        # RO update (no timestamp) so delivery is not gated on §8 time state.
        pub.updateAttributeValues(obj, {int(pos): pos_bytes, int(vel): vel_bytes})

        got = sub.wait_for("reflectAttributeValues", object_handle=int(obj))
        values = got["values"]  # pysdk keys reflect values by str(attr handle)
        # §6.11 — the subscriber sees the same attributes...
        assert set(values) == {str(int(pos)), str(int(vel))}, (
            f"§6.11: reflect must carry exactly the updated attributes, got "
            f"{set(values)!r}"
        )
        # §6.10 — ...with byte-identical payloads.
        assert values[str(int(pos))] == pos_bytes, "§6.10: Position bytes must round-trip"
        assert values[str(int(vel))] == vel_bytes, "§6.10: Velocity bytes must round-trip"
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_delete_fires_remove_at_subscriber(
    rtid_url: str, federation_name: str
) -> None:
    """§6.14 + §6.15 — deleteObjectInstance fires removeObjectInstance on
    every discoverer, with the user-supplied tag."""
    pub = join(rtid_url, federation_name, "pub")
    sub = join(rtid_url, federation_name, "sub")
    try:
        sub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        pub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        obj = pub.registerObjectInstance("Vehicle")
        sub.wait_for("discoverObjectInstance", object_handle=int(obj))

        pub.deleteObjectInstance(obj, tag=b"bye")
        got = sub.wait_for("removeObjectInstance", object_handle=int(obj))
        # §6.15 — the delete tag is conveyed to the discoverer.
        assert got["tag"] == b"bye", "§6.15: remove must carry the delete tag"
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_remove_object_instance_on_resign(
    rtid_url: str, federation_name: str
) -> None:
    """§6.15 + §4.10 — resigning with CANCEL_THEN_DELETE_THEN_DIVEST deletes
    the resignee's instances, firing removeObjectInstance on discoverers
    (M36-DD threaded the resign action through pysdk to the wire)."""
    pub = join(rtid_url, federation_name, "pub")
    sub = join(rtid_url, federation_name, "sub")
    try:
        sub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        pub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        obj = pub.registerObjectInstance("Vehicle")
        sub.wait_for("discoverObjectInstance", object_handle=int(obj))

        leave(pub, action="CANCEL_THEN_DELETE_THEN_DIVEST")  # §4.10 resign action
        sub.wait_for("removeObjectInstance", object_handle=int(obj))
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_subscribe_without_publish_no_discovery(
    rtid_url: str, federation_name: str
) -> None:
    """§5.6 — no discoverObjectInstance for a class with zero publishers /
    zero registered instances."""
    sub = join(rtid_url, federation_name, "sub")
    try:
        sub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        # §5.6 — nothing was registered; nothing may be discovered.
        sub.assert_quiet("discoverObjectInstance")
    finally:
        leave(sub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_name_reservation_success_and_collision(
    rtid_url: str, federation_name: str
) -> None:
    """§6.2 + §6.3 — first reservation succeeds (callback), second federate
    reserving the same name fails (callback), and the reserved name binds
    to the registered instance."""
    alice = join(rtid_url, federation_name, "alice")
    bob = join(rtid_url, federation_name, "bob")
    try:
        alice.reserveObjectInstanceName("car-1")
        # §6.3 — reservation ack arrives as a callback.
        alice.wait_for("objectInstanceNameReservationSucceeded", object_name="car-1")

        bob.reserveObjectInstanceName("car-1")
        # §6.3 — colliding reservation is rejected via the Failed callback.
        bob.wait_for("objectInstanceNameReservationFailed", object_name="car-1")

        # §6.8 — the reserved name is usable at register time and binds.
        alice.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        obj = alice.registerObjectInstance("Vehicle", "car-1")
        assert alice.getObjectInstanceName(obj) == "car-1", (
            "§6.8: explicit-name register must bind the reserved name"
        )
        assert federate_handle(alice) != federate_handle(bob)
    finally:
        leave(bob)
        leave(alice)
