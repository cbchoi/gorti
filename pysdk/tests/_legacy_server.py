"""Isolated server processes for the legacy wire integration tests."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
from functools import cache
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


@cache
def _artifacts() -> Path:
    parent = Path(os.environ.get("GORTI_LIVE_ARTIFACTS", tempfile.gettempdir())).resolve()
    assert parent != ROOT and ROOT not in parent.parents, "raw artifacts must stay outside repo"
    parent.mkdir(parents=True, exist_ok=True)
    return Path(tempfile.mkdtemp(prefix="gorti-legacy-sdk-", dir=parent))


@cache
def build_rtid() -> Path:
    directory = _artifacts()
    binary = directory / ("rtid.exe" if os.name == "nt" else "rtid")
    go = shutil.which("go")
    assert go is not None, "Go is required for the legacy wire integration tests"
    env = os.environ.copy()
    env.setdefault("GOCACHE", str(ROOT.parent / ".gocache"))
    env.setdefault("GOMODCACHE", str(ROOT.parent / ".tools" / "gomodcache"))
    with (directory / "build.log").open("wb") as output:
        result = subprocess.run(  # noqa: S603 - explicit local tool, no shell
            [go, "build", "-o", str(binary), "./rti/cmd/rtid"],
            cwd=ROOT,
            env=env,
            stdout=output,
            stderr=subprocess.STDOUT,
            timeout=180,
            check=False,
        )
    assert result.returncode == 0, f"rtid build failed; see {directory / 'build.log'}"
    return binary


def spawn_rtid(
    binary: Path,
    port: int,
    metrics_port: int,
    *,
    save_dir: Path | None = None,
    log_dir: Path | None = None,
    tls_cert: Path | None = None,
    tls_key: Path | None = None,
) -> subprocess.Popen[bytes]:
    directory = Path(tempfile.mkdtemp(prefix="server-", dir=_artifacts()))
    for path in (save_dir, log_dir):
        if path is not None:
            resolved = path.resolve()
            assert resolved != ROOT and ROOT not in resolved.parents, (
                "raw artifacts must stay outside repo"
            )
    assert (tls_cert is None) == (tls_key is None), (
        "TLS certificate and key must be supplied together"
    )
    env = os.environ.copy()
    env["GORTI_RESEARCH_CONFIG"] = ""
    with (directory / "server.log").open("wb") as output:
        return subprocess.Popen(  # noqa: S603 - owned server binary, no shell
            [
                str(binary),
                f"--listen=127.0.0.1:{port}",
                "--admin-listen=127.0.0.1:0",
                f"--metrics-listen=127.0.0.1:{metrics_port}",
                f"--save-dir={save_dir if save_dir is not None else directory / 'saves'}",
                f"--audit-replay-plugin={'event-journal' if log_dir is not None else 'none'}",
                f"--log-dir={log_dir if log_dir is not None else ''}",
                "--log-level=warn",
                *(
                    [f"--tls-cert={tls_cert}", f"--tls-key={tls_key}"]
                    if tls_cert is not None
                    else []
                ),
            ],
            cwd=ROOT,
            env=env,
            stdout=output,
            stderr=subprocess.STDOUT,
            start_new_session=True,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
