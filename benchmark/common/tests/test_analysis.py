from __future__ import annotations

# ruff: noqa: E402
import copy
import sys
import unittest
from pathlib import Path

COMMON = Path(__file__).resolve().parents[1]
if str(COMMON) not in sys.path:
    sys.path.insert(0, str(COMMON))

from analyze import analyze_document, quantile
from fixture_factory import make_result_document


class AnalysisTests(unittest.TestCase):
    def test_linear_r7_quantile(self) -> None:
        values = list(range(1, 31))
        self.assertEqual(quantile(values, 0.5), 15.5)
        self.assertAlmostEqual(quantile(values, 0.95), 28.55)
        self.assertAlmostEqual(quantile(values, 0.99), 29.71)

    def test_reports_expected_percentiles_and_paired_difference(self) -> None:
        analysis = analyze_document(
            make_result_document(), bootstrap_resamples=200, bootstrap_seed=1516
        )
        latency = analysis["metrics"][0]
        portico = latency["summaries"][0]
        gorti = latency["summaries"][1]

        self.assertEqual(portico["n"], 30)
        self.assertEqual(gorti["n"], 30)
        self.assertEqual(portico["median"]["estimate"], 115.5)
        self.assertAlmostEqual(portico["p95"]["estimate"], 128.55)
        self.assertAlmostEqual(portico["p99"]["estimate"], 129.71)
        self.assertEqual(gorti["median"]["estimate"], 95.5)
        self.assertEqual(
            latency["comparison"]["median_difference"]["estimate"], -20.0
        )
        self.assertEqual(
            latency["comparison"]["order_adjusted_median_difference"]["estimate"],
            -20.0,
        )

    def test_bootstrap_is_deterministic(self) -> None:
        document = make_result_document()
        first = analyze_document(
            document, bootstrap_resamples=250, bootstrap_seed=987654
        )
        second = analyze_document(
            copy.deepcopy(document), bootstrap_resamples=250, bootstrap_seed=987654
        )
        self.assertEqual(first, second)

    def test_pair_bootstrap_is_stratified_by_balanced_order(self) -> None:
        analysis = analyze_document(
            make_result_document(), bootstrap_resamples=100, bootstrap_seed=7
        )
        comparison = analysis["metrics"][0]["comparison"]
        self.assertEqual(comparison["n_pairs"], 30)
        self.assertEqual(
            [stratum["n_pairs"] for stratum in comparison["order_strata"]],
            [15, 15],
        )
        self.assertEqual(
            analysis["bootstrap"]["paired_resampling"],
            "stratified-by-execution-order",
        )

    def test_ratio_is_omitted_when_baseline_contains_zero(self) -> None:
        document = make_result_document()
        baseline_run = next(
            run
            for run in document["runs"]
            if run["pair_index"] == 1 and run["implementation"] == "portico"
        )
        baseline_run["measurements"]["latency_us"] = 0.0
        analysis = analyze_document(
            document, bootstrap_resamples=100, bootstrap_seed=1516
        )
        comparison = analysis["metrics"][0]["comparison"]
        self.assertIsNone(comparison["median_ratio"])
        self.assertIsNone(comparison["order_adjusted_median_ratio"])

    def test_rejects_too_few_bootstrap_resamples(self) -> None:
        with self.assertRaisesRegex(ValueError, "at least 100"):
            analyze_document(make_result_document(), bootstrap_resamples=99)


if __name__ == "__main__":
    unittest.main()
