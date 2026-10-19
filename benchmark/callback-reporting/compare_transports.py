"""Paired DEVStone-HLA transport comparison with explicit build and mode evidence."""

import argparse
import copy
import json
import os
from pathlib import Path
import platform

from compare import (
    REPO, WorkloadConfig, analyze, digest, generate_workload, load_workload,
    materialize_plan, measure, save, semantics, write_plan, write_workload,
)
from crossover import analyze_crossover, make_schedule, run_crossover


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--server", required=True, type=Path)
    parser.add_argument("--client", required=True, type=Path)
    parser.add_argument("--baseline-server", type=Path)
    parser.add_argument("--baseline-client", type=Path)
    parser.add_argument("--baseline-mode", choices=("confirmed", "local-lrc"), default="confirmed")
    parser.add_argument("--candidate-mode", choices=("confirmed", "local-lrc"), default="local-lrc")
    parser.add_argument("--callback-profile", choices=("sync", "async"), default="async")
    parser.add_argument("--legacy-reference", action="store_true",
                        help="baseline predates callback receipts; absent transport counters are unavailable, not zero")
    parser.add_argument("--fom", type=Path, default=REPO / "verification/commercial-rti/fom/CommercialRtiVerifier.xml")
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--runs", type=int, default=20)
    parser.add_argument("--external-events", type=int, default=5)
    parser.add_argument("--batch-size", type=int, choices=(32, 64, 128, 256), default=32)
    parser.add_argument("--design", choices=("paired", "crossover"), default="paired",
                        help="crossover runs both AB and BA for every seed; warmup/runs count four-trial blocks")
    parser.add_argument("--order-seed", type=int, default=1516,
                        help="reproducible balanced ABBA/BAAB allocation, independent of workload seeds")
    args = parser.parse_args()
    if args.legacy_reference and (args.baseline_server is None or args.baseline_client is None):
        parser.error("legacy-reference requires explicit baseline-server and baseline-client")
    args.baseline_server = args.baseline_server or args.server
    args.baseline_client = args.baseline_client or args.client
    args.candidate_server, args.candidate_client = args.server, args.client
    args.output = args.output.resolve()
    if args.output == REPO or REPO in args.output.parents:
        parser.error("results must be outside the repository")
    if args.warmup < 0 or args.runs < 1 or args.external_events < 1:
        parser.error("warmup >= 0, runs >= 1 and external-events >= 1 required")
    if args.design == "crossover" and args.runs < 2:
        parser.error("crossover requires at least two measured seed blocks")
    args.output.mkdir(parents=True, exist_ok=False)
    workload_path = write_workload(args.output / "workload.json", generate_workload(
        WorkloadConfig(topology="HI", width=10, depth=10, external_event_count=args.external_events, seed=1516)))
    schedule = None
    if args.design == "crossover":
        schedule = make_schedule(args.warmup, args.runs, args.order_seed)
        save(args.output / "schedule.json", schedule)
    pairs_per_unit = 2 if schedule is not None else 1
    save(args.output / "manifest.json", {
        "platform": platform.platform(), "logical_cpus": os.cpu_count(),
        "gomaxprocs": os.environ.get("GOMAXPROCS", "runtime default"),
        "warmup_pairs": args.warmup * pairs_per_unit, "measured_pairs": args.runs * pairs_per_unit,
        "design": args.design,
        "warmup_seed_blocks": args.warmup if schedule is not None else None,
        "measured_seed_blocks": args.runs if schedule is not None else None,
        "order_seed": args.order_seed if schedule is not None else None,
        "schedule_sha256": digest(args.output / "schedule.json") if schedule is not None else None,
        "fom_sha256": digest(args.fom), "workload_sha256": digest(workload_path),
        "callback_profile": args.callback_profile, "batch_size": args.batch_size,
        "legacy_reference": args.legacy_reference,
        "legacy_scope": "performance reference only, not current IEEE service conformance evidence" if args.legacy_reference else None,
        "conditions": {"baseline": args.baseline_mode, "candidate": args.candidate_mode},
        "order": "balanced randomized ABBA/BAAB, both orders for each seed" if schedule is not None else "alternating AB/BA",
        "independent_federate_processes": 2, "fresh_server_per_trial": True,
        "operation_warmup_per_channel": 128, "logging": "compact summaries only",
        "measurement_boundary": "subscriber armed before VERIFY_START to dequeue timestamp of last subsequently validated callback, before DispatchEvent; final ACK drain required outside timer",
        "binaries": {name: {"path": str(value.resolve()), "sha256": digest(value)}
                     for name, value in vars(args).items() if name.endswith(("_server", "_client"))},
    })
    workload, results = load_workload(workload_path), []
    if schedule is not None:
        results = run_crossover(args, workload, schedule)
        summary = analyze_crossover(results, schedule)
        save(args.output / "comparison.json", summary)
        print(json.dumps(summary, indent=2), flush=True)
        return
    profiles = ("baseline", "candidate")
    for index in range(args.warmup + args.runs):
        seed = 1516 + index
        plan = materialize_plan(workload, seed)
        plan_path = write_plan(args.output / f"plan-{seed}.bin", plan)
        pair = {}
        for profile in profiles if index % 2 == 0 else reversed(profiles):
            condition = copy.copy(args)
            condition.producer_mode = getattr(args, profile + "_mode")
            condition.legacy_reference = args.legacy_reference and profile == "baseline"
            try:
                row = measure(condition, profile, index, seed, plan_path, plan.record_count)
                pair[profile] = row
                print(f"pair {index:02d} {profile}: {row['seconds']:.6f}s", flush=True)
            except Exception as error:
                results.append({"profile": profile, "pair": index, "seed": seed,
                                "phase": "warmup" if index < args.warmup else "measured",
                                "status": "failed", "error": str(error)})
                save(args.output / "results.json", results)
                raise
            results.append(row)
            save(args.output / "results.json", results)
        if semantics(pair[profiles[0]]) != semantics(pair[profiles[1]]):
            raise RuntimeError(f"semantic evidence differs in pair {index}")
    summary = analyze(results, profiles)
    save(args.output / "comparison.json", summary)
    print(json.dumps(summary, indent=2), flush=True)


if __name__ == "__main__":
    main()
