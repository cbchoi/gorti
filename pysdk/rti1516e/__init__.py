"""rti1516e — Python SDK for the gorti RTI (IEEE 1516-2010 / HLA Evolved).

See `docs/agent-c-pysdk.md` for the SDK contract and `pysdk/README.md` for
the package layout.

Public API:

    from rti1516e import (
        # Layer 1 — idiomatic asyncio (primary)
        RtiConnection, FederationSpec, Federate,
        # Layer 2 — standard-shaped ambassador (Java/C++ port-friendly)
        Rti1516eAmbassador,
        # M28 — Pitch-style typed handles + typed collections
        ObjectClassHandle, AttributeHandle, InteractionClassHandle,
        ParameterHandle, ObjectInstanceHandle, FederateHandle,
        DimensionHandle, RegionHandle, MessageRetractionHandle,
        AttributeHandleSet, ParameterHandleSet, FederateHandleSet,
        DimensionHandleSet, RegionHandleSet,
        AttributeHandleValueMap, ParameterHandleValueMap, AttributeRegionMap,
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

from rti1516e._grpc_errors import (
    InvalidOwnershipState,
    InvalidSaveState,
    InvalidSyncState,
    OwnershipNotPermitted,
    RegionNotFound,
    SaveBundleAlreadyExists,
    SaveBundleNotFound,
    SyncPointAlreadyExists,
    SyncPointNotFound,
)
from rti1516e.connection import Federate, FederationSpec, RtiConnection
from rti1516e.ddm import AttributeRegions, DDMClient
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
from rti1516e.handles import (
    AttributeHandle,
    DimensionHandle,
    FederateHandle,
    InteractionClassHandle,
    MessageRetractionHandle,
    ObjectClassHandle,
    ObjectInstanceHandle,
    ParameterHandle,
    RegionHandle,
)
from rti1516e.ownership import OwnershipClient
from rti1516e.savepoint import RestoreState, SavepointClient, SaveState
from rti1516e.sets import (
    AttributeHandleSet,
    AttributeHandleValueMap,
    AttributeRegionMap,
    DimensionHandleSet,
    FederateHandleSet,
    ParameterHandleSet,
    ParameterHandleValueMap,
    RegionHandleSet,
)
from rti1516e.standard import Rti1516eAmbassador
from rti1516e.sync import SyncClient

__all__ = [
    "AttributeHandle",
    "AttributeHandleSet",
    "AttributeHandleValueMap",
    "AttributeRegionMap",
    "AttributeRegions",
    "DDMClient",
    "DimensionHandle",
    "DimensionHandleSet",
    "DiscoverObjectInstance",
    "EncodingInsufficientBytes",
    "EncodingPaddingViolation",
    "EncodingTypeMismatch",
    "Federate",
    "FederateHandle",
    "FederateHandleSet",
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
    "InteractionClassHandle",
    "InvalidOwnershipState",
    "InvalidSaveState",
    "InvalidSyncState",
    "MessageRetractionHandle",
    "ObjectAttributeNotOwned",
    "ObjectClassHandle",
    "ObjectClassNotPublished",
    "ObjectInstanceHandle",
    "ObjectNotFound",
    "OwnershipClient",
    "OwnershipNotPermitted",
    "ParameterHandle",
    "ParameterHandleSet",
    "ParameterHandleValueMap",
    "ReceiveInteraction",
    "ReflectAttributeValues",
    "RegionHandle",
    "RegionHandleSet",
    "RegionNotFound",
    "RestoreState",
    "Rti1516eAmbassador",
    "RtiConnection",
    "RtiError",
    "SaveBundleAlreadyExists",
    "SaveBundleNotFound",
    "SaveState",
    "SavepointClient",
    "SyncClient",
    "SyncPointAlreadyExists",
    "SyncPointNotFound",
    "TimeAdvanceGrant",
    "TimeAlreadyConstrained",
    "TimeAlreadyRegulating",
    "TimeInvalidLookahead",
    "TimeNotConstrained",
    "TimeNotRegulating",
    "TimeRequestInPast",
    "__version__",
]
