"""Runner delegation and cross-process smoke coverage."""
# ruff: noqa: S101

from __future__ import annotations

import argparse
import asyncio
import importlib.util
import re
import shutil
import sys
from pathlib import Path
from typing import Any

import pytest

HERE = Path(__file__).resolve().parent
REPO_ROOT = HERE.parents[1]

SPEC = importlib.util.spec_from_file_location(
    "_pyjevsim_real_model_runner",
    HERE / "runner.py",
)
assert SPEC is not None and SPEC.loader is not None
RUNNER = importlib.util.module_from_spec(SPEC)
sys.modules.setdefault("_pyjevsim_real_model_runner", RUNNER)
SPEC.loader.exec_module(RUNNER)


class _FakeBaseRunner:
    def __init__(self, *, ok: bool = True) -> None:
        self.ok = ok
        self.calls: list[dict[str, Any]] = []
        self.PRODUCER_SCRIPT = Path("old-producer.py")
        self.CONSUMER_SCRIPT = Path("old-consumer.py")

    async def run_once(self, **kwargs: Any) -> dict[str, Any]:
        self.calls.append(kwargs)
        return {
            "published": [1, 2],
            "received": [1, 2],
            "workdir": str(kwargs["workdir"]),
        }

    def verify(self, result: dict[str, Any]) -> tuple[bool, str]:
        assert result["received"] == result["published"]
        return self.ok, "ok" if self.ok else "failed"


def test_run_delegates_to_shared_runner_with_local_federates(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base = _FakeBaseRunner()
    monkeypatch.setattr(RUNNER, "_load_base_runner", lambda: base)
    args = argparse.Namespace(
        ticks=6,
        drain_ticks=3,
        tick_period=0.125,
        timeout=17.0,
        rtid_binary=None,
        workdir=tmp_path,
        no_keep_workdir=True,
        log_level="warn",
    )

    exit_code = asyncio.run(RUNNER.run(args))

    executable = "rtid.exe" if sys.platform.startswith("win") else "rtid"
    assert exit_code == 0
    assert base.PRODUCER_SCRIPT == HERE / "producer_main.py"
    assert base.CONSUMER_SCRIPT == HERE / "consumer_main.py"
    assert base.calls == [
        {
            "ticks": 6,
            "drain_ticks": 3,
            "tick_period": 0.125,
            "rtid_binary": REPO_ROOT / "bin" / executable,
            "workdir": tmp_path,
            "keep_workdir": False,
            "federate_timeout": 17.0,
            "log_level": "warn",
        }
    ]


def test_default_workdirs_are_collision_resistant() -> None:
    first = RUNNER._default_workdir()
    second = RUNNER._default_workdir()

    assert first.parent == HERE / ".run"
    assert second.parent == HERE / ".run"
    assert first != second
    pattern = r"^\d{8}-\d{6}-\d{6}-\d+-[0-9a-f]{8}$"
    assert re.fullmatch(pattern, first.name)
    assert re.fullmatch(pattern, second.name)


@pytest.mark.integration
def test_real_models_exchange_pulses_cross_process(tmp_path: Path) -> None:
    pytest.importorskip(
        "pyjevsim.behavior_model",
        reason="pyjevsim is an optional runtime dependency",
    )
    pytest.importorskip("grpc", reason="grpcio is required for the real RTI smoke")
    executable = "rtid.exe" if sys.platform.startswith("win") else "rtid"
    rtid = REPO_ROOT / "bin" / executable
    if not rtid.exists() and shutil.which("go") is None:
        pytest.skip("rtid is absent and the Go toolchain is unavailable")

    args = argparse.Namespace(
        ticks=4,
        drain_ticks=2,
        tick_period=0.02,
        timeout=20.0,
        rtid_binary=rtid,
        workdir=tmp_path / "run",
        no_keep_workdir=False,
        log_level="warn",
    )

    assert asyncio.run(RUNNER.run(args)) == 0
