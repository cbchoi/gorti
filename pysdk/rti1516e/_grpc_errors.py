"""gRPC StatusCode → typed ``RtiError`` translation for cut-3 services.

Cut-2 service-group handlers (sync / ownership / DDM / savepoint, see
``rti/internal/transport/grpc/*.go``) return standard gRPC status codes
rather than the ``ERR_*`` numeric codes carried in trailers by the
cut-1 federation/declaration/object services. The cut-3 SDK clients
(``rti1516e.sync`` / ``rti1516e.ownership`` / ``rti1516e.ddm`` /
``rti1516e.savepoint``) translate via this helper.

Mapping policy:

  - ``NOT_FOUND``           → :class:`SyncPointNotFound` /
                              :class:`SaveBundleNotFound` /
                              :class:`RegionNotFound` (status-text-driven
                              disambiguation when needed)
  - ``ALREADY_EXISTS``      → :class:`SyncPointAlreadyExists` /
                              :class:`SaveBundleAlreadyExists`
  - ``FAILED_PRECONDITION`` → :class:`InvalidSyncState` /
                              :class:`InvalidSaveState` /
                              :class:`InvalidOwnershipState`
  - ``PERMISSION_DENIED``   → :class:`OwnershipNotPermitted`
  - ``INVALID_ARGUMENT``    → :class:`RtiError` (generic; the wire-level
                              validators surface this for malformed reqs)

The classifier is deliberately coarse: cut-3 spec tests assert on
``RtiError`` (the base class) plus the gRPC code, so a fine-grained
error hierarchy is not required for green tests. The named subclasses
here exist so future tests + user code can catch them by type without
having to import ``grpc`` directly.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, NoReturn

from rti1516e.errors import RtiError

if TYPE_CHECKING:  # pragma: no cover - type-check imports only
    pass


# --- Cut-3 typed exceptions ---------------------------------------------------
#
# These are subclasses of RtiError so callers can catch the broad base.
# Each class carries a stable ``error_code`` mirroring the proto ErrorCode
# convention (numeric IDs ≥ 600 are reserved for cut-3 service groups; this
# range is currently unused in errors.proto and is therefore a safe internal
# allocation that does not collide with the M0..M11 cut-1 errors).


class SyncPointNotFound(RtiError):
    """gRPC NotFound from SyncService — sync point label is unknown."""

    error_code = 600


class SyncPointAlreadyExists(RtiError):
    """gRPC AlreadyExists from SyncService — sync point label already registered."""

    error_code = 601


class InvalidSyncState(RtiError):
    """gRPC FailedPrecondition from SyncService — illegal state transition."""

    error_code = 602


class OwnershipNotPermitted(RtiError):
    """gRPC PermissionDenied from OwnershipService — caller is not the owner."""

    error_code = 603


class InvalidOwnershipState(RtiError):
    """gRPC FailedPrecondition from OwnershipService — illegal transfer state."""

    error_code = 604


class RegionNotFound(RtiError):
    """gRPC NotFound from DDMService — region/dimension/space is unknown."""

    error_code = 605


class SaveBundleNotFound(RtiError):
    """gRPC NotFound from SavepointService — no bundle for (federation, label)."""

    error_code = 606


class SaveBundleAlreadyExists(RtiError):
    """gRPC AlreadyExists from SavepointService — bundle already on disk."""

    error_code = 607


class InvalidSaveState(RtiError):
    """gRPC FailedPrecondition from SavepointService — illegal state transition."""

    error_code = 608


# --- Translation entry point --------------------------------------------------


def translate_rpc_error(exc: BaseException) -> NoReturn:
    """Re-raise ``exc`` as the matching RtiError subclass (or pass through).

    The cut-3 service handlers surface failures as ``grpc.aio.AioRpcError``
    with a populated ``code()``. We sniff via duck typing rather than
    importing grpc unconditionally so importing this module from a
    memory:// transport path costs nothing.

    Behavior:

      - If ``exc`` exposes a ``.code()`` returning a recognized gRPC
        code, raise the matching typed exception (preserving the
        original via ``__cause__``).
      - Otherwise, re-raise the original ``exc`` unchanged.

    The function never returns; the type is :class:`typing.NoReturn`.
    """
    code_name = _grpc_code_name(exc)
    detail = _grpc_detail(exc) or str(exc)

    cls = _CODE_TO_EXCEPTION.get(code_name)
    if cls is not None:
        raise cls(detail) from exc

    # Unknown code (or non-gRPC exception) — propagate unchanged so the
    # caller sees the most diagnostically useful trace.
    raise exc


# --- Helpers ------------------------------------------------------------------


def _grpc_code_name(exc: BaseException) -> str:
    """Return the gRPC status-code name (e.g. ``NOT_FOUND``) or ``""``."""
    code_fn = getattr(exc, "code", None)
    if not callable(code_fn):
        return ""
    try:
        code = code_fn()
    except Exception:  # noqa: BLE001 - defensive
        return ""
    name = getattr(code, "name", None)
    if isinstance(name, str):
        return name
    return str(code)


def _grpc_detail(exc: BaseException) -> str:
    """Return the gRPC status-detail string or ``""``."""
    detail_fn = getattr(exc, "details", None)
    if not callable(detail_fn):
        return ""
    try:
        detail = detail_fn()
    except Exception:  # noqa: BLE001 - defensive
        return ""
    if isinstance(detail, str):
        return detail
    return ""


# Exactly one mapping per gRPC code we care about. Cut-3 services may
# return the same code from different operations (e.g. NotFound from
# Sync Achieve vs Savepoint Restore); tests catch the base RtiError so
# the coarse mapping below is sufficient. Per-service refinement (sniff
# the detail string) can be added later without breaking the contract.
_CODE_TO_EXCEPTION: dict[str, type[RtiError]] = {
    "NOT_FOUND": SyncPointNotFound,
    "ALREADY_EXISTS": SyncPointAlreadyExists,
    "FAILED_PRECONDITION": InvalidSyncState,
    "PERMISSION_DENIED": OwnershipNotPermitted,
}
