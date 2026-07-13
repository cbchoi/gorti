"""Analyze a complete fair-comparison manifest."""

from __future__ import annotations

import argparse
from collections.abc import Sequence
from pathlib import Path

from fair_comparison.analyzer import analyze_manifest, write_analysis
from fair_comparison.contract import ContractError


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--min-measured-pairs", type=int, default=20)
    parser.add_argument("--bootstrap-seed", type=int, default=1516)
    parser.add_argument("--resamples", type=int, default=10_000)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        report = analyze_manifest(
            args.manifest,
            min_measured_pairs=args.min_measured_pairs,
            bootstrap_seed=args.bootstrap_seed,
            resamples=args.resamples,
        )
        write_analysis(args.output.resolve(), report)
    except ContractError as error:
        raise SystemExit(f"comparison rejected: {error}") from error
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
