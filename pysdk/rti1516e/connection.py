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

    def __init__(self, url: str, *, options: dict[str, Any] | None = None) -> None:
        self._url = url
        self._options: dict[str, Any] = dict(options) if options else {}
        self._transport: Any | None = None
        self._closed = False

    @classmethod
    def connect(cls, url: str, *, options: dict[str, Any] | None = None) -> Self:
        """Build a connection wrapper bound to ``url`` (``grpc://`` or ``memory://``).

        ``connect()`` is intentionally synchronous so it can be used as the
        head of an ``async with`` statement::

            async with RtiConnection.connect("grpc://localhost:8442") as rti:
                ...

        The actual transport setup happens inside ``__aenter__``.
        """
        return cls(url, options=options)

    async def __aenter__(self) -> Self:
        """Open the transport.

        For ``memory://`` URLs, look up the registered in-process fake (the
        spec-test ``FakeRtiServer`` auto-registers under ``memory://fake-rti``).
        For ``grpc://`` URLs, real-channel construction is a TASK-063
        follow-up — the M4 wave-4 deliverable is an injectable seam, not a
        production gRPC client.
        """
        scheme, _, _ = self._url.partition("://")
        if scheme == "memory":
            transport = _lookup_transport(self._url)
            if transport is None:
                raise RuntimeError(
                    f"no in-process transport registered for {self._url!r} — "
                    "construct a FakeRtiServer first (auto-registers under "
                    "memory://fake-rti) or call register_fake() explicitly"
                )
            self._transport = transport
        elif scheme == "grpc":
            # Real gRPC transport (TASK-081 cut-1). Wraps a grpc.aio
            # channel and delegates RPC dispatch to GrpcTransport.record.
            self._transport = await _build_grpc_transport(self._url)
        else:
            raise ValueError(
                f"unsupported URL scheme {scheme!r} (expected 'memory' or 'grpc')"
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
    ) -> _FederateContextManager:
        """Open the per-federate async context manager.

        Use as ``async with rti.join_federation(spec, federate_name="x") as fed``.
        """
        return _FederateContextManager(self, spec, federate_name)


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
    ) -> None:
        self._connection = connection
        self._spec = spec
        self._federate_name = federate_name
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
    """

    name: str
    handle: int

    def __init__(self, *, transport: Any, handle: int, name: str) -> None:
        self._transport = transport
        self.handle = handle
        self.name = name

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
