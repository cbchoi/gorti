"""Validate a structured DEVStone-HLA result without parsing process logs."""

from __future__ import annotations

import argparse
import sys

from benchmark_core import ResultValidationError, load_result_document


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Validate a DEVStone-HLA benchmark result JSON file."
    )
    parser.add_argument("input", help="path to result-schema-v1 JSON")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        load_result_document(args.input)
    except ResultValidationError as exc:
        for error in exc.errors:
            print(f"validation error: {error}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
