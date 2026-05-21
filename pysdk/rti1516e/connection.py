"""Layer 1 — RtiConnection + Federate. Agent C implements per TASK-063..067.

Idiomatic asyncio API per docs/agent-c-pysdk.md §4.4 Layer 1:

    async with RtiConnection.connect(url="grpc://localhost:8442") as rti:
        async with rti.join_federation(
            FederationSpec(name="demo", fom_modules=["./demo.fom.xml"]),
            federate_name="alice",
        ) as fed:
            await fed.publish_object_class("Vehicle", attributes=["pos"])
            async for event in fed.events():
                ...

The connection-level transport is gRPC over HTTP/2 to the rtid binary in
production; spec tests inject an in-process ``FakeRtiServer`` via the
``memory://`` URL scheme (see rti1516e._transport). Generated stubs live
in rti1516e._generated/ (gitignored; regenerate with `make py-codegen`).

This file is FROZEN-shape — Agent C may add private methods and dataclass
fields with defaults, but the public method names + signatures are part
of the M4 contract.
"""

from __future__ import annotations

import contextlib
import inspect
from collections.abc import AsyncIterator
from dataclasses import dataclass, field
from types import TracebackType
from typing import Any, Self

from rti1516e._transport import build_grpc_transport as _build_grpc_transport
from rti1516e._transport import lookup as _lookup_transport


async def _dispatch(transport: Any, method: str, **kwargs: Any) -> Any:
    """Call ``transport.record(method, **kwargs)`` and await if coroutine.

    The fake (FakeRtiServer) returns synchronously; the real GrpcTransport
    returns a coroutine. Both call sites share the same source by funneling
    through this helper. Wrapping the await in ``inspect.isawaitable`` keeps
    the fake path zero-cost (no extra event-loop tick) while letting the
    gRPC path do its real RPC.
    """
    result = transport.record(method, **kwargs)
    if inspect.isawaitable(result):
        return await result
    return result


@dataclass(frozen=True)
class FederationSpec:
    """Description of a federation to create or join."""

    name: str
    fom_modules: list[str] = field(default_factory=list)
    mode: str = "verbose"  # "verbose" | "best-effort" — wired by TASK-076
    seed: int = 0  # 0 = use server default; non-zero pins for determinism
    stall_timeout_seconds: int = 0  # 0 = server default (60s)


