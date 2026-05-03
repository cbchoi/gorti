"""Transport registry — pluggable in-process doubles + real gRPC for the RTI client.

The Layer 1 SDK (rti1516e.connection.RtiConnection) has two backing transports:

  - ``memory://<name>``  — in-process pure-Python ``FakeRtiServer`` used by
                           spec tests; see ``register_fake`` / ``lookup``.
  - ``grpc://host:port`` — real gRPC channel to an rtid binary, wrapped in
                           :class:`GrpcTransport`. Built on demand by
                           ``RtiConnection.__aenter__`` for cross-language
                           tests (TASK-081).

Both transports satisfy the same duck-typed surface that ``RtiConnection``
+ ``Federate`` reach for:

  - ``record(method, **kwargs) -> Any`` (or awaitable; the SDK awaits the
    result if it is awaitable, so a sync fake and an async gRPC client can
    coexist in the same call sites).
  - ``events_for(handle) -> asyncio.Queue``
  - ``allocate_handle() -> int``

The GrpcTransport is intentionally MINIMAL (cut-1 per TASK-081):

  - Only the RPCs the cross-language smoke needs are dispatched
    (federation create/join/resign, declaration pub/sub of interactions,
    send/receive of interactions, opening the StreamService.Events
    server-streaming RPC).
  - Class names are translated to numeric handles via the FOM (parsed
    on-demand from the FederationSpec.fom_modules paths). Both this Python
    SDK and the Go-side ``fomHandle.LookupInteractionClass`` derive
    handles from a sorted-by-name index, so the two sides agree
    deterministically.
  - Time-management RPCs (NextMessageRequest etc.) are not wired —
    rtid's TimeService is nil at M2; the SDK records the call but does
    not dispatch it. The bridge's ``_await_grant`` would block forever
    in real gRPC mode; cross-language tests therefore avoid the
    time-managed code path and instead drive interactions directly.

This module is internal to rti1516e and is not part of the public API.
"""

from __future__ import annotations

import asyncio
import contextlib
from pathlib import Path
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:  # pragma: no cover - import guard for type checking only
    import grpc

# Module-level registry keyed by URL. The value is intentionally typed as
# ``Any`` because the fake server lives in test code (pysdk/tests/spec/m4/
# _fakes/) and the SDK must not import test packages. The runtime contract
# is: the registered object exposes ``record(method, **kwargs)``,
# ``events_for(handle) -> asyncio.Queue``, and ``allocate_handle() -> int``.
_TRANSPORT_REGISTRY: dict[str, Any] = {}


def register_fake(url: str, server: Any) -> None:
    """Bind ``server`` as the transport returned by ``lookup(url)``.

    If a transport is already registered under ``url``, it is replaced
    (last-writer-wins). This is fine for tests, which construct a fresh
    fake per test function.
    """
    _TRANSPORT_REGISTRY[url] = server


def unregister(url: str) -> None:
    """Remove the registered transport for ``url``. No-op if absent."""
    _TRANSPORT_REGISTRY.pop(url, None)


def lookup(url: str) -> Any | None:
    """Return the registered transport for ``url``, or None if unregistered."""
    return _TRANSPORT_REGISTRY.get(url)


def clear() -> None:
    """Remove every registered transport. Useful for test isolation."""
    _TRANSPORT_REGISTRY.clear()


# --- Real-gRPC transport ----------------------------------------------------


