"""Transport registry — pluggable in-process doubles + real gRPC for the RTI client.

The Layer 1 SDK (rti1516e.connection.RtiConnection) has three backing transports:

  - ``memory://<name>``   — in-process pure-Python ``InProcessTransport``
                            (production-suitable; historically called
                            ``FakeRtiServer`` and re-exported under that
                            name for back-compat).
  - ``grpc://host:port``  — real gRPC channel to an rtid binary, wrapped
                            in :class:`GrpcTransport`. Built on demand
                            by ``RtiConnection.__aenter__`` for cross-
                            language tests (TASK-081).
  - ``grpcs://host:port`` — TLS-secured variant of ``grpc://``. Server-
                            side TLS is provided by rtid's
                            ``--tls-cert/--tls-key`` flag pair (M6 W1B);
                            the client supplies a CA bundle via
                            ``RtiConnection.connect(url, ca_cert=...)``
                            or relies on the system trust store when
                            ``ca_cert`` is omitted.

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
  - Time-management RPCs (NextMessageRequest, EnableTimeRegulation,
    EnableTimeConstrained) are dispatched to rtid's TimeService as of
    M21 (TASK-208). Earlier cuts short-circuited because rtid's
    timeService field was nil; M21 W2A composed it unconditionally
    and W2B closed the grant-on-the-wire conversion gap. The
    bridge's ``_await_grant`` now resolves once a real
    TimeAdvanceGrant arrives on ``StreamService.Events``.

This module is internal to rti1516e and is not part of the public API.
"""

from __future__ import annotations

import asyncio
import contextlib
from pathlib import Path
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:  # pragma: no cover - import guard for type checking only
    import grpc

# M39 (HA-1): FederateEvent oneof variants that reached _translate_event
# without a branch. Warned once per variant per process (see the default
# branch at the bottom of _translate_event).
_UNTRANSLATED_EVENT_WARNED: set[str] = set()

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


# --- Resign-action mapping (M36) ---------------------------------------------

# IEEE 1516.1-2010 §4.10 resign-action designators → ``rti.v1.ResignAction``
# wire enum names (M24 W2). Three of the proto names are shortened forms of
# the IEEE designators, so a plain ``"RESIGN_ACTION_" + name`` concat is NOT
# correct — keep this table exhaustive and explicit.
_RESIGN_ACTION_WIRE_NAMES: dict[str, str] = {
    "UNCONDITIONALLY_DIVEST_ATTRIBUTES": "RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES",
    "DELETE_OBJECTS": "RESIGN_ACTION_DELETE_OBJECTS",
    "CANCEL_PENDING_OWNERSHIP_ACQUISITIONS": "RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP",
    "DELETE_OBJECTS_THEN_DIVEST": "RESIGN_ACTION_DELETE_THEN_DIVEST",
    "CANCEL_THEN_DELETE_THEN_DIVEST": "RESIGN_ACTION_CANCEL_THEN_DELETE",
    "NO_ACTION": "RESIGN_ACTION_NO_ACTION",
}

#: Public view of the accepted IEEE §4.10 designator strings (Layer 2
#: validates against this before dispatching the resign).
RESIGN_ACTION_NAMES = frozenset(_RESIGN_ACTION_WIRE_NAMES)