class RtiConnection:
    """Async connection to a single rtid instance."""

    def __init__(
        self,
        url: str,
        *,
        options: dict[str, Any] | None = None,
        ca_cert: bytes | None = None,
        client_cert: bytes | None = None,
        client_key: bytes | None = None,
        bearer_token: str | None = None,
    ) -> None:
        self._url = url
        self._options: dict[str, Any] = dict(options) if options else {}
        # PEM-encoded trusted CA bundle for ``grpcs://`` URLs. ``None``
        # means "use system roots" (handed straight through to
        # ``grpc.ssl_channel_credentials(root_certificates=None)``).
        self._ca_cert = ca_cert
        # M14 W3 — mTLS + bearer token.
        self._client_cert = client_cert
        self._client_key = client_key
        self._bearer_token = bearer_token
        self._transport: Any | None = None
        self._closed = False

    @classmethod
    def connect(
        cls,
        url: str,
        *,
        options: dict[str, Any] | None = None,
        ca_cert: bytes | None = None,
        client_cert: bytes | None = None,
        client_key: bytes | None = None,
        bearer_token: str | None = None,
    ) -> Self:
        """Build a connection wrapper bound to ``url``.

        Supported URL schemes:

          - ``memory://<name>``   — in-process driver registered via
            ``InProcessTransport`` (or the legacy ``FakeRtiServer`` alias).
          - ``grpc://host:port``  — real gRPC over plaintext TCP.
          - ``grpcs://host:port`` — real gRPC over TLS. ``ca_cert``
            (PEM bytes) populates the trust store for verifying the
            rtid server cert; pass ``None`` to rely on system roots
            (typical when the rtid cert chains to a publicly trusted CA).

        ``connect()`` is intentionally synchronous so it can be used as the
        head of an ``async with`` statement::

            async with RtiConnection.connect(
                "grpcs://rtid.example.com:8442",
                ca_cert=Path("ca.pem").read_bytes(),
            ) as rti:
                ...

        The actual transport setup happens inside ``__aenter__``.
        """
        return cls(
            url,
            options=options,
            ca_cert=ca_cert,
            client_cert=client_cert,
            client_key=client_key,
            bearer_token=bearer_token,
        )

    async def __aenter__(self) -> Self:
        """Open the transport.

        Dispatch by URL scheme:

          - ``memory://``  — look up the registered in-process driver.
          - ``grpc://``    — open a plaintext ``grpc.aio.insecure_channel``.
          - ``grpcs://``   — open a TLS ``grpc.aio.secure_channel`` using
            ``self._ca_cert`` as the root CA bundle (or system roots if
            ``ca_cert`` was not supplied).

        Anything else raises ``ValueError`` at connect time.
        """
        scheme, _, _ = self._url.partition("://")
        if scheme == "memory":
            transport = _lookup_transport(self._url)
            if transport is None:
                raise RuntimeError(
                    f"no in-process transport registered for {self._url!r} — "
                    "construct an InProcessTransport (or the legacy "
                    "FakeRtiServer alias) first; it auto-registers under "
                    "memory://fake-rti"
                )
            self._transport = transport
        elif scheme in ("grpc", "grpcs"):
            # Real gRPC transport. Plaintext for ``grpc://``;
            # TLS-secured (server auth, no mTLS) for ``grpcs://``.
            # ``ca_cert`` is forwarded to ``ssl_channel_credentials``;
            # ``grpc://`` ignores it.
            self._transport = await _build_grpc_transport(
                self._url,
                ca_cert=self._ca_cert,
                client_cert=self._client_cert,
                client_key=self._client_key,
                bearer_token=self._bearer_token,
            )
        else:
            raise ValueError(
                f"unsupported URL scheme {scheme!r} "
                "(expected 'memory', 'grpc', or 'grpcs')"
            )
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.close()

    async def close(self) -> None:
        """Tear down the connection. Idempotent."""
        if self._closed:
            return
        self._closed = True
        transport = self._transport
        self._transport = None
        # GrpcTransport owns a real gRPC channel + background stream
        # tasks; close them. The fake has no close() and we ignore the
        # AttributeError silently.
        close_fn = getattr(transport, "close", None)
        if callable(close_fn):
            with contextlib.suppress(BaseException):
                result = close_fn()
                if inspect.isawaitable(result):
                    await result

    @property
    def transport(self) -> Any:
        """Internal: the open transport (fake or gRPC). Raises if not entered."""
        if self._transport is None:
            raise RuntimeError(
                "RtiConnection is not open — use `async with RtiConnection.connect(...)`"
            )
        return self._transport

    def join_federation(
        self,
        spec: FederationSpec,
        *,
        federate_name: str,
        federate_type: str = "",
    ) -> _FederateContextManager:
        """Open the per-federate async context manager.

        Use as ``async with rti.join_federation(spec, federate_name="x") as fed``.

        ``federate_type`` is the optional HLAfederateType designator for the
        joining federate (M13 thread B — docs/srs.md §10.4); defaults to
        empty string (cut-1 behavior). When set, the rtid records it on
        the federation roster and surfaces it via the MOM HLAfederate
        attribute set.
        """
        return _FederateContextManager(self, spec, federate_name, federate_type)