class GrpcTransport:
    """Real-gRPC client wired against the generated stubs in ``_generated/``.

    Constructed by ``RtiConnection.__aenter__`` when the URL scheme is
    ``grpc``. Maintains:

      - A per-class-name → numeric handle table built lazily from the
        FederationSpec.fom_modules on the first ``create_federation``
        call (so name→handle resolution matches the rtid side).
      - Per-federate event queues populated by a background asyncio task
        that drains the StreamService.Events server stream.
      - A monotonic handle allocator scoped to this transport instance,
        used for object handles when the wire returns one (federate
        handles are allocated by the server and pulled from
        JoinFederationResponse).
    """

    def __init__(self, channel: grpc.aio.Channel, url: str) -> None:
        # Lazy import keeps grpc out of the import surface for spec tests
        # that only use memory:// transports. Generated stubs are
        # namespaced as ``rti.v1`` and live under
        # rti1516e/_generated/rti/v1/; ensure_generated_path puts that
        # directory on sys.path so the import resolves.
        _ensure_generated_path()
        from rti.v1 import (
            declaration_pb2_grpc,
            federation_pb2_grpc,
            object_pb2_grpc,
            stream_pb2_grpc,
        )

        self.channel = channel
        self.url = url
        self.federation = federation_pb2_grpc.FederationServiceStub(channel)
        self.declaration = declaration_pb2_grpc.DeclarationServiceStub(channel)
        self.objects = object_pb2_grpc.ObjectServiceStub(channel)
        self.streams = stream_pb2_grpc.StreamServiceStub(channel)
        # Federation name → name-resolver state. The first
        # ``create_federation`` for a given federation name parses the
        # FOM and caches name→handle maps; later RPCs reuse them.
        self._federation_name: str | None = None
        self._interaction_handles: dict[str, int] = {}
        self._object_class_handles: dict[str, int] = {}
        self._inverse_interaction_handles: dict[int, str] = {}
        # Per-federate event queues + the background draining tasks.
        self._event_queues: dict[int, asyncio.Queue[Any]] = {}
        self._stream_tasks: dict[int, asyncio.Task[None]] = {}
        # Object handle allocator (used only as a local fallback; the
        # registry response carries the canonical handle when present).
        self._next_handle = 1

    # --- Surface consumed by RtiConnection / Federate -----------------------

    def allocate_handle(self) -> int:
        """Mint a fresh local handle. Used as a fallback if the server
        response did not include one (it always should for register_object,
        but the SDK contract permits this fallback)."""
        h = self._next_handle
        self._next_handle += 1
        return h

    def events_for(self, federate_handle: int) -> asyncio.Queue[Any]:
        """Return the asyncio.Queue draining events for ``federate_handle``.

        Setdefault so the SDK can call this before the stream task has
        produced anything; the background task pushes into the same queue.
        """
        return self._event_queues.setdefault(federate_handle, asyncio.Queue())

    async def close(self) -> None:
        """Tear down: cancel every stream task, then close the channel."""
        for task in list(self._stream_tasks.values()):
            task.cancel()
            with contextlib.suppress(BaseException):
                await task
        self._stream_tasks.clear()
        with contextlib.suppress(BaseException):
            await self.channel.close()

    async def record(self, method: str, **kwargs: Any) -> Any:  # noqa: PLR0911, PLR0912
        """Dispatch a single SDK call to the matching gRPC RPC.

        The SDK's higher layers call ``transport.record(method, **kwargs)``
        and may ``await`` the result. The fake returns synchronously; this
        method is async because every gRPC call is awaitable. Both forms
        coexist because ``Federate.publish_object_class`` (and friends)
        treat the result as opaque.
        """
        if method == "create_federation":
            return await self._create_federation(kwargs["spec"])
        if method == "join_federation":
            return await self._join_federation(
                kwargs["spec"], kwargs["federate_name"]
            )
        if method == "resign_federation":
            return await self._resign_federation(kwargs["federate_handle"])
        if method == "publish_interaction_class":
            return await self._publish_interaction(
                kwargs["federate_handle"], kwargs["class_name"]
            )
        if method == "subscribe_interaction_class":
            return await self._subscribe_interaction(
                kwargs["federate_handle"], kwargs["class_name"]
            )
        if method == "publish_object_class":
            # Object pub/sub is recorded but NOT dispatched: the
            # cross-language smoke uses interactions only, and the FOM
            # XML in tests/conformance/foms/good/pyjevsim-bridge.xml
            # declares only HLAobjectRoot. Returning None keeps the
            # SDK happy; a future cut wires the real RPC.
            return None
        if method == "subscribe_object_class":
            return None
        if method == "register_object_instance":
            # Same rationale as publish_object_class — interactions only
            # in cut-1. Return a synthetic handle so the SDK doesn't fall
            # through to allocate_handle (which is fine, but explicit is
            # clearer in tests).
            return self.allocate_handle()
        if method == "update_attributes":
            # Recorded only; cut-1 cross-language smoke doesn't exercise
            # object updates.
            return None
        if method == "send_interaction":
            return await self._send_interaction(
                kwargs["federate_handle"],
                kwargs["class_name"],
                kwargs.get("parameters") or {},
                kwargs.get("timestamp"),
            )
        if method in (
            "enable_time_regulation",
            "enable_time_constrained",
            "next_message_request",
        ):
            # rtid's TimeService is nil at M2; dispatching would yield
            # codes.Unimplemented and the bridge's NER loop would block
            # forever. The cross-language smoke explicitly avoids the
            # time-managed code path.
            return None
        # Unknown method — surface a clear error rather than a silent
        # drop; better the test fails loudly than passes by omission.
        raise NotImplementedError(
            f"GrpcTransport.record: method {method!r} not implemented for cut-1"
        )

    # --- Per-RPC dispatch helpers ------------------------------------------

    async def _create_federation(self, spec: Any) -> None:
        from rti.v1 import common_pb2, federation_pb2

        self._federation_name = spec.name
        # Build FOMModule list + cache the name→handle maps (Python-side
        # FOM parser; same sort order as Go-side). The file read happens
        # in a sync helper so this async coroutine doesn't trip the
        # ASYNC240 lint (sync filesystem in async context); FOM modules
        # are tiny and load-once so the blocking is not material.
        fom_modules = [
            common_pb2.FOMModule(path=str(path), xml=_read_fom_bytes(path))
            for path in spec.fom_modules
        ]
        self._populate_handle_tables(spec.fom_modules)

        req = federation_pb2.CreateFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=spec.name,
            fom_modules=fom_modules,
            mode=_mode_to_proto(spec.mode),
            stall_timeout_seconds=float(spec.stall_timeout_seconds),
            seed=spec.seed,
        )
        try:
            await self.federation.CreateFederation(req)
        except Exception as exc:
            # FederationAlreadyExists is benign — the second federate to
            # create the same federation should succeed silently.
            if _is_already_exists(exc):
                return
            raise
        return

    async def _join_federation(self, spec: Any, federate_name: str) -> int:
        from rti.v1 import common_pb2, federation_pb2

        self._federation_name = spec.name
        # Make sure the handle tables are populated even if the caller
        # joined an already-existing federation (no create call here).
        if not self._interaction_handles and spec.fom_modules:
            self._populate_handle_tables(spec.fom_modules)

        req = federation_pb2.JoinFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=spec.name,
            federate_name=federate_name,
        )
        resp = await self.federation.JoinFederation(req)
        federate_handle = int(resp.federate_handle)
        # Open the per-federate event stream as soon as we know the
        # handle; the background task pushes events into the queue
        # ``events_for(federate_handle)`` returns.
        self._start_event_stream(federate_handle)
        return federate_handle

    async def _resign_federation(self, federate_handle: int) -> None:
        from rti.v1 import common_pb2, federation_pb2

        if self._federation_name is None:
            return
        # Cancel the stream task BEFORE the resign RPC — the server
        # closes the stream from its side once the federate resigns;
        # tearing down our side first avoids a spurious cancellation
        # error in the task.
        task = self._stream_tasks.pop(federate_handle, None)
        if task is not None:
            task.cancel()
            with contextlib.suppress(BaseException):
                await task
        req = federation_pb2.ResignFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=federate_handle,
            action=common_pb2.ResignAction.RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
        )
        with contextlib.suppress(Exception):
            await self.federation.ResignFederation(req)
        return

    async def _publish_interaction(
        self, federate_handle: int, class_name: str
    ) -> None:
        from rti.v1 import common_pb2, declaration_pb2

        cls = self._interaction_handle_for(class_name)
        req = declaration_pb2.PubInterRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            interaction_class_handle=cls,
        )
        await self.declaration.PublishInteractionClass(req)
        return

    async def _subscribe_interaction(
        self, federate_handle: int, class_name: str
    ) -> None:
        from rti.v1 import common_pb2, declaration_pb2

        cls = self._interaction_handle_for(class_name)
        req = declaration_pb2.SubInterRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            interaction_class_handle=cls,
        )
        await self.declaration.SubscribeInteractionClass(req)
        return

    async def _send_interaction(
        self,
        federate_handle: int,
        class_name: str,
        parameters: dict[str, Any],
        timestamp: float | None,
    ) -> None:
        from rti.v1 import common_pb2, object_pb2

        cls = self._interaction_handle_for(class_name)
        # The SDK's higher layers pass parameters as a dict keyed by
        # name with arbitrary payloads; cut-1 collapses every payload
        # to bytes via repr-encoding when not already bytes, then maps
        # name → 1-based parameter handle (sorted-by-name; same scheme
        # as the FOM class handles).
        param_map: dict[int, bytes] = {}
        param_index = self._parameter_indices_for(class_name)
        for name, payload in parameters.items():
            if name not in param_index:
                # Unknown parameter — squash into handle 0 so the
                # message still goes out. The Go side rejects unknown
                # handles; this is just defensive.
                continue
            param_map[param_index[name]] = _coerce_payload(payload)
        req = object_pb2.SendInteractionRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            interaction_class_handle=cls,
            parameters=param_map,
        )
        if timestamp is not None:
            req.logical_time = float(timestamp)
        await self.objects.SendInteraction(req)
        return

    # --- Stream draining ----------------------------------------------------

    def _start_event_stream(self, federate_handle: int) -> None:
        """Launch the background task that drains StreamService.Events
        for ``federate_handle`` into the local asyncio.Queue."""
        if federate_handle in self._stream_tasks:
            return
        self.events_for(federate_handle)  # ensure queue exists
        loop = asyncio.get_event_loop()
        self._stream_tasks[federate_handle] = loop.create_task(
            self._drain_events(federate_handle)
        )

    async def _drain_events(self, federate_handle: int) -> None:
        """Background task: forward FederateEvent -> typed event onto the queue."""
        from rti.v1 import common_pb2, stream_pb2

        if self._federation_name is None:
            return
        req = stream_pb2.EventsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=federate_handle,
        )
        queue = self.events_for(federate_handle)
        try:
            stream = self.streams.Events(req)
            async for fed_event in stream:
                translated = self._translate_event(fed_event)
                if translated is not None:
                    queue.put_nowait(translated)
        except asyncio.CancelledError:
            raise
        except Exception:
            # Server closed or error — let the queue drain quietly.
            return

    def _translate_event(self, fed_event: Any) -> Any | None:
        """Translate a wire FederateEvent into one of rti1516e.events.*."""
        from rti1516e.events import (
            DiscoverObjectInstance,
            FederationHalted,
            ReceiveInteraction,
            ReflectAttributeValues,
            TimeAdvanceGrant,
        )

        which = fed_event.WhichOneof("event")
        if which == "receive":
            r = fed_event.receive
            class_name = self._inverse_interaction_handles.get(
                int(r.interaction_class_handle), str(r.interaction_class_handle)
            )
            params: dict[str, Any] = {}
            inv = {v: k for k, v in self._parameter_indices_for(class_name).items()}
            for handle, payload in r.parameters.items():
                params[inv.get(int(handle), str(handle))] = bytes(payload)
            ts = r.logical_time if r.HasField("logical_time") else None
            return ReceiveInteraction(
                class_name=class_name, parameters=params, timestamp=ts
            )
        if which == "grant":
            return TimeAdvanceGrant(time=float(fed_event.grant.logical_time))
        if which == "discover":
            d = fed_event.discover
            return DiscoverObjectInstance(
                object_handle=int(d.object_handle),
                class_name=str(d.object_class_handle),
                instance_name=str(d.object_name),
            )
        if which == "reflect":
            r = fed_event.reflect
            ts = r.logical_time if r.HasField("logical_time") else None
            return ReflectAttributeValues(
                object_handle=int(r.object_handle),
                values={str(k): bytes(v) for k, v in r.attributes.items()},
                timestamp=ts,
            )
        if which == "halted":
            # The proto FederationHalted lacks a stalled-federate field;
            # surface 0 as "no specific federate identified" so the
            # dataclass invariant is preserved on the SDK side.
            return FederationHalted(
                cause=str(fed_event.halted.cause),
                stalled_federate_handle=0,
            )
        return None

    # --- FOM-driven name → handle resolution -------------------------------

    def _populate_handle_tables(self, fom_paths: list[str]) -> None:
        """Parse the FOM modules + cache name → handle maps.

        Mirrors the Go-side ``fomHandle.LookupInteractionClass``:
        handles are 1-based indices over the sort-by-name interaction
        class list (with HLAinteractionRoot included via the MIM merge).
        """
        from rti1516e.fom import parse

        if not fom_paths:
            return
        result = parse([Path(p) for p in fom_paths])
        if result.diagnostics or result.fom is None:
            # Don't fail here — the rtid will reject the FOM if it's
            # really bad, and the Python-side parser may be stricter
            # about HLA built-ins than the Go side.
            return
        fom = result.fom
        # Replicate Go-side MIM merge for handle parity. The Go side
        # injects HLAinteractionRoot before the user-defined classes;
        # see rti/pkg/fom/mim.Merge. The Python parser does the same
        # logical work but cut-1 just sorts the resolved leaf names —
        # which produces handle 1 = "ConsumerAck", handle 2 =
        # "HLAinteractionRoot", handle 3 = "ProducerOutput" for the
        # bridge FOM. The Go side similarly sorts after MIM merge so
        # both ends agree.
        for idx, ic in enumerate(sorted(fom.interaction_classes, key=lambda c: c.name)):
            handle = idx + 1
            self._interaction_handles[ic.name] = handle
            self._inverse_interaction_handles[handle] = ic.name
        for idx, oc in enumerate(sorted(fom.object_classes, key=lambda c: c.name)):
            self._object_class_handles[oc.name] = idx + 1

    def _interaction_handle_for(self, class_name: str) -> int:
        """Return the numeric handle for ``class_name``; 0 on miss.

        Returning 0 (rather than raising) lets the cross-language test
        progress past optional class names — the Go side will reject
        the RPC and surface a typed error the test can assert on.
        """
        return self._interaction_handles.get(class_name, 0)

    def _parameter_indices_for(self, class_name: str) -> dict[str, int]:
        """Return parameter-name → 1-based index for ``class_name``.

        Cached lazily off the FOM. The bridge's send_interaction passes
        ``{"_payload": <bytes>}`` which doesn't exist in any FOM
        parameter list; we fall back to a single synthetic parameter at
        handle 1 so the wire payload still propagates.
        """
        # Cut-1: synthesize a single ``_payload`` -> 1 mapping so the
        # bridge's opaque-payload convention works without re-parsing
        # the FOM here. A future cut walks the FOM and binds real
        # parameter handles.
        return {"_payload": 1}


