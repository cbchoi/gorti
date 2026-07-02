"""IVCT-inspired TC-010 analogue — time regulation & constrained federate.

Spec-anchored assertions (IEEE 1516.1-2010):

- §8.2  ``enableTimeRegulation`` — the federate becomes time-regulating.
- §8.4  ``enableTimeConstrained`` — the federate becomes time-constrained.
- §8.7/§8.19-8.21 lookahead — query returns the enabled value; modify is
        honoured; invalid lookahead is rejected.
- §8.8  ``timeAdvanceRequest`` — grant at exactly the requested time
        (M37-EB §8.10 exact-grant behavior), exactly once per request.
- §8.10 ``nextMessageRequest`` — grant lands on the next TSO message
        time, and the TSO message is delivered BEFORE the grant (§8.14
        delivery-before-grant ordering).
- §8.1.2 outgoing TSO floor — a regulating sender may not stamp a TSO
        message below currentTime + lookahead (M37-EB validation).
- §8.13 LBTS gating + regulation disable — a lagging regulator blocks a
        constrained federate's grant; disabling regulation unblocks it.

pysdk surface notes:

- pysdk has NO ``timeRegulationEnabled`` / ``timeConstrainedEnabled``
  callbacks (the wire stream carries no such events either); §8.2/§8.4
  are asserted via their observable state instead: the query surface
  (``queryLookahead``) and the typed already-enabled rejection on
  re-enable.
- pysdk's TIME transport paths translate gRPC failures into the typed
  ``rti1516e.errors.Time*`` exceptions (detail-string sniffing in
  ``_grpc_errors``), but the OBJECT path (``updateAttributeValues``)
  does not — the §8.1.2 floor violation therefore surfaces as a raw
  ``grpc.aio.AioRpcError`` (INVALID_ARGUMENT, gorti's
  ``ErrTimeInvalidLogicalTime`` text "invalid logical time"). Known
  pysdk mapping gap; asserted via code + message substring.
"""

from __future__ import annotations

import grpc
import pytest

from _driver import join, leave

