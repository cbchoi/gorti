"""Minimal shared scaffolding for the bridged dashboard's federate
entry points. Same shape as
``examples/pyjevsim-dashboard/_federate_common.py`` -- argparse,
sys.path, federation spec, write_result. The bridged variant adds
no shared driver loop because the two federates have heterogeneous
shapes (one drives publish/register/update; one drains
discover/reflect events).
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

_HERE = Path(__file__).resolve().parent
_REPO_ROOT = _HERE.parents[1]
_PYSDK = _REPO_ROOT / "pysdk"
for _path in (_PYSDK, _HERE):
    if str(_path) not in sys.path:
        sys.path.insert(0, str(_path))

# ruff: noqa: E402
from rti1516e.connection import FederationSpec

FOM_PATH = _HERE / "dashboard-fom.xml"
FEDERATION_NAME = "pyjevsim-dashboard-bridged-cross-process"


def common_parser(prog: str) -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(prog=prog)
    p.add_argument("--url", required=True, help="grpc://host:port of the rtid")
    p.add_argument("--result", required=True, help="path to write the JSON result")
    p.add_argument("--ticks", type=int, default=10)
    p.add_argument("--mode", choices=("sequence", "sine"), default="sequence")
    p.add_argument("--amplitude", type=int, default=100)
    p.add_argument("--tick-period", type=float, default=0.05)
    p.add_argument("--drain-ticks", type=int, default=20)
    p.add_argument("--startup-delay", type=float, default=0.0)
    return p


def federation_spec() -> FederationSpec:
    return FederationSpec(name=FEDERATION_NAME, fom_modules=[str(FOM_PATH)], seed=0)


def write_result(path: str, payload: dict[str, Any]) -> None:
    out = Path(path)
    tmp = out.with_suffix(out.suffix + ".tmp")
    tmp.parent.mkdir(parents=True, exist_ok=True)
    tmp.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    tmp.replace(out)
