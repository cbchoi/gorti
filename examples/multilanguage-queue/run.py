"""Run the model experiment and compare every job with an independent scheduler."""

from __future__ import annotations

import argparse
from collections import deque
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import platform
import random
import re
import socket
import subprocess
import sys
import time
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]


def write(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def reference(services):
    """Independent time-stepped scheduler, not the Go reservation recurrence."""
    pending, completed, active = deque(), [], None
    for now in range(1, len(services) * 3 + 4):
        if active is not None and active["finish"] == now:
            completed.append(active)
            active = None
        if 2 <= now <= len(services) + 1:
            pending.append({"sequence": now-1, "arrival": now, "service": services[now-2]})
        if active is None and pending:
            active = pending.popleft()
            active.update(start=now, finish=now+active["service"], wait=now-active["arrival"])
    if len(completed) != len(services):
        raise RuntimeError("reference horizon too short")
    return completed


def stop(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)


def execute(directory, rtid, peer, services, horizon, env):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    endpoint = f"127.0.0.1:{port}"
    processes, logs = [], []

    def spawn(name, command):
        log = (directory / f"{name}.log").open("wb")
        logs.append(log)
        process = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT,
                                   creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        processes.append(process)
        return process

    try:
        server = spawn("rtid", [str(rtid), "--listen", endpoint, "--metrics-listen", "127.0.0.1:0", "--admin-listen", "127.0.0.1:0"])
        deadline = time.monotonic()+120
        while True:
            if server.poll() is not None:
                raise RuntimeError(f"server exited; see {directory}")
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                    break
            except OSError:
                if time.monotonic() > deadline:
                    raise TimeoutError("server startup")
                time.sleep(0.02)
        common = ["--federation", "queue-"+uuid.uuid4().hex, "--fom", str(HERE / "federation.fom.xml"),
                  "--output", str(directory), "--horizon", str(horizon)]
        go = spawn("go", [str(peer), "--url", endpoint, *common])
        python = spawn("python", [sys.executable, str(HERE / "python_source.py"), "--url", "grpc://"+endpoint, *common])
        ready = [directory / "go-ready.json", directory / "python-ready.json"]
        while not all(path.exists() for path in ready):
            if any(p.poll() is not None for p in (server, go, python)):
                raise RuntimeError(f"participant exited during setup; see {directory}")
            if time.monotonic() > deadline:
                raise TimeoutError(f"participant setup; see {directory}")
            time.sleep(0.02)
        identities = [json.loads(path.read_text()) for path in ready]
        if len({row["pid"] for row in identities} | {server.pid}) != 3 or len({row["handle"] for row in identities}) != 2:
            raise RuntimeError("participants are not distinct")
        (directory / "start").touch()
        for process in (python, go):
            if process.wait(timeout=max(1, deadline-time.monotonic())) != 0:
                raise RuntimeError(f"model run failed; see {directory}")
        return {"server_pid": server.pid, "federates": identities}
    finally:
        for process in reversed(processes):
            stop(process)
        for log in logs:
            log.close()


def compare(directory, expected, horizon):
    go = json.loads((directory / "go-result.json").read_text())
    python = json.loads((directory / "python-result.json").read_text())
    sequences = list(range(1, len(expected)+1))
    departures = [{"sequence": row["sequence"], "finish": row["finish"]} for row in expected]
    checks = {
        "all_job_fields_match_reference": go["jobs"] == expected,
        "completion_callbacks_match_reference": python["departures"] == departures,
        "generator_sequence": python["published"] == sequences,
        "sink_exactly_once_in_order": python["received"] == sequences,
        "both_grant_sequences": go["grants"] == python["grants"] == list(range(1, horizon+1)),
    }
    write(directory / "checks.json", checks)
    if not all(checks.values()):
        raise RuntimeError(f"trajectory mismatch: {checks}; see {directory}")
    return {"jobs": len(expected), "final_departure": expected[-1]["finish"],
            "mean_wait": sum(row["wait"] for row in go["jobs"])/len(expected),
            "mean_sojourn": sum(row["finish"]-row["arrival"] for row in go["jobs"])/len(expected),
            "max_time_error": max(abs(row[field]-expected[i][field]) for i, row in enumerate(go["jobs"])
                                  for field in ("arrival", "start", "finish", "wait")),
            "checks": checks}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--jobs", type=int, default=100)
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--seed", type=int, default=1516)
    parser.add_argument("--source-commit", help="Verified full commit when running a checkout through a cross-OS mount")
    args = parser.parse_args()
    if not 1 <= args.jobs <= 10000 or not 1 <= args.repeats <= 100:
        parser.error("jobs must be 1..10000 and repeats 1..100")
    commit = args.source_commit or subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        parser.error("source commit must be a full hexadecimal Git identifier")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    env.update(PYTHONPATH=str(ROOT / "pysdk"), PYTHONDONTWRITEBYTECODE="1", GORTI_RESEARCH_CONFIG="")
    env.pop("GORTI_FEDERATE_HOST", None)
    suffix = ".exe" if os.name == "nt" else ""
    rtid, peer = output / ("rtid"+suffix), output / ("workstation"+suffix)
    for target, package in ((rtid, "./rti/cmd/rtid"), (peer, "./examples/multilanguage-queue/go_workstation")):
        subprocess.run(["go", "build", "-o", str(target), package], cwd=ROOT, env=env, check=True, timeout=180)
    rng = random.Random(args.seed)
    cases = {"balanced": [1]*args.jobs, "bottleneck": [2]*args.jobs,
             "variable": [rng.randint(1, 3) for _ in range(args.jobs)]}
    results = []
    for name, services in cases.items():
        expected = reference(services)
        horizon = args.jobs*3+3
        for repeat in range(1, args.repeats+1):
            directory = output / f"{name}-{repeat}"
            directory.mkdir()
            write(directory / "inputs.json", {"services": services, "seed": args.seed, "transfer_delay": 1})
            write(directory / "reference.json", expected)
            identity = execute(directory, rtid, peer, services, horizon, env)
            row = {"profile": name, "repeat": repeat, **compare(directory, expected, horizon), **identity}
            results.append(row)
            print(json.dumps(row), flush=True)
    paths = [p for p in HERE.rglob("*") if p.is_file() and "__pycache__" not in p.parts]
    paths.extend([ROOT / "examples/pyjevsim-real-model/models.py", ROOT / "examples/pyjevsim/_real_pyjevsim_adapter.py"])
    manifest = {"source_commit": commit, "commit_origin": "caller-supplied" if args.source_commit else "git-rev-parse",
                "platform": platform.platform(), "python": sys.version, "pyjevsim": importlib.metadata.version("pyjevsim"),
                "pyjevsim_origin": importlib.metadata.distribution("pyjevsim").read_text("direct_url.json"),
                "go": subprocess.check_output(["go", "version"], text=True).strip(),
                "source_sha256": {p.relative_to(ROOT).as_posix(): sha(p) for p in paths},
                "rtid_sha256": sha(rtid), "go_peer_sha256": sha(peer), "results": results,
                "seed": args.seed, "note": "Functional trajectory experiment, not a wall-clock performance benchmark."}
    write(output / "summary.json", manifest)


if __name__ == "__main__":
    main()
