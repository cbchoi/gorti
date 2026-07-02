"""IVCT-inspired TC-001 analogue — federation create/join/resign lifecycle.

Spec-anchored assertions (IEEE 1516.1-2010):

- §4.5  ``createFederationExecution`` — federation exists after the call;
        second create raises ``FederationExecutionAlreadyExists``.
- §4.9  ``joinFederationExecution`` — joining a non-existent federation
        raises ``FederationExecutionDoesNotExist``; joining a live one
        returns a monotone-increasing FederateHandle.
- §4.10 ``resignFederationExecution`` — post-resign calls that require
        membership raise ``FederateNotExecutionMember``.
- §4.6  ``destroyFederationExecution`` — succeeds only when zero
        federates remain joined; otherwise raises
        ``FederatesCurrentlyJoined``.

pysdk surface notes (why some tests drive the raw wire):

- Since M39, Layer 2 exposes ``createFederationExecution`` (eager,
  typed ``FederationExecutionAlreadyExists``) and
  ``destroyFederationExecution`` (typed ``FederatesCurrentlyJoined`` /
  ``FederationExecutionDoesNotExist``) — the pysdk-level halves of
  §4.5/§4.6 are covered in pysdk/tests/spec/m39/test_m39_layer2_api.py.
  The tests below intentionally stay on the raw
  ``rti.v1.FederationService`` stubs to pin the WIRE-level contract
  (gRPC status codes per rti/internal/transport/grpc/errs.go),
  independent of any SDK translation layer. §4.9 join-nonexistent is
  raw-only by necessity: pysdk Layer 1 always creates before joining.
- gorti reports these failures as gRPC status codes (ALREADY_EXISTS /
  NOT_FOUND / FAILED_PRECONDITION); each assertion notes the 1516 name.
"""

from __future__ import annotations

import grpc
import pytest

