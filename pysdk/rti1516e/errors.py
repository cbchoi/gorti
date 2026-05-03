"""Typed exceptions, one per ErrorCode in proto/rti/v1/errors.proto.

Agent C (TASK-067) wires these so each gRPC error response from the RTI is
mapped to the matching exception class. Spec tests in
pysdk/tests/spec/m4/test_spec_m4_events_stream.py assert the mapping is
1:1 against the proto definition.

This file is FROZEN-shape — Agent C may add helper methods on RtiError but
must not rename, remove, or change inheritance of existing classes.
"""

from __future__ import annotations


class RtiError(Exception):
    """Base class for all RTI-emitted typed exceptions.

    Carries the underlying gRPC status code (when available) on the
    ``code`` attribute and the per-error-class numeric ID from
    proto/rti/v1/errors.proto on the ``error_code`` attribute.

    Agent C wires ``error_code`` from gRPC trailers in TASK-067.
    """

    error_code: int = 0

    def __init__(self, message: str = "", *, error_code: int | None = None) -> None:
        super().__init__(message)
        if error_code is not None:
            self.error_code = error_code


# --- Federation lifecycle (errors.proto 1..7) -------------------------------


class FederationNotFound(RtiError):
    """ERR_FED_NOT_FOUND (1)."""

    error_code = 1


class FederationAlreadyExists(RtiError):
    """ERR_FED_ALREADY_EXISTS (2)."""

    error_code = 2


class FederationNotJoined(RtiError):
    """ERR_FED_NOT_JOINED (3)."""

    error_code = 3


class FederationAlreadyJoined(RtiError):
    """ERR_FED_ALREADY_JOINED (4)."""

    error_code = 4


class FederationHasFederatesJoined(RtiError):
    """ERR_FED_HAS_FEDERATES_JOINED (5)."""

    error_code = 5


class FederationHaltedError(RtiError):
    """ERR_FED_HALTED (6) — federation halted (e.g. by stall timeout).

    Named with -Error suffix to disambiguate from the FederationHalted
    *event* dataclass in events.py.
    """

    error_code = 6


class FederationInvalidName(RtiError):
    """ERR_FED_INVALID_NAME (7)."""

    error_code = 7


# --- FOM (errors.proto 100..101) --------------------------------------------


class FomValidationFailed(RtiError):
    """ERR_FOM_VALIDATION_FAILED (100)."""

    error_code = 100


class FomMimRedefinition(RtiError):
    """ERR_FOM_MIM_REDEFINITION (101)."""

    error_code = 101


# --- Object management (errors.proto 200..206) ------------------------------


class ObjectNotFound(RtiError):
    """ERR_OBJ_NOT_FOUND (200)."""

    error_code = 200


class ObjectClassNotPublished(RtiError):
    """ERR_OBJ_CLASS_NOT_PUBLISHED (201)."""

    error_code = 201


class ObjectAttributeNotOwned(RtiError):
    """ERR_OBJ_ATTR_NOT_OWNED (202)."""

    error_code = 202


class ObjectHandleInvalid(RtiError):
    """ERR_OBJ_HANDLE_INVALID (203)."""

    error_code = 203


class InteractionNotPublished(RtiError):
    """ERR_OBJ_INTERACTION_NOT_PUBLISHED (204)."""

    error_code = 204


class ObjectClassNotFound(RtiError):
    """ERR_OBJ_CLASS_NOT_FOUND (205)."""

    error_code = 205


class ObjectAttributeNotFound(RtiError):
    """ERR_OBJ_ATTR_NOT_FOUND (206)."""

    error_code = 206


# --- Time management (errors.proto 300..305) --------------------------------


class TimeNotRegulating(RtiError):
    """ERR_TIME_NOT_REGULATING (300)."""

    error_code = 300


class TimeNotConstrained(RtiError):
    """ERR_TIME_NOT_CONSTRAINED (301)."""

    error_code = 301


class TimeInvalidLookahead(RtiError):
    """ERR_TIME_INVALID_LOOKAHEAD (302)."""

    error_code = 302


class TimeRequestInPast(RtiError):
    """ERR_TIME_REQUEST_IN_PAST (303)."""

    error_code = 303


class TimeAlreadyRegulating(RtiError):
    """ERR_TIME_ALREADY_REGULATING (304)."""

    error_code = 304


class TimeAlreadyConstrained(RtiError):
    """ERR_TIME_ALREADY_CONSTRAINED (305)."""

    error_code = 305


# --- Encoding (errors.proto 400..402) ---------------------------------------


class EncodingInsufficientBytes(RtiError):
    """ERR_ENC_INSUFFICIENT_BYTES (400)."""

    error_code = 400


class EncodingTypeMismatch(RtiError):
    """ERR_ENC_TYPE_MISMATCH (401)."""

    error_code = 401


class EncodingPaddingViolation(RtiError):
    """ERR_ENC_PADDING_VIOLATION (402)."""

    error_code = 402


# --- Wire (errors.proto 500..501) -------------------------------------------


class WireVersionMismatch(RtiError):
    """ERR_WIRE_VERSION_MISMATCH (500)."""

    error_code = 500


class WireMalformedMessage(RtiError):
    """ERR_WIRE_MALFORMED_MESSAGE (501)."""

    error_code = 501


# Lookup table from numeric error code -> exception class. Agent C uses this
# in TASK-067 to map gRPC trailers to typed exceptions.
ERROR_CODE_TO_EXCEPTION: dict[int, type[RtiError]] = {
    cls.error_code: cls
    for cls in (
        FederationNotFound,
        FederationAlreadyExists,
        FederationNotJoined,
        FederationAlreadyJoined,
        FederationHasFederatesJoined,
        FederationHaltedError,
        FederationInvalidName,
        FomValidationFailed,
        FomMimRedefinition,
        ObjectNotFound,
        ObjectClassNotPublished,
        ObjectAttributeNotOwned,
        ObjectHandleInvalid,
        InteractionNotPublished,
        ObjectClassNotFound,
        ObjectAttributeNotFound,
        TimeNotRegulating,
        TimeNotConstrained,
        TimeInvalidLookahead,
        TimeRequestInPast,
        TimeAlreadyRegulating,
        TimeAlreadyConstrained,
        EncodingInsufficientBytes,
        EncodingTypeMismatch,
        EncodingPaddingViolation,
        WireVersionMismatch,
        WireMalformedMessage,
    )
}
