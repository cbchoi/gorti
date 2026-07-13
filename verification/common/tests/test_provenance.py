from __future__ import annotations

import hashlib
import json
import subprocess
from pathlib import Path

import pytest

from verification.common.provenance import (
    SCHEMA,
    ProvenanceError,
    capture_provenance,
    finalize_provenance,
    sha256_file,
    write_provenance,
)


def test_sha256_file(tmp_path: Path) -> None:
    target = tmp_path / "binary.exe"
    target.write_bytes(b"gorti-provenance")

    assert sha256_file(target) == hashlib.sha256(b"gorti-provenance").hexdigest()


def test_capture_provenance_records_exact_runtime_contract(tmp_path: Path, monkeypatch) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()
    rtid = tmp_path / "rtid.exe"
    python = tmp_path / "python.exe"
    rtid.write_bytes(b"rtid")
    python.write_bytes(b"python")

    def fake_run(arguments, **_kwargs):
        command = tuple(str(item) for item in arguments)
        if command[:3] == ("git", "status", "--porcelain"):
            output = " M improvement.md\n"
        elif command[:3] == ("git", "rev-parse", "HEAD"):
            output = "abc123\n"
        elif command[:3] == ("git", "branch", "--show-current"):
            output = "perf/improvement-p0\n"
        elif command[-1] == "--version" and command[0] == str(rtid.resolve()):
            output = "rtid dev\n"
        else:
            output = "Python 3.14.0\n"
        return subprocess.CompletedProcess(command, 0, stdout=output, stderr="")

    monkeypatch.setattr(subprocess, "run", fake_run)
    record = capture_provenance(
        repo_root=repo,
        rtid_path=rtid,
        python_path=python,
        server_arguments=("--listen=127.0.0.1:8442", "--log-dir=logs"),
        verifier_arguments=("verifier.py", "--count", "100"),
        benchmark={"seed": 1516, "count": 100, "logging_mode": "file"},
    )

    assert record["schema"] == SCHEMA
    assert record["source"]["commit"] == "abc123"
    assert record["source"]["branch"] == "perf/improvement-p0"
    assert record["source"]["dirty"] is True
    assert record["server"]["version"] == "rtid dev"
    assert record["server"]["arguments"] == [
        "--listen=127.0.0.1:8442",
        "--log-dir=logs",
    ]
    assert record["benchmark"]["logging_mode"] == "file"
    assert record["processes"][1]["argv"][-3:] == ["verifier.py", "--count", "100"]

    destination = tmp_path / "run-metadata.json"
    write_provenance(destination, record)
    assert json.loads(destination.read_text(encoding="utf-8"))["schema"] == SCHEMA
    finalized = finalize_provenance(destination, outcome="passed", exit_code=0)
    assert finalized["outcome"] == {"status": "passed", "exit_code": 0}
    assert finalized["timestamps"]["finished_at_utc"] is not None


def test_capture_provenance_does_not_report_failed_git_as_clean(
    tmp_path: Path, monkeypatch
) -> None:
    executable = tmp_path / "runtime.exe"
    executable.write_bytes(b"runtime")

    def fail_git(*_args, **_kwargs):
        raise subprocess.CalledProcessError(1, "git")

    monkeypatch.setattr(subprocess, "run", fail_git)
    with pytest.raises(ProvenanceError, match="required provenance command failed"):
        capture_provenance(
            repo_root=tmp_path,
            rtid_path=executable,
            python_path=executable,
            server_arguments=(),
            benchmark={"logging_mode": "file"},
        )
