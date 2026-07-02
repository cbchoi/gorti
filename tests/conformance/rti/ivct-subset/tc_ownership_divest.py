"""IVCT-inspired TC-015 analogue — attribute ownership divest / acquire.

Spec-anchored assertions (IEEE 1516.1-2010):

- §7.3/§7.5 negotiated divestiture — the offer reaches acquisition
        candidates (``requestAttributeOwnershipAssumption``); an engaged
        acquirer completes the transfer with
        ``attributeOwnershipAcquisitionNotification``.
- §7.6  ``confirmDivestiture`` (M37 real two-phase) — with the M37
        ``two_phase`` flag the transfer parks on
        ``requestDivestitureConfirmation`` and completes ONLY after the
        divester's ConfirmDivestiture.
- §7.8  ``attributeOwnershipAcquisitionIfAvailable`` — owned attributes
        are NOT granted and NO pending acquire is queued.
- §7.10 ``attributeOwnershipUnavailable`` — the losing acquirer is told.
- §7.11 ``requestAttributeOwnershipRelease`` — a plain acquire against
        owned attributes asks the owner to release.
- §7.12 ``attributeOwnershipDivestitureIfWanted`` — transfers only if a
        wanting acquirer is queued; NO-OP otherwise.
- §7.15/§7.17 ownership queries — ``queryAttributeOwnership`` /
        ``isAttributeOwnedByFederate`` report the current owner.

pysdk surface notes:

- ``OwnershipClient`` never sets the M37 request flags
  (``NegotiatedDivestRequest.two_phase``, ``AcquireRequest.if_available``)
  and has no ``ConfirmDivestiture`` wrapper — those flows are driven via
  the raw ``rti.v1.OwnershipService`` stub (_driver helpers, each naming
  the gap).
- The M37 wire events ``ownership_unavailable`` (stream.proto tag 34)
  and ``ownership_release_requested`` (tag 33) are DROPPED by pysdk's
  ``_transport._translate_event`` — the §7.10/§7.11 callback assertions
  xfail with that named gap; the state-level outcomes are asserted
  instead where observable.
"""

from __future__ import annotations

import pytest

from _driver import (
    federate_handle,
    join,
    leave,
    raw_acquire_if_available,
    raw_channel,
    raw_confirm_divestiture,
    raw_negotiated_divest_two_phase,
    raw_ownership_stub,
)

VEHICLE_ATTRS = ["Position", "Velocity"]