def resign_action_to_proto(action: int | str | None) -> int:
    """Map a resign action to the ``rti.v1.ResignAction`` enum value.

    - ``None``  → ``RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES``
      (the pre-M24 default).
    - ``int``   → passed through unchanged (caller already holds the
      proto enum value; M24 W2 contract).
    - ``str``   → IEEE 1516.1-2010 §4.10 designator, translated via
      :data:`_RESIGN_ACTION_WIRE_NAMES`. Unknown names raise
      ``ValueError`` (§4.10 InvalidResignAction).
    """
    from rti.v1 import common_pb2

    if action is None:
        return int(
            common_pb2.ResignAction.RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES
        )
    if isinstance(action, int):
        return action
    wire_name = _RESIGN_ACTION_WIRE_NAMES.get(action)
    if wire_name is None:
        valid = ", ".join(sorted(_RESIGN_ACTION_WIRE_NAMES))
        raise ValueError(f"invalid resign action {action!r}; expected one of: {valid}")
    return int(common_pb2.ResignAction.Value(wire_name))


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
            time_pb2_grpc,
        )

        self.channel = channel
        self.url = url
        self.federation = federation_pb2_grpc.FederationServiceStub(channel)
        self.declaration = declaration_pb2_grpc.DeclarationServiceStub(channel)
        self.objects = object_pb2_grpc.ObjectServiceStub(channel)
        self.streams = stream_pb2_grpc.StreamServiceStub(channel)
        # M21 TASK-208: TimeService is now wired (was nil at M2 / M3).
        self.time = time_pb2_grpc.TimeServiceStub(channel)
        # Federation name → name-resolver state. The first
        # ``create_federation`` for a given federation name parses the
        # FOM and caches name→handle maps; later RPCs reuse them.
        self._federation_name: str | None = None
        self._interaction_handles: dict[str, int] = {}
        self._object_class_handles: dict[str, int] = {}
        self._inverse_interaction_handles: dict[int, str] = {}
        # M12 W2: lazily-populated cache for attribute name → handle
        # lookups; keyed by class name. Populated on first
        # publish_object_class / subscribe_object_class call by
        # walking the cached FOM (see _populate_handle_tables for the
        # FOM cache).
        self._attribute_handle_cache: dict[str, dict[str, int]] = {}
        # The parsed FOM is cached so attribute lookups don't need to
        # re-parse. Set on every successful _populate_handle_tables.
        self._fom_cache: Any | None = None
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
            return await self._create_federation(
                kwargs["spec"], exist_ok=kwargs.get("exist_ok", True),
            )
        if method == "destroy_federation":
            return await self._destroy_federation(kwargs["federation_name"])
        if method == "join_federation":
            return await self._join_federation(
                kwargs["spec"],
                kwargs["federate_name"],
                kwargs.get("federate_type", ""),
            )
        if method == "resign_federation":
            return await self._resign_federation(
                kwargs["federate_handle"], kwargs.get("action"),
            )
        if method == "publish_interaction_class":
            return await self._publish_interaction(
                kwargs["federate_handle"], kwargs["class_name"]
            )
        if method == "subscribe_interaction_class":
            return await self._subscribe_interaction(
                kwargs["federate_handle"], kwargs["class_name"]
            )
        if method == "publish_object_class":
            return await self._publish_object_class(
                kwargs["federate_handle"],
                kwargs["class_name"],
                kwargs["attributes"],
            )
        if method == "subscribe_object_class":
            return await self._subscribe_object_class(
                kwargs["federate_handle"],
                kwargs["class_name"],
                kwargs["attributes"],
            )
        if method == "register_object_instance":
            return await self._register_object_instance(
                kwargs["federate_handle"],
                kwargs["class_name"],
                kwargs.get("instance_name"),
            )
        if method == "update_attributes":
            return await self._update_attributes(
                kwargs["federate_handle"],
                kwargs["object_handle"],
                kwargs["values"],
                kwargs.get("timestamp"),
            )
        if method == "send_interaction":
            return await self._send_interaction(
                kwargs["federate_handle"],
                kwargs["class_name"],
                kwargs.get("parameters") or {},
                kwargs.get("timestamp"),
            )
        if method == "enable_time_regulation":
            return await self._enable_time_regulation(
                kwargs["federate_handle"], kwargs["lookahead"],
            )
        if method == "enable_time_constrained":
            return await self._enable_time_constrained(
                kwargs["federate_handle"],
            )
        if method == "next_message_request":
            return await self._next_message_request(
                kwargs["federate_handle"], kwargs["time"],
            )
        if method == "disable_time_regulation":
            return await self._disable_time_regulation(kwargs["federate_handle"])
        if method == "disable_time_constrained":
            return await self._disable_time_constrained(kwargs["federate_handle"])
        if method == "modify_lookahead":
            return await self._modify_lookahead(
                kwargs["federate_handle"], kwargs["lookahead"],
            )
        if method == "next_message_request_available":
            return await self._next_message_request_available(
                kwargs["federate_handle"], kwargs["time"],
            )
        if method == "time_advance_request":
            return await self._time_advance_request(
                kwargs["federate_handle"], kwargs["time"],
            )
        if method == "time_advance_request_available":
            return await self._time_advance_request_available(
                kwargs["federate_handle"], kwargs["time"],
            )
        if method == "flush_queue_request":
            return await self._flush_queue_request(
                kwargs["federate_handle"], kwargs["time"],
            )
        if method == "query_logical_time":
            return await self._query_logical_time(kwargs["federate_handle"])
        if method == "query_lookahead":
            return await self._query_lookahead(kwargs["federate_handle"])
        if method == "query_lbts":
            return await self._query_lbts()
        if method == "enable_asynchronous_delivery":
            return await self._enable_asynchronous_delivery(kwargs["federate_handle"])
        if method == "disable_asynchronous_delivery":
            return await self._disable_asynchronous_delivery(kwargs["federate_handle"])
        if method == "delete_object_instance":
            return await self._delete_object_instance(
                kwargs["federate_handle"],
                kwargs["object_handle"],
                kwargs.get("tag") or b"",
                kwargs.get("timestamp"),
            )
        if method == "local_delete_object_instance":
            return await self._local_delete_object_instance(
                kwargs["federate_handle"], kwargs["object_handle"],
            )
        if method == "request_attribute_value_update":
            return await self._request_attribute_value_update(
                kwargs["federate_handle"],
                kwargs["object_handle"],
                list(kwargs.get("attribute_handles") or []),
                kwargs.get("tag") or b"",
            )
        if method == "request_class_attribute_value_update":
            return await self._request_class_attribute_value_update(
                kwargs["federate_handle"],
                kwargs["object_class_handle"],
                list(kwargs.get("attribute_handles") or []),
                kwargs.get("tag") or b"",
            )
        if method == "change_attribute_transportation_type":
            return await self._change_attribute_transportation_type(
                kwargs["federate_handle"],
                kwargs["object_handle"],
                list(kwargs.get("attribute_handles") or []),
                int(kwargs["transport_type"]),
            )
        if method == "change_interaction_transportation_type":
            return await self._change_interaction_transportation_type(
                kwargs["federate_handle"],
                kwargs["interaction_class_handle"],
                int(kwargs["transport_type"]),
            )
        # Unknown method — surface a clear error rather than a silent
        # drop; better the test fails loudly than passes by omission.
        raise NotImplementedError(
            f"GrpcTransport.record: method {method!r} not implemented for cut-1"
        )

    # --- Per-RPC dispatch helpers ------------------------------------------

    async def _create_federation(self, spec: Any, *, exist_ok: bool = True) -> None:
        """CreateFederation. ``exist_ok=True`` (the rolled create-on-join
        path) swallows FederationAlreadyExists so the second federate to
        create the same federation succeeds silently; ``exist_ok=False``
        (§4.5 createFederationExecution, M39 HA-2) surfaces it as the
        typed ``FederationExecutionAlreadyExists``. Every other failure
        is translated either way.
        """
        from rti.v1 import common_pb2, federation_pb2

        from ._grpc_errors import translate_rpc_error

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
        except Exception as exc:  # noqa: BLE001 — translate_rpc_error reraises
            # FederationAlreadyExists is benign on the rolled path — the
            # second federate to create the same federation should
            # succeed silently.
            if exist_ok and _is_already_exists(exc):
                return
            translate_rpc_error(exc)
        return

    async def _destroy_federation(self, federation_name: str) -> None:
        """§4.6 destroyFederationExecution (M39 HA-2). Typed failures:
        FederatesCurrentlyJoined while members remain,
        FederationExecutionDoesNotExist for an unknown name."""
        from rti.v1 import common_pb2, federation_pb2

        from ._grpc_errors import translate_rpc_error

        req = federation_pb2.DestroyFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=federation_name,
        )
        try:
            await self.federation.DestroyFederation(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _join_federation(
        self, spec: Any, federate_name: str, federate_type: str = ""
    ) -> int:
        from rti.v1 import common_pb2, federation_pb2

        from ._grpc_errors import translate_rpc_error

        self._federation_name = spec.name
        # Make sure the handle tables are populated even if the caller
        # joined an already-existing federation (no create call here).
        if not self._interaction_handles and spec.fom_modules:
            self._populate_handle_tables(spec.fom_modules)

        # M13 thread B (docs/srs.md §10.4): forward the optional
        # federate_type. Old SDK callers that don't pass it land here
        # with an empty string — the rtid treats absent as "no type
        # declared", preserving cut-1 wire-version compatibility.
        req = federation_pb2.JoinFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=spec.name,
            federate_name=federate_name,
            federate_type=federate_type,
        )
        try:
            resp = await self.federation.JoinFederation(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
            raise  # unreachable; translate_rpc_error always raises
        federate_handle = int(resp.federate_handle)
        # Open the per-federate event stream as soon as we know the
        # handle; the background task pushes events into the queue
        # ``events_for(federate_handle)`` returns.
        self._start_event_stream(federate_handle)
        return federate_handle

    async def _resign_federation(
        self, federate_handle: int, action: int | str | None = None,
    ) -> None:
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
        # M24 W2 — caller may pass an explicit ResignAction value.
        # M36 — Layer 2 passes the IEEE §4.10 designator string; map it
        # here so the higher layers stay proto-free.
        # Default = UNCONDITIONALLY_DIVEST_ATTRIBUTES (matches pre-M24).
        action = resign_action_to_proto(action)
        req = federation_pb2.ResignFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=federate_handle,
            action=action,
        )
        with contextlib.suppress(Exception):
            await self.federation.ResignFederation(req)
        return

    async def _publish_interaction(
        self, federate_handle: int, class_arg: int | str
    ) -> None:
        """M27 Phase D: ``class_arg`` accepts ``int`` (FOM handle) or
        ``str`` (FOM name). Subscriber federates that joined an
        already-created federation may have an empty local FOM cache;
        passing the handle directly (resolved via SupportService) is
        the safe path."""
        from rti.v1 import common_pb2, declaration_pb2

        from ._grpc_errors import translate_rpc_error

        cls = self._resolve_interaction_class_handle(class_arg)
        req = declaration_pb2.PubInterRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            interaction_class_handle=cls,
        )
        try:
            await self.declaration.PublishInteractionClass(req)
        except Exception as exc:  # noqa: BLE001 — translate_rpc_error reraises
            translate_rpc_error(exc)
        return

    async def _subscribe_interaction(
        self, federate_handle: int, class_arg: int | str
    ) -> None:
        """M27 Phase D: see _publish_interaction."""
        from rti.v1 import common_pb2, declaration_pb2

        from ._grpc_errors import translate_rpc_error

        cls = self._resolve_interaction_class_handle(class_arg)
        req = declaration_pb2.SubInterRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            interaction_class_handle=cls,
        )
        try:
            await self.declaration.SubscribeInteractionClass(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        return

    async def _send_interaction(
        self,
        federate_handle: int,
        class_arg: int | str,
        parameters: dict[int | str, Any],
        timestamp: float | None,
    ) -> None:
        """M27 Phase B: ``class_arg`` and parameter dict keys accept
        ``int`` (handle, Pitch-style) or ``str`` (FOM name)."""
        from rti.v1 import common_pb2, object_pb2

        cls = self._resolve_interaction_class_handle(class_arg)
        # Resolve parameter keys: int → use directly; str → look up.
        param_map: dict[int, bytes] = {}
        class_name_for_index: str | None = None
        for key, payload in parameters.items():
            if isinstance(key, int):
                param_map[int(key)] = _coerce_payload(payload)
                continue
            if class_name_for_index is None:
                class_name_for_index = self._interaction_class_name_for(class_arg)
            if class_name_for_index is None:
                continue  # unknown class; can't resolve names
            param_index = self._parameter_indices_for(class_name_for_index)
            if key not in param_index:
                continue
            param_map[param_index[key]] = _coerce_payload(payload)
        req = object_pb2.SendInteractionRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            interaction_class_handle=cls,
            parameters=param_map,
        )
        if timestamp is not None:
            req.logical_time = float(timestamp)
        try:
            await self.objects.SendInteraction(req)
        except Exception as exc:  # noqa: BLE001
            from ._grpc_errors import translate_rpc_error

            translate_rpc_error(exc)
        return

    # --- M21 TASK-208: TimeService dispatchers ---------------------------------

    async def _enable_time_regulation(
        self, federate_handle: int, lookahead: float,
    ) -> None:
        """Dispatch TimeService.EnableTimeRegulation (M21)."""
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.EnableRegulationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            lookahead=float(lookahead),
        )
        try:
            await self.time.EnableTimeRegulation(req)
        except Exception as exc:  # noqa: BLE001 — translate_rpc_error reraises
            translate_rpc_error(exc)

    async def _enable_time_constrained(self, federate_handle: int) -> None:
        """Dispatch TimeService.EnableTimeConstrained (M21)."""
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.EnableConstrainedRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
        )
        try:
            await self.time.EnableTimeConstrained(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _next_message_request(
        self, federate_handle: int, t: float,
    ) -> None:
        """Dispatch TimeService.NextMessageRequest (M21)."""
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.NERRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            logical_time=float(t),
        )
        try:
            await self.time.NextMessageRequest(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    # --- M22 TASK-222: TimeService dispatchers (parity with rti/pkg/federate) -

    async def _disable_time_regulation(self, federate_handle: int) -> None:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.DisableRegulationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
        )
        try:
            await self.time.DisableTimeRegulation(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _disable_time_constrained(self, federate_handle: int) -> None:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.DisableConstrainedRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
        )
        try:
            await self.time.DisableTimeConstrained(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _modify_lookahead(
        self, federate_handle: int, lookahead: float,
    ) -> None:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.ModifyLookaheadRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            lookahead=float(lookahead),
        )
        try:
            await self.time.ModifyLookahead(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _next_message_request_available(
        self, federate_handle: int, t: float,
    ) -> None:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.NMRARequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            logical_time=float(t),
        )
        try:
            await self.time.NextMessageRequestAvailable(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _time_advance_request(
        self, federate_handle: int, t: float,
    ) -> None:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.TARRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            logical_time=float(t),
        )
        try:
            await self.time.TimeAdvanceRequest(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _time_advance_request_available(
        self, federate_handle: int, t: float,
    ) -> None:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.TARARequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            logical_time=float(t),
        )
        try:
            await self.time.TimeAdvanceRequestAvailable(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _flush_queue_request(
        self, federate_handle: int, t: float,
    ) -> None:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.FQRRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            logical_time=float(t),
        )
        try:
            await self.time.FlushQueueRequest(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _query_logical_time(self, federate_handle: int) -> float:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.QueryFederateTimeRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
        )
        try:
            resp = await self.time.QueryLogicalTime(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
            raise  # unreachable; translate_rpc_error always raises
        return float(resp.logical_time)

    async def _query_lookahead(self, federate_handle: int) -> float:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.QueryFederateTimeRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
        )
        try:
            resp = await self.time.QueryLookahead(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
            raise
        return float(resp.lookahead)

    async def _query_lbts(self) -> tuple[float, bool]:
        """Return (lbts, finite). Federation-scoped (no federate handle)."""
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.QueryLBTSRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
        )
        try:
            resp = await self.time.QueryLBTS(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
            raise
        return (float(resp.lbts), bool(resp.finite))

    # --- M22 TASK-234: AsynchronousDelivery dispatchers (W2) -------------------

    async def _enable_asynchronous_delivery(self, federate_handle: int) -> None:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.EnableAsynchronousDeliveryRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
        )
        try:
            await self.time.EnableAsynchronousDelivery(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _disable_asynchronous_delivery(self, federate_handle: int) -> None:
        from rti.v1 import common_pb2, time_pb2

        from ._grpc_errors import translate_rpc_error

        req = time_pb2.DisableAsynchronousDeliveryRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
        )
        try:
            await self.time.DisableAsynchronousDelivery(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    # --- M23 TASK-250: ObjectService.DeleteObjectInstance dispatcher ---

    async def _delete_object_instance(
        self,
        federate_handle: int,
        object_handle: int,
        tag: bytes,
        timestamp: float | None,
    ) -> None:
        from rti.v1 import common_pb2, object_pb2

        from ._grpc_errors import translate_rpc_error

        req = object_pb2.DeleteObjectInstanceRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            object_handle=object_handle,
            user_supplied_tag=tag,
        )
        if timestamp is not None:
            req.logical_time = float(timestamp)
        try:
            await self.objects.DeleteObjectInstance(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _local_delete_object_instance(
        self, federate_handle: int, object_handle: int,
    ) -> None:
        from rti.v1 import common_pb2, object_pb2

        from ._grpc_errors import translate_rpc_error

        req = object_pb2.LocalDeleteObjectInstanceRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            object_handle=object_handle,
        )
        try:
            await self.objects.LocalDeleteObjectInstance(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _request_attribute_value_update(
        self,
        federate_handle: int,
        object_handle: int,
        attribute_handles: list[int],
        tag: bytes,
    ) -> None:
        from rti.v1 import common_pb2, object_pb2

        from ._grpc_errors import translate_rpc_error

        req = object_pb2.RequestAttributeValueUpdateRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            object_handle=object_handle,
            attribute_handles=[int(h) for h in attribute_handles],
            user_supplied_tag=tag,
        )
        try:
            await self.objects.RequestAttributeValueUpdate(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _request_class_attribute_value_update(
        self,
        federate_handle: int,
        object_class_handle: int,
        attribute_handles: list[int],
        tag: bytes,
    ) -> None:
        from rti.v1 import common_pb2, object_pb2

        from ._grpc_errors import translate_rpc_error

        req = object_pb2.RequestClassAttributeValueUpdateRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            object_class_handle=object_class_handle,
            attribute_handles=[int(h) for h in attribute_handles],
            user_supplied_tag=tag,
        )
        try:
            await self.objects.RequestClassAttributeValueUpdate(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _change_attribute_transportation_type(
        self,
        federate_handle: int,
        object_handle: int,
        attribute_handles: list[int],
        transport_type: int,
    ) -> None:
        from rti.v1 import common_pb2, object_pb2

        from ._grpc_errors import translate_rpc_error

        req = object_pb2.ChangeAttributeTransportRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            object_handle=object_handle,
            attribute_handles=[int(h) for h in attribute_handles],
            transport_type=transport_type,
        )
        try:
            await self.objects.ChangeAttributeTransportationType(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _change_interaction_transportation_type(
        self,
        federate_handle: int,
        interaction_class_handle: int,
        transport_type: int,
    ) -> None:
        from rti.v1 import common_pb2, object_pb2

        from ._grpc_errors import translate_rpc_error

        req = object_pb2.ChangeInteractionTransportRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            interaction_class_handle=interaction_class_handle,
            transport_type=transport_type,
        )
        try:
            await self.objects.ChangeInteractionTransportationType(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def _publish_object_class(
        self,
        federate_handle: int,
        class_arg: int | str,
        attributes: list[int | str],
    ) -> None:
        """Dispatch DeclarationService.PublishObjectClassAttributes (M12 W2).

        M27 Phase B: ``class_arg`` and each entry of ``attributes`` accept
        either ``int`` (already-resolved handle, Pitch-style) or ``str``
        (FOM name, pysdk convenience). Mixed lists are allowed. Unknown
        names resolve to handle 0 and are silently dropped — the Go side
        rejects unknown handles at the wire layer.
        """
        from rti.v1 import common_pb2, declaration_pb2

        cls = self._resolve_object_class_handle(class_arg)
        attr_handles = self._resolve_attribute_handles(class_arg, attributes)
        req = declaration_pb2.PubObjAttrsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            object_class_handle=cls,
            attribute_handles=attr_handles,
        )
        try:
            await self.declaration.PublishObjectClassAttributes(req)
        except Exception as exc:  # noqa: BLE001
            from ._grpc_errors import translate_rpc_error

            translate_rpc_error(exc)
        return

    async def _subscribe_object_class(
        self,
        federate_handle: int,
        class_arg: int | str,
        attributes: list[int | str],
    ) -> None:
        """Dispatch DeclarationService.SubscribeObjectClassAttributes.

        See _publish_object_class for the M27 Phase B int|str semantics.
        """
        from rti.v1 import common_pb2, declaration_pb2

        cls = self._resolve_object_class_handle(class_arg)
        attr_handles = self._resolve_attribute_handles(class_arg, attributes)
        req = declaration_pb2.SubObjAttrsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            object_class_handle=cls,
            attribute_handles=attr_handles,
        )
        try:
            await self.declaration.SubscribeObjectClassAttributes(req)
        except Exception as exc:  # noqa: BLE001
            from ._grpc_errors import translate_rpc_error

            translate_rpc_error(exc)
        return

    async def _register_object_instance(
        self,
        federate_handle: int,
        class_arg: int | str,
        instance_name: str | None,
    ) -> int:
        """Dispatch ObjectService.RegisterObjectInstance (M12 W2).

        M27 Phase B: ``class_arg`` accepts ``int`` (Pitch-style handle)
        or ``str`` (FOM name). Returns the minted object handle.
        """
        from rti.v1 import common_pb2, object_pb2

        cls = self._resolve_object_class_handle(class_arg)
        req = object_pb2.RegisterObjectRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            object_class_handle=cls,
            object_name=instance_name or "",
        )
        try:
            resp = await self.objects.RegisterObjectInstance(req)
        except Exception as exc:  # noqa: BLE001
            from ._grpc_errors import translate_rpc_error

            translate_rpc_error(exc)
            raise  # unreachable; translate_rpc_error always raises
        return int(resp.object_handle)

    async def _update_attributes(
        self,
        federate_handle: int,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None,
    ) -> None:
        """Dispatch ObjectService.UpdateAttributeValues (M12 W2).

        Cut-3 simplification: attribute-name → handle resolution
        requires the originating object class. The SDK's
        ``Federate.update_attributes`` does not currently track the
        class for each minted object handle, so unknown attribute
        names default to handle 0 (rtid rejects). Callers that
        need real DDM/ownership-driven update fan-out should resolve
        handles via the FOM directly until a future cut adds a
        per-handle class index.
        """
        from rti.v1 import common_pb2, object_pb2

        attr_map: dict[int, bytes] = {}
        # Accept either string keys (FOM names — best-effort lookup
        # against every known class until one matches) or int keys
        # (already-resolved attribute handles — used by tests that
        # bypass the FOM lookup).
        for key, payload in values.items():
            if isinstance(key, int):
                attr_map[int(key)] = _coerce_payload(payload)
                continue
            handle = 0
            for class_name in self._object_class_handles:
                handle = self._attribute_handle_for(class_name, str(key))
                if handle != 0:
                    break
            if handle != 0:
                attr_map[handle] = _coerce_payload(payload)
        req = object_pb2.UpdateAttributeValuesRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name or "",
            federate_handle=federate_handle,
            object_handle=int(object_handle),
            attributes=attr_map,
        )
        if timestamp is not None:
            req.logical_time = float(timestamp)
        try:
            await self.objects.UpdateAttributeValues(req)
        except Exception as exc:  # noqa: BLE001
            from ._grpc_errors import translate_rpc_error

            translate_rpc_error(exc)
        return

    def _object_class_handle_for(self, class_name: str) -> int:
        """Return the numeric object-class handle for ``class_name``; 0 on miss."""
        return self._object_class_handles.get(class_name, 0)

    def _object_class_name_for(self, handle: int) -> str | None:
        """Return the FOM name for an object-class handle, or None on miss.
        M27 Phase B — inverse lookup for handle-keyed dispatch paths
        that still need to resolve attribute names. Linear over the
        cached map; the map is small enough (handful of classes) that
        the cost is negligible vs caching an inverse dict."""
        for name, h in self._object_class_handles.items():
            if h == handle:
                return name
        return None

    def _interaction_class_name_for(self, class_arg: int | str) -> str | None:
        """Return the FOM name for an interaction class identifier.

        M27 Phase B helper. Accepts either ``int`` (handle, reverse-
        looked-up via the existing inverse map) or ``str`` (passes
        through if known to the FOM). Returns None if the identifier
        does not resolve.
        """
        if isinstance(class_arg, int):
            return self._inverse_interaction_handles.get(class_arg)
        if class_arg in self._interaction_handles:
            return class_arg
        return None

    def _resolve_object_class_handle(self, class_arg: int | str) -> int:
        """M27 Phase B: int → identity; str → FOM lookup; 0 on miss."""
        if isinstance(class_arg, int):
            return int(class_arg)
        return self._object_class_handle_for(class_arg)

    def _resolve_interaction_class_handle(self, class_arg: int | str) -> int:
        """M27 Phase B: int → identity; str → FOM lookup; 0 on miss."""
        if isinstance(class_arg, int):
            return int(class_arg)
        return self._interaction_handle_for(class_arg)

    def _resolve_attribute_handles(
        self,
        class_arg: int | str,
        attributes: list[int | str],
    ) -> list[int]:
        """M27 Phase B: resolve mixed-type attribute list to handles.

        For ``int`` entries: pass through as already-resolved handles.
        For ``str`` entries: look up via the FOM, using ``class_arg`` to
        scope the lookup. If ``class_arg`` is an ``int`` (handle), the
        class name is inverse-looked-up first; failure to resolve the
        class skips all string-keyed attribute lookups.
        """
        out: list[int] = []
        class_name: str | None = None
        for a in attributes:
            if isinstance(a, int):
                out.append(int(a))
                continue
            if class_name is None:
                class_name = (
                    class_arg
                    if isinstance(class_arg, str)
                    else self._object_class_name_for(class_arg)
                )
            if class_name is None:
                continue
            h = self._attribute_handle_for(class_name, a)
            if h != 0:
                out.append(h)
        return out

    def _attribute_handle_for(self, class_name: str, attr_name: str) -> int:
        """Return the numeric attribute handle for (class, attr); 0 on miss.

        Cached lazily on first lookup. The handle index is the
        attribute's 1-based position within the class's parsed
        attribute list (same convention as the Go-side fomHandle.
        LookupAttribute — see ``rti/cmd/rtid/foms.go``).
        """
        cache = self._attribute_handle_cache.get(class_name)
        if cache is None:
            cache = self._build_attribute_cache(class_name)
            self._attribute_handle_cache[class_name] = cache
        return cache.get(attr_name, 0)

    def _build_attribute_cache(self, class_name: str) -> dict[str, int]:
        """Walk the FOM and build an attribute-name → 1-based-handle map.

        Returns an empty map when the FOM has not been parsed (no
        federation create with modules) or when the class is not
        present in the FOM. The Go side's LookupAttribute returns
        InvalidAttributeHandle in the same scenario; the cache mirror
        keeps name resolution cheap and consistent.
        """
        if self._fom_cache is None:
            return {}
        for oc in self._fom_cache.object_classes:
            if oc.name == class_name:
                return {a.name: i + 1 for i, a in enumerate(oc.attributes)}
        return {}

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
        except Exception:  # noqa: BLE001
            # Server closed or error — let the queue drain quietly.
            return

    def _translate_event(self, fed_event: Any) -> Any | None:  # noqa: PLR0911, PLR0912, PLR0915
        """Translate a wire FederateEvent into one of rti1516e.events.*.

        M39 (HA-1): every FederateEvent oneof variant on the M37 wire has
        a branch here. Variants with NO branch (a future wire addition,
        or an unknown field from a newer server) hit the default at the
        bottom, which warns ONCE per variant instead of silently
        dropping the event.
        """
        from rti1516e.events import (
            AttributeOwnershipAcquisitionNotification,
            DiscoverObjectInstance,
            FederationHalted,
            FederationNotSaved,
            FederationSaved,
            FederationSynchronized,
            InitiateFederateSave,
            ReceiveInteraction,
            ReflectAttributeValues,
            RequestAttributeOwnershipAssumption,
            RequestDivestitureConfirmation,
            SynchronizationPointAnnounced,
            TimeAdvanceGrant,
        )
        from rti1516e.handles import AttributeHandle, ObjectClassHandle

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
                # DEPRECATED identity carrier (stringified handle);
                # M39 adds the typed object_class alongside (§6.9).
                class_name=str(d.object_class_handle),
                instance_name=str(d.object_name),
                object_class=ObjectClassHandle(int(d.object_class_handle)),
            )
        if which == "reflect":
            r = fed_event.reflect
            ts = r.logical_time if r.HasField("logical_time") else None
            return ReflectAttributeValues(
                object_handle=int(r.object_handle),
                # DEPRECATED string-keyed map (stringified handles);
                # M39 adds the typed attribute_values alongside (§6.11).
                values={str(k): bytes(v) for k, v in r.attributes.items()},
                timestamp=ts,
                attribute_values={
                    AttributeHandle(int(k)): bytes(v)
                    for k, v in r.attributes.items()
                },
            )
        if which == "remove":
            # M23 — RemoveObjectInstance per IEEE 1516.1 §6.16.
            from .events import RemoveObjectInstance
            rm = fed_event.remove
            ts = rm.logical_time if rm.HasField("logical_time") else None
            return RemoveObjectInstance(
                object_handle=int(rm.object_handle),
                tag=bytes(rm.user_supplied_tag),
                timestamp=ts,
            )
        if which == "provide_update":
            # M23 — ProvideAttributeValueUpdate per IEEE 1516.1 §6.26.
            from .events import ProvideAttributeValueUpdate
            pv = fed_event.provide_update
            return ProvideAttributeValueUpdate(
                object_handle=int(pv.object_handle),
                attribute_handles=tuple(int(h) for h in pv.attribute_handles),
                tag=bytes(pv.user_supplied_tag),
            )
        # M12 W2 cut-2 service-group callbacks (deferral #1 close).
        if which == "sync_announced":
            a = fed_event.sync_announced
            return SynchronizationPointAnnounced(
                label=str(a.label),
                tag=bytes(a.tag),
                required_federates=tuple(int(h) for h in a.required_federates),
            )
        if which == "sync_synchronized":
            s = fed_event.sync_synchronized
            return FederationSynchronized(
                label=str(s.label),
                # §4.15 failed-to-sync set (M37, additive field 2).
                failed_to_sync=tuple(int(h) for h in s.failed_to_sync),
            )
        # M37 §4.12 — sync registration acks (tags 22/23).
        if which == "sync_registration_succeeded":
            from .events import SynchronizationPointRegistrationSucceeded
            return SynchronizationPointRegistrationSucceeded(
                label=str(fed_event.sync_registration_succeeded.label),
            )
        if which == "sync_registration_failed":
            from .events import (
                SynchronizationPointFailureReason,
                SynchronizationPointRegistrationFailed,
            )
            f = fed_event.sync_registration_failed
            reason_map = {
                1: SynchronizationPointFailureReason.SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE,
                2: SynchronizationPointFailureReason.SYNCHRONIZATION_SET_MEMBER_NOT_JOINED,
            }
            return SynchronizationPointRegistrationFailed(
                label=str(f.label),
                reason=reason_map.get(int(f.reason)),
            )
        if which == "ownership_assumption":
            o = fed_event.ownership_assumption
            return RequestAttributeOwnershipAssumption(
                object_handle=int(o.object_handle),
                attribute_handles=tuple(int(h) for h in o.attribute_handles),
                divesting_federate=int(o.divesting_federate),
                tag=bytes(o.tag),
            )
        if which == "ownership_acquired":
            o = fed_event.ownership_acquired
            return AttributeOwnershipAcquisitionNotification(
                object_handle=int(o.object_handle),
                attribute_handles=tuple(int(h) for h in o.attribute_handles),
                owning_federate=int(o.owning_federate),
            )
        if which == "ownership_divest_confirmed":
            o = fed_event.ownership_divest_confirmed
            return RequestDivestitureConfirmation(
                object_handle=int(o.object_handle),
                attribute_handles=tuple(int(h) for h in o.attribute_handles),
            )
        # M37 §7.11 — the current owner is asked to release (tag 33).
        if which == "ownership_release_requested":
            from .events import RequestAttributeOwnershipRelease
            o = fed_event.ownership_release_requested
            return RequestAttributeOwnershipRelease(
                object_handle=int(o.object_handle),
                attribute_handles=tuple(int(h) for h in o.attribute_handles),
                tag=bytes(o.tag),
            )
        # M37 §7.10 — acquisition-if-available lost (tag 34).
        if which == "ownership_unavailable":
            from .events import AttributeOwnershipUnavailable
            o = fed_event.ownership_unavailable
            return AttributeOwnershipUnavailable(
                object_handle=int(o.object_handle),
                attribute_handles=tuple(int(h) for h in o.attribute_handles),
            )
        if which == "save_initiate":
            s = fed_event.save_initiate
            save_time = s.save_time if s.HasField("save_time") else None
            return InitiateFederateSave(label=str(s.label), save_time=save_time)
        if which == "save_completed":
            return FederationSaved(label=str(fed_event.save_completed.label))
        if which == "save_failed":
            return FederationNotSaved(label=str(fed_event.save_failed.label))
        # Restore family. Tags 43-45 predate M37 (M17.25) but were
        # silently dropped by this switch until M39; 46-48 are M37.
        if which == "restore_initiate":
            from .events import InitiateFederateRestore
            r = fed_event.restore_initiate
            return InitiateFederateRestore(
                label=str(r.label),
                federate_handle=int(r.federate_handle),
                federate_name=str(r.federate_name),
            )
        if which == "restore_completed":
            from .events import FederationRestored
            return FederationRestored(label=str(fed_event.restore_completed.label))
        if which == "restore_failed":
            from .events import FederationNotRestored
            return FederationNotRestored(label=str(fed_event.restore_failed.label))
        if which == "restore_request_succeeded":
            from .events import RequestFederationRestoreSucceeded
            return RequestFederationRestoreSucceeded(
                label=str(fed_event.restore_request_succeeded.label),
            )
        if which == "restore_request_failed":
            from .events import RequestFederationRestoreFailed
            r = fed_event.restore_request_failed
            return RequestFederationRestoreFailed(
                label=str(r.label), reason=str(r.reason),
            )
        if which == "restore_begun":
            from .events import FederationRestoreBegun
            return FederationRestoreBegun()
        # M37 §5.10-§5.13 — registration / interaction advisories.
        if which == "start_registration":
            from .events import StartRegistrationForObjectClass
            return StartRegistrationForObjectClass(
                object_class_handle=int(
                    fed_event.start_registration.object_class_handle
                ),
            )
        if which == "stop_registration":
            from .events import StopRegistrationForObjectClass
            return StopRegistrationForObjectClass(
                object_class_handle=int(
                    fed_event.stop_registration.object_class_handle
                ),
            )
        if which == "turn_interactions_on":
            from .events import TurnInteractionsOn
            return TurnInteractionsOn(
                interaction_class_handle=int(
                    fed_event.turn_interactions_on.interaction_class_handle
                ),
            )
        if which == "turn_interactions_off":
            from .events import TurnInteractionsOff
            return TurnInteractionsOff(
                interaction_class_handle=int(
                    fed_event.turn_interactions_off.interaction_class_handle
                ),
            )
        # M37 §6.17/§6.18 — DDM scope advisories.
        if which == "attributes_in_scope":
            from .events import AttributesInScope
            s = fed_event.attributes_in_scope
            return AttributesInScope(
                object_handle=int(s.object_handle),
                attribute_handles=tuple(int(h) for h in s.attribute_handles),
            )
        if which == "attributes_out_of_scope":
            from .events import AttributesOutOfScope
            s = fed_event.attributes_out_of_scope
            return AttributesOutOfScope(
                object_handle=int(s.object_handle),
                attribute_handles=tuple(int(h) for h in s.attribute_handles),
            )
        # M37 §8.22 — retraction of a delivered TSO message.
        if which == "retraction_requested":
            from .events import RequestRetraction
            r = fed_event.retraction_requested
            return RequestRetraction(
                sender_federate=int(r.sender_federate),
                retraction_handle=int(r.message_retraction_handle),
            )
        # M26 Phase F — object instance name reservation events.
        if which == "reservation_succeeded":
            from .events import ObjectInstanceNameReservationSucceeded
            return ObjectInstanceNameReservationSucceeded(
                object_name=str(fed_event.reservation_succeeded.object_name),
            )
        if which == "reservation_failed":
            from .events import ObjectInstanceNameReservationFailed
            return ObjectInstanceNameReservationFailed(
                object_name=str(fed_event.reservation_failed.object_name),
            )
        if which == "reservation_multi_succeeded":
            from .events import MultipleObjectInstanceNameReservationSucceeded
            return MultipleObjectInstanceNameReservationSucceeded(
                object_names=tuple(
                    str(n) for n in fed_event.reservation_multi_succeeded.object_names
                ),
            )
        if which == "reservation_multi_failed":
            from .events import MultipleObjectInstanceNameReservationFailed
            mf = fed_event.reservation_multi_failed
            return MultipleObjectInstanceNameReservationFailed(
                requested_names=tuple(str(n) for n in mf.requested_names),
                colliding_names=tuple(str(n) for n in mf.colliding_names),
            )
        if which == "halted":
            # The proto FederationHalted lacks a stalled-federate field;
            # surface 0 as "no specific federate identified" so the
            # dataclass invariant is preserved on the SDK side.
            return FederationHalted(
                cause=str(fed_event.halted.cause),
                stalled_federate_handle=0,
            )
        # No branch matched. Two ways to get here:
        #   - ``which`` names a variant this switch forgot (a wire
        #     addition without a translation — the pre-M39 silent-drop
        #     bug class), or
        #   - ``which is None``: the event arrived from a NEWER server
        #     whose oneof tag this client's generated stubs don't know
        #     (proto3 open-set unknown-field path).
        # Either way: warn ONCE per variant so the gap is visible, then
        # drop the event (the wire contract says unknown variants are
        # skippable).
        tag = which if which is not None else "<unknown-wire-tag>"
        if tag not in _UNTRANSLATED_EVENT_WARNED:
            _UNTRANSLATED_EVENT_WARNED.add(tag)
            import warnings

            warnings.warn(
                f"rti1516e: FederateEvent variant {tag!r} "
                f"(seq={int(getattr(fed_event, 'seq', 0))}) has no pysdk "
                "translation and was dropped — add a branch in "
                "rti1516e/_transport.py _translate_event (warning fires "
                "once per variant)",
                RuntimeWarning,
                stacklevel=2,
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
        # M12 W2: cache the parsed FOM so attribute name → handle
        # lookups (used by publish_object_class / subscribe_object_class /
        # update_attributes) can walk it without re-parsing.
        self._fom_cache = fom
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


async def build_grpc_transport(
    url: str,
    *,
    ca_cert: bytes | None = None,
    client_cert: bytes | None = None,
    client_key: bytes | None = None,
    bearer_token: str | None = None,
) -> GrpcTransport:
    """Open a real ``grpc.aio`` channel for ``url`` and wrap it.

    Two URL schemes are supported:

      - ``grpc://host:port``  — plaintext ``grpc.aio.insecure_channel``.
      - ``grpcs://host:port`` — TLS-secured ``grpc.aio.secure_channel``.
        ``ca_cert`` (PEM bytes) populates ``root_certificates``; pass
        ``None`` to rely on the system trust store.

    M14 W3 — additional auth knobs:

      - ``client_cert`` + ``client_key`` (both PEM bytes) → mTLS. The
        rtid must have been started with ``--tls-client-ca`` set.
      - ``bearer_token`` → ``authorization: Bearer <token>`` metadata
        on every RPC. Combinable with TLS / mTLS.
    """
    import grpc

    if url.startswith("grpcs://"):
        target = url.removeprefix("grpcs://")
        ssl_creds = grpc.ssl_channel_credentials(
            root_certificates=ca_cert,
            private_key=client_key,
            certificate_chain=client_cert,
        )
        if bearer_token:
            # M14 W3: composite credentials = TLS + per-call metadata.
            # Mirrors Go SDK's bearerCreds path which requires TLS too.
            call_creds = grpc.metadata_call_credentials(
                _bearer_token_plugin(bearer_token)
            )
            ssl_creds = grpc.composite_channel_credentials(ssl_creds, call_creds)
        channel = grpc.aio.secure_channel(target, ssl_creds)
        return GrpcTransport(channel, url=url)
    if url.startswith("grpc://"):
        if bearer_token:
            raise ValueError(
                "build_grpc_transport: bearer_token requires grpcs:// "
                "(matches Go SDK's RequireTransportSecurity contract)"
            )
        target = url.removeprefix("grpc://")
        channel = grpc.aio.insecure_channel(target)
        return GrpcTransport(channel, url=url)
    raise ValueError(
        f"build_grpc_transport: unsupported URL scheme in {url!r} "
        "(expected 'grpc://' or 'grpcs://')"
    )


def _bearer_token_plugin(token: str) -> Any:
    """Return a grpc.AuthMetadataPlugin that attaches authorization:
    Bearer <token> to every RPC. M14 W3.

    Typed as ``Any`` because grpc.AuthMetadataPlugin is duck-typed at
    the call site; declaring the precise type would force a hard
    dependency on grpc's stubs.
    """

    def plugin(_context: Any, callback: Any) -> None:
        callback((("authorization", f"Bearer {token}"),), None)

    return plugin
