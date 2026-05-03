"""M4 determinism gate — examples/pyjevsim/ runs 10× same seed produce
sha256-identical RTI-side event logs.

SCAFFOLD: this test is intentionally skipped until examples/pyjevsim/
lands (TASK-073) and TASK-074 wires the harness. Agent C unskips it
when their implementation is ready, mirroring the M3 pattern
(rti/spec/M3/replay_test.go was scaffolded then unskipped by W4).

Implements: NFR-DET-1, NFR-DET-2; M4 exit criterion #2.
"""

from __future__ import annotations

import pytest


@pytest.mark.spec
def test_spec_m4_determinism_10x_same_seed() -> None:
    pytest.skip(
        "scaffolded; Agent C turns this into a real test once "
        "examples/pyjevsim/ + TASK-074 land"
    )
