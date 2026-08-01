"""Shared imports and federation configuration for the two federates."""

from __future__ import annotations

import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO_ROOT = HERE.parents[1]
BASE_EXAMPLE = REPO_ROOT / "examples" / "pyjevsim"
PYSDK = REPO_ROOT / "pysdk"

for path in (HERE, BASE_EXAMPLE, PYSDK):
    if str(path) not in sys.path:
        sys.path.insert(0, str(path))

from _federate_common import (  # noqa: E402
    common_parser,
    declare_pub_sub,
    mark_ready,
    run_drain_only_loop,
    run_untimed_loop,
    staggered_start,
    write_result,
)
from _real_pyjevsim_adapter import RealPyjevsimAdapter  # noqa: E402
from rti1516e.connection import FederationSpec  # noqa: E402


def federation_spec() -> FederationSpec:
    return FederationSpec(
        name="pyjevsim-real-model",
        fom_modules=[str(HERE / "federation.fom.xml")],
        seed=1516,
    )


__all__ = [
    "RealPyjevsimAdapter",
    "common_parser",
    "declare_pub_sub",
    "federation_spec",
    "mark_ready",
    "run_drain_only_loop",
    "run_untimed_loop",
    "staggered_start",
    "write_result",
]
