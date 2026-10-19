"""Owned, bounded RTI/Go/Python subprocesses for the M21 live scenarios."""

from __future__ import annotations

import json
import os
import queue
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
import uuid
from contextlib import ExitStack, contextmanager
from functools import cache
from pathlib import Path

from tests._legacy_server import ROOT, _artifacts, build_rtid, spawn_rtid

FOM = ROOT / "examples/pyjevsim-time-advance/time-advance-fom.xml"


def child_environment():
    env = os.environ.copy()
    env.update(
        {
            "PYTHONPATH": str(ROOT / "pysdk"),
            "PYTHONDONTWRITEBYTECODE": "1",
            "PYTHONUNBUFFERED": "1",
            "GORTI_RESEARCH_CONFIG": "",
        }
    )
    env.pop("GORTI_FEDERATE_HOST", None)
    env.setdefault("GOCACHE", str(ROOT.parent / ".gocache"))
    env.setdefault("GOMODCACHE", str(ROOT.parent / ".tools/gomodcache"))
    return env


@cache
def build_go_peer():
    directory = _artifacts()
    binary = directory / ("time-peer.exe" if os.name == "nt" else "time-peer")
    go = shutil.which("go")
    assert go is not None, "Go is required for the M21 separate-federate tests"
    with (directory / "time-peer-build.log").open("wb") as log:
        result = subprocess.run(  # noqa: S603 - local test helper build
            [go, "build", "-o", str(binary), "./pysdk/tests/fixtures/time_peer"],
            cwd=ROOT,
            env=child_environment(),
            stdout=log,
            stderr=subprocess.STDOUT,
            timeout=180,
            check=False,
        )
    assert result.returncode == 0, f"Go peer build failed: {directory / 'time-peer-build.log'}"
    return binary