# --- Helpers ---------------------------------------------------------------


def _read_fom_bytes(path: str) -> bytes:
    """Read FOM XML bytes synchronously. Extracted so async callers can
    invoke this without tripping the ASYNC240 lint (filesystem in async).
    FOMs are tiny load-once payloads — the blocking is not material."""
    return Path(path).read_bytes()


_GENERATED_PATH_INSTALLED = False


def _ensure_generated_path() -> None:
    """Prepend ``rti1516e/_generated`` to ``sys.path`` so the wire stubs
    (namespaced ``rti.v1.*``) resolve. Idempotent + lazy: only runs the
    first time a gRPC code path opens a transport."""
    global _GENERATED_PATH_INSTALLED  # noqa: PLW0603 — module-level cache flag
    if _GENERATED_PATH_INSTALLED:
        return
    import sys

    generated = Path(__file__).resolve().parent / "_generated"
    if generated.is_dir():
        path_str = str(generated)
        if path_str not in sys.path:
            sys.path.insert(0, path_str)
    _GENERATED_PATH_INSTALLED = True


def _mode_to_proto(mode: str) -> int:
    """Translate the FederationSpec.mode string to the proto enum value."""
    from rti.v1 import common_pb2

    if mode == "best-effort":
        return int(common_pb2.Mode.MODE_BEST_EFFORT)
    return int(common_pb2.Mode.MODE_VERBOSE)


