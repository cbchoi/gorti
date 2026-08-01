"""Render validated DEVStone-HLA analysis JSON as publication-ready LaTeX."""

from __future__ import annotations

import argparse
import json
import math
from pathlib import Path
from typing import Any

from benchmark_core import ANALYSIS_SCHEMA_VERSION


class AnalysisFormatError(ValueError):
    """Raised when analysis JSON is not suitable for table rendering."""


def _latex_escape(value: Any) -> str:
    replacements = {
        "&": r"\&",
        "%": r"\%",
        "$": r"\$",
        "#": r"\#",
        "_": r"\_",
        "{": r"\{",
        "}": r"\}",
        "~": r"\textasciitilde{}",
        "^": r"\textasciicircum{}",
        "\\": r"\textbackslash{}",
    }
    return "".join(replacements.get(character, character) for character in str(value))


def _number(value: Any) -> str:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise AnalysisFormatError(f"expected a numeric statistic, got {value!r}")
    numeric = float(value)
    if not math.isfinite(numeric):
        raise AnalysisFormatError("statistics must be finite")
    return format(numeric, ".6g")


def _statistic(value: Any) -> str:
    if not isinstance(value, dict) or set(value) != {"estimate", "ci95"}:
        raise AnalysisFormatError("expected a statistic with estimate and ci95")
    ci = value["ci95"]
    if not isinstance(ci, dict) or set(ci) != {"low", "high"}:
        raise AnalysisFormatError("expected ci95.low and ci95.high")
    return (
        f"{_number(value['estimate'])} "
        f"[{_number(ci['low'])}, {_number(ci['high'])}]"
    )


def _nullable_statistic(value: Any) -> str:
    return "--" if value is None else _statistic(value)


def _require_analysis(document: Any) -> dict[str, Any]:
    if not isinstance(document, dict):
        raise AnalysisFormatError("analysis root must be an object")
    if document.get("analysis_schema_version") != ANALYSIS_SCHEMA_VERSION:
        raise AnalysisFormatError(
            f"analysis_schema_version must be {ANALYSIS_SCHEMA_VERSION!r}"
        )
    implementations = document.get("implementations")
    if not isinstance(implementations, list) or len(implementations) != 2:
        raise AnalysisFormatError("analysis must contain exactly two implementations")
    if not isinstance(document.get("metrics"), list) or not document["metrics"]:
        raise AnalysisFormatError("analysis must contain at least one metric")
    return document


def render_latex(document: dict[str, Any]) -> str:
    """Render two booktabs tables from a v1 analysis document."""

    analysis = _require_analysis(document)
    labels = {
        implementation["id"]: implementation["label"]
        for implementation in analysis["implementations"]
    }
    summary_rows: list[str] = []
    comparison_rows: list[str] = []

    for metric in analysis["metrics"]:
        try:
            metric_name = _latex_escape(metric["label"])
            unit = _latex_escape(metric["unit"])
            summaries = metric["summaries"]
            comparison = metric["comparison"]
        except (KeyError, TypeError) as exc:
            raise AnalysisFormatError(f"malformed metric entry: {exc}") from exc

        if not isinstance(summaries, list) or len(summaries) != 2:
            raise AnalysisFormatError("each metric must contain two implementation summaries")
        for summary in summaries:
            implementation_id = summary.get("implementation")
            if implementation_id not in labels:
                raise AnalysisFormatError(
                    f"unknown implementation in summary: {implementation_id!r}"
                )
            summary_rows.append(
                " & ".join(
                    (
                        metric_name,
                        unit,
                        _latex_escape(labels[implementation_id]),
                        _statistic(summary.get("median")),
                        _statistic(summary.get("p95")),
                        _statistic(summary.get("p99")),
                    )
                )
                + r" \\"
            )

        baseline = comparison.get("baseline")
        candidate = comparison.get("candidate")
        if baseline not in labels or candidate not in labels:
            raise AnalysisFormatError("comparison references an unknown implementation")
        comparison_name = _latex_escape(f"{labels[candidate]} / {labels[baseline]}")
        comparison_rows.append(
            " & ".join(
                (
                    metric_name,
                    comparison_name,
                    _statistic(comparison.get("median_difference")),
                    _statistic(comparison.get("order_adjusted_median_difference")),
                    _nullable_statistic(comparison.get("median_ratio")),
                    _nullable_statistic(comparison.get("order_adjusted_median_ratio")),
                )
            )
            + r" \\"
        )

    resamples = analysis.get("bootstrap", {}).get("resamples")
    if not isinstance(resamples, int):
        raise AnalysisFormatError("bootstrap.resamples must be an integer")
    benchmark = analysis.get("benchmark", {}).get("name", "DEVStone-HLA")

    lines = [
        "% Generated from structured benchmark analysis JSON; do not edit by hand.",
        r"\begin{table*}[tb]",
        r"\centering",
        r"\scriptsize",
        (
            r"\caption{"
            + _latex_escape(benchmark)
            + " per-run statistics. Values in brackets are deterministic bootstrap "
            + r"95\% confidence intervals based on "
            + str(resamples)
            + r" resamples.}"
        ),
        r"\label{tab:devstone-hla-implementation-summary}",
        r"\begin{tabular}{llllll}",
        r"\toprule",
        r"Metric & Unit & Implementation & Median [95\% CI] & P95 [95\% CI] & P99 [95\% CI] \\",
        r"\midrule",
        *summary_rows,
        r"\bottomrule",
        r"\end{tabular}",
        r"\end{table*}",
        "",
        r"\begin{table*}[tb]",
        r"\centering",
        r"\scriptsize",
        (
            r"\caption{Paired DEVStone-HLA comparison. Differences are candidate "
            r"minus baseline; ratios are candidate divided by baseline. "
            r"Order-adjusted estimates give equal weight to the two "
            r"execution-order strata.}"
        ),
        r"\label{tab:devstone-hla-paired-comparison}",
        r"\begin{tabular}{llllll}",
        r"\toprule",
        (
            r"Metric & Comparison & Paired median difference [95\% CI] & "
            r"Order-adjusted difference [95\% CI] & Paired median ratio [95\% CI] & "
            r"Order-adjusted ratio [95\% CI] \\"
        ),
        r"\midrule",
        *comparison_rows,
        r"\bottomrule",
        r"\end{tabular}",
        r"\end{table*}",
        "",
    ]
    return "\n".join(lines)


def load_analysis(path: str | Path) -> dict[str, Any]:
    analysis_path = Path(path)
    if not analysis_path.is_file():
        raise AnalysisFormatError(f"{analysis_path}: expected an analysis JSON file")
    try:
        document = json.loads(analysis_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise AnalysisFormatError(f"{analysis_path}: cannot read JSON: {exc}") from exc
    return _require_analysis(document)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Render DEVStone-HLA analysis JSON as LaTeX booktabs tables."
    )
    parser.add_argument("input", help="path to analysis-schema-v1 JSON")
    parser.add_argument("--output", required=True, help="LaTeX output path")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = _parser()
    args = parser.parse_args(argv)
    try:
        latex = render_latex(load_analysis(args.input))
        output = Path(args.output)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(latex, encoding="utf-8")
    except (OSError, AnalysisFormatError) as exc:
        parser.error(str(exc))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
