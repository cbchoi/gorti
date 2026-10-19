"""Fresh source/target SDK processes across an actual rtid process restart."""

import json
import os
import socket
import subprocess
import sys
import time
from contextlib import contextmanager
from pathlib import Path

import pytest

from tests import test_runtime_checkpoint_live as live
from tests.test_live_service_e2e import ROOT, live_artifacts  # noqa: F401

pytestmark = pytest.mark.skipif(os.environ.get("GORTI_RUN_LIVE_E2E") != "1",
                                reason="production localhost tests are opt-in")


@contextmanager
def owned_server(binary, run, savedir, stage):
    listeners = [socket.socket() for _ in range(2)]
    try:
        for listener in listeners:
            listener.bind(("127.0.0.1", 0))
        ports = [listener.getsockname()[1] for listener in listeners]
    finally:
        for listener in listeners:
            listener.close()
    env = os.environ.copy()
    env["GORTI_RESEARCH_CONFIG"] = ""
    with (run / f"{stage}-server.log").open("wb") as output:
        child = subprocess.Popen([  # noqa: S603 - owned local server binary, no shell
            str(binary), f"--listen=127.0.0.1:{ports[0]}",
            f"--admin-listen=127.0.0.1:{ports[1]}", "--metrics-listen=127.0.0.1:0",
            f"--save-dir={savedir}", "--audit-replay-plugin=none", "--log-dir=",
            "--log-level=warn",
        ], cwd=ROOT, env=env, stdout=output, stderr=subprocess.STDOUT,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
        try:
            deadline = time.monotonic() + 15
            while True:
                assert child.poll() is None, f"rtid exited: {run / f'{stage}-server.log'}"
                try:
                    with socket.create_connection(("127.0.0.1", ports[0]), timeout=0.2):
                        break
                except OSError:
                    assert time.monotonic() < deadline, "owned restart server startup timed out"
                    time.sleep(0.05)
            yield child, f"grpc://127.0.0.1:{ports[0]}"
        finally:
            if child.poll() is None:
                child.terminate()
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)


def run_client_phase(action, url, base, family, evidence, run):
    result = subprocess.run([  # noqa: S603 - current interpreter and local test script, no shell
        sys.executable, "-B", str(Path(live.__file__).resolve()), action,
        url, str(base), family, str(evidence),
    ], capture_output=True, text=True, timeout=90,
        creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
    (run / f"{action}-client.log").write_text(result.stdout + result.stderr, encoding="utf-8")
    assert result.returncode == 0, result.stdout + result.stderr


@pytest.mark.parametrize("family", ["HLAfloat64Time", "HLAinteger64Time"])
def test_live_fresh_python_clients_after_actual_server_restart(live_artifacts, family):  # noqa: F811
    directory, binary, base, _ = live_artifacts
    run = directory / f"actual-server-restart-{family}"
    run.mkdir()
    savedir = run / "saves"
    evidence = run / "source-checkpoint.json"
    with owned_server(binary, run, savedir, "source") as (source, source_url):
        # Save and resign source clients, but do not destroy the source execution.
        run_client_phase("save-restart", source_url, base, family, evidence, run)
        assert evidence.is_file()
        assert any(path.is_file() for path in savedir.rglob("*")), "no committed save files"
        source_pid = source.pid
    assert source.poll() is not None, "source server must exit before target launch"
    with owned_server(binary, run, savedir, "target") as (target, target_url):
        target_pid = target.pid
        assert source_pid != target_pid, "target must be a new actual server process"
        run_client_phase("restore", target_url, base, family, evidence, run)
    assert target.poll() is not None
    (run / "server-restart-result.json").write_text(json.dumps({
        "family": family, "binary": str(binary), "shared_save_dir": str(savedir),
        "source_server_pid": source_pid, "target_server_pid": target_pid,
        "source_exited_before_target_start": True, "both_owned_servers_stopped": True,
        "fresh_source_and_restore_client_processes": True,
    }), encoding="utf-8")
