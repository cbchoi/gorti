"""Shared scaffolding for the regulator federate entry point.

Mirrors examples/pyjevsim-relay-cross-process/_federate_common.py
but specialised to the time-advance use case. Each federate runs N
cycles of next_message_request(t), waiting for a TimeAdvanceGrant
on its events stream between requests.

W3B (TASK-208) flipped pysdk's NER dispatcher from no-op to real,
so this example is now functional cross-process — was blocked
in cut-3 prior to M21.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path
from typing import Any

# sys.path bootstrap: pysdk + this dir.
_HERE = Path(__file__).resolve().parent
_REPO_ROOT = _HERE.parents[1]
_PYSDK = _REPO_ROOT / "pysdk"
for _path in (_PYSDK, _HERE):
    if str(_path) not in sys.path:
        sys.path.insert(0, str(_path))

# ruff: noqa: E402  (sys.path tweaks above must precede project imports)
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import TimeAdvanceGrant

FOM_PATH = _HERE / "time-advance-fom.xml"
FEDERATION_NAME = "pyjevsim-time-advance"


def common_parser(prog: str) -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(prog=prog)
    p.add_argument("--url", required=True, help="grpc://host:port of rtid")
    p.add_argument("--result", required=True, help="path to write the JSON result")
    p.add_argument("--name", required=True, choices=("fast", "normal", "slow"))
    p.add_argument("--lookahead", type=float, required=True)
    p.add_argument("--cycles", type=int, default=10)
    p.add_argument("--tick-step", type=float, default=3.0,
                   help="logical-time advance per cycle (must exceed max lookahead)")
    p.add_argument("--constrained", action="store_true", default=True)
    p.add_argument("--grant-timeout", type=float, default=30.0)
    return p


def federation_spec() -> FederationSpec:
    return FederationSpec(
        name=FEDERATION_NAME,
        fom_modules=[str(FOM_PATH)],
        seed=0,
    )


async def wait_for_grant(fed: Any, timeout: float) -> float:
    """Drain fed.events() until a TimeAdvanceGrant arrives.
    Other event types (Tick interactions etc.) are ignored.

    Note: returns on the FIRST grant — accepts forced (partial) grants
    as cycle completion. Use ``wait_for_full_grant`` for NER cycles
    that must hold out for full advance.
    """
    deadline = asyncio.get_event_loop().time() + timeout
    async for evt in fed.events():
        if isinstance(evt, TimeAdvanceGrant):
            return float(evt.time)
        if asyncio.get_event_loop().time() > deadline:
            break
    raise TimeoutError(f"no TimeAdvanceGrant within {timeout}s")


async def wait_for_full_grant(fed: Any, requested: float, timeout: float) -> float:
    """Drain fed.events() until a *full* TimeAdvanceGrant (grant.time >=
    requested) arrives, accumulating any forced (partial) grants.

    M22 W3: forced grants from advance.go::decideGrant's NER/NMRA
    sole-pending escape hatch leave the federate in time-advancing
    state per IEEE 1516.1. Reissuing an advance primitive at that
    point correctly returns ErrTimeAdvancingState. The federate must
    keep waiting on the SAME NER until a full grant arrives.
    """
    deadline = asyncio.get_event_loop().time() + timeout
    async for evt in fed.events():
        if isinstance(evt, TimeAdvanceGrant):
            t = float(evt.time)
            if t >= requested:
                return t
            print(
                f"regulator: forced grant @ {t} < requested {requested}; "
                f"waiting for full",
                flush=True,
            )
            # accumulate; loop on
        if asyncio.get_event_loop().time() > deadline:
            break
    raise TimeoutError(f"no full TimeAdvanceGrant within {timeout}s (requested={requested})")


def write_result(path: str, payload: dict[str, Any]) -> None:
    out = Path(path)
    tmp = out.with_suffix(out.suffix + ".tmp")
    tmp.parent.mkdir(parents=True, exist_ok=True)
    tmp.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    tmp.replace(out)
