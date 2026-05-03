"""End-to-end cross-language smoke test (TASK-081 cut-1).

This is the *example-tree* counterpart to
``pysdk/tests/spec/m5/test_spec_m5_cross_language.py`` — both call
``cross_lang_runner.run_cross_language_smoke``. Keeping a copy under
``examples/pyjevsim/`` lets contributors run the smoke from the example
tree without pulling in the full pysdk test infrastructure (mirrors the
M4 W7 ``determinism_test.py`` pattern).

The cut-1 acceptance is:
  - rtid binary launches as a subprocess
  - Two Python federates (publisher + subscriber) join via real gRPC
  - The publisher's send_count == subscriber's receive_count
"""

from __future__ import annotations

import asyncio
import sys
from pathlib import Path

import pytest

_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

# ruff: noqa: E402  (sys.path tweak above must precede project imports)
from cross_lang_runner import run_cross_language_smoke


@pytest.mark.spec
@pytest.mark.integration
def test_cross_language_python_and_go_federates_share_state() -> None:
    """Smoke: rtid + two Python federates exchange interactions consistently.

    The "and Go" in the test name is aspirational for the bidirectional
    cut-2; cut-1 ships a Python-publisher / Python-subscriber pair against
    a real rtid. See cross_lang_runner.py module docstring for the
    rationale + deferral note.
    """
    result = asyncio.run(run_cross_language_smoke(rounds=10))
    assert result["py_pub_sent"] == 10
    assert result["py_sub_received"] == 10
    # Wire-payload consistency: subscriber's payloads should match the
    # publisher's monotonic seq numbers, in order.
    expected = [
        (i + 1).to_bytes(4, byteorder="big", signed=False) for i in range(10)
    ]
    assert result["py_sub_payloads"] == expected