class _FederateContextManager:
    """Internal: returned by RtiConnection.join_federation.

    On enter: records a ``create_federation`` call (idempotent on the server
    side; canned exceptions like ``FederationAlreadyExists`` propagate from
    the fake) followed by ``join_federation``, then allocates a federate
    handle and constructs the Federate.

    On exit: records ``resign_federation``. The connection itself is owned
    by the outer ``async with RtiConnection.connect(...)`` — we do not close
    it here.
    """

    def __init__(
        self,
        connection: RtiConnection,
        spec: FederationSpec,
        federate_name: str,
        federate_type: str = "",
    ) -> None:
        self._connection = connection
        self._spec = spec
        self._federate_name = federate_name
        self._federate_type = federate_type
        self._federate: Federate | None = None

    async def __aenter__(self) -> Federate:
        transport = self._connection.transport
        # create_federation is idempotent on the server side; if it
        # already exists with a compatible FOM the fake/server returns
        # success. If the server rejects (e.g. ERR_FED_ALREADY_EXISTS
        # cannot be reconciled), the typed exception propagates.
        await _dispatch(transport, "create_federation", spec=self._spec)
        join_response = await _dispatch(
            transport,
            "join_federation",
            spec=self._spec,
            federate_name=self._federate_name,
            federate_type=self._federate_type,
        )
        # The real gRPC transport returns the federate handle from
        # JoinFederationResponse; the fake returns None and the SDK falls
        # back to allocate_handle() (matching the legacy contract).
        if isinstance(join_response, int):
            handle = int(join_response)
        else:
            handle = int(transport.allocate_handle())
        federate = Federate(
            transport=transport,
            handle=handle,
            name=self._federate_name,
        )
        self._federate = federate
        return federate

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        federate = self._federate
        if federate is None:
            return
        # Resign even if the body raised; swallowing typed RTI errors here
        # would mask the original exception. The only swallowed case is
        # RuntimeError from a closed connection (outer __aexit__ ran first).
        with contextlib.suppress(RuntimeError):
            await _dispatch(
                self._connection.transport,
                "resign_federation",
                federate_handle=federate.handle,
                federate_name=federate.name,
            )


