import copy
import json
from pathlib import Path
from tempfile import TemporaryDirectory
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from compare_portico import analyze_portico_crossover, run_portico_crossover
from crossover import make_schedule
from test_crossover import observations


class PorticoCrossoverTests(unittest.TestCase):
    def rows(self, schedule):
        rows = observations(schedule, lambda b, p: 2 if b["sequence"][p] == "A" else 1)
        for row in rows:
            row.pop("summaries")
            row["implementation"] = "portico" if row["profile"] == "baseline" else "gorti"
            row["result"] = {"evidence": {"seed": row["seed"], "delivered_callbacks": 100, "trace": "same"}}
        return rows

    def test_ratio_direction_and_normalized_cross_language_evidence(self):
        schedule = make_schedule(5, 20, 20261007)
        result = analyze_portico_crossover(self.rows(schedule), schedule)
        self.assertEqual(result["portico"]["n"], 40)
        self.assertEqual(result["portico"]["aggregate_callbacks_per_second"], 50)
        self.assertEqual(result["gorti"]["aggregate_callbacks_per_second"], 100)
        self.assertEqual(result["completion_speedup"], 2)
        self.assertEqual(result["gorti_over_portico_mean_completion_ratio"], .5)
        self.assertEqual(result["ratio_seed_block_stratified_bootstrap_ci95"], [.5, .5])

    def test_rejects_incomplete_identity_or_semantic_evidence(self):
        schedule = make_schedule(0, 2, 1516)
        original = self.rows(schedule)
        for kind in ("missing", "duplicate", "implementation", "semantic", "failed"):
            with self.subTest(kind=kind):
                rows = copy.deepcopy(original)
                if kind == "missing": rows.pop()
                elif kind == "duplicate": rows[1] = rows[0]
                elif kind == "implementation": rows[0]["implementation"] = "gorti" if rows[0]["implementation"] == "portico" else "portico"
                elif kind == "semantic": rows[0]["result"]["evidence"]["trace"] = "bad"
                elif kind == "failed": rows[0]["status"] = "failed"
                with self.assertRaises(ValueError):
                    analyze_portico_crossover(rows, schedule)

    def test_runner_separates_trials_and_retains_failure_without_retry(self):
        schedule = make_schedule(0, 2, 1516)
        for fail in (False, True):
            with self.subTest(fail=fail), TemporaryDirectory() as tmp:
                args = SimpleNamespace(output=Path(tmp), gorti_profile="async")
                calls = []

                def value(plan, seconds):
                    return {"batch_ns": int(seconds * 1e9), "evidence": {"seed": plan.seed, "delivered_callbacks": 4}}

                def record(path):
                    calls.append(path)
                    if fail and len(calls) == 2:
                        raise RuntimeError("injected trial failure")

                def portico(condition, path, plan):
                    record(path)
                    return value(plan, 2)

                def measure(condition, profile, index, seed, path, count):
                    record(condition.output / "gorti")
                    return {"server_arguments": [], "client_arguments": {}, "summaries": {
                        "subscriber": {"callback_report_transport": {}}, "publisher": {"local_lrc_transport": {}}}}

                with patch("compare_portico.callback.materialize_plan", return_value=SimpleNamespace(record_count=2, topology_identity=b"x" * 32)), \
                        patch("compare_portico.callback.write_plan", side_effect=lambda path, plan: path), \
                        patch("compare_portico.callback.digest", return_value="0" * 64), \
                        patch("compare_portico.portico_run", side_effect=portico), \
                        patch("compare_portico.callback.measure", side_effect=measure), \
                        patch("compare_portico.validate", side_effect=lambda path, implementation, plan: value(plan, 1)), \
                        patch("builtins.print"):
                    if fail:
                        with self.assertRaisesRegex(RuntimeError, "injected"):
                            run_portico_crossover(args, None, schedule)
                    else:
                        run_portico_crossover(args, None, schedule)
                rows = json.loads((Path(tmp) / "results.json").read_text())
                self.assertEqual(len(rows), 2 if fail else 8)
                self.assertEqual(len(calls), len(set(calls)))
                self.assertEqual(rows[-1]["status"], "failed" if fail else "accepted")
                if not fail:
                    self.assertEqual(analyze_portico_crossover(rows, schedule)["completion_speedup"], 2)


if __name__ == "__main__":
    unittest.main()