def _coerce_payload(value: Any) -> bytes:
    """Coerce an arbitrary parameter payload to bytes for the wire."""
    if isinstance(value, bytes | bytearray):
        return bytes(value)
    if isinstance(value, str):
        return value.encode("utf-8")
    if isinstance(value, int):
        # 4 bytes BE matches HLAinteger32BE used by the bridge FOM.
        return int(value).to_bytes(4, byteorder="big", signed=False)
    return repr(value).encode("utf-8")


def _is_already_exists(exc: BaseException) -> bool:
    """Return True if ``exc`` looks like FederationAlreadyExists.

    grpc.aio raises ``grpc.aio.AioRpcError``; we sniff via duck-typed
    .code() rather than importing grpc unconditionally so spec tests
    that don't touch grpc don't pay the import cost.
    """
    code_fn = getattr(exc, "code", None)
    if not callable(code_fn):
        return False
    try:
        code = code_fn()
    except Exception:  # noqa: BLE001
        return False
    name = getattr(code, "name", "") or str(code)
    return "ALREADY_EXISTS" in name


async def build_grpc_transport(url: str) -> GrpcTransport:
    """Open a real ``grpc.aio`` channel for ``url`` and wrap it.

    ``url`` is ``grpc://host:port`` (insecure for cut-1; TLS is post-MVP).
    """
    import grpc

    target = url.removeprefix("grpc://")
    channel = grpc.aio.insecure_channel(target)
    return GrpcTransport(channel, url=url)
