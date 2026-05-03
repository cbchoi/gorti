"""pyjevsim API drift smoke test.

Imports specific pyjevsim symbols and asserts presence. Fails loudly
on version drift that breaks any required symbol — diagnostic identifies
the missing symbol so future maintainers can patch the version pin or
update the bridge.

This test is the only spec test that imports REAL pyjevsim. All other
bridge tests use the StubCoupledModel double. If pyjevsim isn't
installed, this test SKIPs (so spec-test-only environments don't fail).

Implements: FR-PYJ-1 (version stability).
"""

from __future__ import annotations

import importlib

import pytest


@pytest.mark.spec
def test_spec_m4_pyjevsim_required_symbols_present() -> None:
    try:
        pyjevsim = importlib.import_module("pyjevsim")
    except ImportError:
        pytest.skip("pyjevsim not installed in this environment")

    # The bridge consumes these symbols. If a future pyjevsim release
    # renames or removes any of them, this test fails with a diagnostic
    # naming the missing one.
    required = ["CoupledModel", "AtomicModel"]
    missing = [name for name in required if not hasattr(pyjevsim, name)]
    assert not missing, (
        f"pyjevsim {getattr(pyjevsim, '__version__', '?')} is missing required symbols: "
        f"{missing}. Either pin to an older version in pyproject.toml or update the "
        f"bridge in pysdk/pyjevsim_bridge/."
    )
