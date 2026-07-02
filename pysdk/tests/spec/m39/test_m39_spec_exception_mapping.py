"""M39 HA-3 — metadata-first IEEE Annex C exception mapping.

CONTRACT (shared with server agent HB): the rtid attaches trailing
gRPC metadata key ``rti-spec-exception`` whose value is the IEEE
1516.1-2010 exception class name in UpperCamelCase exactly as in
Annex C (e.g. ``ObjectClassNotPublished``, ``AttributeNotOwned``,
``InvalidLogicalTime``, ``FederationExecutionAlreadyExists``).

Contract asserted here:

  1. ``translate_rpc_error`` reads that key FIRST and raises the pysdk
     exception class of the same name (regardless of gRPC code).
  2. The name -> class table is built programmatically from pysdk's
     exception modules — every RtiError subclass is resolvable by its
     ``__name__``; the Annex-C names subclass the legacy gorti names so
     existing ``except`` clauses keep catching.
  3. Fallback: with no metadata (old servers) the pre-M39 code/detail
     heuristics still apply, now extended with the federation/object
     sentinel texts (``_detail_class_for``).

The fake error object below mirrors the ``grpc.aio.AioRpcError``
surface pysdk duck-types against (code() / details() /
trailing_metadata()) — the FakeRtiServer pattern at the unit boundary
where the translator actually runs.
"""

from __future__ import annotations

from typing import Any

import pytest

from rti1516e import _grpc_errors as grpc_errors_module
from rti1516e import errors as errors_module
from rti1516e._grpc_errors import (
    InvalidSyncState,
    SyncPointAlreadyExists,
    spec_exception_table,
    translate_rpc_error,
)
from rti1516e.errors import (
    AttributeNotOwned,
    FederatesCurrentlyJoined,
    FederationAlreadyExists,
    FederationExecutionAlreadyExists,
    FederationExecutionDoesNotExist,
    FederationHasFederatesJoined,
    InvalidLogicalTime,
    ObjectAttributeNotOwned,
    ObjectClassNotPublished,
    RtiError,
)


class _Code:
    def __init__(self, name: str) -> None:
        self.name = name


class FakeAioRpcError(Exception):
    """Duck-typed grpc.aio.AioRpcError double (FakeRtiServer pattern)."""

    def __init__(
        self,
        code: str = "UNKNOWN",
        details: str = "",
        trailing: list[tuple[str, Any]] | None = None,
    ) -> None:
        super().__init__(details or code)
        self._code = _Code(code)
        self._details = details
        self._trailing = trailing

    def code(self) -> _Code:
        return self._code

    def details(self) -> str:
        return self._details

    def trailing_metadata(self) -> list[tuple[str, Any]] | None:
        return self._trailing


# --- 1: metadata wins ---------------------------------------------------------


@pytest.mark.parametrize(
    ("spec_name", "expected"),
    [
        ("ObjectClassNotPublished", ObjectClassNotPublished),
        ("AttributeNotOwned", AttributeNotOwned),
        ("InvalidLogicalTime", InvalidLogicalTime),
        ("FederationExecutionAlreadyExists", FederationExecutionAlreadyExists),
        ("FederatesCurrentlyJoined", FederatesCurrentlyJoined),
    ],
)
def test_metadata_names_map_to_matching_class(
    spec_name: str, expected: type[RtiError]
) -> None:
    """The CONTRACT examples: metadata value -> same-named pysdk class."""
    exc = FakeAioRpcError(
        code="FAILED_PRECONDITION",
        details="whatever the server said",
        trailing=[("rti-spec-exception", spec_name)],
    )
    with pytest.raises(expected) as exc_info:
        translate_rpc_error(exc)
    assert type(exc_info.value) is expected  # exact class, not a parent
    assert exc_info.value.__cause__ is exc


def test_metadata_beats_code_heuristics() -> None:
    """Metadata is read FIRST: a code that heuristically maps elsewhere
    (ALREADY_EXISTS -> SyncPointAlreadyExists) is overridden by the
    explicit spec name."""
    exc = FakeAioRpcError(
        code="ALREADY_EXISTS",
        details="federation already exists",
        trailing=[("rti-spec-exception", "FederationExecutionAlreadyExists")],
    )
    with pytest.raises(FederationExecutionAlreadyExists):
        translate_rpc_error(exc)


def test_metadata_value_accepts_bytes() -> None:
    """gRPC metadata values may arrive as bytes; decoded before lookup."""
    exc = FakeAioRpcError(
        code="UNKNOWN",
        trailing=[("rti-spec-exception", b"AttributeNotOwned")],
    )
    with pytest.raises(AttributeNotOwned):
        translate_rpc_error(exc)


