#!/usr/bin/env python3
# normalize.py — canonicalization of subscriber event logs for the DLC
# conformance suite (gorti-only and parity modes).
#
# Per docs/DLC_COMPLIANCE_PROGRAM.md §5.2.1:
#   - Handle integers     → replace `handle=N` with `handle=<H>`
#   - Wall-clock          → strip; not in spec-mandated callback args anyway
#   - RO events within a logical-time bucket → sort within the bucket
#                                              (spec §6 only mandates causal
#                                              order; within-bucket is RTI-defined)
#   - TSO events          → strict; DO NOT sort (spec §8 mandates strict TSO)
#
# Used by both the gorti-only diff (canon(log) == load_golden) and the
# parity diff (canon(gorti_log) == canon(reference_rti_log)).
#
# Test PASS contract:
#   gorti-only: canonicalize(gorti_log) == load_golden(expected.*.log)
#   parity   : canonicalize(gorti_log) == canonicalize(reference_rti_log)
#
# Pin: docs/DLC_COMPLIANCE_PROGRAM.md §5.3 (reference_rti recorded local build).

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from typing import Iterable

# ---- Regex patterns --------------------------------------------------------

# `handle=12345` or `handle=12345)` — replace with `handle=<H>`.
# Same for any spec-shape *Handle int (object/attribute/parameter/dimension/...)
_HANDLE_INT = re.compile(r"\bhandle=([0-9]+)\b")
_OBJECT_HANDLE = re.compile(r"\bobject=([0-9]+)\b")
_CLASS_HANDLE = re.compile(r"\bclass=([0-9]+)\b")
_ATTR_HANDLE = re.compile(r"\battribute=([0-9]+)\b")
_PARAM_HANDLE = re.compile(r"\bparam=([0-9]+)\b")
_FEDERATE_HANDLE = re.compile(r"\bfederate=([0-9]+)\b")
_DIM_HANDLE = re.compile(r"\bdimension=([0-9]+)\b")
_REGION_HANDLE = re.compile(r"\bregion=([0-9]+)\b")
_RETRACTION_HANDLE = re.compile(r"\bretraction=([0-9]+)\b")

# M36 — RTI-assigned MOM object instance names embed the federate handle
# (`name=HLAfederate.3`, IEEE 1516 service-style `HLAfederate.<h>`; IEEE 1516.1-2010
# §6.2 requires instance-name uniqueness, so the RTI suffixes the handle).
# Same §5.2.1 policy as bare handle ints: normalize the numeric suffix.
_MOM_INSTANCE_NAME = re.compile(r"\bname=(HLAfederate|HLAfederation)\.([0-9]+)\b")

# `2026-06-30T12:34:56.789Z` — strip ISO-8601 wall-clock timestamps.
_WALL_CLOCK = re.compile(
    r"\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?\b"
)

# `t=12.345` or `time=12.345` — logical time (KEEP — spec-meaningful).
_LOGICAL_TIME = re.compile(r"\b(t|time)=([0-9.+-]+)\b")

# Line prefix tags for sort grouping.
_TSO_PREFIX = re.compile(r"^(SUB:|PUB:)\s+(REFLECT-TSO|RECEIVE-TSO|REMOVE-TSO)\b")
_RO_PREFIX = re.compile(r"^(SUB:|PUB:)\s+(REFLECT|RECEIVE|REMOVE)\b")


def normalize_handles(line: str) -> str:
    """Replace handle integers with placeholders.

    Per §5.2.1: handle-int normalization is the highest-frequency canonicalization;
    RTIs assign integer handles non-deterministically across runs.
    """
    for pat in (
        _HANDLE_INT,
        _OBJECT_HANDLE,
        _CLASS_HANDLE,
        _ATTR_HANDLE,
        _PARAM_HANDLE,
        _FEDERATE_HANDLE,
        _DIM_HANDLE,
        _REGION_HANDLE,
        _RETRACTION_HANDLE,
    ):
        line = pat.sub(lambda m: m.group(0).split("=")[0] + "=<H>", line)
    # MOM instance names: keep the class prefix, normalize the handle
    # suffix (`name=HLAfederate.3` → `name=HLAfederate.<H>`).
    line = _MOM_INSTANCE_NAME.sub(lambda m: "name=" + m.group(1) + ".<H>", line)
    return line


def strip_wallclock(line: str) -> str:
    """Strip ISO-8601 wall-clock timestamps."""
    return _WALL_CLOCK.sub("<TIMESTAMP>", line)


def extract_logical_time(line: str) -> str | None:
    """Return the logical-time bucket key for a line, or None if RO with no time."""
    m = _LOGICAL_TIME.search(line)
    if m:
        return m.group(2)
    return None


def is_tso_event(line: str) -> bool:
    return bool(_TSO_PREFIX.search(line))


def is_ro_event(line: str) -> bool:
    # RO is the catch-all for receive-style events without TSO tag.
    if is_tso_event(line):
        return False
    return bool(_RO_PREFIX.search(line))


def canonicalize(lines: Iterable[str]) -> list[str]:
    """Apply the full canonicalization pipeline.

    Pipeline per §5.2.1:
      1. Strip wall-clock timestamps.
      2. Replace handle integers with `<H>`.
      3. Group RO events by logical-time bucket; sort within bucket.
      4. Leave TSO events in strict spec-mandated order.
    """
    cleaned: list[str] = []
    for raw in lines:
        line = raw.rstrip("\n")
        line = strip_wallclock(line)
        line = normalize_handles(line)
        cleaned.append(line)

    # Bucket pass: walk linearly, accumulate RO events within the same logical
    # time, sort each bucket on flush.
    result: list[str] = []
    ro_bucket: list[str] = []
    ro_time: str | None = None

    def flush_bucket():
        if ro_bucket:
            result.extend(sorted(ro_bucket))
        ro_bucket.clear()

    for line in cleaned:
        if is_ro_event(line):
            t = extract_logical_time(line)
            # If we see a new logical-time bucket, flush and start a new one.
            if t != ro_time and ro_time is not None:
                flush_bucket()
            ro_time = t
            ro_bucket.append(line)
        else:
            # Non-RO event (TSO or other) — flush any pending bucket first.
            flush_bucket()
            ro_time = None
            result.append(line)
    flush_bucket()

    return result


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(
        description="Canonicalize a federate event log for diff against a golden "
        "or a sibling-RTI run. See docs/DLC_COMPLIANCE_PROGRAM.md §5.2.1.",
    )
    ap.add_argument("input", help="path to raw event-log file ('-' for stdin)")
    ap.add_argument(
        "-o",
        "--output",
        default="-",
        help="path to canonical output ('-' for stdout)",
    )
    args = ap.parse_args(argv)

    if args.input == "-":
        lines = sys.stdin.readlines()
    else:
        lines = Path(args.input).read_text(encoding="utf-8").splitlines(keepends=True)

    canonical = canonicalize(lines)
    out = "\n".join(canonical) + "\n"

    if args.output == "-":
        sys.stdout.write(out)
    else:
        Path(args.output).write_text(out, encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
