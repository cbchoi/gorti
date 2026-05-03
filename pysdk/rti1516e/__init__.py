"""rti1516e — Python SDK for the gorti RTI (IEEE 1516-2010 / HLA Evolved).

See `docs/agent-c-pysdk.md` for the SDK contract and `pysdk/README.md` for
the package layout.

Public API:

    from rti1516e import (
        # Layer 1 — idiomatic asyncio (primary)
        RtiConnection, FederationSpec, Federate,
        # Layer 2 — standard-shaped ambassador (Java/C++ port-friendly)
        Rti1516eAmbassador,
        # Events emitted by Federate.events()
        DiscoverObjectInstance, ReflectAttributeValues,
        ReceiveInteraction, TimeAdvanceGrant, FederationHalted,
        # Typed exceptions (one per ErrorCode in proto/rti/v1/errors.proto)
        RtiError, FederationNotFound, FederationAlreadyExists,
        FederationNotJoined, FederationAlreadyJoined,
        FederationHasFederatesJoined, FederationHaltedError,
        FomValidationFailed, FomMimRedefinition,
        ObjectNotFound, ObjectClassNotPublished, ObjectAttributeNotOwned,
        TimeNotRegulating, TimeNotConstrained, TimeInvalidLookahead,
        TimeRequestInPast, TimeAlreadyRegulating, TimeAlreadyConstrained,
        EncodingInsufficientBytes, EncodingTypeMismatch, EncodingPaddingViolation,
    )
"""

from __future__ import annotations

__version__ = "0.1.0"

# Re-export the public surface. Imports are lazy-ish (these submodules contain
# only stubs in the pre-dispatch state) but mypy --strict requires them to be
# importable, so each submodule's symbols are placeholders raising
# NotImplementedError at call time, not at import time.

from rti1516e.connection import Federate, FederationSpec, RtiConnection
from rti1516e.errors import (
    EncodingInsufficientBytes,
    EncodingPaddingViolation,
    EncodingTypeMismatch,
    FederationAlreadyExists,
    FederationAlreadyJoined,
    FederationHaltedError,
    FederationHasFederatesJoined,
    FederationNotFound,
    FederationNotJoined,
    FomMimRedefinition,
    FomValidationFailed,
    ObjectAttributeNotOwned,
    ObjectClassNotPublished,
    ObjectNotFound,
    RtiError,
    TimeAlreadyConstrained,
    TimeAlreadyRegulating,
    TimeInvalidLookahead,
    TimeNotConstrained,
    TimeNotRegulating,
    TimeRequestInPast,
)
from rti1516e.events import (
    DiscoverObjectInstance,
    FederationHalted,
    ReceiveInteraction,
    ReflectAttributeValues,
    TimeAdvanceGrant,
)
from rti1516e.standard import Rti1516eAmbassador

__all__ = [
    "DiscoverObjectInstance",
    "EncodingInsufficientBytes",
    "EncodingPaddingViolation",
    "EncodingTypeMismatch",
    "Federate",
    "FederationAlreadyExists",
    "FederationAlreadyJoined",
    "FederationHalted",
    "FederationHaltedError",
    "FederationHasFederatesJoined",
    "FederationNotFound",
    "FederationNotJoined",
    "FederationSpec",
    "FomMimRedefinition",
    "FomValidationFailed",
    "ObjectAttributeNotOwned",
    "ObjectClassNotPublished",
    "ObjectNotFound",
    "ReceiveInteraction",
    "ReflectAttributeValues",
    "Rti1516eAmbassador",
    "RtiConnection",
    "RtiError",
    "TimeAdvanceGrant",
    "TimeAlreadyConstrained",
    "TimeAlreadyRegulating",
    "TimeInvalidLookahead",
    "TimeNotConstrained",
    "TimeNotRegulating",
    "TimeRequestInPast",
    "__version__",
]