def test_unknown_metadata_name_falls_back_to_heuristics() -> None:
    """A spec name this pysdk predates falls through to the code/detail
    heuristics instead of failing the translation."""
    exc = FakeAioRpcError(
        code="ALREADY_EXISTS",
        details="synchronization point already registered in federation",
        trailing=[("rti-spec-exception", "SomeFutureAnnexCException")],
    )
    with pytest.raises(SyncPointAlreadyExists):
        translate_rpc_error(exc)


# --- 2: programmatic table -----------------------------------------------------


def test_table_covers_every_rtierror_subclass_by_name() -> None:
    """The table is built from the exception modules — every RtiError
    subclass in rti1516e.errors / rti1516e._grpc_errors resolves by its
    __name__ (no hand-maintained list to drift)."""
    table = spec_exception_table()
    for module in (errors_module, grpc_errors_module):
        for obj in vars(module).values():
            if isinstance(obj, type) and issubclass(obj, RtiError):
                assert table.get(obj.__name__) is not None, obj.__name__
    # Spot-check CONTRACT names resolve to classes named identically.
    for name in (
        "ObjectClassNotPublished",
        "AttributeNotOwned",
        "InvalidLogicalTime",
        "FederationExecutionAlreadyExists",
        "FederatesCurrentlyJoined",
        "FederateNotExecutionMember",
        "LogicalTimeAlreadyPassed",
        "InvalidLookahead",
    ):
        assert table[name].__name__ == name


def test_table_covers_every_server_sent_name() -> None:
    """Drift guard against agent HB's server half: every Annex C name the
    rtid's specExceptionTable can attach (quoted strings in
    rti/internal/transport/grpc/spec_exception.go) must resolve to a
    pysdk class of the same name. Skipped when the Go tree is absent
    (pysdk packaged standalone)."""
    import re
    from pathlib import Path

    server_table = (
        Path(__file__).resolve().parents[4]
        / "rti" / "internal" / "transport" / "grpc" / "spec_exception.go"
    )
    if not server_table.is_file():
        pytest.skip("Go server tree not present; cross-check n/a")
    source = server_table.read_text(encoding="utf-8")
    block = source[source.index("specExceptionTable = []") :]
    block = block[: block.index("\n}\n")]
    names = set(re.findall(r'"([A-Z][A-Za-z]+)"', block))
    assert names, "failed to parse any names from specExceptionTable"
    table = spec_exception_table()
    missing = sorted(n for n in names if n not in table)
    assert not missing, (
        f"server can send rti-spec-exception values with no pysdk class: {missing}"
    )


def test_annex_c_names_subclass_legacy_gorti_names() -> None:
    """except-clauses against the pre-M39 gorti names keep catching."""
    assert issubclass(FederationExecutionAlreadyExists, FederationAlreadyExists)
    assert issubclass(FederatesCurrentlyJoined, FederationHasFederatesJoined)
    assert issubclass(AttributeNotOwned, ObjectAttributeNotOwned)
    assert issubclass(
        grpc_errors_module.InTimeAdvancingState, grpc_errors_module.TimeAdvancingState
    )


# --- 3: no-metadata fallbacks --------------------------------------------------


@pytest.mark.parametrize(
    ("code", "detail", "expected"),
    [
        # M39 federation/object sentinel-text heuristics (old servers).
        ("ALREADY_EXISTS", "federation already exists", FederationExecutionAlreadyExists),
        (
            "FAILED_PRECONDITION",
            "federation has federates joined",
            FederatesCurrentlyJoined,
        ),
        ("NOT_FOUND", "federation not found", FederationExecutionDoesNotExist),
        (
            "FAILED_PRECONDITION",
            "object class not published by federate",
            ObjectClassNotPublished,
        ),
        ("PERMISSION_DENIED", "attribute not owned by federate", AttributeNotOwned),
        (
            "INVALID_ARGUMENT",
            "invalid logical time: TSO timestamp precedes current time plus lookahead",
            InvalidLogicalTime,
        ),
        # Pre-M39 heuristics unchanged.
        (
            "ALREADY_EXISTS",
            "synchronization point already registered in federation",
            SyncPointAlreadyExists,
        ),
        ("FAILED_PRECONDITION", "some unmapped precondition", InvalidSyncState),
    ],
)
def test_no_metadata_falls_back_to_detail_and_code(
    code: str, detail: str, expected: type[RtiError]
) -> None:
    exc = FakeAioRpcError(code=code, details=detail, trailing=None)
    with pytest.raises(expected):
        translate_rpc_error(exc)


def test_non_grpc_exception_passes_through_unchanged() -> None:
    """Objects without the gRPC surface propagate as-is (pre-M39 contract)."""
    plain = ValueError("not a gRPC error")
    with pytest.raises(ValueError, match="not a gRPC error"):
        translate_rpc_error(plain)
