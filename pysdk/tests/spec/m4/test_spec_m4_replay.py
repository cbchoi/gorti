"""M4 replay gate — Python-driven replay: an event log captured from
the Python pyjevsim example replays byte-identical when re-fed through
the rtid replay-from-log mode.

SCAFFOLD: skipped until examples/pyjevsim/ lands (TASK-073). Agent C
unskips when ready.

Implements: FR-EVT-3, NFR-DET-2.
"""

from __future__ import annotations

import pytest


@pytest.mark.spec
def test_spec_m4_python_example_replays_byte_identical() -> None:
    pytest.skip("scaffolded; Agent C turns this into a real test once examples/pyjevsim/ lands")
