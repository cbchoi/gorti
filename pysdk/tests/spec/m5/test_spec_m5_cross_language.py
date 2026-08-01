"""M5 — Cross-language smoke: Python pyjevsim federate joins a rtid-hosted
federation; bidirectional event flow goes through the real-gRPC transport
end-to-end.

Implements: M5 cross-language end-to-end goal; TASK-081 contract.

Cut-1 scope (per W1C "pragmatic cut-1" in docs/M5_DISPATCH_PLAN.md §3):
  - The rtid binary launches as a subprocess.
  - Two Python federates (publisher + subscriber) join the same federation
    over real gRPC and exchange ``ROUNDS`` interactions.
  - Subscriber receives every interaction the publisher sent, in order,
    with bytewise-identical payloads.

Why two-Python instead of Python+Go: the Go-side reference example
(examples/go-pingpong) is a subprocess-shim that spawns its own rtid in
a special demo mode; it does NOT open a gRPC channel to a separately
running rtid. Implementing a Go federate that DOES connect by gRPC
requires touching ``rti/`` ( territory) and is deferred. The
two-Python smoke still validates the cut-1 acceptance criteria —
real-gRPC transport works cross-process, both ends observe consistent
state, declarations + event-stream subscriptions wire correctly.
"""

from __future__ import annotations

import asyncio
import shutil
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[4]
EXAMPLES_DIR = REPO_ROOT / "examples" / "pyjevsim"
if str(EXAMPLES_DIR) not in sys.path:
    sys.path.insert(0, str(EXAMPLES_DIR))


@pytest.mark.spec
@pytest.mark.integration
def test_spec_m5_python_federate_joins_go_federation() -> None:
    """End-to-end: rtid binary + two Python federates over real gRPC.

    The test name preserves the original spec-scaffold name ("python
    federate joins go federation") for traceability; the
    cut-1 implementation drives two Python federates against a real
    rtid (cross-process, real gRPC). A Python+Go follow-up is tracked
    in the deferral notes inside ``examples/pyjevsim/cross_lang_runner.py``.
    """
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; cannot build rtid for the smoke test")

    # Lazy import: keeps the module-level test-collection cost low and
    # lets the skip above fire BEFORE we try to import code that may
    # itself require a running rtid (or a freshly built one).
    from cross_lang_runner import (
        run_cross_language_smoke,
    )

    result = asyncio.run(run_cross_language_smoke(rounds=5))
    assert result["py_pub_sent"] == 5, (
        f"publisher sent {result['py_pub_sent']} interactions; "
        f"expected exactly 5"
    )
    assert result["py_sub_received"] == 5, (
        f"subscriber received {result['py_sub_received']} interactions; "
        f"expected exactly 5 (consistent state with publisher)"
    )
    expected_payloads = [
        (i + 1).to_bytes(4, byteorder="big", signed=False) for i in range(5)
    ]
    assert result["py_sub_payloads"] == expected_payloads, (
        "subscriber received payloads in unexpected order or with wrong bytes"
    )
