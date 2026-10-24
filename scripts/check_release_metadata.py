#!/usr/bin/env python3
"""Fail when release-facing version metadata disagrees."""

from __future__ import annotations

import argparse
import json
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

import tomllib

ROOT = Path(__file__).resolve().parents[1]
MAVEN = {"m": "http://maven.apache.org/POM/4.0.0"}


def require_match(path: Path, pattern: str, label: str) -> str:
    text = path.read_text(encoding="utf-8")
    match = re.search(pattern, text, flags=re.MULTILINE)
    if match is None:
        raise ValueError(f"{label}: version not found in {path}")
    return match.group(1)


def check_versions(root: Path, tag: str | None = None) -> list[str]:
    pyproject = tomllib.loads((root / "pysdk" / "pyproject.toml").read_text(encoding="utf-8"))
    expected = str(pyproject["project"]["version"])
    versions = {
        "pysdk/pyproject.toml": expected,
        "CITATION.cff": require_match(
            root / "CITATION.cff", r"^version:\s*[\"']?([^\s\"']+)", "citation"
        ),
        "codemeta.json": str(
            json.loads((root / "codemeta.json").read_text(encoding="utf-8"))["version"]
        ),
    }

    if tag:
        versions["release tag"] = tag.removeprefix("v")

    mismatches = {name: value for name, value in versions.items() if value != expected}
    errors = [
        f"version mismatch: {name}={value!r}, expected {expected!r}"
        for name, value in mismatches.items()
    ]

    # Java has its own artifact version; every reactor module must inherit it.
    parent = ET.parse(root / "javasdk/pom.xml").getroot()  # noqa: S314 - repository POM
    java_version = parent.findtext("m:version", namespaces=MAVEN)
    if not java_version:
        errors.append("javasdk/pom.xml: missing version")
    modules = parent.findall("m:modules/m:module", MAVEN)
    if not modules:
        errors.append("javasdk/pom.xml: missing modules")
    for module in modules:
        path = root / "javasdk" / str(module.text) / "pom.xml"
        child = ET.parse(path).getroot()  # noqa: S314 - repository POM
        inherited = child.findtext("m:parent/m:version", namespaces=MAVEN)
        explicit = child.findtext("m:version", namespaces=MAVEN)
        if inherited != java_version or (explicit is not None and explicit != java_version):
            errors.append(
                f"{path.relative_to(root)}: expected Java reactor version {java_version!r}, "
                f"got parent={inherited!r}, module={explicit!r}"
            )
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tag", help="release tag, for example 1.0.0 or v1.0.0")
    args = parser.parse_args()
    try:
        errors = check_versions(ROOT, args.tag)
    except (OSError, ValueError, KeyError, ET.ParseError) as exc:
        errors = [str(exc)]
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1

    print("release metadata: product versions and Java reactor versions are consistent")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
