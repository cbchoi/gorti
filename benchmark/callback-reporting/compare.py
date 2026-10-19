"""Paired N=2 DEVStone-HLA comparison of callback transport builds.

Uses the normal Go SDK federates, with identical confirmed producer calls.
Outputs summaries and plans outside the repository, never execution logs.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import random
import socket
import statistics
import subprocess
import sys
import time

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO / "benchmark/devstone/workload"))
from model import WorkloadConfig, generate_workload, write_workload
from plan import load_workload, materialize_plan, write_plan


def digest(path):
    with Path(path).open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def start(args):
    return subprocess.Popen(
        [str(arg) for arg in args], cwd=REPO,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
    )


def stop(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)


def measure(args, profile, index, seed, plan_path, count):
    producer_mode = getattr(args, "producer_mode", "confirmed")
    callback_profile = getattr(args, "callback_profile", profile)
    directory = args.output / f"{index:02d}-{profile}"
    directory.mkdir()
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    address = f"127.0.0.1:{port}"
    server_path = getattr(args, f"{profile}_server")
    client_path = getattr(args, f"{profile}_client")
    server_args = [server_path, f"--listen={address}", "--admin-listen=",
                   "--metrics-listen=127.0.0.1:0", "--log-level=error",
                   "--outbox-event-capacity=8192", "--audit-replay-plugin=none",
                   f"--save-dir={directory / 'saves'}"]
    children = []
    try:
        server = start(server_args)
        children.append(server)
        deadline = time.monotonic() + 20
        while True:
            if server.poll() is not None:
                raise RuntimeError(f"server exited: {server.communicate()!r}")
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.1):
                    break
            except OSError:
                if time.monotonic() >= deadline:
                    raise TimeoutError("server startup")
                time.sleep(0.02)
        common = [client_path, "--address", address, "--federation", f"reports-{index}",
                  "--fom", args.fom, "--seed", seed, "--count", count,
                  "--operation-warmup", 128, "--timeout", "120s", "--receive-order",
                  "--" + producer_mode, "--callback-representation", "handles",
                  "--workload-plan", plan_path, "--workload-plan-sha256", digest(plan_path),
                  "--compact-summary"]
        if producer_mode == "local-lrc":
            common += ["--local-lrc-batch-size", getattr(args, "batch_size", 32)]
        for role in ("subscriber", "publisher"):
            children.append(start(common + ["--role", role, "--output", directory / role]))
        deadline = time.monotonic() + 150
        while any(process.poll() is None for process in children[1:]):
            for process in children[1:]:
                if process.poll() not in (None, 0):
                    stdout, stderr = process.communicate()
                    raise RuntimeError(f"participant {process.args!r}: " + (stdout + stderr).decode(errors="replace")[-5000:])
            if time.monotonic() >= deadline:
                raise TimeoutError("participants")
            time.sleep(0.01)
        for process in children[1:]:
            stdout, stderr = process.communicate()
            if process.returncode:
                raise RuntimeError(f"participant {process.args!r}: " + (stdout + stderr).decode(errors="replace")[-5000:])
        summaries = {
            role: json.loads((directory / role / f"{role}-summary.json").read_text())
            for role in ("publisher", "subscriber")
        }
        for summary in summaries.values():
            assert summary["status"] == "accepted"
            assert summary["seed"] == seed and summary["count"] == count
            assert summary["plan_sha256"] == digest(plan_path)
            assert all(summary[key] for key in ("ready", "measure", "start", "done"))
        subscriber = summaries["subscriber"]
        accounting = subscriber["callback_accounting"]
        assert accounting["delivered"] == accounting["expected"] == 2 * count
        assert accounting["attribute_delivered"] == accounting["interaction_delivered"] == count
        assert not any(accounting[k] for k in ("rejected", "dropped", "unexpected", "duplicates", "invalid"))
        legacy = getattr(args, "legacy_reference", False)
        if not legacy:
            stats = subscriber["callback_report_transport"]
            if callback_profile == "async":
                assert stats["AsyncOutcomes"] >= 2 * count, stats
                assert stats["StreamReports"] >= 4 * count, stats
            else:
                assert all(value == 0 for value in stats.values()), stats
        if producer_mode == "local-lrc" and not legacy:
            local = summaries["publisher"]["local_lrc_transport"]
            assert local["Enqueued"] == local["Sent"] == local["Acked"] == 2 * (count + 128), local
            assert local["Failures"] == 0 and local["OperationFrames"] < local["Sent"], local
        duration = subscriber["completed_receive_order_batch_ns"] / 1e9
        return {"profile": profile, "pair": index, "seed": seed,
                "phase": "warmup" if index < args.warmup else "measured",
                "status": "accepted", "seconds": duration,
                "transport_attestation": "unavailable in legacy reference" if legacy else "verified",
                "callbacks_per_second": 2 * count / duration,
                "summaries": summaries,
                "server_arguments": list(map(str, server_args)),
                "client_arguments": list(map(str, common))}
    finally:
        for process in reversed(children):
            stop(process)


def semantics(result):
    keys = ("status", "seed", "count", "plan_sha256", "topology_sha256", "ready", "measure", "start", "done",
            "callback_accounting", "attribute_arrival_order_sha256",
            "interaction_arrival_order_sha256", "callback_trace_sha256")
    return {role: {key: summary[key] for key in keys}
            for role, summary in result["summaries"].items()}


def analyze(results, profiles=("sync", "async")):
    measured = [r for r in results if r["phase"] == "measured" and r["status"] == "accepted"]
    summary = {"failures": [r for r in results if r["status"] != "accepted"]}
    for profile in profiles:
        values = [r["seconds"] for r in measured if r["profile"] == profile]
        if values:
            summary[profile] = {"n": len(values), "mean_seconds": statistics.mean(values),
                                "median_seconds": statistics.median(values),
                                "mean_callbacks_per_second": statistics.mean(r["callbacks_per_second"] for r in measured if r["profile"] == profile),
                                "stdev_seconds": statistics.stdev(values) if len(values) > 1 else 0}
    grouped = {}
    for row in measured:
        grouped.setdefault(row["pair"], {})[row["profile"]] = row
    pairs = [p for p in grouped.values() if set(p) == set(profiles)]
    summary["paired_count"] = len(pairs)
    if pairs:
        baseline = [p[profiles[0]]["seconds"] for p in pairs]
        candidate = [p[profiles[1]]["seconds"] for p in pairs]
        speedup = statistics.mean(baseline) / statistics.mean(candidate)
        summary["completion_speedup"] = speedup
        summary["completion_reduction_percent"] = 100 * (1 - 1 / speedup)
        summary[profiles[1] + "_faster_pairs"] = sum(a < s for s, a in zip(baseline, candidate))
        rng = random.Random(1516)
        strata = [[i for i, p in enumerate(pairs) if p[profiles[0]]["pair"] % 2 == parity] for parity in (0, 1)]
        bootstraps = []
        for _ in range(10000):
            indexes = [rng.choice(group) for group in strata for _ in group]
            bootstraps.append(sum(baseline[i] for i in indexes) / sum(candidate[i] for i in indexes))
        bootstraps.sort()
        summary["speedup_paired_order_stratified_bootstrap_ci95"] = [bootstraps[249], bootstraps[9749]]
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    for profile in ("sync", "async"):
        for executable in ("server", "client"):
            parser.add_argument(f"--{profile}-{executable}", required=True, type=Path)
    parser.add_argument("--fom", type=Path, default=REPO / "verification/commercial-rti/fom/CommercialRtiVerifier.xml")
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--runs", type=int, default=20)
    parser.add_argument("--external-events", type=int, default=5)
    args = parser.parse_args()
    args.output = args.output.resolve()
    if args.output == REPO or REPO in args.output.parents:
        parser.error("results must be outside the repository")
    if args.warmup < 0 or args.runs < 1:
        parser.error("warmup >= 0 and runs >= 1 required")
    args.output.mkdir(parents=True, exist_ok=False)
    workload_path = write_workload(args.output / "workload.json", generate_workload(WorkloadConfig(topology="HI", width=10, depth=10, external_event_count=args.external_events, seed=1516)))
    manifest = {"platform": platform.platform(), "logical_cpus": os.cpu_count(),
                "warmup_pairs": args.warmup, "measured_pairs": args.runs,
                "fom_sha256": digest(args.fom), "workload_sha256": digest(workload_path),
                "independent_federate_processes": 2, "fresh_server_per_trial": True,
                "operation_warmup_per_channel": 128, "producer_mode": "confirmed",
                "order": "alternating AB/BA", "logging": "compact summaries only",
                "measurement_boundary": "subscriber armed before VERIFY_START to dequeue timestamp of last subsequently validated callback, before DispatchEvent; ACK drain checked before teardown but outside timed interval",
                "scope": "callback report profiles; eligible RO entry may be negotiated locally in async builds; no Portico run in this comparison",
                "binaries": {name: {"path": str(value.resolve()), "sha256": digest(value)} for name, value in vars(args).items() if name.endswith(("_server", "_client"))}}
    save(args.output / "manifest.json", manifest)
    workload = load_workload(workload_path)
    results = []
    for index in range(args.warmup + args.runs):
        seed = 1516 + index
        plan = materialize_plan(workload, seed)
        plan_path = write_plan(args.output / f"plan-{seed}.bin", plan)
        order = ("sync", "async") if index % 2 == 0 else ("async", "sync")
        pair = {}
        for profile in order:
            try:
                row = measure(args, profile, index, seed, plan_path, plan.record_count)
                pair[profile] = row
                print(f"pair {index:02d} {profile}: {row['seconds']:.6f}s", flush=True)
            except Exception as error:
                row = {"profile": profile, "pair": index, "seed": seed,
                       "phase": "warmup" if index < args.warmup else "measured",
                       "status": "failed", "error": str(error)}
                print(f"pair {index:02d} {profile} FAILED: {error}", flush=True)
            results.append(row)
            save(args.output / "results.json", results)
        if len(pair) == 2 and semantics(pair["sync"]) != semantics(pair["async"]):
            raise RuntimeError(f"semantic evidence differs in pair {index}")
    summary = analyze(results)
    save(args.output / "comparison.json", summary)
    print(json.dumps(summary, indent=2), flush=True)
    return 1 if summary["failures"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
