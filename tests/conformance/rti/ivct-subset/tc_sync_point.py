"""IVCT-inspired TC-002 analogue — synchronization-point lifecycle.

Spec-anchored assertions (IEEE 1516.1-2010):

- §4.11 ``registerFederationSynchronizationPoint`` — succeeds when the
        label is unique; duplicate label is a registration failure.
- §4.12 ``synchronizationPointRegistrationSucceeded/Failed`` — M37 put
        the ack events on the wire (stream.proto ``sync_registration_
        succeeded``/``failed``, tags 22/23) and M39 closed the pysdk
        translation gap, so the Layer-2 callbacks fire and are asserted
        hard. (gorti also rejects a duplicate register at the RPC
        itself; the typed ``SyncPointAlreadyExists`` path is asserted
        alongside.)
- §4.13 ``announceSynchronizationPoint`` — every federate in the sync
        set receives the announce exactly once; tag bytes round-trip.
- §4.14 ``synchronizationPointAchieved`` — all required federates
        achieving transitions the point.
- §4.15 ``federationSynchronized`` — fires exactly once per label per
        federate in the sync set.
"""

from __future__ import annotations

import pytest

from _driver import federate_handle, join, leave


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc002_register_sync_point_success_callback(
    rtid_url: str, federation_name: str
) -> None:
    """§4.12 — registrar gets synchronizationPointRegistrationSucceeded.

    Hard assertion since M39: pysdk translates the M37 wire event
    (stream.proto sync_registration_succeeded, tag 22) into the Layer-2
    ack callback.
    """
    amb = join(rtid_url, federation_name, "alice")
    try:
        amb.registerFederationSynchronizationPoint("ready", b"tag")
        # Registration is observably in effect (the announce arrives)...
        amb.wait_for("announceSynchronizationPoint", label="ready")
        # ...and the §4.12 ack callback fires at the registrant.
        amb.wait_for("synchronizationPointRegistrationSucceeded", label="ready")
        # §4.12 — the ack fires on the REGISTERING federate exactly once.
        assert amb.count("synchronizationPointRegistrationSucceeded") == 1, (
            "§4.12: the registration ack must fire exactly once"
        )
    finally:
        leave(amb)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc002_announce_reaches_all_federates(
    rtid_url: str, federation_name: str
) -> None:
    """§4.13 — every joined federate receives announceSynchronizationPoint."""
    alice = join(rtid_url, federation_name, "alice")
    bob = join(rtid_url, federation_name, "bob")
    try:
        alice.registerFederationSynchronizationPoint("phase1", b"")
        # §4.13 — announce reaches the registrant AND the other federate.
        alice.wait_for("announceSynchronizationPoint", label="phase1")
        bob.wait_for("announceSynchronizationPoint", label="phase1")
        # §4.13 — exactly once per federate.
        assert alice.count("announceSynchronizationPoint") == 1, (
            "§4.13: announce must fire exactly once at the registrant"
        )
        assert bob.count("announceSynchronizationPoint") == 1, (
            "§4.13: announce must fire exactly once at the peer"
        )
    finally:
        leave(bob)
        leave(alice)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc002_sync_tag_round_trips_bytes(rtid_url: str, federation_name: str) -> None:
    """§4.13 — the tag bytes on announce match the registration tag exactly."""
    tag = bytes(range(16)) + b"\x00\xff ivct \n"
    alice = join(rtid_url, federation_name, "alice")
    bob = join(rtid_url, federation_name, "bob")
    try:
        alice.registerFederationSynchronizationPoint("tagged", tag)
        got = bob.wait_for("announceSynchronizationPoint", label="tagged")
        # §4.13 — user-supplied tag is delivered byte-identical.
        assert got["tag"] == tag, "§4.13: announce tag must round-trip byte-identical"
    finally:
        leave(bob)
        leave(alice)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc002_federation_synchronized_fires_once(
    rtid_url: str, federation_name: str
) -> None:
    """§4.14+§4.15 — synchronized fires exactly once per label per federate,
    and only after ALL required federates have achieved."""
    alice = join(rtid_url, federation_name, "alice")
    bob = join(rtid_url, federation_name, "bob")
    try:
        alice.registerFederationSynchronizationPoint("gate", b"")
        alice.wait_for("announceSynchronizationPoint", label="gate")
        bob.wait_for("announceSynchronizationPoint", label="gate")

        alice.synchronizationPointAchieved("gate")
        # §4.15 — one achiever of two is NOT federation-synchronized.
        alice.assert_quiet("federationSynchronized")

        bob.synchronizationPointAchieved("gate")
        # §4.15 — all achieved → synchronized fires on every member.
        alice.wait_for("federationSynchronized", label="gate")
        bob.wait_for("federationSynchronized", label="gate")
        # §4.15 — exactly once per federate per label.
        assert alice.count("federationSynchronized") == 1, (
            "§4.15: federationSynchronized must fire exactly once (alice)"
        )
        assert bob.count("federationSynchronized") == 1, (
            "§4.15: federationSynchronized must fire exactly once (bob)"
        )
    finally:
        leave(bob)
        leave(alice)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc002_duplicate_label_registration_fails(
    rtid_url: str, federation_name: str
) -> None:
    """§4.11/§4.12 — re-registering a live label is a registration failure.

    pysdk surface note: the §4.12 Failed *callback* is not delivered
    (same _translate_event gap as the Succeeded ack); gorti instead
    rejects the RPC itself, which pysdk's cut-3 sync client translates
    to the typed ``SyncPointAlreadyExists`` exception — the observable
    equivalent asserted here.
    """
    from rti1516e._grpc_errors import SyncPointAlreadyExists

    alice = join(rtid_url, federation_name, "alice")
    try:
        alice.registerFederationSynchronizationPoint("once", b"")
        alice.wait_for("announceSynchronizationPoint", label="once")
        with pytest.raises(SyncPointAlreadyExists):
            # §4.11 — duplicate label must not register a second point.
            alice.registerFederationSynchronizationPoint("once", b"")
    finally:
        leave(alice)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc002_subset_sync_excludes_nonmembers(
    rtid_url: str, federation_name: str
) -> None:
    """§4.11 — a sync set restricted to one federate synchronizes without
    the excluded federate achieving (and without blocking on it)."""
    alice = join(rtid_url, federation_name, "alice")
    bob = join(rtid_url, federation_name, "bob")
    try:
        h_alice = federate_handle(alice)
        alice.registerFederationSynchronizationPoint(
            "solo", b"", sync_set=[h_alice]
        )
        alice.wait_for("announceSynchronizationPoint", label="solo")
        # §4.14+§4.15 — only the sync-set member needs to achieve; bob
        # (excluded) never achieves and must not gate synchronization.
        alice.synchronizationPointAchieved("solo")
        alice.wait_for("federationSynchronized", label="solo")
        assert alice.count("federationSynchronized") == 1
    finally:
        leave(bob)
        leave(alice)
