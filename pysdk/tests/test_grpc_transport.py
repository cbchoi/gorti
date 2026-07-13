"""Unit tests for ``rti1516e._transport.GrpcTransport``.

Focus: the parts of the GrpcTransport that DON'T require a running rtid
(name→handle resolution, payload coercion, mode mapping). The end-to-end
gRPC roundtrip is exercised by
``pysdk/tests/spec/m5/test_spec_m5_cross_language.py``.
"""

from __future__ import annotations

import asyncio
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]


def _ensure_generated_path() -> None:
    """Mirror the production helper so this module's imports of rti.v1
    succeed even when the SDK has not yet been touched in this process."""
    generated = REPO_ROOT / "pysdk" / "rti1516e" / "_generated"
    if generated.is_dir() and str(generated) not in sys.path:
        sys.path.insert(0, str(generated))


_ensure_generated_path()


def test_grpc_transport_constructs_against_a_dead_channel() -> None:
    """Smoke: GrpcTransport binds the four service stubs to a channel
    that is never opened. Useful as a guard against import-cycle
    regressions when the gRPC stubs are regenerated."""
    pytest.importorskip("grpc")
    from rti1516e._transport import GrpcTransport, build_grpc_transport

    async def _exercise() -> GrpcTransport:
        return await build_grpc_transport("grpc://127.0.0.1:1")  # bogus on purpose

    transport = asyncio.run(_exercise())
    assert transport.federation is not None
    assert transport.declaration is not None
    assert transport.objects is not None
    assert transport.streams is not None
    # Ensure close() is awaitable + does not raise even on a never-opened
    # channel (the cross-language smoke depends on this for cleanup).
    asyncio.run(transport.close())


def test_grpc_transport_payload_coercion_round_trip() -> None:
    """The wire payload coercion handles bytes / str / int / arbitrary
    objects deterministically. Used for the bridge's opaque payloads."""
    pytest.importorskip("grpc")
    from rti1516e._transport import _coerce_payload

    assert _coerce_payload(b"\x00\x01") == b"\x00\x01"
    assert _coerce_payload(bytearray(b"abc")) == b"abc"
    assert _coerce_payload("hello") == b"hello"
    # int → 4-byte BE (matches the bridge FOM's HLAinteger32BE).
    assert _coerce_payload(42) == b"\x00\x00\x00\x2a"
    # Anything else falls back to repr-encoded bytes for diagnostics.
    assert _coerce_payload([1, 2]) == b"[1, 2]"


def test_grpc_transport_mode_to_proto_enum_mapping() -> None:
    """``_mode_to_proto`` maps the FederationSpec.mode string to the
    proto enum value the gRPC server expects."""
    pytest.importorskip("grpc")
    from rti.v1 import common_pb2

    from rti1516e._transport import _mode_to_proto

    assert _mode_to_proto("verbose") == common_pb2.Mode.MODE_VERBOSE
    assert _mode_to_proto("best-effort") == common_pb2.Mode.MODE_BEST_EFFORT
    # Unknown modes default to verbose (fail-safe per the docstring).
    assert _mode_to_proto("totally-made-up") == common_pb2.Mode.MODE_VERBOSE


