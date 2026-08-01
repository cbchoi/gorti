"""StubCoupledModel — pyjevsim CoupledModel substitute for bridge tests.

The bridge tests (test_spec_m4_time_advance.py, test_spec_m4_select_preserve.py)
drive HLAFederate against this stub instead of a real pyjevsim coupled
model. That keeps the spec tests free of a hard pyjevsim runtime
dependency and gives full control over ta() / output / external
transition behavior.

Real pyjevsim's drift is caught separately by
test_spec_m4_pyjevsim_smoke.py (TASK-072), which DOES import pyjevsim
and fails loudly on missing symbols.
"""

from __future__ import annotations

from collections import deque
from dataclasses import dataclass
from typing import Any


@dataclass
class ExternalTransitionCall:
    """Recorded external_transition invocation for spec test asserts."""

    port: str
    payload: Any


class StubCoupledModel:
    """In-memory CoupledModel stand-in.

    Construct with a pre-loaded ta_schedule (deque of float values; each
    call to time_advance pops one) and a per-call output_schedule (deque
    of dict[port, payload]; each output_handler call pops one).

    Records every internal_transition + external_transition call so the
    test can assert delivery order.
    """

    def __init__(
        self,
        *,
        ta_schedule: list[float] | None = None,
        output_schedule: list[dict[str, Any]] | None = None,
    ) -> None:
        self._ta = deque(ta_schedule or [])
        self._outputs = deque(output_schedule or [])
        self.internal_transitions: int = 0
        self.external_transitions: list[ExternalTransitionCall] = []
        # Last value returned by time_advance (for tests asserting "now+ta").
        self.last_ta: float = 0.0

    # --- CoupledModelProtocol surface ---------------------------------------

    def time_advance(self) -> float:
        """Pop the next ta from the schedule."""
        if not self._ta:
            raise RuntimeError("StubCoupledModel: ta_schedule exhausted")
        ta = self._ta.popleft()
        self.last_ta = ta
        return ta

    def output_handler(self) -> dict[str, Any]:
        """Pop the next output dict from the schedule (or {} if exhausted)."""
        if not self._outputs:
            return {}
        return self._outputs.popleft()

    def internal_transition(self) -> None:
        """Count internal transitions for assertions."""
        self.internal_transitions += 1

    def external_transition(self, port: str, payload: Any) -> None:
        """Record external transitions in arrival order."""
        self.external_transitions.append(ExternalTransitionCall(port, payload))

    # --- Test-only helpers --------------------------------------------------

    def remaining_ta(self) -> int:
        """Number of ta values left in the schedule."""
        return len(self._ta)

    def remaining_outputs(self) -> int:
        """Number of output dicts left in the schedule."""
        return len(self._outputs)
