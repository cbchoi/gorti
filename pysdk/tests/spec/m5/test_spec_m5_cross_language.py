"""M5 — Cross-language smoke: Python pyjevsim federate joins a Go-hosted
federation; both Python and Go federates exchange interactions; both
observe consistent state.

Implements: M5 cross-language end-to-end goal; TASK-081 contract.
"""

from __future__ import annotations

import pytest


@pytest.mark.spec
def test_spec_m5_python_federate_joins_go_federation() -> None:
    """End-to-end: rtid binary + go-pingpong federate + Python federate.

    Test orchestration:
      1. Build the rtid binary (`go build ./rti/cmd/rtid`).
      2. Launch rtid as a subprocess on a free port.
      3. Launch go-pingpong as a subprocess (it joins the federation).
      4. Drive a Python pyjevsim HLAFederate that joins the same
         federation, exchanges N interactions with the Go side.
      5. Assert: both federates see consistent state in the RTI's
         event log.

    SCAFFOLD: depends on (a) Agent A's TASK-078 hardening landing the
    soak path so the rtid handles cross-language RPCs cleanly, (b) the
    Python SDK's real-gRPC transport (M4 follow-up). Agent C unskips
    in TASK-081.
    """
    pytest.skip(
        "scaffolded; Agent C wires examples/pyjevsim/cross_lang_test.py in "
        "TASK-081 once Agent A's hardening + Python SDK gRPC transport land."
    )
