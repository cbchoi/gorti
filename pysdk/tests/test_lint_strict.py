"""CI gate: ``mypy --strict`` and ``ruff check`` stay clean across pysdk/.

These run as part of pytest so a future commit that introduces type or
lint errors fails loudly inside the test suite, not just in the
``make py-typecheck`` / ``make py-lint`` targets.

Both tests are tagged ``@pytest.mark.slow`` because each shells out to a
linter (~1-3s wall time); ``pytest -m 'not slow'`` skips them for fast
local iteration.

Implements: M4 exit criteria #4 (mypy --strict clean) + #5 (ruff clean).
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

import pytest

PYSDK_ROOT = Path(__file__).resolve().parents[1]


@pytest.mark.slow
def test_mypy_strict_clean() -> None:
    """``mypy --strict pysdk/`` exits 0 with no diagnostics."""
    result = subprocess.run(  # noqa: S603 — argv built from sys.executable + literals
        [sys.executable, "-m", "mypy", "--strict", str(PYSDK_ROOT)],
        capture_output=True,
        text=True,
        check=False,
        cwd=str(PYSDK_ROOT),
    )
    assert result.returncode == 0, (
        f"mypy --strict reported errors:\n--- stdout ---\n{result.stdout}\n"
        f"--- stderr ---\n{result.stderr}"
    )


@pytest.mark.slow
def test_ruff_clean() -> None:
    """``ruff check pysdk/`` exits 0 with no diagnostics."""
    result = subprocess.run(  # noqa: S603 — argv built from sys.executable + literals
        [sys.executable, "-m", "ruff", "check", str(PYSDK_ROOT)],
        capture_output=True,
        text=True,
        check=False,
        cwd=str(PYSDK_ROOT),
    )
    assert result.returncode == 0, (
        f"ruff reported errors:\n--- stdout ---\n{result.stdout}\n"
        f"--- stderr ---\n{result.stderr}"
    )
