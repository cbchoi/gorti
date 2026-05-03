"""M5 — Federate-side mode verification.

When the federation is created with `mode=best-effort` AND a published
attribute is declared best-effort in the FOM, the Python SDK transmits
the update with RO (Receive Order) semantics — no timestamp; the
subscriber's ReflectAttributeValues event yields `timestamp=None`.

Implements: FR-OM-3 (Python side); NFR-PERF-1..4. M5 mode contract.
"""

from __future__ import annotations

import pytest


@pytest.mark.spec
def test_spec_m5_best_effort_attribute_delivers_ro() -> None:
    """Publish a best-effort attribute; subscriber receives RO event.

    SCAFFOLD: requires (1) Agent A's TASK-077 best-effort RO path
    landing in the Go RTI, (2) Agent C's real-gRPC transport in the
    Python SDK (M4 follow-up; bundled with TASK-081). Until both
    land, this test skips; Agent C unskips in TASK-082.
    """
    pytest.skip(
        "scaffolded; Agent C unskips in TASK-082 once "
        "(a) Agent A's TASK-077 best-effort RO landing path is on main "
        "and (b) the Python SDK's real-gRPC transport is wired."
    )


@pytest.mark.spec
def test_spec_m5_verbose_attribute_delivers_tso() -> None:
    """Regression check: in verbose mode, normal attributes still TSO.

    SCAFFOLD: same dependencies as above.
    """
    pytest.skip(
        "scaffolded; Agent C unskips in TASK-082 alongside the best-effort test"
    )
