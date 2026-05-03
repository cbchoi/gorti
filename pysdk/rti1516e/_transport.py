"""Transport registry — pluggable in-process doubles for the RTI gRPC client.

The Layer 1 SDK (rti1516e.connection.RtiConnection) talks to an rtid binary
over gRPC in production. For spec tests, a pure-Python ``FakeRtiServer``
(in pysdk/tests/spec/m4/_fakes/fake_rti_server.py) substitutes for the
real channel. The registry here is the seam: ``register_fake(url, srv)``
binds an in-memory transport under a URL; ``lookup(url)`` returns it (or
None for unregistered URLs).

URL convention:
    - ``memory://<name>``      — in-process fake; must be registered first
    - ``grpc://host:port``     — real gRPC channel (TASK-063 follow-up;
                                  raises NotImplementedError for now)

This module is internal to rti1516e and is not part of the public API.
The fake server auto-registers itself under ``memory://fake-rti`` from
its ``__init__`` so spec tests can construct a fake and immediately
``connect("memory://fake-rti")`` without extra wiring.
"""

from __future__ import annotations

from typing import Any

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
