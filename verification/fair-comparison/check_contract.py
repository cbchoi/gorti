"""Check fair-comparison configuration, result, and manifest contracts."""

from __future__ import annotations

import argparse
from collections.abc import Sequence
from pathlib import Path

from fair_comparison.contract import (
    ContractError,
    load_json,
    validate_config,
    validate_manifest,
    validate_result,
    validate_workload,
)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="kind", required=True)
    config = commands.add_parser("config")
    config.add_argument("path", type=Path)
    workload = commands.add_parser("workload")
    workload.add_argument("path", type=Path)
    result = commands.add_parser("result")
    result.add_argument("path", type=Path)
    result.add_argument("--expected-workload", type=Path)
    result.add_argument("--implementation", choices=("reference_rti", "go"))
    result.add_argument("--run-id")
    manifest = commands.add_parser("manifest")
    manifest.add_argument("path", type=Path)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        if args.kind == "config":
            validate_config(load_json(args.path))
        elif args.kind == "workload":
            validate_workload(load_json(args.path))
        elif args.kind == "result":
            workload = None
            if args.expected_workload:
                workload = validate_workload(load_json(args.expected_workload))
            validate_result(
                load_json(args.path),
                expected_workload=workload,
                expected_implementation=args.implementation,
                expected_run_id=args.run_id,
            )
        else:
            validate_manifest(load_json(args.path))
    except ContractError as error:
        raise SystemExit(f"contract rejected: {error}") from error
    print(f"valid {args.kind}: {args.path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