def _setup_owned_instance(pub, sub):  # type: ignore[no-untyped-def]
    """pub publishes+registers a Vehicle; sub subscribes+publishes (making
    it an acquisition candidate) and discovers. Returns (obj, pos_handle)."""
    vehicle = pub.getObjectClassHandle("Vehicle")
    pos = pub.getAttributeHandle(vehicle, "Position")
    sub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
    # §7 — only publishers of the attributes are acquisition candidates.
    sub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
    pub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
    obj = pub.registerObjectInstance("Vehicle")
    sub.wait_for("discoverObjectInstance", object_handle=int(obj))
    return obj, pos


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc015_acquire_after_unconditional_divest_notifies(
    rtid_url: str, federation_name: str
) -> None:
    """§7.4 + §7.5 — after the owner divests unconditionally, an acquirer
    of the now-unowned attributes receives
    attributeOwnershipAcquisitionNotification and becomes the owner.

    gorti semantics note: UnconditionalDivest only marks the attributes
    unowned — a *previously queued* plain acquire is NOT completed by it
    (rti/internal/ownership/manager.go processes pending acquires at
    divest-negotiation time only), so this test acquires AFTER the
    divest, which is the §7.8 unowned-attribute fast path.
    """
    pub = join(rtid_url, federation_name, "owner")
    sub = join(rtid_url, federation_name, "acquirer")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        # §7.15 — the registrant owns its published attributes initially.
        owner, owned = pub.queryAttributeOwnership(obj, pos)
        assert owned and owner == federate_handle(pub), (
            "§7.15: the registrant must initially own its published attributes"
        )

        pub.unconditionalAttributeOwnershipDivestiture(obj, [int(pos)])  # §7.4
        sub.attributeOwnershipAcquisition(obj, [int(pos)], b"want")  # §7.8 unowned

        got = sub.wait_for(
            "attributeOwnershipAcquisitionNotification", object_handle=int(obj)
        )
        # §7.5 — the notification names the transferred attributes.
        assert int(pos) in got["attribute_handles"], (
            "§7.5: acquisition notification must list the transferred attribute"
        )
        # §7.15 — the RTI now reports the acquirer as owner.
        owner, owned = sub.queryAttributeOwnership(obj, pos)
        assert owned and owner == federate_handle(sub), (
            "§7.15: post-transfer owner must be the acquirer"
        )
        assert sub.isAttributeOwnedByFederate(obj, pos) is True  # §7.17
        assert pub.isAttributeOwnedByFederate(obj, pos) is False  # §7.17
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc015_negotiated_divest_two_phase_confirmation_flow(
    rtid_url: str, federation_name: str
) -> None:
    """§7.3 + §7.6 (M37 real two-phase) — negotiated divest parks until
    confirmDivestiture: offer → assumption request at the candidate →
    acquire → requestDivestitureConfirmation at the divester → confirm →
    acquisition notification + ownership transfer.

    Driven via the raw OwnershipService stub: pysdk's OwnershipClient
    exposes neither NegotiatedDivestRequest.two_phase nor the
    ConfirmDivestiture RPC (named pysdk gaps, see _driver helpers).
    """
    pub = join(rtid_url, federation_name, "divester")
    sub = join(rtid_url, federation_name, "assumer")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        h_pub = federate_handle(pub)

        with raw_channel(rtid_url) as ch:
            stub = raw_ownership_stub(ch)
            raw_negotiated_divest_two_phase(
                stub, federation_name, h_pub, int(obj), [int(pos)], b"offer"
            )
            # §7.3 — the offer is announced to the acquisition candidate.
            got = sub.wait_for(
                "requestAttributeOwnershipAssumption", object_handle=int(obj)
            )
            assert int(pos) in got["attribute_handles"]
            assert got["divesting_federate"] == h_pub, (
                "§7.3: the assumption request must name the divesting federate"
            )

            # §7.5 — candidate engages.
            sub.attributeOwnershipAcquisition(obj, [int(pos)], b"take")
            # §7.6 (two-phase) — divester is asked to confirm...
            pub.wait_for("requestDivestitureConfirmation", object_handle=int(obj))
            # ...and the transfer has NOT completed yet.
            owner, owned = pub.queryAttributeOwnership(obj, pos)
            assert owned and owner == h_pub, (
                "§7.6: two-phase transfer must park until confirmDivestiture"
            )
            sub.assert_quiet("attributeOwnershipAcquisitionNotification")

            # §7.6 — confirm completes the transfer atomically.
            raw_confirm_divestiture(stub, federation_name, h_pub, int(obj), [int(pos)])
            sub.wait_for(
                "attributeOwnershipAcquisitionNotification", object_handle=int(obj)
            )
            owner, owned = sub.queryAttributeOwnership(obj, pos)
            assert owned and owner == federate_handle(sub), (
                "§7.6: post-confirm owner must be the assumer"
            )
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc015_acquire_if_available_on_owned_attrs_no_transfer(
    rtid_url: str, federation_name: str
) -> None:
    """§7.8 — acquireIfAvailable against currently-owned attributes grants
    nothing AND queues no pending acquire: a later divestitureIfWanted by
    the owner finds no wanting acquirer and keeps ownership."""
    pub = join(rtid_url, federation_name, "owner")
    sub = join(rtid_url, federation_name, "grabber")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        h_pub = federate_handle(pub)

        with raw_channel(rtid_url) as ch:
            raw_acquire_if_available(
                raw_ownership_stub(ch),
                federation_name,
                federate_handle(sub),
                int(obj),
                [int(pos)],
            )
        # §7.8 — no transfer happened...
        sub.assert_quiet("attributeOwnershipAcquisitionNotification")
        owner, owned = pub.queryAttributeOwnership(obj, pos)
        assert owned and owner == h_pub, (
            "§7.8: ifAvailable against owned attrs must not transfer"
        )
        # ...and no pending acquire was queued: §7.12 divestitureIfWanted
        # finds no wanting acquirer → ownership stays put.
        pub.attributeOwnershipDivestitureIfWanted(obj, [int(pos)])
        owner, owned = pub.queryAttributeOwnership(obj, pos)
        assert owned and owner == h_pub, (
            "§7.8: ifAvailable must NOT queue a pending acquire "
            "(divestitureIfWanted found a wanting acquirer)"
        )
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc015_acquire_if_available_unavailable_callback(
    rtid_url: str, federation_name: str
) -> None:
    """§7.10 — the losing acquirer receives attributeOwnershipUnavailable.

    xfail: the M37 wire event exists (stream.proto ownership_unavailable,
    tag 34) but pysdk's ``_transport._translate_event`` has no branch for
    it, so no Layer-2 callback can ever observe §7.10.
    """
    pub = join(rtid_url, federation_name, "owner")
    sub = join(rtid_url, federation_name, "loser")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        with raw_channel(rtid_url) as ch:
            raw_acquire_if_available(
                raw_ownership_stub(ch),
                federation_name,
                federate_handle(sub),
                int(obj),
                [int(pos)],
            )
        pytest.xfail(
            "pysdk gap: _transport._translate_event has no branch for "
            "stream.proto ownership_unavailable (tag 34) — the §7.10 "
            "attributeOwnershipUnavailable callback never reaches Layer 2"
        )
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc015_plain_acquire_requests_release_from_owner(
    rtid_url: str, federation_name: str
) -> None:
    """§7.11 — a plain acquisition against owned attributes asks the owner
    to release (requestAttributeOwnershipRelease at the owner).

    xfail: the M37 wire event exists (stream.proto
    ownership_release_requested, tag 33) but pysdk's
    ``_transport._translate_event`` drops it — no Layer-2 callback.
    """
    pub = join(rtid_url, federation_name, "owner")
    sub = join(rtid_url, federation_name, "asker")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        sub.attributeOwnershipAcquisition(obj, [int(pos)], b"please")
        pytest.xfail(
            "pysdk gap: _transport._translate_event has no branch for "
            "stream.proto ownership_release_requested (tag 33) — the §7.11 "
            "requestAttributeOwnershipRelease callback never reaches Layer 2"
        )
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc015_divest_if_wanted_noop_without_assumer(
    rtid_url: str, federation_name: str
) -> None:
    """§7.12 — divestitureIfWanted with no wanting acquirer transfers
    nothing; the caller remains owner. With a queued acquirer it
    transfers atomically."""
    pub = join(rtid_url, federation_name, "owner")
    sub = join(rtid_url, federation_name, "wanter")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        h_pub = federate_handle(pub)

        # §7.12 — no wanting acquirer → NO-OP.
        pub.attributeOwnershipDivestitureIfWanted(obj, [int(pos)])
        owner, owned = pub.queryAttributeOwnership(obj, pos)
        assert owned and owner == h_pub, (
            "§7.12: divestitureIfWanted without an acquirer must be a no-op"
        )

        # §7.12 — with a queued acquirer → transfer.
        sub.attributeOwnershipAcquisition(obj, [int(pos)], b"want")
        pub.attributeOwnershipDivestitureIfWanted(obj, [int(pos)])
        sub.wait_for(
            "attributeOwnershipAcquisitionNotification", object_handle=int(obj)
        )
        owner, owned = sub.queryAttributeOwnership(obj, pos)
        assert owned and owner == federate_handle(sub), (
            "§7.12: divestitureIfWanted with a queued acquirer must transfer"
        )
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc015_query_ownership_reports_unowned_after_divest(
    rtid_url: str, federation_name: str
) -> None:
    """§7.15 — queryAttributeOwnership reports owned=False once the owner
    unconditionally divests with no acquirer queued."""
    pub = join(rtid_url, federation_name, "owner")
    sub = join(rtid_url, federation_name, "watcher")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        pub.unconditionalAttributeOwnershipDivestiture(obj, [int(pos)])
        owner, owned = sub.queryAttributeOwnership(obj, pos)
        # §7.15 — unowned attributes report no owner.
        assert owned is False, (
            "§7.15: after unconditional divest with no acquirer the attribute "
            "must be unowned"
        )
        assert owner == 0, "§7.15: unowned attributes must report owner handle 0"
    finally:
        leave(sub)
        leave(pub)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc015_ownership_transfer_reflects_on_updates(
    rtid_url: str, federation_name: str
) -> None:
    """§7.2 post-condition — after a transfer, the NEW owner's updates
    flow to subscribers; the OLD owner's update is rejected
    AttributeNotOwned.

    Hard assertions on both halves since M38: update ingestion consults
    per-instance §7 ownership (rti/internal/object/update.go — the
    Options.Ownership gate over ownership.Manager.IsOwnedBy) in
    addition to the class-level §5 publication check, so the divested
    old owner's update raises with "attribute not owned by federate"
    (pysdk: OwnershipNotPermitted via PERMISSION_DENIED).
    """
    pub = join(rtid_url, federation_name, "old-owner")
    sub = join(rtid_url, federation_name, "new-owner")
    try:
        obj, pos = _setup_owned_instance(pub, sub)

        # Divest-then-acquire (gorti's unconditional path does not match
        # previously queued acquires; see the first test's semantics note).
        pub.unconditionalAttributeOwnershipDivestiture(obj, [int(pos)])
        sub.attributeOwnershipAcquisition(obj, [int(pos)], b"take")
        sub.wait_for(
            "attributeOwnershipAcquisitionNotification", object_handle=int(obj)
        )

        # §7.2 — the new owner's update succeeds and reaches subscribers.
        pub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
        sub.updateAttributeValues(obj, {int(pos): b"\x22" * 8})
        pub.wait_for("reflectAttributeValues", object_handle=int(obj))

        # §7.2 — the old owner may no longer update the attribute.
        with pytest.raises(Exception, match="(?i)not owned"):
            pub.updateAttributeValues(obj, {int(pos): b"\x00" * 8})
    finally:
        leave(sub)
        leave(pub)