VEHICLE_ATTRS = ["Position", "Velocity"]


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc010_enable_time_regulation_takes_effect(
    rtid_url: str, federation_name: str
) -> None:
    """§8.2 — after enableTimeRegulation the federate IS regulating.

    pysdk gap note: no timeRegulationEnabled callback exists in pysdk (or
    on the wire); regulation is asserted via queryLookahead (only valid
    while regulating, §8.16) and via the §8.2 already-regulating
    rejection on a second enable.
    """
    amb = join(rtid_url, federation_name, "reg")
    try:
        amb.enableTimeRegulation(1.5)
        # §8.16 — lookahead query reflects the enabled value.
        assert amb.queryLookahead() == 1.5, (
            "§8.2+§8.16: queryLookahead must return the enable-time lookahead"
        )
        # §8.2 — enabling twice is TimeRegulationAlreadyEnabled; pysdk
        # translates the wire failure into the typed exception.
        from rti1516e._grpc_errors import TimeRegulationAlreadyEnabled

        with pytest.raises(TimeRegulationAlreadyEnabled):
            amb.enableTimeRegulation(1.5)
    finally:
        leave(amb)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc010_enable_time_constrained_takes_effect(
    rtid_url: str, federation_name: str
) -> None:
    """§8.4 — after enableTimeConstrained the federate IS constrained.

    pysdk gap note: no timeConstrainedEnabled callback exists in pysdk;
    the constrained state is asserted via the §8.4 already-constrained
    rejection on a second enable, plus disable→re-enable round-trip.
    """
    amb = join(rtid_url, federation_name, "cons")
    try:
        from rti1516e._grpc_errors import (
            TimeConstrainedAlreadyEnabled,
            TimeConstrainedNotEnabled,
        )

        amb.enableTimeConstrained()
        # §8.4 — enabling twice is TimeConstrainedAlreadyEnabled.
        with pytest.raises(TimeConstrainedAlreadyEnabled):
            amb.enableTimeConstrained()
        # §8.12 — disable succeeds exactly once from the constrained state.
        amb.disableTimeConstrained()
        with pytest.raises(TimeConstrainedNotEnabled):
            amb.disableTimeConstrained()  # §8.12 TimeConstrainedIsNotEnabled
    finally:
        leave(amb)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc010_lookahead_query_modify_and_floor(
    rtid_url: str, federation_name: str
) -> None:
    """§8.19-8.21 — queryLookahead reflects modifyLookahead immediately;
    §8.1.2 — a below-floor TSO update (t < currentTime + lookahead) is
    rejected InvalidLogicalTime (M37-EB validation)."""
    amb = join(rtid_url, federation_name, "look")
    try:
        amb.enableTimeRegulation(1.0)
        amb.modifyLookahead(2.0)
        # §8.21 — modification is visible immediately.
        assert amb.queryLookahead() == 2.0, "§8.21: modifyLookahead must be honoured"

        amb.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        vehicle = amb.getObjectClassHandle("Vehicle")
        pos = amb.getAttributeHandle(vehicle, "Position")
        obj = amb.registerObjectInstance("Vehicle")

        # §8.1.2 — currentTime=0, lookahead=2 → TSO stamp 0.5 is below the
        # floor and must be rejected InvalidLogicalTime.
        # pysdk mapping gap: raw AioRpcError instead of a typed error;
        # assert gorti's ErrTimeInvalidLogicalTime text.
        with pytest.raises(grpc.aio.AioRpcError) as exc_info:
            amb.updateAttributeValues(obj, {int(pos): b"\x00" * 8}, timestamp=0.5)
        assert exc_info.value.code() == grpc.StatusCode.INVALID_ARGUMENT, (
            "§8.1.2: below-floor TSO send must be InvalidLogicalTime"
        )
        assert "invalid logical time" in exc_info.value.details().lower()

        # ...and a stamp exactly ON the floor is legal (§8.1.2 boundary).
        amb.updateAttributeValues(obj, {int(pos): b"\x00" * 8}, timestamp=2.0)
    finally:
        leave(amb)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc010_time_advance_grant_exact_and_once(
    rtid_url: str, federation_name: str
) -> None:
    """§8.8 — TAR(t) yields timeAdvanceGrant at exactly t (M37-EB §8.10
    exact-grant), exactly once per request; logical time advances to t."""
    amb = join(rtid_url, federation_name, "tar")
    try:
        amb.enableTimeRegulation(1.0)
        amb.enableTimeConstrained()
        amb.timeAdvanceRequest(10.0)
        got = amb.wait_for("timeAdvanceGrant")
        # §8.8/§8.10 — the sole regulator advances to exactly t.
        assert got["time"] == 10.0, "§8.10: grant must land at the requested time"
        # §8.8 — exactly one grant per request.
        assert amb.count("timeAdvanceGrant") == 1, (
            "§8.8: exactly one grant per timeAdvanceRequest"
        )
        # §8.17 — queryLogicalTime reflects the granted time.
        assert amb.queryLogicalTime() == 10.0, (
            "§8.17: logical time must equal the granted time"
        )
        # A second TAR yields a second (single) grant.
        amb.timeAdvanceRequest(12.5)
        amb.wait_for("timeAdvanceGrant", time=12.5)
        assert amb.count("timeAdvanceGrant") == 2
    finally:
        leave(amb)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc010_ner_delivers_tso_before_grant(
    rtid_url: str, federation_name: str
) -> None:
    """§8.10 + §8.14 — NER(t) grants at the next TSO message time, and the
    TSO reflect is delivered BEFORE its grant, never after.

    Hard assertions: TSO buffering (no delivery before the advance),
    delivery-before-grant ordering, and the TSO timestamp on the reflect.
    xfail: the §8.10 grant-lands-on-message-time refinement — gorti's
    time.Manager decides NER grants purely from LBTS (advance.go
    decideGrant has no TSO-queue time input), so the grant lands at LBTS
    instead of the pending message time.
    """
    pub = join(rtid_url, federation_name, "pub")
    sub = join(rtid_url, federation_name, "sub")
    try:
        sub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        sub.enableTimeConstrained()

        pub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        pub.enableTimeRegulation(1.0)
        vehicle = pub.getObjectClassHandle("Vehicle")
        pos = pub.getAttributeHandle(vehicle, "Position")
        obj = pub.registerObjectInstance("Vehicle")
        sub.wait_for("discoverObjectInstance", object_handle=int(obj))

        # TSO update at t=5 (floor: 0 + 1.0 → legal).
        pub.updateAttributeValues(obj, {int(pos): b"\x11" * 8}, timestamp=5.0)
        # §8.16 (asynchronous delivery OFF is the spec default) — the TSO
        # message must NOT reach the constrained subscriber before it
        # advances past t=5.
        sub.assert_quiet("reflectAttributeValues")

        # Raise the publisher's LBTS contribution past the message time so
        # the subscriber's advance can be granted: TAR(10) → LBTS 11.
        pub.timeAdvanceRequest(10.0)
        pub.wait_for("timeAdvanceGrant", time=10.0)

        sub.nextMessageRequest(20.0)
        got = sub.wait_for("timeAdvanceGrant")
        # §8.14 — the TSO reflect precedes the grant in the callback order.
        sub.wait_for("reflectAttributeValues", object_handle=int(obj))
        kinds = [name for name, _ in sub.snapshot()]
        assert kinds.index("reflectAttributeValues") < kinds.index("timeAdvanceGrant"), (
            f"§8.14: TSO delivery must precede its grant; callback order {kinds!r}"
        )
        # §6.11 — the reflect carries the TSO timestamp.
        reflect = sub.wait_for("reflectAttributeValues", object_handle=int(obj))
        assert reflect["timestamp"] == 5.0, (
            "§8.14: the delivered reflect must carry its TSO timestamp"
        )
        # §8.10 — NER's grant should land ON the message time (t=5).
        if got["time"] != 5.0:
            pytest.xfail(
                "gorti gap: NER grants at LBTS, not at the next TSO message "
                "time — rti/internal/time/advance.go decideGrant has no "
                f"TSO-queue time input (granted at {got['time']}, wanted 5.0)"
            )
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc010_lagging_regulator_blocks_then_disable_unblocks(
    rtid_url: str, federation_name: str
) -> None:
    """§8.6 + §8.13 — a regulator sitting at t=0 (lookahead 1 → LBTS 1)
    blocks a constrained federate's TAR(10); disableTimeRegulation removes
    it from the LBTS computation and the pending grant fires."""
    reg = join(rtid_url, federation_name, "reg")
    cons = join(rtid_url, federation_name, "cons")
    try:
        reg.enableTimeRegulation(1.0)
        cons.enableTimeConstrained()

        cons.timeAdvanceRequest(10.0)
        # §8.6 — 10 > LBTS(=1): the grant MUST NOT fire yet.
        cons.assert_quiet("timeAdvanceGrant")

        # §8.13 — regulator leaves the time-management game...
        reg.disableTimeRegulation()
        # ...and the constrained federate's pending TAR is granted in full.
        got = cons.wait_for("timeAdvanceGrant")
        assert got["time"] == 10.0, (
            "§8.13: after the sole regulator disables, TAR(10) must grant at 10"
        )
        assert cons.count("timeAdvanceGrant") == 1
    finally:
        leave(cons)
        leave(reg)
