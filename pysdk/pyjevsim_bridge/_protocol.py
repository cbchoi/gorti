"""typing.Protocol shims for pyjevsim symbols the bridge depends on.

Spec tests under pysdk/tests/spec/m4/ avoid importing real pyjevsim
(it's an optional runtime dep). They consume these Protocols via the
StubCoupledModel test double; the real pyjevsim classes also satisfy
them at runtime via duck typing.

Agent C's TASK-072 pyjevsim API drift smoke test is what catches
breaking changes in real pyjevsim — these Protocols are the contract,
the smoke test is the runtime check.
"""

from __future__ import annotations

from typing import Any, Protocol, runtime_checkable


@runtime_checkable
class CoupledModelProtocol(Protocol):
    """Subset of pyjevsim.CoupledModel the bridge requires.

    Real pyjevsim exposes more — the bridge only reaches for these.
    """

    def time_advance(self) -> float:
        """DEVS ``ta`` — when the next internal event fires (seconds)."""
        ...

    def output_handler(self) -> dict[str, Any]:
        """Returns a dict of {output_port_name: payload} drained on
        internal-transition cycle. The bridge maps each port to its
        corresponding FOM interaction class via PortMapping."""
        ...

    def internal_transition(self) -> None:
        """Advance internal state after output_handler."""
        ...

    def external_transition(self, port: str, payload: Any) -> None:
        """Apply an externally-arrived event."""
        ...