from _driver import (
    federate_handle,
    join,
    leave,
    raw_channel,
    raw_create,
    raw_destroy,
    raw_federation_stub,
    raw_join,
    raw_resign,
)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc001_create_join_resign_happy_path(rtid_url: str, federation_name: str) -> None:
    """Happy-path lifecycle: create → join → resign → destroy."""
    amb = join(rtid_url, federation_name, "alice")
    try:
        # §4.9 — join yields a valid (>0) federate handle.
        assert federate_handle(amb) > 0, "§4.9: join must assign a federate handle"
    finally:
        leave(amb)  # §4.10 — resign

    # §4.6 — destroy succeeds once zero federates remain joined.
    with raw_channel(rtid_url) as ch:
        raw_destroy(raw_federation_stub(ch), federation_name)  # raises on failure

        # Post-destroy the federation is gone: §4.9 join now fails
        # FederationExecutionDoesNotExist (gorti: gRPC NOT_FOUND).
        with pytest.raises(grpc.RpcError) as exc_info:
            raw_join(raw_federation_stub(ch), federation_name, "late-joiner")
        assert exc_info.value.code() == grpc.StatusCode.NOT_FOUND, (
            "§4.6+§4.9: destroyed federation must not accept joins"
        )


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc001_join_nonexistent_federation_raises(
    rtid_url: str, federation_name: str
) -> None:
    """§4.9 — join before create raises FederationExecutionDoesNotExist.

    Driven via the raw FederationService stub: pysdk Layer 1 always
    issues CreateFederation before JoinFederation, so the ambassador can
    never observe a join against a non-existent federation.
    """
    with raw_channel(rtid_url) as ch:
        with pytest.raises(grpc.RpcError) as exc_info:
            raw_join(raw_federation_stub(ch), federation_name, "orphan")
        # gorti maps ErrFederationNotFound → NOT_FOUND (errs.go); the
        # 1516 name for this failure is FederationExecutionDoesNotExist.
        assert exc_info.value.code() == grpc.StatusCode.NOT_FOUND, (
            "§4.9: joining a non-existent federation must fail "
            "FederationExecutionDoesNotExist"
        )


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc001_double_create_raises(rtid_url: str, federation_name: str) -> None:
    """§4.5 — second create raises FederationExecutionAlreadyExists.

    Raw-stub test: pysdk Layer 1 swallows ALREADY_EXISTS by design
    (idempotent create-on-join), so the duplicate-create failure is only
    observable on the wire.
    """
    with raw_channel(rtid_url) as ch:
        stub = raw_federation_stub(ch)
        raw_create(stub, federation_name)  # first create succeeds
        with pytest.raises(grpc.RpcError) as exc_info:
            raw_create(stub, federation_name)
        # gorti maps ErrFederationAlreadyExists → ALREADY_EXISTS; 1516
        # name: FederationExecutionAlreadyExists.
        assert exc_info.value.code() == grpc.StatusCode.ALREADY_EXISTS, (
            "§4.5: duplicate create must fail FederationExecutionAlreadyExists"
        )
        raw_destroy(stub, federation_name)  # cleanup


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc001_destroy_with_joined_federates_raises(
    rtid_url: str, federation_name: str
) -> None:
    """§4.6 — destroy while a federate is joined raises FederatesCurrentlyJoined."""
    amb = join(rtid_url, federation_name, "alice")
    try:
        with raw_channel(rtid_url) as ch:
            stub = raw_federation_stub(ch)
            with pytest.raises(grpc.RpcError) as exc_info:
                raw_destroy(stub, federation_name)
            # gorti maps ErrFederationHasFederatesJoined →
            # FAILED_PRECONDITION; 1516 name: FederatesCurrentlyJoined.
            assert exc_info.value.code() == grpc.StatusCode.FAILED_PRECONDITION, (
                "§4.6: destroy with a joined federate must fail "
                "FederatesCurrentlyJoined"
            )
    finally:
        leave(amb)
    # §4.6 — and once the federate resigned, the same destroy succeeds.
    with raw_channel(rtid_url) as ch:
        raw_destroy(raw_federation_stub(ch), federation_name)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc001_federate_handles_monotone_increasing(
    rtid_url: str, federation_name: str
) -> None:
    """§4.9 — successive joins yield strictly increasing federate handles,
    and a resigned federate's handle is not immediately reused."""
    alice = join(rtid_url, federation_name, "alice")
    bob = join(rtid_url, federation_name, "bob")
    try:
        h_alice = federate_handle(alice)
        h_bob = federate_handle(bob)
        # §4.9 — join order determines handle order.
        assert h_alice < h_bob, "§4.9: join-order handles must be increasing"
    finally:
        leave(alice)
    # alice resigned; a fresh joiner must NOT be handed alice's handle back.
    carol = join(rtid_url, federation_name, "carol")
    try:
        h_carol = federate_handle(carol)
        assert h_carol > h_bob, (
            "§4.9: handles must stay monotone after a resign (no reuse)"
        )
    finally:
        leave(carol)
        leave(bob)


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc001_post_resign_calls_rejected(rtid_url: str, federation_name: str) -> None:
    """§4.10 — after resign, membership-requiring calls fail
    FederateNotExecutionMember (gorti: ERR_FED_NOT_JOINED on the wire)."""
    amb = join(rtid_url, federation_name, "alice")
    handle = federate_handle(amb)
    leave(amb)

    with raw_channel(rtid_url) as ch:
        stub = raw_federation_stub(ch)
        with pytest.raises(grpc.RpcError) as exc_info:
            raw_resign(stub, federation_name, handle)  # second resign, same handle
        # The 1516 name is FederateNotExecutionMember; gorti surfaces
        # ErrFederationNotJoined. Accept the two codes gorti's errs.go
        # can map membership errors to.
        assert exc_info.value.code() in (
            grpc.StatusCode.FAILED_PRECONDITION,
            grpc.StatusCode.NOT_FOUND,
        ), (
            "§4.10: a resigned federate handle must be rejected "
            f"(FederateNotExecutionMember); got {exc_info.value.code()}"
        )
