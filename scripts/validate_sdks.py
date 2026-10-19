#!/usr/bin/env python3
"""Run supported SDK service tests against a freshly built local RTI."""

from __future__ import annotations

import argparse
import hashlib
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from contextlib import contextmanager
from pathlib import Path

from verify_python_hla import inventory, read_configuration

ROOT = Path(__file__).resolve().parents[1]


def run(command: list[str], env: dict[str, str]) -> None:
    print("+ " + subprocess.list2cmdline(command), flush=True)
    subprocess.run(command, cwd=ROOT, env=env, check=True)  # noqa: S603


def tool(name: str) -> str:
    found = shutil.which(name)
    if found is None:
        raise RuntimeError(f"Required executable not found: {name}")
    return found


def test_environment(binary: Path, artifacts: Path) -> dict[str, str]:
    env = os.environ.copy()
    env.update(
        {
            "GORTI_RTID": str(binary),
            "GORTI_LIVE_RTID": str(binary),
            "RTID_BINARY": str(binary),
            "GORTI_RTID_SHA256": hashlib.sha256(binary.read_bytes()).hexdigest(),
            "GORTI_RUN_LIVE_E2E": "1",
            "GORTI_LIVE_ARTIFACTS": str(artifacts),
            "GORTI_INDEPENDENT_EVIDENCE": str(artifacts),
            "GORTI_RESEARCH_CONFIG": "",
            "PYTHONPATH": os.pathsep.join(
                (str(ROOT / "pysdk"), str(ROOT / "pysdk/rti1516e/_generated"))
            ),
            "PYTEST_ADDOPTS": "",
        }
    )
    return env


@contextmanager
def local_server(binary: Path, artifacts: Path, env: dict[str, str]):
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    with (artifacts / "java-server.log").open("wb") as log:
        process = subprocess.Popen(  # noqa: S603
            [
                str(binary),
                "--listen",
                f"127.0.0.1:{port}",
                "--admin-listen=127.0.0.1:0",
                "--metrics-listen=127.0.0.1:0",
                "--log-dir=",
                f"--save-dir={artifacts / 'java-saves'}",
            ],
            cwd=ROOT,
            env=env,
            stdout=log,
            stderr=subprocess.STDOUT,
        )
        try:
            deadline = time.monotonic() + 30
            while True:
                if process.poll() is not None:
                    raise RuntimeError(f"rtid exited; see {artifacts / 'java-server.log'}")
                try:
                    with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                        break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise TimeoutError("rtid did not become ready") from None
                    time.sleep(0.05)
            yield f"grpc://127.0.0.1:{port}"
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=10)


def validate_python(env: dict[str, str], artifacts: Path) -> None:
    selected = inventory(ROOT, read_configuration())
    run(
        [
            sys.executable,
            "-m",
            "pytest",
            *selected,
            "-c",
            "pysdk/pyproject.toml",
            "--rootdir",
            str(ROOT),
            "-q",
            "-ra",
            "--capture=sys",
            "-p",
            "no:cacheprovider",
            f"--junitxml={artifacts / 'python.xml'}",
        ],
        env,
    )


def validate_java(binary: Path, env: dict[str, str], artifacts: Path, maven: str) -> None:
    with local_server(binary, artifacts, env) as endpoint:
        command = [
            tool(maven),
            "--batch-mode",
            "--no-transfer-progress",
            "-f",
            str(ROOT / "javasdk/pom.xml"),
            "clean",
            "verify",
            f"-Dgorti.endpoint={endpoint}",
            f"-Dgorti.server.binary={binary}",
            f"-Dgorti.fom={ROOT / 'examples/go-tar-wait/tar-wait-fom.xml'}",
            f"-Dgorti.python={sys.executable}",
            f"-Dgorti.evidence.dir={artifacts}",
            f"-Dgorti.live.evidence={artifacts}",
        ]
        if os.environ.get("GORTI_MAVEN_REPO"):
            command.append(f"-Dmaven.repo.local={Path(os.environ['GORTI_MAVEN_REPO']).resolve()}")
        try:
            run(command, env)
        finally:
            reports = ROOT / "javasdk/client/target/surefire-reports"
            if reports.is_dir():
                shutil.copytree(reports, artifacts / "java-reports", dirs_exist_ok=True)
    run(
        [
            tool("java"),
            "--class-path",
            str(ROOT / "javasdk/ieee1516-api/target/classes"),
            str(ROOT / "javasdk/tools/GenerateAmbassador.java"),
            str(ROOT / "javasdk"),
            "--check",
        ],
        env,
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("languages", nargs="+", choices=("python", "java", "ivct"))
    parser.add_argument("--artifacts", type=Path, help="Output directory outside the repository")
    parser.add_argument("--maven", default=os.environ.get("GORTI_MAVEN", "mvn"))
    args = parser.parse_args()
    artifacts = (args.artifacts or Path(tempfile.mkdtemp(prefix="gorti-sdk-validation-"))).resolve()
    if artifacts == ROOT or ROOT in artifacts.parents:
        parser.error("artifacts must be outside the repository")
    artifacts.mkdir(parents=True, exist_ok=True)
    print(f"Validation artifacts: {artifacts}", flush=True)
    binary = artifacts / ("rtid.exe" if os.name == "nt" else "rtid")
    try:
        run([tool("go"), "build", "-o", str(binary), "./rti/cmd/rtid"], os.environ.copy())
        env = test_environment(binary, artifacts)
        for language in args.languages:
            if language == "python":
                validate_python(env, artifacts)
            elif language == "java":
                validate_java(binary, env, artifacts, args.maven)
            else:
                run(
                    [
                        sys.executable,
                        "-m",
                        "pytest",
                        "tests/conformance/rti/ivct-subset",
                        "-v",
                        "-p",
                        "no:cacheprovider",
                        f"--junitxml={artifacts / 'ivct.xml'}",
                    ],
                    env,
                )
    except (OSError, RuntimeError, subprocess.CalledProcessError) as exc:
        print(f"SDK validation failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
