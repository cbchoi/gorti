"""Command-line entry point for the deterministic DEVStone-HLA workload."""

from __future__ import annotations

import argparse
from pathlib import Path

try:
    from .model import (
        DEFAULT_DEPTH,
        DEFAULT_EXTERNAL_EVENTS,
        DEFAULT_SEED,
        DEFAULT_TOPOLOGY,
        DEFAULT_WIDTH,
        WorkloadConfig,
        generate_workload,
        validate_identity,
        write_workload,
    )
except ImportError:  # Direct execution: python path/to/generate.py
    from model import (  # type: ignore[no-redef]
        DEFAULT_DEPTH,
        DEFAULT_EXTERNAL_EVENTS,
        DEFAULT_SEED,
        DEFAULT_TOPOLOGY,
        DEFAULT_WIDTH,
        WorkloadConfig,
        generate_workload,
        validate_identity,
        write_workload,
    )


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Generate a deterministic LI or HI DEVStone-derived HLA workload."
        )
    )
    parser.add_argument("--topology", choices=("LI", "HI"), default=DEFAULT_TOPOLOGY)
    parser.add_argument("--width", type=int, default=DEFAULT_WIDTH)
    parser.add_argument("--depth", type=int, default=DEFAULT_DEPTH)
    parser.add_argument(
        "--external-events", type=int, default=DEFAULT_EXTERNAL_EVENTS
    )
    parser.add_argument("--seed", type=int, default=DEFAULT_SEED)
    parser.add_argument(
        "--output",
        type=Path,
        default=Path(__file__).with_name("workload.json"),
        help="destination JSON path (default: workload.json beside this script)",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        config = WorkloadConfig(
            topology=args.topology,
            width=args.width,
            depth=args.depth,
            external_event_count=args.external_events,
            seed=args.seed,
        )
    except (TypeError, ValueError) as error:
        _parser().error(str(error))

    workload = generate_workload(config)
    if not validate_identity(workload):
        raise RuntimeError("generated workload identity did not validate")
    destination = write_workload(args.output, workload)
    counts = workload["expected_counts"]["total"]
    print(
        f"wrote {destination} "
        f"({counts['hla_interactions']} interactions, "
        f"topology_sha256={workload['topology_identity']['digest']}, "
        f"document_sha256={workload['identity']['digest']})"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
