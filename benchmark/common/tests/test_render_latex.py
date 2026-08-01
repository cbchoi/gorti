from __future__ import annotations

# ruff: noqa: E402
import copy
import json
import sys
import tempfile
import unittest
from pathlib import Path

COMMON = Path(__file__).resolve().parents[1]
if str(COMMON) not in sys.path:
    sys.path.insert(0, str(COMMON))

from analyze import analyze_document
from analyze import main as analyze_main
from fixture_factory import make_result_document
from render_latex import AnalysisFormatError, render_latex
from render_latex import main as render_main


class LatexRendererTests(unittest.TestCase):
    def setUp(self) -> None:
        self.analysis = analyze_document(
            make_result_document(), bootstrap_resamples=100, bootstrap_seed=1516
        )

    def test_renders_summary_and_paired_tables(self) -> None:
        latex = render_latex(self.analysis)
        self.assertIn(r"\label{tab:devstone-hla-implementation-summary}", latex)
        self.assertIn(r"\label{tab:devstone-hla-paired-comparison}", latex)
        self.assertIn("Portico RTI", latex)
        self.assertIn("candidate minus baseline", latex)
        self.assertIn("100 resamples", latex)
        self.assertIn(r"Median [95\% CI]", latex)
        self.assertNotIn(r"95\\%", latex)

    def test_escapes_latex_metacharacters(self) -> None:
        analysis = copy.deepcopy(self.analysis)
        analysis["metrics"][0]["label"] = "Latency_95% & completed"
        latex = render_latex(analysis)
        self.assertIn(r"Latency\_95\% \& completed", latex)

    def test_rejects_malformed_statistic(self) -> None:
        analysis = copy.deepcopy(self.analysis)
        del analysis["metrics"][0]["summaries"][0]["median"]["ci95"]
        with self.assertRaises(AnalysisFormatError):
            render_latex(analysis)

    def test_file_pipeline_uses_json_inputs_and_outputs(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "results.json"
            analysis_path = root / "analysis.json"
            latex_path = root / "comparison.tex"
            source.write_text(json.dumps(make_result_document()), encoding="utf-8")

            self.assertEqual(
                analyze_main(
                    [
                        str(source),
                        "--output",
                        str(analysis_path),
                        "--bootstrap-resamples",
                        "100",
                    ]
                ),
                0,
            )
            self.assertEqual(
                render_main([str(analysis_path), "--output", str(latex_path)]),
                0,
            )
            parsed = json.loads(analysis_path.read_text(encoding="utf-8"))
            self.assertEqual(parsed["design_checks"]["paired_runs"], 30)
            self.assertIn(r"\begin{table*}", latex_path.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
