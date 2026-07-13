#!/usr/bin/env python3
"""Fail when release-facing version metadata disagrees."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

import tomllib

ROOT = Path(__file__).resolve().parents[1]


def require_match(path: Path, pattern: str, label: str) -> str:
    text = path.read_text(encoding="utf-8")
    match = re.search(pattern, text, flags=re.MULTILINE)
    if match is None:
        raise ValueError(f"{label}: version not found in {path.relative_to(ROOT)}")
    return match.group(1)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tag", help="release tag, for example v0.9.0")
    args = parser.parse_args()

    pyproject = tomllib.loads((ROOT / "pysdk" / "pyproject.toml").read_text(encoding="utf-8"))
    expected = str(pyproject["project"]["version"])
    versions = {
        "pysdk/pyproject.toml": expected,
        "cppsdk/CMakeLists.txt": require_match(
            ROOT / "cppsdk" / "CMakeLists.txt",
            r"project\(gorti_cppsdk\s+VERSION\s+([^\s\)]+)",
            "C++ SDK",
        ),
        "CITATION.cff": require_match(
            ROOT / "CITATION.cff", r"^version:\s*[\"']?([^\s\"']+)", "citation"
        ),
        "codemeta.json": str(
            json.loads((ROOT / "codemeta.json").read_text(encoding="utf-8"))["version"]
        ),
    }

    if args.tag:
        versions["release tag"] = args.tag.removeprefix("v")

    mismatches = {name: value for name, value in versions.items() if value != expected}
    if mismatches:
        for name, value in mismatches.items():
            print(f"version mismatch: {name}={value!r}, expected {expected!r}", file=sys.stderr)
        return 1

    print(f"release metadata: all versions match {expected}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
