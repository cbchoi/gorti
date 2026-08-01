"""M5 — Federate-side mode verification.

When the federation is created with ``mode="best-effort"`` AND a
published interaction class (or attribute) is declared
``<order>Receive</order>`` in the FOM, the Python SDK transmits the
update with RO (Receive Order) semantics — the subscriber's
``ReceiveInteraction.timestamp`` field arrives as ``None``. In
``mode="verbose"`` the federation operating mode forces TSO regardless
of the per-class order declaration: the subscriber sees the publisher's
timestamp unchanged.

Implements: FR-OM-3 (Python side); NFR-PERF-1..4. M5 mode contract.

Cut-1 scope (per ``docs/M5_DISPATCH_PLAN.md`` §3 W2B "pragmatic cut-1"):

  - The cross-language smoke from W1C (TASK-081) wires only interaction
    pub/sub/send through the real-gRPC transport; object attribute
    update dispatch is intentionally record-only in
    ``rti1516e/_transport.py``. The interaction RO/TSO decision in the
    Go object registry exercises the SAME mode-and-order code path as
    attribute updates do (see
    ``rti/internal/object/interaction.go::deliveryTimestampForInteraction``
    versus ``update.go::deliveryTimestampForAttributes``), so swapping
    the dimension keeps the M5 modes contract exercised end-to-end
    without depending on the deferred attribute-wire dispatch. The test
    name preserves the stable contract identifier
    ("attribute") while the body verifies the equivalent interaction
    path.

  - Test 1 (best-effort + Receive-order interaction → RO event) is now
    LIVE post M6 W1A + W1C. W1A landed cross-language handle alignment
    in ``pysdk/rti1516e/fom/parser.py::_load_mim`` (no longer dedupes
    IEEE 1516.1-2010 standard MIM duplicate-name interaction classes
    such as ``HLAadjust``, ``HLArequest``, ``HLAreport``,
    ``HLAreportFOMmoduleData``, ``HLArequestFOMmoduleData``,
    ``HLAsetSwitches``). W1C wired
    ``rti/cmd/rtid/main.go`` to call
    ``fomRepository.RememberFor(name, handle)`` after every successful
    ``CreateFederation`` (via the new
    ``grpcsvc.Options.OnCreateFederationSuccess`` hook on the
    federation gRPC service), so
    ``FOMRepoOrderLookup.InteractionOrder`` in
    ``rti/internal/transport/grpc/best_effort.go`` now resolves the
    FOM handle and consults the FOM's per-class ``<order>`` element.
    The pairwise handle alignment is locked in by
    ``pysdk/tests/test_handle_alignment.py`` and the post-create
    wiring is locked in by
    ``rti/cmd/rtid/foms_test.go::TestRTID_CreateFederationViaGRPC_PopulatesFOMRepoMap``
    plus
    ``rti/internal/transport/grpc/federation_test.go::TestCreateFederation_PostSuccessHook_*``.

  - Test 2 (verbose + any order → TSO) does not depend on the
    ``FOMOrderResolver`` upgrade nor the handle-alignment fix: in
    verbose mode the registry never considers the FOM order, so the
    publisher's timestamp passes through verbatim regardless of which
    handle the wire carries. This test was already PASSING before
    handle alignment.
"""

from __future__ import annotations

import asyncio
import shutil

import pytest

from tests.spec.m5._helpers import run_modes_smoke

# Logical timestamp the publisher attaches to the probe interaction. The
# value is arbitrary — what matters is that it's deterministic across
# runs (so a debugger inspecting the captured event sees a stable
# expected value) and that the subscriber's TSO assertion can compare
# bytewise against it.
PROBE_TIMESTAMP = 42.0


@pytest.mark.spec
@pytest.mark.integration
def test_spec_m5_best_effort_attribute_delivers_ro() -> None:
    """In best-effort mode, a Receive-order class delivers RO (timestamp=None).

    End-to-end:
      1. Build the rtid binary if needed.
      2. Launch rtid as a subprocess on a free port.
      3. Federate A and B join a federation with ``mode="best-effort"``.
      4. Both declare pub/sub on ``ModesProbe`` (an inline FOM
         interaction class declared with ``<order>Receive</order>``).
      5. Federate A sends ``ModesProbe`` WITH a logical timestamp.
      6. Subscriber drains its event stream and captures the first
         ``ReceiveInteraction``.
      7. Assert ``event.timestamp is None`` (RO semantics).

    Status (M6 W1C, 2026-05-03): both blockers fixed and this test is
    LIVE. W1A landed cross-language handle alignment
    (``pysdk/tests/test_handle_alignment.py`` PASSES, proving Python
    and Go agree on the wire-side handle for ModesProbe and every
    other class in the merged FOM). W1C wired the rtid composition to
    call ``fomRepository.RememberFor(name, handle)`` post-success in
    every CreateFederation gRPC call (see
    ``rti/cmd/rtid/main.go::OnCreateFederationSuccess`` and
    ``rti/internal/transport/grpc/federation.go::onCreateFederationSuccess``),
    so ``FOMRepoOrderLookup.InteractionOrder`` now resolves the FOM
    handle for the federation and consults the per-class
    ``<order>Receive</order>`` declaration end-to-end.
    """
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; cannot build rtid for the smoke test")

    result = asyncio.run(
        run_modes_smoke(
            federation_mode="best-effort",
            interaction_order="Receive",
            timestamp=PROBE_TIMESTAMP,
        )
    )

    assert result["received_count"] == 1, (
        f"subscriber received {result['received_count']} interactions; "
        "expected exactly 1 (publisher sent exactly one ModesProbe)"
    )

    received_ts = result["received_timestamp"]
    assert received_ts is None, (
        "best-effort + Receive-order interaction must deliver RO; "
        f"got timestamp={received_ts!r}"
    )


@pytest.mark.spec
@pytest.mark.integration
def test_spec_m5_verbose_attribute_delivers_tso() -> None:
    """In verbose mode, even a Receive-order class delivers TSO.

    Regression check that the federation operating mode is the dominant
    knob: when ``mode="verbose"``, the per-class FOM order is ignored
    by the registry and the publisher's timestamp is preserved on the
    subscriber's ``ReceiveInteraction``. Same end-to-end shape as the
    best-effort test above; only the ``mode`` differs.
    """
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; cannot build rtid for the smoke test")

    result = asyncio.run(
        run_modes_smoke(
            federation_mode="verbose",
            interaction_order="Receive",
            timestamp=PROBE_TIMESTAMP,
        )
    )

    assert result["received_count"] == 1, (
        f"subscriber received {result['received_count']} interactions; "
        "expected exactly 1 (publisher sent exactly one ModesProbe)"
    )

    received_ts = result["received_timestamp"]
    assert received_ts is not None, (
        "verbose mode must preserve TSO regardless of FOM order; "
        f"got timestamp={received_ts!r}, expected {PROBE_TIMESTAMP!r}"
    )
    assert received_ts == PROBE_TIMESTAMP, (
        "verbose-mode TSO must round-trip the publisher's timestamp verbatim; "
        f"got {received_ts!r}, expected {PROBE_TIMESTAMP!r}"
    )
