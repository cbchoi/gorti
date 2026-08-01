"""Portable FOM and launcher contract tests."""
# ruff: noqa: S101

from __future__ import annotations

import importlib.util
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

HERE = Path(__file__).resolve().parent
PYSDK = HERE.parents[1] / "pysdk"
if str(PYSDK) not in sys.path:
    sys.path.insert(0, str(PYSDK))

from rti1516e.fom import parse  # noqa: E402

PREFLIGHT_SPEC = importlib.util.spec_from_file_location(
    "_pyjevsim_real_model_preflight",
    HERE / "preflight.py",
)
assert PREFLIGHT_SPEC is not None and PREFLIGHT_SPEC.loader is not None
PREFLIGHT = importlib.util.module_from_spec(PREFLIGHT_SPEC)
PREFLIGHT_SPEC.loader.exec_module(PREFLIGHT)


def test_fom_declares_the_single_pulse_wire_parameter() -> None:
    result = parse([HERE / "federation.fom.xml"])

    assert not result.diagnostics, result.diagnostics
    assert result.fom is not None
    pulse = result.fom.find_interaction_class("Pulse")
    assert pulse is not None
    assert pulse.order == "Receive"
    assert pulse.transportation == "HLAreliable"
    assert [(item.name, item.data_type) for item in pulse.parameters] == [
        ("seq", "HLAinteger32BE")
    ]


def test_launchers_keep_the_same_portable_contract() -> None:
    shell = (HERE / "run.sh").read_text(encoding="utf-8")
    powershell = (HERE / "run.ps1").read_text(encoding="utf-8")

    assert "Python 3.11 or newer" in shell
    assert "Python 3.11 or newer" in powershell
    assert "$env:PYTHON" in powershell
    assert "${BASH_SOURCE[0]}" in shell
    assert "$PSScriptRoot" in powershell
    assert "preflight.py" in shell
    assert "preflight.py" in powershell
    assert 'Prefix = @("-3.11")' in powershell
    assert '"$@"' in shell
    assert "@RunnerArgs" in powershell
    assert "exec" in shell
    assert "exit $LASTEXITCODE" in powershell


def test_preflight_checks_every_release_runtime_surface() -> None:
    imported: list[str] = []

    class _Transport:
        @staticmethod
        def _ensure_generated_path() -> None:
            return

    def importer(name: str) -> object:
        imported.append(name)
        if name == "rti1516e._transport":
            return _Transport()
        return object()

    assert PREFLIGHT.runtime_errors(version_info=(3, 11), importer=importer) == []
    assert imported == [
        *(name for name, _package in PREFLIGHT.REQUIRED_MODULES),
        "rti1516e._transport",
        *PREFLIGHT.GENERATED_MODULES,
    ]


def test_preflight_reports_old_python_and_missing_packages() -> None:
    class _Transport:
        @staticmethod
        def _ensure_generated_path() -> None:
            return

    def importer(name: str) -> object:
        if name in {
            "google.protobuf",
            "grpc",
            "pyjevsim.behavior_model",
            "rti.v1.object_pb2",
        }:
            raise ModuleNotFoundError(name)
        if name == "rti1516e._transport":
            return _Transport()
        return object()

    errors = PREFLIGHT.runtime_errors(version_info=(3, 10), importer=importer)

    assert any("Python 3.11" in error for error in errors)
    assert any("grpcio" in error for error in errors)
    assert any("protobuf" in error for error in errors)
    assert any("pyjevsim" in error for error in errors)
    assert any("generated binding rti.v1.object_pb2" in error for error in errors)


@pytest.mark.skipif(
    not sys.platform.startswith("win"),
    reason="the py launcher fallback is Windows-specific",
)
def test_powershell_falls_back_to_py_311(tmp_path: Path) -> None:
    host = shutil.which("powershell.exe") or shutil.which("pwsh")
    if host is None:
        pytest.skip("PowerShell is unavailable")

    log_path = tmp_path / "py-prefixes.txt"
    shim = tmp_path / "py_shim.py"
    shim.write_text(
        """import os
import subprocess
import sys

with open(os.environ["PY_LAUNCHER_LOG"], "a", encoding="utf-8") as stream:
    stream.write(sys.argv[1] + "\\n")
if len(sys.argv) < 2 or sys.argv[1] != "-3.11":
    raise SystemExit(91)
raise SystemExit(subprocess.call([sys.executable, *sys.argv[2:]]))
""",
        encoding="utf-8",
    )
    launcher = tmp_path / "py.cmd"
    launcher.write_text(
        f'@echo off\n"{sys.executable}" "{shim}" %*\n',
        encoding="utf-8",
    )
    env = os.environ.copy()
    env.pop("PYTHON", None)
    env.pop("RTID_VENV", None)
    env["PATH"] = str(tmp_path)
    env["PY_LAUNCHER_LOG"] = str(log_path)

    completed = subprocess.run(  # noqa: S603
        [
            host,
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-File",
            str(HERE / "run.ps1"),
            "--help",
        ],
        cwd=tmp_path,
        env=env,
        check=False,
        capture_output=True,
        text=True,
        timeout=15,
    )

    assert completed.returncode == 0, completed.stderr
    assert log_path.read_text(encoding="utf-8").splitlines() == [
        "-3.11",
        "-3.11",
    ]


def test_platform_launcher_forwards_help_from_another_working_directory(
    tmp_path: Path,
) -> None:
    env = {**os.environ, "PYTHON": sys.executable}
    if sys.platform.startswith("win"):
        host = shutil.which("powershell.exe") or shutil.which("pwsh")
        if host is None:
            pytest.skip("PowerShell is unavailable")
        command = [
            host,
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-File",
            str(HERE / "run.ps1"),
            "--help",
        ]
    else:
        bash = shutil.which("bash")
        if bash is None:
            pytest.skip("bash is unavailable")
        command = [bash, str(HERE / "run.sh"), "--help"]

    completed = subprocess.run(  # noqa: S603
        command,
        cwd=tmp_path,
        env=env,
        check=False,
        capture_output=True,
        text=True,
        timeout=15,
    )

    assert completed.returncode == 0, completed.stderr
    assert "--ticks" in completed.stdout
    assert "--rtid-binary" in completed.stdout
