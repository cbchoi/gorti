"""Native-process DEVStone-HLA comparison: Portico and a gorti report profile.

Reuses the existing Java/Go participants and their normalized evidence contract.
No smoke, staging or freeze-state gates run before measurement.
"""

from __future__ import annotations

import argparse
import copy
from datetime import datetime, timezone
import json
import os
import platform
import random
import shutil
import socket
import statistics
import subprocess
import sys
import threading
import time
from pathlib import Path

import compare as callback
from crossover import PROFILES, analyze_crossover, make_schedule

sys.path.insert(0, str(callback.REPO / "verification/portico"))
import compare_receive_order as fair
from build_transport_override import build_override


class Participant:
    def __init__(self, arguments, directory):
        self.arguments = list(map(str, arguments))
        self.process = subprocess.Popen(
            self.arguments, cwd=directory, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        self.output = None
        self.reader = threading.Thread(target=self.capture, daemon=True)
        self.reader.start()

    def capture(self):
        self.output = self.process.communicate()

    def check(self):
        if self.process.poll() not in (None, 0):
            self.reader.join(timeout=2)
            detail = b" ".join(self.output or ()).decode(errors="replace")
            raise RuntimeError(f"Portico exit={self.process.returncode}: {detail[-5000:]}")

    def stop(self):
        callback.stop(self.process)
        self.reader.join(timeout=5)


def validate(directory, implementation, plan):
    evidence = directory / "evidence"
    evidence.mkdir()
    for role in ("publisher", "subscriber"):
        shutil.copyfile(directory / role / f"{role}-summary.json", evidence / f"{role}-summary.json")
    return fair.validate_run(evidence, implementation, plan)


def portico_run(args, directory, plan):
    directory.mkdir()
    with socket.socket() as first, socket.socket() as second:
        first.bind(("127.0.0.1", 0))
        second.bind(("127.0.0.1", 0))
        ports = {"publisher": first.getsockname()[1], "subscriber": second.getsockname()[1]}
    classpath = os.pathsep.join(map(str, (args.override, args.java_classes, args.portico_jar)))
    ready = directory / ".publisher.ready"
    joined = directory / ".subscriber.joined"
    release = directory / ".publisher.release"
    children = []
    commands = {}

    def launch(role):
        command = [
            args.java, "-Dportico.loglevel=OFF", "-Dportico.jgroups.responseTimeout=5000",
            "-Djava.net.preferIPv4Stack=true", "-Dportico.jgroups.tcp.bindAddress=127.0.0.1",
            f"-Dportico.jgroups.tcp.bindPort={ports[role]}",
            f"-Dportico.jgroups.tcp.initialHosts=127.0.0.1[{ports['publisher']}]",
            "-cp", classpath, fair.VERIFIER_CLASS, "--role", role,
            "--seed", plan.seed, "--count", plan.count, "--operation-warmup", 128,
            "--all-federates-sync", "true", "--receive-order", "true",
            "--workload-plan", plan.path, "--workload-plan-sha256", plan.sha256,
            "--compact-summary", "true", "--federation", f"reports-{plan.seed}",
            "--fom", args.fom, "--output", directory / role, "--timeout-ms", 120000,
            "--teardown-ready-file", directory / ".teardown.ready",
        ]
        if role == "publisher":
            command += ["--startup-ready-file", ready, "--startup-release-file", release]
        else:
            command += ["--join-ready-file", joined]
        child = Participant(command, directory)
        children.append(child)
        commands[role] = child.arguments

    def await_file(path):
        deadline = time.monotonic() + 120
        while not path.exists():
            for child in children:
                child.check()
                if child.process.poll() == 0:
                    raise RuntimeError(f"Portico exited before {path.name}")
            if time.monotonic() > deadline:
                raise TimeoutError(f"Portico readiness: {path.name}")
            time.sleep(0.02)

    try:
        launch("publisher")
        await_file(ready)
        launch("subscriber")
        await_file(joined)
        release.touch()
        deadline = time.monotonic() + 150
        while any(child.process.poll() is None for child in children):
            for child in children:
                child.check()
            if time.monotonic() > deadline:
                raise TimeoutError("Portico workload/teardown")
            time.sleep(0.02)
        for child in children:
            child.check()
        return validate(directory, "portico", plan) | {"commands": commands}
    finally:
        for child in reversed(children):
            child.stop()


def analyze(rows):
    failures = [row for row in rows if not row["semantic_match"]]
    pairs = [row for row in rows if row["phase"] == "measured" and row["semantic_match"]]
    result = {"failed_pairs": failures, "accepted_measured_pairs": len(pairs),
              "semantic_matching_pairs": sum(row["semantic_match"] for row in rows)}
    if not pairs:
        return result
    for implementation in ("portico", "gorti"):
        values = [row["results"][implementation] for row in pairs]
        seconds = [value["batch_ns"] / 1e9 for value in values]
        result[implementation] = {
            "n": len(values), "mean_seconds": statistics.mean(seconds),
            "median_seconds": statistics.median(seconds),
            "stdev_seconds": statistics.stdev(seconds) if len(seconds) > 1 else 0,
            "mean_callbacks_per_second": statistics.mean(value["throughput_deliveries_per_second"] for value in values),
            "aggregate_callbacks_per_second": sum(value["evidence"]["delivered_callbacks"] for value in values) / sum(seconds),
            "mean_trial_api_medians_us_not_equivalent_completion_boundaries": {
                operation: statistics.mean(value["operation_medians_ns"][operation] / 1000 for value in values)
                for operation in ("updateAttributeValues", "sendInteraction")
            },
        }
    p = [row["results"]["portico"]["batch_ns"] for row in pairs]
    g = [row["results"]["gorti"]["batch_ns"] for row in pairs]
    result["gorti_over_portico_mean_completion_ratio"] = sum(g) / sum(p)
    result["ratio_interpretation"] = "greater than one favors Portico"
    result["portico_faster_pairs"] = sum(left < right for left, right in zip(p, g))
    result["gorti_faster_pairs"] = sum(right < left for left, right in zip(p, g))
    groups = [[i for i, row in enumerate(pairs) if row["order"][0] == first]
              for first in ("portico", "gorti")]
    rng = random.Random(1516)
    bootstrap = []
    for _ in range(10000):
        indexes = [rng.choice(group) for group in groups for _ in group]
        bootstrap.append(sum(g[i] for i in indexes) / sum(p[i] for i in indexes))
    bootstrap.sort()
    result["ratio_paired_order_stratified_bootstrap_ci95"] = [bootstrap[249], bootstrap[9749]]
    return result


def analyze_portico_crossover(rows, schedule):
    for row in rows:
        if row["implementation"] != {"baseline": "portico", "candidate": "gorti"}.get(row["profile"]):
            raise ValueError("crossover implementation/profile mismatch")
    result = analyze_crossover(
        rows, schedule, semantic_key=lambda row: row["result"]["evidence"],
        delivered_count=lambda row: row["result"]["evidence"]["delivered_callbacks"],
    )
    result["conditions"] = {"baseline": "portico", "candidate": "gorti"}
    result["portico"], result["gorti"] = result["baseline"], result["candidate"]
    result["gorti_over_portico_mean_completion_ratio"] = 1 / result["completion_speedup"]
    low, high = result["speedup_seed_block_stratified_bootstrap_ci95"]
    result["ratio_seed_block_stratified_bootstrap_ci95"] = [1 / high, 1 / low]
    result["ratio_interpretation"] = "gorti/Portico completion: greater than one favors Portico; completion_speedup is the inverse"
    result["semantic_matching_seed_blocks"] = len(schedule)
    return result


def run_portico_crossover(args, workload, schedule):
    rows = []
    for block in schedule:
        index, seed, sequence = block["block"], block["seed"], block["sequence"]
        model = callback.materialize_plan(workload, seed)
        path = callback.write_plan(args.output / f"plan-{seed}.bin", model)
        plan = fair.PairPlan(path, seed, model.record_count, callback.digest(path), model.topology_identity.hex())
        reference = None
        for position, label in enumerate(sequence):
            implementation = "portico" if label == "A" else "gorti"
            order = sequence[(position // 2) * 2:(position // 2) * 2 + 2]
            condition = copy.copy(args)
            condition.output = args.output / f"block-{index:02d}" / order
            condition.output.mkdir(parents=True, exist_ok=True)
            row = {**block, "position": position, "order": order, "order_position": position % 2,
                   "profile": PROFILES[label], "implementation": implementation,
                   "trial_started_utc": datetime.now(timezone.utc).isoformat()}
            started = time.monotonic()
            try:
                if implementation == "portico":
                    value = portico_run(condition, condition.output / "portico", plan)
                else:
                    measured = callback.measure(condition, args.gorti_profile, index, seed, path, plan.count)
                    value = validate(condition.output / f"{index:02d}-{args.gorti_profile}", "gorti", plan)
                    value.update(server_arguments=measured["server_arguments"], client_arguments=measured["client_arguments"],
                                 callback_report_transport=measured["summaries"]["subscriber"]["callback_report_transport"],
                                 local_lrc_transport=measured["summaries"]["publisher"]["local_lrc_transport"])
                row.update(result=value, seconds=value["batch_ns"] / 1e9)
                if reference is not None and value["evidence"] != reference:
                    raise ValueError(f"semantic evidence differs within seed block {index}")
                reference = value["evidence"]
                row["status"] = "accepted"
            except Exception as error:
                row.update(status="failed", error=str(error), trial_wall_seconds=time.monotonic() - started)
                rows.append(row)
                callback.save(args.output / "results.json", rows)
                callback.save(args.output / "comparison.json", {"status": "incomplete", "error": str(error),
                                                               "completed_trials": len(rows) - 1})
                raise
            row["trial_wall_seconds"] = time.monotonic() - started
            rows.append(row)
            callback.save(args.output / "results.json", rows)
            print(f"block {index:02d} {sequence} pos {position + 1} {implementation}: {row['seconds']:.6f}s", flush=True)
    return rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gorti-server", type=Path, required=True)
    parser.add_argument("--gorti-client", type=Path, required=True)
    parser.add_argument("--gorti-profile", choices=("sync", "async"), default="async")
    parser.add_argument("--producer-mode", choices=("confirmed", "local-lrc"), default="confirmed")
    parser.add_argument("--batch-size", type=int, choices=(32, 64, 128, 256), default=32)
    parser.add_argument("--external-events", type=int, default=5)
    parser.add_argument("--java", type=Path, required=True)
    parser.add_argument("--java-classes", type=Path, required=True)
    parser.add_argument("--portico-jar", type=Path, required=True)
    parser.add_argument("--fom", type=Path, default=callback.REPO / "verification/commercial-rti/fom/CommercialRtiVerifier.xml")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--runs", type=int, default=20)
    parser.add_argument("--design", choices=("paired", "crossover"), default="paired",
                        help="crossover uses both orders per seed; warmup/runs count four-trial blocks")
    parser.add_argument("--order-seed", type=int, default=1516)
    args = parser.parse_args()
    if args.warmup < 0 or args.runs < 1 or args.external_events < 1:
        parser.error("warmup >= 0, runs >= 1 and external-events >= 1 required")
    if args.design == "crossover" and args.runs < 2:
        parser.error("crossover requires at least two measured seed blocks")
    for name, value in vars(args).items():
        if isinstance(value, Path):
            setattr(args, name, value.resolve())
    if args.output == callback.REPO or callback.REPO in args.output.parents:
        parser.error("results must be outside the repository")
    args.output.mkdir(parents=True, exist_ok=False)
    args.override = args.output / "portico-tcp-override.jar"
    build_override(args.portico_jar, args.override)
    setattr(args, f"{args.gorti_profile}_server", args.gorti_server)
    setattr(args, f"{args.gorti_profile}_client", args.gorti_client)
    workload_path = callback.write_workload(args.output / "workload.json", callback.generate_workload(
        callback.WorkloadConfig(topology="HI", width=10, depth=10, external_event_count=args.external_events, seed=1516)))
    schedule = (make_schedule(args.warmup, args.runs, args.order_seed) if args.design == "crossover"
                else fair.build_pair_schedule(1516, args.warmup, args.runs))
    callback.save(args.output / "schedule.json", schedule)
    manifest = {
        "platform": platform.platform(), "logical_cpus": os.cpu_count(),
        "gorti_profile": args.gorti_profile, "gorti_producer": args.producer_mode,
        "gorti_batch_size": args.batch_size, "external_events": args.external_events,
        "gomaxprocs": os.environ.get("GOMAXPROCS", "runtime default"),
        "schedule": schedule, "warmup_pairs": args.warmup * (2 if args.design == "crossover" else 1),
        "measured_pairs": args.runs * (2 if args.design == "crossover" else 1),
        "design": args.design, "order_seed": args.order_seed if args.design == "crossover" else None,
        "schedule_sha256": callback.digest(args.output / "schedule.json"),
        "crossover_labels": {"A": "portico", "B": "gorti"} if args.design == "crossover" else None,
        "independent_federate_processes": 2, "fresh_processes_each_trial": True,
        "operation_warmup_per_channel": 128,
        "fom_sha256": callback.digest(args.fom), "workload_sha256": callback.digest(workload_path),
        "portico_transport": "JGroups TCP/TCPPING override, IPv4 loopback",
        "logging": "compact participant summaries only; process stdout/stderr held in memory",
        "measurement_boundary": "timing arm before VERIFY_START to final data callback receive timestamp; data/order validation and successful teardown required, final outcome ACK drain outside timer",
        "timestamp_detail": "Go pump dequeue before DispatchEvent; Java callback entry before payload validation",
        "runtime_limitations": "fresh JVMs, 128 operations/channel warmup; host background activity not isolated; not a steady-state long-lived-JVM or multi-host study",
        "java_version": subprocess.run([str(args.java), "-version"], capture_output=True, text=True, check=True).stderr,
        "artifacts": {name: {"path": str(path), "sha256": callback.digest(path)} for name, path in (
            ("gorti_server", args.gorti_server), ("gorti_client", args.gorti_client),
            ("portico_jar", args.portico_jar), ("transport_override", args.override))},
        "java_classes": {str(path.relative_to(args.java_classes)): callback.digest(path)
                         for path in sorted(args.java_classes.rglob("*.class"))},
    }
    callback.save(args.output / "manifest.json", manifest)
    workload = callback.load_workload(workload_path)
    if args.design == "crossover":
        rows = run_portico_crossover(args, workload, schedule)
        result = analyze_portico_crossover(rows, schedule)
        callback.save(args.output / "comparison.json", result)
        print(json.dumps(result, indent=2), flush=True)
        return 0
    rows = []
    for index, entry in enumerate(schedule):
        model = callback.materialize_plan(workload, entry["seed"])
        path = callback.write_plan(args.output / f"plan-{entry['seed']}.bin", model)
        plan = fair.PairPlan(path, entry["seed"], model.record_count, callback.digest(path), model.topology_identity.hex())
        row = dict(entry, results={}, semantic_match=False)
        print(f"{entry['phase']} pair {entry['pair']} seed={entry['seed']} order={entry['order']}", flush=True)
        for implementation in entry["order"]:
            started = time.monotonic()
            try:
                if implementation == "portico":
                    result = portico_run(args, args.output / f"{index:02d}-portico", plan)
                else:
                    measured = callback.measure(args, args.gorti_profile, index, plan.seed, plan.path, plan.count)
                    result = validate(args.output / f"{index:02d}-{args.gorti_profile}", "gorti", plan)
                    result.update({"server_arguments": measured["server_arguments"],
                                   "client_arguments": measured["client_arguments"],
                                   "callback_report_transport": measured["summaries"]["subscriber"]["callback_report_transport"]})
                result["status"] = "accepted"
                print(f"{implementation}: {result['batch_ns']/1e9:.6f}s", flush=True)
            except Exception as error:
                result = {"status": "failed", "error": str(error)}
                print(f"{implementation}: FAILED {error}", flush=True)
            result["elapsed_wall_seconds_including_setup"] = time.monotonic() - started
            row["results"][implementation] = result
            callback.save(args.output / "results.json", rows + [row])
        if all(result["status"] == "accepted" for result in row["results"].values()):
            try:
                fair.assert_pair_semantics(entry["phase"], entry["pair"], row["results"])
                row["semantic_match"] = True
            except ValueError as error:
                row["semantic_error"] = str(error)
        rows.append(row)
        callback.save(args.output / "results.json", rows)
        if not row["semantic_match"]:
            callback.save(args.output / "comparison.json", analyze(rows))
            return 1
    result = analyze(rows)
    callback.save(args.output / "comparison.json", result)
    print(json.dumps(result, indent=2), flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