def test_grpc_transport_populates_handle_tables_from_bridge_fom() -> None:
    """Loading the pyjevsim-bridge FOM yields the same handle ordering
    the rtid uses (sorted-by-name; 1-based). This is the contract that
    lets the cross-language smoke send_interaction("ProducerOutput")
    and have rtid see the right interaction-class handle."""
    pytest.importorskip("grpc")

    from rti1516e._transport import GrpcTransport, build_grpc_transport

    fom_path = (
        REPO_ROOT
        / "tests"
        / "conformance"
        / "foms"
        / "good"
        / "pyjevsim-bridge.xml"
    )

    async def _exercise() -> GrpcTransport:
        return await build_grpc_transport("grpc://127.0.0.1:1")

    transport = asyncio.run(_exercise())
    transport._populate_handle_tables([str(fom_path)])  # noqa: SLF001
    # After MIM merge + sort-by-name the bridge FOM produces handles in
    # alphabetical order. ConsumerAck precedes the HLA* MOM/MIM
    # interactions (capital C < capital H), so ConsumerAck always
    # lands at handle 1; ProducerOutput lands AFTER all the HLA* names.
    #
    # NB: there is currently a known *parity gap* between the Python
    # SDK's MIM merge and the Go-side mim.Merge — the two produce
    # handles that align for the FOM-only-defined classes (ConsumerAck,
    # ProducerOutput) but at DIFFERENT absolute positions, because the
    # two MIM corpora include slightly different sets of HLA*
    # interactions. The cross-language smoke survives this gap because
    # the test runs Python-only federates: both ends agree on the
    # Python-side handle. A Python+Go bidirectional smoke would expose
    # the gap; that is tracked as a deferral in
    # examples/pyjevsim/cross_lang_runner.py's docstring.
    handles = transport._interaction_handles  # noqa: SLF001
    assert handles.get("ConsumerAck") == 1
    # ProducerOutput is far down the sorted list; assert presence + that
    # the lookup helper returns the same value as the table.
    producer_handle = handles.get("ProducerOutput")
    assert producer_handle is not None and producer_handle > 1
    assert (
        transport._interaction_handle_for("ProducerOutput") == producer_handle  # noqa: SLF001
    )
    # Unknown class names return 0 (cut-1 fail-soft contract).
    assert transport._interaction_handle_for("NonExistent") == 0  # noqa: SLF001
    asyncio.run(transport.close())


def test_grpc_transport_maps_declared_interaction_parameters() -> None:
    """Callback translation uses FOM parameter names, not the bridge fallback."""
    pytest.importorskip("grpc")

    from rti1516e._transport import GrpcTransport, build_grpc_transport

    fom_path = REPO_ROOT / "examples" / "pitch-chat-parity" / "federation.fom.xml"

    async def _exercise() -> GrpcTransport:
        return await build_grpc_transport("grpc://127.0.0.1:1")

    transport = asyncio.run(_exercise())
    transport._populate_handle_tables([str(fom_path)])  # noqa: SLF001
    assert transport._parameter_indices_for("Communication") == {  # noqa: SLF001
        "Message": 1,
        "Sender": 2,
    }
    assert transport._parameter_indices_for("NotInFom") == {  # noqa: SLF001
        "_payload": 1
    }
    asyncio.run(transport.close())


def test_grpc_transport_maps_bridge_payload_to_single_declared_parameter() -> None:
    """The pyjevsim opaque alias is accepted only for a one-parameter class."""
    pytest.importorskip("grpc")

    from rti1516e._transport import GrpcTransport, build_grpc_transport

    fom_path = REPO_ROOT / "examples" / "pyjevsim" / "pyjevsim-fom.xml"

    async def _exercise() -> GrpcTransport:
        return await build_grpc_transport("grpc://127.0.0.1:1")

    transport = asyncio.run(_exercise())
    transport._populate_handle_tables([str(fom_path)])  # noqa: SLF001
    assert transport._parameter_indices_for("ProducerOutput") == {  # noqa: SLF001
        "seq": 1
    }
    asyncio.run(transport.close())


def test_grpc_transport_unknown_method_raises_not_implemented() -> None:
    """``record(method=...)`` raises NotImplementedError for methods we
    haven't wired in cut-1. This protects against silent drops if the
    SDK's higher layers grow new RPC dispatch points."""
    pytest.importorskip("grpc")

    from rti1516e._transport import build_grpc_transport

    async def _exercise() -> None:
        transport = await build_grpc_transport("grpc://127.0.0.1:1")
        try:
            with pytest.raises(NotImplementedError):
                await transport.record("totally_made_up_method")
        finally:
            await transport.close()

    asyncio.run(_exercise())


def test_grpc_transport_direct_event_sink_and_queue_fallback() -> None:
    pytest.importorskip("grpc")

    from rti1516e._transport import build_grpc_transport

    async def _exercise() -> None:
        transport = await build_grpc_transport("grpc://127.0.0.1:1")
        try:
            queue = transport.events_for(7)
            transport._deliver_event(7, "queued")  # noqa: SLF001
            assert await queue.get() == "queued"

            received: list[object] = []
            transport.set_event_sink(7, received.append)
            transport._deliver_event(7, "direct")  # noqa: SLF001
            assert received == ["direct"]
            assert queue.empty()

            transport.set_event_sink(7, None)
            transport._deliver_event(7, "queued-again")  # noqa: SLF001
            assert await queue.get() == "queued-again"
        finally:
            await transport.close()

    asyncio.run(_exercise())
