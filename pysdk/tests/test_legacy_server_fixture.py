"""Legacy integration launchers must not depend on shared default listeners."""

from __future__ import annotations

import importlib
from pathlib import Path

import pytest


@pytest.mark.parametrize(
    "module_name",
    [
        "m5._helpers",
        "m17.test_cpp_python_interop",
        "m28.test_ieee1516e_typed_smoke",
        "m25.test_support_service",
        "m26.test_ieee1516e_ambassador_smoke",
        "m26.test_reservation",
        "m27.test_handle_keyed_api",
        "m27.test_ieee1516e_ambassador_smoke_cross",
        "m27.test_instance_handles",
        "m27.test_mom_ambassador",
        "m27.test_outbox_race",
    ],
)
def test_legacy_server_launcher_isolates_ports_and_artifacts(monkeypatch, module_name):
    module = importlib.import_module(f"tests.spec.{module_name}")
    calls = []
    process = object()

    def popen(argv, **kwargs):
        calls.append((argv, kwargs))
        return process

    monkeypatch.setattr("subprocess.Popen", popen)
    assert module._spawn_rtid(Path("test-rtid"), 12345, 12346) is process
    argv, options = calls[0]
    assert "--admin-listen=127.0.0.1:0" in argv
    assert "--listen=127.0.0.1:12345" in argv
    assert "--metrics-listen=127.0.0.1:12346" in argv
    assert "--log-dir=" in argv
    save_dir = Path(next(arg.split("=", 1)[1] for arg in argv if arg.startswith("--save-dir=")))
    assert module.REPO_ROOT not in save_dir.parents
    assert options["env"]["GORTI_RESEARCH_CONFIG"] == ""


def test_tls_launcher_keeps_certificate_and_key(monkeypatch, tmp_path):
    from tests._legacy_server import spawn_rtid

    calls = []
    monkeypatch.setattr("subprocess.Popen", lambda argv, **kw: calls.append((argv, kw)))
    cert, key = tmp_path / "cert.pem", tmp_path / "key.pem"
    spawn_rtid(Path("test-rtid"), 12345, 0, tls_cert=cert, tls_key=key)
    argv, _ = calls[0]
    assert f"--tls-cert={cert}" in argv
    assert f"--tls-key={key}" in argv
    assert "--admin-listen=127.0.0.1:0" in argv


@pytest.mark.parametrize("kind", ["save_dir", "log_dir"])
def test_launcher_rejects_repository_artifacts(kind):
    from tests._legacy_server import ROOT, spawn_rtid

    with pytest.raises(AssertionError, match="outside repo"):
        spawn_rtid(Path("test-rtid"), 12345, 0, **{kind: ROOT / "artifacts"})


def test_build_output_is_external_and_platform_executable(monkeypatch, tmp_path):
    import os
    from types import SimpleNamespace

    from tests import _legacy_server

    calls = []
    monkeypatch.setattr(_legacy_server, "_artifacts", lambda: tmp_path)
    monkeypatch.setattr(_legacy_server.shutil, "which", lambda _: "go")

    def run(argv, **options):
        calls.append((argv, options))
        return SimpleNamespace(returncode=0)

    monkeypatch.setattr(_legacy_server.subprocess, "run", run)
    binary = _legacy_server.build_rtid.__wrapped__()
    assert binary == tmp_path / ("rtid.exe" if os.name == "nt" else "rtid")
    assert calls[0][0] == ["go", "build", "-o", str(binary), "./rti/cmd/rtid"]
    assert calls[0][1]["cwd"] == _legacy_server.ROOT


def test_m12_server_launcher_preserves_journal_and_isolates_admin(monkeypatch, tmp_path):
    from tests.spec.m12 import _helpers

    calls = []
    monkeypatch.setattr("subprocess.Popen", lambda argv, **kw: calls.append((argv, kw)))
    _helpers._spawn_rtid(
        Path("test-rtid"), 12345, 12346,
        save_dir=tmp_path / "saves", log_dir=tmp_path / "journal",
    )
    argv, options = calls[0]
    assert "--admin-listen=127.0.0.1:0" in argv
    assert "--listen=127.0.0.1:12345" in argv
    assert f"--save-dir={tmp_path / 'saves'}" in argv
    assert f"--log-dir={tmp_path / 'journal'}" in argv
    assert "--audit-replay-plugin=event-journal" in argv
    assert options["env"]["GORTI_RESEARCH_CONFIG"] == ""


def test_failed_federate_cleanup_preserves_exit_failure(tmp_path):
    from io import StringIO
    from types import SimpleNamespace

    from tests.spec.m21._processes import Peer

    class BrokenInput(StringIO):
        def close(self):
            super().close()
            raise OSError(22, "Invalid argument")

    peer = Peer.__new__(Peer)
    peer.name = "failed-peer"
    peer.directory = tmp_path
    peer.stderr = StringIO()
    peer.transcript = StringIO()
    peer.reader = SimpleNamespace(join=lambda **kwargs: None)
    peer.process = SimpleNamespace(
        poll=lambda: 7, returncode=7, stdin=BrokenInput(), stdout=StringIO(),
    )
    with pytest.raises(AssertionError, match="failed-peer exit failure"):
        peer.close()
    assert peer.stderr.closed and peer.transcript.closed and peer.process.stdout.closed


def test_forced_windows_federate_cleanup_stops_owned_process_tree(monkeypatch):
    from types import SimpleNamespace

    from tests.spec.m21 import _processes

    calls = []
    process = SimpleNamespace(
        pid=12345, poll=lambda: None,
        terminate=lambda: calls.append("terminate-parent-only"),
        wait=lambda **kwargs: calls.append(("wait", kwargs)),
    )
    monkeypatch.setattr(_processes, "os", SimpleNamespace(name="nt"))
    monkeypatch.setattr(_processes.shutil, "which", lambda _: "test-taskkill.exe")
    monkeypatch.setattr(
        _processes.subprocess, "run",
        lambda argv, **kwargs: calls.append((argv, kwargs)),
    )
    _processes.stop(process)
    assert "terminate-parent-only" not in calls
    argv, options = calls[0]
    assert argv == ["test-taskkill.exe", "/PID", "12345", "/T", "/F"]
    assert options["timeout"] == 5
    assert options.get("shell", False) is False
    assert calls[1] == ("wait", {"timeout": 5})
