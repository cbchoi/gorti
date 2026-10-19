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


def test_grpc_transport_close_propagates_channel_failure() -> None:
    from rti1516e._transport import GrpcTransport

    class FailingChannel:
        async def close(self) -> None:
            raise RuntimeError("channel close failed")

    transport = object.__new__(GrpcTransport)
    transport._stream_tasks = {}  # noqa: SLF001
    transport._event_sinks = {}  # noqa: SLF001
    transport.channel = FailingChannel()

    with pytest.raises(RuntimeError, match="channel close failed"):
        asyncio.run(transport.close())


def test_grpc_transport_close_drains_channel_after_failed_stream_task() -> None:
    from rti1516e._transport import GrpcTransport

    class Channel:
        def __init__(self) -> None:
            self.close_calls = 0

        async def close(self) -> None:
            self.close_calls += 1

    async def exercise() -> None:
        async def failed_stream() -> None:
            raise RuntimeError("stream failed before close")

        stream = asyncio.create_task(failed_stream())
        await asyncio.gather(stream, return_exceptions=True)
        channel = Channel()
        transport = object.__new__(GrpcTransport)
        transport._stream_tasks = {7: stream}  # noqa: SLF001
        transport._event_sinks = {}  # noqa: SLF001
        transport.channel = channel

        with pytest.raises(RuntimeError, match="stream failed before close"):
            await transport.close()
        assert channel.close_calls == 1
        assert transport._stream_tasks == {}  # noqa: SLF001
        await transport.close()
        assert channel.close_calls == 2

    asyncio.run(exercise())


def test_grpc_transport_resign_propagates_rpc_failure() -> None:
    pytest.importorskip("grpc")
    from rti1516e._transport import GrpcTransport

    class FailingFederation:
        async def ResignFederation(self, request: object) -> None:  # noqa: N802
            del request
            raise RuntimeError("resign rpc failed")

    transport = object.__new__(GrpcTransport)
    transport._federation_name = "demo"  # noqa: SLF001
    transport._stream_tasks = {}  # noqa: SLF001
    transport.federation = FailingFederation()

    with pytest.raises(RuntimeError, match="resign rpc failed"):
        asyncio.run(transport._resign_federation(7))  # noqa: SLF001


@pytest.mark.asyncio
@pytest.mark.parametrize("rpc_fails", [False, True])
async def test_grpc_transport_resign_sends_session_metadata(rpc_fails):
    from rti.v1 import common_pb2

    from rti1516e._transport import GrpcTransport

    calls = []
    failure = RuntimeError("resign rejected")

    class Federation:
        async def ResignFederation(self, request, *, metadata=()):  # noqa: N802
            calls.append((request, metadata))
            if rpc_fails:
                raise failure

    transport = object.__new__(GrpcTransport)
    transport._federation_name = "demo"  # noqa: SLF001
    transport._stream_tasks = {}  # noqa: SLF001
    transport._session_tokens = {7: b"t" * 32, 9: b"other" * 6}  # noqa: SLF001
    transport.federation = Federation()
    if rpc_fails:
        with pytest.raises(RuntimeError) as caught:
            await transport._resign_federation(7, "NO_ACTION")  # noqa: SLF001
        assert caught.value is failure
        assert transport.session_token(7) == b"t" * 32
    else:
        await transport._resign_federation(7, "NO_ACTION")  # noqa: SLF001
        assert transport.session_token(7) == b""
    assert transport.session_token(9) == b"other" * 6
    assert len(calls) == 1
    request, metadata = calls[0]
    assert metadata == (("gorti-session-token-bin", b"t" * 32),)
    assert request.federation_name == "demo"
    assert request.federate_handle == 7
    assert request.action == common_pb2.RESIGN_ACTION_NO_ACTION
    assert "session_token" not in request.DESCRIPTOR.fields_by_name


@pytest.mark.asyncio
@pytest.mark.parametrize("token", [None, b""])
async def test_grpc_transport_resign_legacy_without_session_metadata(token):
    from rti1516e._transport import GrpcTransport

    calls = []

    class LegacyFederation:
        async def ResignFederation(self, request):  # noqa: N802
            calls.append(request)

    transport = object.__new__(GrpcTransport)
    transport._federation_name = "demo"  # noqa: SLF001
    transport._stream_tasks = {}  # noqa: SLF001
    if token is not None:
        transport._session_tokens = {7: token}  # noqa: SLF001
    transport.federation = LegacyFederation()
    await transport._resign_federation(7)  # noqa: SLF001
    assert len(calls) == 1
    assert calls[0].federate_handle == 7


def test_build_failure_closes_channel_before_propagating(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    grpc = pytest.importorskip("grpc")
    import rti1516e._transport as transport_module

    class RetainedChannel:
        def __init__(self) -> None:
            self.close_calls = 0

        async def close(self) -> None:
            self.close_calls += 1

    channel = RetainedChannel()
    monkeypatch.setattr(grpc.aio, "insecure_channel", lambda target: channel)

    class BrokenTransport:
        def __init__(self, candidate: object, *, url: str) -> None:
            assert candidate is channel
            del url
            raise RuntimeError("stub construction failed")

    monkeypatch.setattr(transport_module, "GrpcTransport", BrokenTransport)
    with pytest.raises(RuntimeError, match="stub construction failed"):
        asyncio.run(transport_module.build_grpc_transport("grpc://broken"))
    assert channel.close_calls == 1


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

    fom_path = REPO_ROOT / "examples" / "ieee1516e-chat-parity" / "federation.fom.xml"

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
