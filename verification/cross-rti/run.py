"""Apply retained public-Java service tests to a selected RTI, without changing oracles."""

from __future__ import annotations

import argparse
import difflib
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import time
import xml.etree.ElementTree as ET


HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]
SOURCE = REPO / "javasdk/client/src/test/java/org/gorti/rti"
CLASSES = ("LiveFederationTest", "LiveExchangeTest", "LiveTimeArithmeticTest")
CONTROL = "PortableInteractionControl"
JUNIT_SHA256 = "b016ef6b1c3454d6d7c2c88ce081dabf289699686af6622d6e4e2e1b54b4a2fc"


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def execute(command, cwd, log, timeout=180):
    with log.open("w", encoding="utf-8") as stream:
        try:
            result = subprocess.run(command, cwd=cwd, stdout=stream, stderr=subprocess.STDOUT,
                                    timeout=timeout, check=False)
            return {"command": list(map(str, command)), "returncode": result.returncode}
        except subprocess.TimeoutExpired:
            return {"command": list(map(str, command)), "returncode": None, "timeout": timeout}


def prepare(output):
    sources = output / "sources/org/gorti/rti"
    sources.mkdir(parents=True)
    changes = []
    for name in CLASSES:
        original = (SOURCE / (name + ".java")).read_text(encoding="utf-8")
        adapted = re.sub(r'@EnabledIfSystemProperty\(named = "gorti.endpoint", matches = "\.\+",\n'
                         r'\s*disabledReason = "[^"\n]*"\)\n', "", original)
        adapted = adapted.replace('RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador()',
                                  "TestRti.ambassador()")
        adapted = re.sub(
            r'(?P<rti>(?:owner\.)?rti)\.connect\((?P<cb>(?:owner\.)?callbacks), '
            r'CallbackModel.HLA_EVOKED, (?:endpoint|System.getProperty\("gorti.endpoint"\))\);',
            r'TestRti.connect(\g<rti>, \g<cb>);', adapted)
        # These four lines validate a gorti endpoint URI, not an HLA service result.
        adapted = re.sub(r'        URI address = URI.create\(endpoint\);\n'
                         r'        assertEquals\("grpc"[^\n]*\n'
                         r'        assertNotNull\(address.getHost\(\)[^\n]*\n'
                         r'        assertTrue\(address.getPort\(\)[^\n]*\n', "", adapted)
        if 'getRtiFactory("gorti")' in adapted or re.search(r'\brti\.connect\(', adapted):
            raise ValueError("Unadapted provider setup in " + name)
        target = sources / (name + ".java")
        target.write_text(adapted, encoding="utf-8")
        diff = "".join(difflib.unified_diff(original.splitlines(True), adapted.splitlines(True),
                                          fromfile=name + ".original.java", tofile=name + ".adapted.java"))
        (output / (name + ".patch")).write_text(diff, encoding="utf-8")
        changes.append({"source": str((SOURCE / (name + ".java")).relative_to(REPO)),
                        "original_sha256": digest(SOURCE / (name + ".java")),
                        "adapted_sha256": digest(target), "patch": name + ".patch"})
    shutil.copyfile(HERE / "TestRti.java", sources / "TestRti.java")
    shutil.copyfile(HERE / (CONTROL + ".java"), sources / (CONTROL + ".java"))
    classes = output / "classes"
    classes.mkdir()
    fom = REPO / "javasdk/client/src/test/resources/exchange-fom.xml"
    shutil.copyfile(fom, classes / "exchange-fom.xml")
    write_json(output / "adaptations.json", {"changes": changes,
               "adapter_sha256": digest(HERE / "TestRti.java"), "fom_sha256": digest(fom),
               "separate_control_sha256": digest(HERE / (CONTROL + ".java")),
               "scope": "Provider/connection setup and gorti URI checks only; service assertions retained."})
    return sources, classes


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def outcomes(reports):
    rows = []
    for path in sorted(reports.glob("TEST-*.xml")):
        root = ET.parse(path).getroot()
        for case in root.iter("testcase"):
            failure = case.find("failure")
            if failure is None:
                failure = case.find("error")
            rows.append({"class": case.get("classname"), "name": case.get("name"),
                         "status": "fail" if failure is not None else
                         "skip" if case.find("skipped") is not None else "pass",
                         "message": failure.get("message") if failure is not None else None,
                         "report": path.name})
    return rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--implementation", required=True, choices=("portico", "gorti"))
    parser.add_argument("--java", default="java")
    parser.add_argument("--javac", default="javac")
    parser.add_argument("--junit-console", type=Path, required=True)
    parser.add_argument("--portico-jar", type=Path)
    parser.add_argument("--gorti-classpath-file", type=Path)
    parser.add_argument("--gorti-client-jar", type=Path,
                        default=REPO / "javasdk/client/target/gorti-java-client-0.1.0-SNAPSHOT.jar")
    parser.add_argument("--rtid", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--select", choices=(*CLASSES, CONTROL), action="append")
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    junit = args.junit_console.resolve()
    if digest(junit) != JUNIT_SHA256:
        raise ValueError("Use the documented JUnit Platform console 1.11.4 artifact")
    sources, classes = prepare(output)
    if args.implementation == "portico":
        if not args.portico_jar:
            parser.error("--portico-jar is required")
        jars = [args.portico_jar.resolve()]
        spec = importlib.util.spec_from_file_location("transport_override", REPO / "verification/portico/build_transport_override.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        override = output / "portico-tcp-override.jar"
        module.build_override(jars[0], override)
        jars.insert(0, override)
    else:
        if not args.gorti_classpath_file or not args.rtid:
            parser.error("--gorti-classpath-file and --rtid are required")
        jars = [args.gorti_client_jar.resolve(), *[
            Path(p).resolve() for p in args.gorti_classpath_file.read_text().strip().split(os.pathsep) if p]]
    classpath = os.pathsep.join(map(str, [classes, *jars, junit]))
    compile_command = [args.javac, "--release", "11", "-encoding", "UTF-8", "-cp", classpath,
                       "-d", str(classes), *map(str, sorted(sources.glob("*.java")))]
    compiled = execute(compile_command, output, output / "compile.log")
    write_json(output / "compile.json", compiled)
    if compiled["returncode"] != 0:
        raise RuntimeError("Test compilation failed; see compile.log")
    runtime = {"implementation": args.implementation,
               "java": execute([args.java, "-version"], output, output / "java-version.log"),
               "classpath": [{"path": str(p), "sha256": digest(p)} for p in jars],
               "junit_sha256": digest(junit), "source_base": subprocess.check_output(
                   ["git", "rev-parse", "HEAD"], cwd=REPO, text=True).strip(),
               "topology": "two or three independent RTI ambassador sessions in one test JVM; not a benchmark"}
    server = None
    server_log = None
    try:
        endpoint = ""
        if args.implementation == "gorti":
            port = free_port()
            endpoint = "grpc://127.0.0.1:" + str(port)
            command = [str(args.rtid.resolve()), "--listen=127.0.0.1:" + str(port),
                       "--admin-listen=", "--metrics-listen=127.0.0.1:0", "--log-level=error",
                       "--save-dir=" + str(output / "save-state")]
            server_log = (output / "server.log").open("w", encoding="utf-8")
            server = subprocess.Popen(command, cwd=output, stdout=server_log, stderr=subprocess.STDOUT)
            runtime["server"] = {"command": command, "sha256": digest(args.rtid)}
            deadline = time.monotonic() + 30
            while True:
                if server.poll() is not None:
                    raise RuntimeError("rtid exited during startup")
                try:
                    with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                        break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise TimeoutError("rtid startup")
                    time.sleep(0.1)
        runs = []
        for name in args.select or (*CLASSES, CONTROL):
            target = output / name
            target.mkdir()
            port = free_port()
            command = [args.java, "-Dcross.rti=" + args.implementation,
                       "-Dcross.port.base=" + str(port), "-Dgorti.endpoint=" + endpoint,
                       "-Dgorti.fom=" + str(classes / "exchange-fom.xml"),
                       "-Dportico.loglevel=OFF", "-Dportico.jgroups.responseTimeout=5000",
                       "-Djava.net.preferIPv4Stack=true", "-Dportico.jgroups.tcp.bindAddress=127.0.0.1",
                       "-Dportico.jgroups.tcp.initialHosts=" + ",".join(
                           "127.0.0.1[" + str(port + offset) + "]" for offset in range(16)),
                       "-cp", classpath, "org.junit.platform.console.ConsoleLauncher", "execute",
                       "--select-class", "org.gorti.rti." + name, "--reports-dir", str(target / "reports"),
                       "--details", "tree", "--disable-ansi-colors", "--fail-if-no-tests"]
            run = execute(command, target, target / "console.log", timeout=210)
            run.update({"class": name, "outcomes": outcomes(target / "reports")})
            runs.append(run)
            write_json(output / "results.json", {"runtime": runtime, "runs": runs})
            print(name, run["returncode"], [(r["name"], r["status"]) for r in run["outcomes"]], flush=True)
        return 0 if all(r["returncode"] == 0 for r in runs) else 1
    finally:
        if server is not None:
            server.terminate()
            try:
                server.wait(timeout=10)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait()
        if server_log is not None:
            server_log.close()


if __name__ == "__main__":
    raise SystemExit(main())
