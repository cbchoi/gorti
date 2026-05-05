"""typing.Protocol shims for the bridge's coupled-model contract.

This Protocol describes what the BRIDGE needs from a coupled-model-shaped
object — it uses DEVS-canonical names (`time_advance`, `output_handler`,
`internal_transition`, `external_transition`) for clarity.

Spec tests under pysdk/tests/spec/m4/ consume this Protocol via the
StubCoupledModel test double (avoids a hard pyjevsim runtime dep).

Real pyjevsim (1.3.x and 2.0.x — API-compatible at every surface the
bridge touches) exports `StructuralModel` (coupled) + `BehaviorModel`
(atomic) with DIFFERENT method names than this Protocol:

  pyjevsim                this Protocol
  --------                -------------
  output()                output_handler()
  int_trans()             internal_transition()
  ext_trans(input_ports)  external_transition(port, payload)
  (computed via SysExecutor) → time_advance() returns ta

W7 (TASK-073 — examples) wires an adapter that maps the real pyjevsim
SysExecutor + StructuralModel surface to this Protocol. W6 (TASK-070/071)
codes against the Protocol and the StubCoupledModel; pyjevsim
adaptation is intentionally out of scope until the example runner.

The TASK-072 smoke test (test_spec_m4_pyjevsim_smoke.py) confirms the
required real-pyjevsim symbols (StructuralModel, BehaviorModel) are
importable; that's the only direct dependency on the upstream API.
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
