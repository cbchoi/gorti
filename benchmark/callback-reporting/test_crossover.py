import copy
import json
from pathlib import Path
from tempfile import TemporaryDirectory
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from crossover import PROFILES, analyze_crossover, make_schedule, run_crossover


def trial(block, position, seconds):
    order = block["sequence"][(position // 2) * 2:(position // 2) * 2 + 2]
    evidence = {
        "status": "accepted", "seed": block["seed"], "count": 2,
        "plan_sha256": str(block["seed"]), "topology_sha256": "topology",
        "ready": True, "measure": True, "start": True, "done": True,
        "callback_accounting": {"delivered": 4},
        "attribute_arrival_order_sha256": "attribute",
        "interaction_arrival_order_sha256": "interaction", "callback_trace_sha256": "trace",
    }
    return {**block, "position": position, "profile": PROFILES[block["sequence"][position]],
            "order": order, "order_position": position % 2, "seconds": seconds,
            "status": "accepted", "summaries": {role: copy.deepcopy(evidence) for role in ("publisher", "subscriber")}}


def observations(schedule, duration):
    return [trial(block, p, duration(block, p)) for block in schedule for p in range(4)]


class CrossoverTests(unittest.TestCase):
    def test_schedule_balances_orders_within_every_seed(self):
        schedule = make_schedule(5, 20, 20261007)
        self.assertEqual(schedule, make_schedule(5, 20, 20261007))
        self.assertNotEqual(schedule, make_schedule(5, 20, 20261008))
        measured = [b for b in schedule if b["phase"] == "measured"]
        self.assertEqual(len(measured), 20)
        self.assertEqual(sum(b["sequence"] == "ABBA" for b in measured), 10)
        self.assertEqual(len({b["seed"] for b in schedule}), 25)
        for block in schedule:
            sequence = block["sequence"]
            self.assertEqual({sequence[:2], sequence[2:]}, {"AB", "BA"})
            self.assertEqual(sequence.count("A"), 2)
            self.assertEqual(sequence.count("B"), 2)

    def test_same_binary_control_recovers_order_effect_without_fake_gain(self):
        schedule = make_schedule(3, 20, 23)
        rows = observations(schedule, lambda b, p: (100 if b["phase"] == "warmup" else 1 + b["block"] / 20) * (1.2 if p % 2 else 1))
        result = analyze_crossover(rows, schedule, bootstrap_samples=200)
        self.assertEqual(result["baseline"]["n"], 40)
        self.assertEqual(result["measured_blocks"], 20)
        self.assertAlmostEqual(result["completion_speedup"], 1)
        self.assertAlmostEqual(result["second_over_first"], 1.2)
        for value in result["speedup_seed_block_stratified_bootstrap_ci95"]:
            self.assertAlmostEqual(value, 1)
        self.assertLess(result["orders"]["AB"]["completion_speedup"], 1)
        self.assertGreater(result["orders"]["BA"]["completion_speedup"], 1)
        self.assertLess(result["baseline"]["mean_seconds"], 10)

    def test_known_gain_with_seed_variation_and_linear_drift(self):
        schedule = make_schedule(0, 20, 23)
        rows = observations(schedule, lambda b, p: (1 + b["block"] * .2 + p * .1) * (.8 if b["sequence"][p] == "B" else 1))
        result = analyze_crossover(rows, schedule, bootstrap_samples=200)
        self.assertAlmostEqual(result["completion_speedup"], 1.25)
        self.assertEqual(result["candidate_faster_blocks"], 20)
        self.assertAlmostEqual(result["candidate"]["aggregate_callbacks_per_second"] / result["baseline"]["aggregate_callbacks_per_second"], 1.25)
        for value in result["speedup_seed_block_stratified_bootstrap_ci95"]:
            self.assertAlmostEqual(value, 1.25)

    def test_analysis_rejects_incomplete_failed_or_mislabelled_data(self):
        schedule = make_schedule(0, 4, 23)
        original = observations(schedule, lambda b, p: 1)
        for kind in ("missing-trial", "missing-block", "duplicate", "failed", "seed", "order", "profile", "semantic", "nan", "zero"):
            with self.subTest(kind=kind):
                rows = copy.deepcopy(original)
                if kind == "missing-trial": rows.pop()
                elif kind == "missing-block": rows = rows[:-4]
                elif kind == "duplicate": rows[1] = rows[0]
                elif kind == "failed": rows[0]["status"] = "failed"
                elif kind == "seed": rows[0]["seed"] += 1
                elif kind == "order": rows[0]["order"] = "AA"
                elif kind == "profile": rows[0]["profile"] = "other"
                elif kind == "semantic": rows[0]["summaries"]["subscriber"]["callback_trace_sha256"] = "bad"
                elif kind == "nan": rows[0]["seconds"] = float("nan")
                elif kind == "zero": rows[0]["seconds"] = 0
                with self.assertRaises(ValueError):
                    analyze_crossover(rows, schedule, bootstrap_samples=20)

    def test_runner_keeps_four_trials_separate_and_stops_on_failure(self):
        schedule = make_schedule(0, 2, 23)
        for fail in (False, True):
            with self.subTest(fail=fail), TemporaryDirectory() as tmp:
                args = SimpleNamespace(output=Path(tmp), warmup=0, legacy_reference=False,
                                       baseline_mode="local-lrc", candidate_mode="local-lrc")
                calls = []

                def measure(condition, profile, index, seed, path, count):
                    calls.append((condition.output, index, profile))
                    if fail and len(calls) == 2:
                        raise RuntimeError("injected participant failure")
                    return trial(schedule[index], (len(calls) - 1) % 4, 1)

                with patch("crossover.materialize_plan", return_value=SimpleNamespace(record_count=2)), \
                        patch("crossover.write_plan", side_effect=lambda path, plan: path), \
                        patch("crossover.measure", side_effect=measure), patch("builtins.print"):
                    if fail:
                        with self.assertRaisesRegex(RuntimeError, "injected"):
                            run_crossover(args, None, schedule)
                    else:
                        run_crossover(args, None, schedule)
                saved = json.loads((Path(tmp) / "results.json").read_text())
                self.assertEqual(len(calls), 2 if fail else 8)
                self.assertEqual(len(set(calls)), len(calls))
                self.assertEqual(saved[-1]["status"], "failed" if fail else "accepted")
                if not fail:
                    result = analyze_crossover(saved, schedule, bootstrap_samples=20)
                    self.assertEqual(result["measured_blocks"], 2)


if __name__ == "__main__":
    unittest.main()
