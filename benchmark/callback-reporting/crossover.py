"""Seed-block crossover scheduling and analysis for fresh-process trials."""

from __future__ import annotations

import copy
from datetime import datetime, timezone
import math
import random
import statistics
import time

from compare import measure, materialize_plan, save, semantics, write_plan

PROFILES = {"A": "baseline", "B": "candidate"}


def make_schedule(warmup, runs, order_seed):
    if warmup < 0 or runs < 2:
        raise ValueError("warmup >= 0 and at least two measured seed blocks required")
    rng = random.Random(order_seed)
    schedule = []
    for phase, count in (("warmup", warmup), ("measured", runs)):
        sequences = ["ABBA", "BAAB"] * (count // 2)
        if count % 2:
            sequences.append(rng.choice(("ABBA", "BAAB")))
        rng.shuffle(sequences)
        for sequence in sequences:
            block = len(schedule)
            schedule.append({"block": block, "seed": 1516 + block,
                             "phase": phase, "sequence": sequence})
    return schedule


def run_crossover(args, workload, schedule):
    results = []
    for block in schedule:
        index, seed, sequence = block["block"], block["seed"], block["sequence"]
        plan = materialize_plan(workload, seed)
        plan_path = write_plan(args.output / f"plan-{seed}.bin", plan)
        reference = None
        for position, label in enumerate(sequence):
            profile = PROFILES[label]
            order = sequence[(position // 2) * 2:(position // 2) * 2 + 2]
            condition = copy.copy(args)
            condition.output = args.output / f"block-{index:02d}" / order
            condition.output.mkdir(parents=True, exist_ok=True)
            condition.producer_mode = getattr(args, profile + "_mode")
            condition.legacy_reference = args.legacy_reference and profile == "baseline"
            metadata = {**block, "position": position, "order": order,
                        "order_position": position % 2, "pair": index * 2 + position // 2,
                        "profile": profile, "trial_started_utc": datetime.now(timezone.utc).isoformat()}
            started = time.perf_counter()
            row = None
            try:
                # Keep the federation name and phase identical within a seed;
                # per-order parent directories isolate repeated profile outputs.
                row = measure(condition, profile, index, seed, plan_path, plan.record_count)
                row.update(metadata)
                evidence = semantics(row)
                if reference is not None and evidence != reference:
                    raise ValueError(f"semantic evidence differs within seed block {index}")
                reference = evidence
            except Exception as error:
                row = row or {}
                row.update(metadata)
                row.update(status="failed", error=str(error), trial_wall_seconds=time.perf_counter() - started)
                results.append(row)
                save(args.output / "results.json", results)
                raise
            row["trial_wall_seconds"] = time.perf_counter() - started
            results.append(row)
            save(args.output / "results.json", results)
            print(f"block {index:02d} {sequence} pos {position + 1} {profile}: {row['seconds']:.6f}s", flush=True)
    return results


def analyze_crossover(results, schedule, bootstrap_samples=10000, bootstrap_seed=1516,
                      *, semantic_key=semantics, delivered_count=None):
    if bootstrap_samples < 1:
        raise ValueError("positive bootstrap sample count required")
    expected = {(b["block"], p): (b, label) for b in schedule for p, label in enumerate(b["sequence"])}
    if len(expected) != 4 * len(schedule) or len(results) != len(expected):
        raise ValueError("missing/duplicate scheduled seed block or trial")
    grouped = {}
    seen = set()
    for row in results:
        key = (row["block"], row["position"])
        if key not in expected or key in seen or row["status"] != "accepted":
            raise ValueError("unexpected, duplicate or failed crossover trial")
        block, label = expected[key]
        order = block["sequence"][(key[1] // 2) * 2:(key[1] // 2) * 2 + 2]
        if any(row[field] != block[field] for field in ("seed", "phase", "sequence")) or \
                row["profile"] != PROFILES[label] or row["order"] != order or row["order_position"] != key[1] % 2:
            raise ValueError("trial identity differs from predeclared schedule")
        if not math.isfinite(row["seconds"]) or row["seconds"] <= 0:
            raise ValueError("nonpositive/nonfinite duration")
        seen.add(key)
        grouped.setdefault(row["block"], []).append(row)
    for rows in grouped.values():
        if any(semantic_key(row) != semantic_key(rows[0]) for row in rows[1:]):
            raise ValueError("seed block semantic mismatch")

    blocks = [grouped[b["block"]] for b in schedule if b["phase"] == "measured"]
    if len(blocks) < 2:
        raise ValueError("at least two measured seed blocks required")
    measured = [row for block in blocks for row in block]
    summary = {"design": "seed-block crossover", "measured_blocks": len(blocks),
               "warmup_blocks": len(schedule) - len(blocks), "failures": [],
               "bootstrap_unit": "whole four-trial seed block, stratified by ABBA/BAAB sequence",
               "bootstrap_samples": bootstrap_samples, "bootstrap_seed": bootstrap_seed}
    for profile in PROFILES.values():
        rows = [r for r in measured if r["profile"] == profile]
        values = [r["seconds"] for r in rows]
        callbacks = sum(delivered_count(r) if delivered_count is not None else
                        r["summaries"]["subscriber"]["callback_accounting"]["delivered"] for r in rows)
        if callbacks <= 0:
            raise ValueError("positive callback count required")
        summary[profile] = {"n": len(rows), "mean_seconds": statistics.mean(values),
                            "median_seconds": statistics.median(values), "stdev_seconds": statistics.stdev(values),
                            "aggregate_callbacks_per_second": callbacks / sum(values)}

    def block_mean(rows, field, value):
        return statistics.mean(r["seconds"] for r in rows if r[field] == value)

    baseline = [block_mean(b, "profile", "baseline") for b in blocks]
    candidate = [block_mean(b, "profile", "candidate") for b in blocks]
    first = [block_mean(b, "order_position", 0) for b in blocks]
    second = [block_mean(b, "order_position", 1) for b in blocks]
    speedup = sum(baseline) / sum(candidate)
    summary.update(completion_speedup=speedup, throughput_gain_percent=100 * (speedup - 1),
                   completion_reduction_percent=100 * (1 - 1 / speedup),
                   candidate_faster_blocks=sum(c < b for b, c in zip(baseline, candidate)),
                   first_in_pair_mean_seconds=statistics.mean(first), second_in_pair_mean_seconds=statistics.mean(second),
                   second_over_first=sum(second) / sum(first))
    summary["orders"] = {}
    for order in ("AB", "BA"):
        rows = [r for r in measured if r["order"] == order]
        b = block_mean(rows, "profile", "baseline")
        c = block_mean(rows, "profile", "candidate")
        summary["orders"][order] = {"pairs": len(blocks), "baseline_mean_seconds": b,
                                    "candidate_mean_seconds": c, "completion_speedup": b / c}
    summary["positions"] = {str(p + 1): {"n": sum(r["position"] == p for r in measured),
                                         "mean_seconds": block_mean(measured, "position", p)} for p in range(4)}

    strata = [[i for i, block in enumerate(blocks) if block[0]["sequence"] == sequence] for sequence in ("ABBA", "BAAB")]
    rng = random.Random(bootstrap_seed)
    speedups, order_effects = [], []
    for _ in range(bootstrap_samples):
        indices = [rng.choice(group) for group in strata for _ in group]
        speedups.append(sum(baseline[i] for i in indices) / sum(candidate[i] for i in indices))
        order_effects.append(sum(second[i] for i in indices) / sum(first[i] for i in indices))
    for name, values in (("speedup_seed_block_stratified_bootstrap_ci95", speedups),
                         ("second_over_first_seed_block_stratified_bootstrap_ci95", order_effects)):
        values.sort()
        summary[name] = [values[max(0, math.ceil(0.025 * len(values)) - 1)], values[math.ceil(0.975 * len(values)) - 1]]
    return summary
