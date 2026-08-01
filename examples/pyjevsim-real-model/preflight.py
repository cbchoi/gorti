"""Runtime prerequisite checks shared by both platform launchers."""

from __future__ import annotations

import importlib
import sys
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
REPO_ROOT = HERE.parents[1]
PYSDK = REPO_ROOT / "pysdk"

REQUIRED_MODULES = (
    ("grpc", "grpcio"),
    ("google.protobuf", "protobuf"),
    ("pyjevsim.behavior_model", "pyjevsim"),
)
GENERATED_MODULES = (
    "rti.v1.declaration_pb2",
    "rti.v1.declaration_pb2_grpc",
    "rti.v1.federation_pb2",
    "rti.v1.federation_pb2_grpc",
    "rti.v1.object_pb2",
    "rti.v1.object_pb2_grpc",
    "rti.v1.stream_pb2",
    "rti.v1.stream_pb2_grpc",
    "rti.v1.time_pb2",
    "rti.v1.time_pb2_grpc",
)


def runtime_errors(
    *,
    version_info: Sequence[int] | None = None,
    importer: Callable[[str], Any] = importlib.import_module,
) -> list[str]:
    """Return every missing or unusable runtime prerequisite."""
    errors: list[str] = []
    version = tuple(version_info or sys.version_info[:3])
    if version < (3, 11):
        errors.append(
            f"Python 3.11 or newer is required; selected {version[0]}.{version[1]}"
        )

    if str(PYSDK) not in sys.path:
        sys.path.insert(0, str(PYSDK))

    for module_name, package_name in REQUIRED_MODULES:
        try:
            importer(module_name)
        except Exception as exc:  # noqa: BLE001 - report broken installations too
            errors.append(f"{package_name} is unavailable: {exc}")

    try:
        transport = importer("rti1516e._transport")
        transport._ensure_generated_path()
    except Exception as exc:  # noqa: BLE001 - preflight must collapse import failures
        errors.append(f"the local rti1516e SDK is unavailable: {exc}")
    else:
        for module_name in GENERATED_MODULES:
            try:
                importer(module_name)
            except Exception as exc:  # noqa: BLE001
                errors.append(f"generated binding {module_name} is unavailable: {exc}")

    return errors


def main() -> int:
    errors = runtime_errors()
    if not errors:
        return 0

    print("pyjevsim-real-model: runtime preflight failed:", file=sys.stderr)
    for error in errors:
        print(f"  - {error}", file=sys.stderr)
    print(
        "Install dependencies from the repository root with:\n"
        "  python -m pip install -e './pysdk[pyjevsim]'\n"
        "Generate bindings with:\n"
        "  python -m rti1516e._proto",
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