class Federate:
    """A joined federate. Created via ``rti.join_federation(...)``.

    Public attributes ``name`` and ``handle`` are FROZEN-shape. The full
    pub/sub/object/interaction surface below is wired by TASK-064..067.

    M12 W2 added cut-3 service-group accessors (:meth:`sync`,
    :meth:`ownership`, :meth:`ddm`, :meth:`savepoint`) that lazily
    construct dedicated client wrappers around the same underlying
    gRPC channel. M12 W3 adds :meth:`mom` for the read-only
    Management Object Model introspection surface.
    """

    name: str
    handle: int

    def __init__(self, *, transport: Any, handle: int, name: str) -> None:
        self._transport = transport
        self.handle = handle
        self.name = name
        # Lazily-instantiated cut-3 service-group clients. Each is
        # built on first attribute access (the transport may not have
        # the channel attribute when running over the in-process
        # FakeRtiServer; the accessors handle that case explicitly).
        self._sync_client: Any | None = None
        self._ownership_client: Any | None = None
        self._ddm_client: Any | None = None
        self._savepoint_client: Any | None = None
        self._mom_client: Any | None = None
        self._support_client: Any | None = None

    # --- Declaration management (TASK-064) ---

    async def publish_object_class(
        self, class_name: str, *, attributes: list[str]
    ) -> None:
        """Declare publication of an object class + its attributes."""
        await _dispatch(
            self._transport,
            "publish_object_class",
            federate_handle=self.handle,
            class_name=class_name,
            attributes=list(attributes),
        )

    async def subscribe_object_class(
        self, class_name: str, *, attributes: list[str]
    ) -> None:
        """Declare subscription to an object class + its attributes."""
        await _dispatch(
            self._transport,
            "subscribe_object_class",
            federate_handle=self.handle,
            class_name=class_name,
            attributes=list(attributes),
        )

    async def publish_interaction_class(self, class_name: str) -> None:
        """Declare publication of an interaction class."""
        await _dispatch(
            self._transport,
            "publish_interaction_class",
            federate_handle=self.handle,
            class_name=class_name,
        )

    async def subscribe_interaction_class(self, class_name: str) -> None:
        """Declare subscription to an interaction class."""
        await _dispatch(
            self._transport,
            "subscribe_interaction_class",
            federate_handle=self.handle,
            class_name=class_name,
        )

    # --- Object management (TASK-065) ---

    async def register_object_instance(
        self, class_name: str, *, instance_name: str | None = None
    ) -> int:
        """Register an instance and return its handle.

        If the transport returns a non-None canned response, treat it as
        the handle (lets tests pin handles). Otherwise, allocate a fresh
        monotonic handle from the transport.
        """
        response = await _dispatch(
            self._transport,
            "register_object_instance",
            federate_handle=self.handle,
            class_name=class_name,
            instance_name=instance_name,
        )
        if isinstance(response, int):
            return response
        return int(self._transport.allocate_handle())

    async def update_attributes(
        self,
        object_handle: int,
        values: dict[str, Any],
        *,
        timestamp: float | None = None,
    ) -> None:
        """Update one or more attribute values on an object instance."""
        await _dispatch(
            self._transport,
            "update_attributes",
            federate_handle=self.handle,
            object_handle=object_handle,
            values=dict(values),
            timestamp=timestamp,
        )

    # --- Interaction management (TASK-066) ---

    async def send_interaction(
        self,
        class_name: str,
        parameters: dict[str, Any],
        *,
        timestamp: float | None = None,
    ) -> None:
        """Send an interaction with the given parameters."""
        await _dispatch(
            self._transport,
            "send_interaction",
            federate_handle=self.handle,
            class_name=class_name,
            parameters=dict(parameters),
            timestamp=timestamp,
        )

    # --- Time management (TASK-067) ---

    async def enable_time_regulation(self, lookahead: float) -> None:
        """Become time-regulating with the given lookahead."""
        await _dispatch(
            self._transport,
            "enable_time_regulation",
            federate_handle=self.handle,
            lookahead=lookahead,
        )

    async def enable_time_constrained(self) -> None:
        """Become time-constrained."""
        await _dispatch(
            self._transport,
            "enable_time_constrained",
            federate_handle=self.handle,
        )

    async def next_message_request(self, time: float) -> None:
        """Request advance to ``time``. Grant arrives via events()."""
        await _dispatch(
            self._transport,
            "next_message_request",
            federate_handle=self.handle,
            time=time,
        )

    # --- M22 TASK-221: TimeService surface parity ---

    async def disable_time_regulation(self) -> None:
        """Stop being time-regulating. ``ErrTimeRegulationNotEnabled`` if not."""
        await _dispatch(
            self._transport,
            "disable_time_regulation",
            federate_handle=self.handle,
        )

    async def disable_time_constrained(self) -> None:
        """Stop being time-constrained. ``ErrTimeConstrainedNotEnabled`` if not."""
        await _dispatch(
            self._transport,
            "disable_time_constrained",
            federate_handle=self.handle,
        )

    async def modify_lookahead(self, lookahead: float) -> None:
        """Mutate lookahead without re-enabling regulation."""
        await _dispatch(
            self._transport,
            "modify_lookahead",
            federate_handle=self.handle,
            lookahead=lookahead,
        )

    async def next_message_request_available(self, time: float) -> None:
        """NMRA(t). Grant time may equal LBTS (vs NER's strict-less)."""
        await _dispatch(
            self._transport,
            "next_message_request_available",
            federate_handle=self.handle,
            time=time,
        )

    async def time_advance_request(self, time: float) -> None:
        """TAR(t). Grant fires at min(t, LBTS); pending always clears on grant."""
        await _dispatch(
            self._transport,
            "time_advance_request",
            federate_handle=self.handle,
            time=time,
        )

    async def time_advance_request_available(self, time: float) -> None:
        """TARA(t). Grant time may equal LBTS."""
        await _dispatch(
            self._transport,
            "time_advance_request_available",
            federate_handle=self.handle,
            time=time,
        )

    async def flush_queue_request(self, time: float) -> None:
        """FQR(t). Force-deliver all messages with timestamp ≤ t."""
        await _dispatch(
            self._transport,
            "flush_queue_request",
            federate_handle=self.handle,
            time=time,
        )

    async def query_logical_time(self) -> float:
        """Return the federate's current logical time."""
        return await _dispatch(
            self._transport,
            "query_logical_time",
            federate_handle=self.handle,
        )

    async def query_lookahead(self) -> float:
        """Return the federate's current lookahead. Errors if not regulating."""
        return await _dispatch(
            self._transport,
            "query_lookahead",
            federate_handle=self.handle,
        )

    async def query_lbts(self) -> tuple[float, bool]:
        """Return ``(lbts, finite)``. ``finite=False`` ⇒ no regulators."""
        return await _dispatch(
            self._transport,
            "query_lbts",
        )

    async def enable_asynchronous_delivery(self) -> None:
        """Enable async TSO delivery. ``TimeAlreadyAsynchronous`` if already on.

        Per IEEE 1516.1 §8.16: when enabled, TSO messages are delivered as
        soon as produced (gorti's pre-M22 behavior). When disabled (the
        default per spec), TSO messages with timestamp t > federate's
        currentTime are buffered server-side until the federate advances
        past t. RO messages are unaffected.
        """
        await _dispatch(
            self._transport,
            "enable_asynchronous_delivery",
            federate_handle=self.handle,
        )

    async def disable_asynchronous_delivery(self) -> None:
        """Disable async TSO delivery. ``TimeNotAsynchronous`` if already off."""
        await _dispatch(
            self._transport,
            "disable_asynchronous_delivery",
            federate_handle=self.handle,
        )

    # --- M23 W1: §6 delete_object_instance ---

    async def delete_object_instance(
        self,
        object_handle: int,
        tag: bytes = b"",
        timestamp: float | None = None,
    ) -> None:
        """Delete an object instance owned by this federate.

        Per IEEE 1516.1-2010 §6.16. Subscribers receive a
        ``RemoveObjectInstance`` event on their events() stream.
        """
        await _dispatch(
            self._transport,
            "delete_object_instance",
            federate_handle=self.handle,
            object_handle=object_handle,
            tag=tag,
            timestamp=timestamp,
        )

    async def local_delete_object_instance(self, object_handle: int) -> None:
        """Federate-local cleanup; no peer notification (§6.18, M23)."""
        await _dispatch(
            self._transport,
            "local_delete_object_instance",
            federate_handle=self.handle,
            object_handle=object_handle,
        )

    async def request_attribute_value_update(
        self, object_handle: int, attribute_handles: list[int], tag: bytes = b"",
    ) -> None:
        """Pull-style resync: ask the owner to emit fresh values (§6.24, M23).

        The owner receives a ``ProvideAttributeValueUpdate`` event on
        its events() stream and is expected to respond with
        ``update_attributes``.
        """
        await _dispatch(
            self._transport,
            "request_attribute_value_update",
            federate_handle=self.handle,
            object_handle=object_handle,
            attribute_handles=attribute_handles,
            tag=tag,
        )

    async def request_class_attribute_value_update(
        self,
        object_class_handle: int,
        attribute_handles: list[int],
        tag: bytes = b"",
    ) -> None:
        """Class-scoped pull (§6.25, M23). Every owner of any instance of
        the class receives a ProvideAttributeValueUpdate event."""
        await _dispatch(
            self._transport,
            "request_class_attribute_value_update",
            federate_handle=self.handle,
            object_class_handle=object_class_handle,
            attribute_handles=attribute_handles,
            tag=tag,
        )

    async def change_attribute_transportation_type(
        self, object_handle: int, attribute_handles: list[int], transport: int,
    ) -> None:
        """Per-instance per-attribute transport override (§6.20, M23).

        ``transport`` is one of the ``TRANSPORTATION_TYPE_*`` enum values
        from ``rti.v1.common_pb2.TransportationType``. M23 ships record-
        only — the wire path doesn't yet route per-message transport.
        """
        await _dispatch(
            self._transport,
            "change_attribute_transportation_type",
            federate_handle=self.handle,
            object_handle=object_handle,
            attribute_handles=attribute_handles,
            transport_type=transport,
        )

    async def change_interaction_transportation_type(
        self, interaction_class_handle: int, transport: int,
    ) -> None:
        """Per-publisher per-class transport override (§6.22, M23)."""
        await _dispatch(
            self._transport,
            "change_interaction_transportation_type",
            federate_handle=self.handle,
            interaction_class_handle=interaction_class_handle,
            transport_type=transport,
        )

    # --- Cut-3 service-group accessors (M12 W2) ---
    #
    # Each property lazily constructs a dedicated thin client wrapper
    # around the same gRPC channel the federate's transport already
    # holds. The clients live in sibling modules (``rti1516e.sync``,
    # ``.ownership``, ``.ddm``, ``.savepoint``) and follow a uniform
    # constructor signature: ``(channel, federation_name, federate_handle)``.
    #
    # Memory:// transports do not own a real gRPC channel, so the
    # accessors raise a clear RuntimeError there. The intended use is
    # cross-process tests + production federates over real gRPC.

    @property
    def sync(self) -> Any:
        """Cut-3 SyncService client for §4.6-4.7 sync-point primitives."""
        if self._sync_client is None:
            from rti1516e.sync import SyncClient

            self._sync_client = SyncClient(
                self._require_channel(),
                federation_name=self._require_federation_name(),
                federate_handle=self.handle,
            )
        return self._sync_client

    @property
    def ownership(self) -> Any:
        """Cut-3 OwnershipService client for §7 negotiated transfer + queries."""
        if self._ownership_client is None:
            from rti1516e.ownership import OwnershipClient

            self._ownership_client = OwnershipClient(
                self._require_channel(),
                federation_name=self._require_federation_name(),
                federate_handle=self.handle,
            )
        return self._ownership_client

    @property
    def ddm(self) -> Any:
        """Cut-3 DDMService client for §6 region-scoped pub/sub + filtering."""
        if self._ddm_client is None:
            from rti1516e.ddm import DDMClient

            self._ddm_client = DDMClient(
                self._require_channel(),
                federation_name=self._require_federation_name(),
                federate_handle=self.handle,
            )
        return self._ddm_client

    @property
    def savepoint(self) -> Any:
        """Cut-3 SavepointService client for §4.8-4.15 federation save/restore."""
        if self._savepoint_client is None:
            from rti1516e.savepoint import SavepointClient

            self._savepoint_client = SavepointClient(
                self._require_channel(),
                federation_name=self._require_federation_name(),
                federate_handle=self.handle,
            )
        return self._savepoint_client

    @property
    def mom(self) -> Any:
        """Cut-3 MomService client for §10 MOM introspection.

        Read-only surface for HLAfederation / HLAfederate object
        snapshots and the per-federate counter set. Federates poll
        these accessors at whatever cadence suits their use case;
        the runtime is goroutine-safe and the snapshot RPCs are
        O(federates).
        """
        if self._mom_client is None:
            from rti1516e.mom import MomClient

            self._mom_client = MomClient(
                self._require_channel(),
                federation_name=self._require_federation_name(),
                federate_handle=self.handle,
            )
        return self._mom_client

    @property
    def support(self) -> Any:
        """§10.2 SupportService client — handle / name / dimension / order /
        transport lookups against the federation's FOM. M25 Phase B.

        Read-only; safe to call concurrently from any task. Lookups are
        FOM-driven so the returned handles match what the federation
        wire RPCs accept.
        """
        if self._support_client is None:
            from rti1516e.support import SupportClient

            self._support_client = SupportClient(
                self._require_channel(),
                federation_name=self._require_federation_name(),
            )
        return self._support_client

    def _require_channel(self) -> Any:
        """Return the underlying ``grpc.aio.Channel`` or raise RuntimeError.

        Cut-3 service-group clients dial real gRPC stubs directly; the
        in-process memory:// transport (FakeRtiServer) has no channel.
        Raise a clear error rather than failing inside the stub
        constructor, which would surface as ``AttributeError``.
        """
        channel = getattr(self._transport, "channel", None)
        if channel is None:
            raise RuntimeError(
                "Federate.{sync,ownership,ddm,savepoint,mom} require a "
                "real gRPC transport (grpc:// or grpcs://); the in-process "
                "memory:// transport does not expose a channel"
            )
        return channel

    def _require_federation_name(self) -> str:
        """Return the active federation name or raise RuntimeError.

        Set by ``GrpcTransport`` on every successful create_federation /
        join_federation. Should always be populated by the time the
        federate is observable via ``join_federation()`` __aenter__,
        but the defensive check guards against tests that skip the
        join (none exist today; documented for future-proofing).
        """
        name = getattr(self._transport, "_federation_name", None)
        if not isinstance(name, str) or not name:
            raise RuntimeError(
                "Federate cut-3 clients require a joined federation name; "
                "the transport reports none — was join_federation called?"
            )
        return name

    # --- Event stream (TASK-067) ---

    def events(self) -> AsyncIterator[Any]:
        """Yield events emitted by the RTI to this federate.

        Each event is one of the dataclasses in rti1516e.events. The stream
        is open-ended; callers exit via ``break`` or by closing the
        federate context. Backed by ``transport.events_for(handle)`` which
        returns an ``asyncio.Queue`` populated by the test fixture (or, in
        production, drained from the server-streaming RPC).
        """
        return self._iter_events()

    async def _iter_events(self) -> AsyncIterator[Any]:
        queue = self._transport.events_for(self.handle)
        while True:
            event = await queue.get()
            yield event