def stop(process, *, has_children=True):
    if process.poll() is not None:
        return
    if os.name == "nt":
        if not has_children:
            # rtid is a direct executable; its Popen handle avoids PID-tool races.
            process.kill()
            process.wait(timeout=5)
            return
        # Virtualenv Python uses a launcher with a child interpreter on Windows.
        taskkill = shutil.which("taskkill")
        assert taskkill is not None, "taskkill is required for owned Windows process cleanup"
        subprocess.run(  # noqa: S603 - only the still-running process we spawned
            [taskkill, "/PID", str(process.pid), "/T", "/F"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5, check=False,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
        )
        process.wait(timeout=5)
        return
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


class Peer:
    def __init__(self, argv, directory, name):
        self.directory = directory
        self.name = name
        self.stderr = (directory / f"{name}.stderr.log").open("w", encoding="utf-8")
        self.process = subprocess.Popen(  # noqa: S603 - owned local test participant
            argv,
            cwd=ROOT,
            env=child_environment(),
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=self.stderr,
            text=True,
            encoding="utf-8",
            bufsize=1,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        self.responses = queue.Queue()
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()
        self.transcript = (directory / f"{name}.commands.jsonl").open("w", encoding="utf-8")

    def _read(self):
        with (self.directory / f"{self.name}.stdout.jsonl").open("w", encoding="utf-8") as log:
            for line in self.process.stdout:
                log.write(line)
                log.flush()
                try:
                    self.responses.put(json.loads(line))
                except ValueError:
                    self.responses.put({"protocol_error": line})
        self.responses.put({"process_exited": True})

    def read(self):
        try:
            response = self.responses.get(timeout=15)
        except queue.Empty:
            raise AssertionError(
                f"{self.name} response timed out; evidence: {self.directory}"
            ) from None
        assert "protocol_error" not in response and "process_exited" not in response, (
            f"{self.name}: {response}; evidence: {self.directory}"
        )
        return response

    def send(self, **command):
        line = json.dumps(command) + "\n"
        self.transcript.write(line)
        self.transcript.flush()
        self.process.stdin.write(line)
        self.process.stdin.flush()

    def call(self, **command):
        self.send(**command)
        response = self.read()
        assert response["op"] == command["op"]
        return response

    def close(self):
        pipe_error = None
        try:
            if self.process.poll() is None:
                try:
                    self.send(op="quit")
                except OSError as error:
                    pipe_error = error
                self.process.wait(timeout=8)
        finally:
            stop(self.process)
            self.reader.join(timeout=2)
            self.stderr.close()
            self.transcript.close()
            try:
                self.process.stdin.close()
            except OSError as error:
                pipe_error = error
            self.process.stdout.close()
        assert self.process.returncode == 0, f"{self.name} exit failure; see {self.directory}"
        assert pipe_error is None, f"{self.name} stdin failure: {pipe_error}; see {self.directory}"


@contextmanager
def federation(*roles):
    """roles are (language, regulating), with 'bridge' selecting the real adapter."""
    directory = Path(tempfile.mkdtemp(prefix="m21-", dir=_artifacts()))
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    server = spawn_rtid(build_rtid(), port, 0)
    with ExitStack() as stack:
        stack.callback(stop, server, has_children=False)
        deadline = time.monotonic() + 15
        while True:
            assert server.poll() is None, f"RTI server exited; evidence: {directory.parent}"
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                    break
            except OSError:
                assert time.monotonic() < deadline, "RTI readiness deadline exceeded"
                time.sleep(0.02)
        peers = []
        federation_name = "m21-" + uuid.uuid4().hex[:12]
        for index, (language, regulating) in enumerate(roles):
            name = f"{language}-{index}"
            argv = (
                [str(build_go_peer())]
                if language == "go"
                else [sys.executable, "-B", str(ROOT / "pysdk/tests/fixtures/time_peer.py")]
            )
            endpoint = f"127.0.0.1:{port}"
            if language != "go":
                endpoint = "grpc://" + endpoint
            argv.extend(
                [
                    "--url",
                    endpoint,
                    "--federation",
                    federation_name,
                    "--fom",
                    str(FOM),
                    "--name",
                    name,
                ]
            )
            if regulating:
                argv.append("--regulator")
            if language == "bridge":
                argv.append("--bridge")
            peer = Peer(argv, directory, name)
            stack.callback(peer.close)
            peer.ready = peer.read()
            assert peer.ready["ready"] is True
            peers.append(peer)
        pids = {peer.ready["pid"] for peer in peers}
        handles = {peer.ready["handle"] for peer in peers}
        assert len(pids) == len(peers) and os.getpid() not in pids and server.pid not in pids
        assert len(handles) == len(peers) and 0 not in handles
        yield peers


def assert_cross_language_delivery(regulator_language, constrained_language):
    with federation((regulator_language, True), (constrained_language, False)) as (
        sender,
        receiver,
    ):
        sender.call(op="send", time=4, value=0x10203040)
        receiver.call(op="request", primitive="TAR", time=5)
        before = receiver.call(op="collect", timeout_ms=200)
        assert before["events"] == [], "TSO callback or grant escaped the regulator's time bound"
        assert before["logical_time"] == 0
        sender.call(op="request", primitive="TAR", time=5)
        received = receiver.call(op="collect", until=5, timeout_ms=5000)
        assert received["events"] == [
            {"kind": "receive", "class": "Tick", "time": 4, "value": 0x10203040},
            {"kind": "grant", "time": 5},
        ], f"unexpected callback sequence: {received}; evidence: {receiver.directory}"
        assert received["logical_time"] == 5
        sent = sender.call(op="collect", until=5, timeout_ms=5000)
        assert sent["events"][-1] == {"kind": "grant", "time": 5}
        assert sent["logical_time"] == 5


def assert_mixed_primitives():
    with federation(("go", True), ("python", True)) as (go, python):
        primitives = ["TAR", "TARA", "NMR", "NMRA", "FQR"]
        pairs = zip(primitives, reversed(primitives), strict=True)
        for index, (go_primitive, py_primitive) in enumerate(pairs, 1):
            target = index * 3
            requests = [(go, go_primitive), (python, py_primitive)]
            # FQR may grant below its target. Establish the peer's bound first
            # so this scenario has one exact expected grant for every primitive.
            requests.sort(key=lambda request: request[1] == "FQR")
            for peer, primitive in requests:
                result = peer.call(op="request", primitive=primitive, time=target)
                assert result["primitive"] == primitive
            for peer in (go, python):
                result = peer.call(op="collect", until=target, timeout_ms=5000)
                assert result["events"] == [{"kind": "grant", "time": target}]
                assert result["logical_time"] == target


def assert_pyjevsim_adapter_cycles():
    with federation(("python", True), ("python", True), ("bridge", True)) as (left, right, bridge):
        for cycle in range(1, 11):
            target = cycle * 3
            for peer in (left, right):
                peer.call(op="request", primitive="TAR", time=target)
            result = bridge.call(op="bridge_step")
            assert result["logical_time"] == target
            assert result["model_cycles"] == cycle
            assert result["output_calls"] == cycle
            for peer in (left, right):
                grants = peer.call(op="collect", until=target, timeout_ms=5000)
                assert grants["events"][-1] == {"kind": "grant", "time": target}
                assert grants["logical_time"] == target
