// Lockfile: IEEE 1516.1-2010 RTI/Enums.h — ALL spec enums are UNSCOPED.
// Catalogue §5 rows 5.1-5.10. FR-DLC-16.
//
// M31 RED — fails until M32 lands `RTI/Enums.h` with the spec enumerator names
// AND the unscoped-enum form (NOT `enum class`).
//
// Why unscoped matters: reference_rti federate source uses `HLA_IMMEDIATE` and
// `RECEIVE` as bare identifiers (no `CallbackModel::` qualifier). Switching to
// `enum class` would force every such federate to add scope qualifiers, breaking
// source compat — FR-DLC-16's whole point. This TU locks the form via implicit
// conversion to int (only legal for unscoped enums) AND via the bare-name lookup.
//
// IEEE 1516.1-2010 API reference: RTI/Enums.h

#include <RTI/Enums.h>
#include <type_traits>

namespace {

// ---------- FR-DLC-16: unscoped form (bare-name access works) ----------

// Bring every spec enumerator in via using-declaration; if any name is missing
// from the rti1516e namespace, this TU fails compile here (RED for that row).
using rti1516e::HLA_IMMEDIATE;
using rti1516e::HLA_EVOKED;

using rti1516e::RECEIVE;
using rti1516e::TIMESTAMP;

using rti1516e::UNCONDITIONALLY_DIVEST_ATTRIBUTES;
using rti1516e::DELETE_OBJECTS;
using rti1516e::CANCEL_PENDING_OWNERSHIP_ACQUISITIONS;
using rti1516e::DELETE_OBJECTS_THEN_DIVEST;
using rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST;
using rti1516e::NO_ACTION;

using rti1516e::RTI_UNABLE_TO_RESTORE;
using rti1516e::FEDERATE_REPORTED_FAILURE_DURING_RESTORE;
using rti1516e::FEDERATE_RESIGNED_DURING_RESTORE;
using rti1516e::RTI_DETECTED_FAILURE_DURING_RESTORE;
using rti1516e::RESTORE_ABORTED;

using rti1516e::NO_RESTORE_IN_PROGRESS;
using rti1516e::FEDERATE_RESTORE_REQUEST_PENDING;
using rti1516e::FEDERATE_WAITING_FOR_RESTORE_TO_BEGIN;
using rti1516e::FEDERATE_PREPARED_TO_RESTORE;
using rti1516e::FEDERATE_RESTORING;
using rti1516e::FEDERATE_WAITING_FOR_FEDERATION_TO_RESTORE;

using rti1516e::RTI_UNABLE_TO_SAVE;
using rti1516e::FEDERATE_REPORTED_FAILURE_DURING_SAVE;
using rti1516e::FEDERATE_RESIGNED_DURING_SAVE;
using rti1516e::RTI_DETECTED_FAILURE_DURING_SAVE;
using rti1516e::SAVE_TIME_CANNOT_BE_HONORED;
using rti1516e::SAVE_ABORTED;

using rti1516e::NO_SAVE_IN_PROGRESS;
using rti1516e::FEDERATE_INSTRUCTED_TO_SAVE;
using rti1516e::FEDERATE_SAVING;
using rti1516e::FEDERATE_WAITING_FOR_FEDERATION_TO_SAVE;

using rti1516e::FEDERATION_MANAGEMENT;
using rti1516e::DECLARATION_MANAGEMENT;
using rti1516e::OBJECT_MANAGEMENT;
using rti1516e::OWNERSHIP_MANAGEMENT;
using rti1516e::TIME_MANAGEMENT;
using rti1516e::DATA_DISTRIBUTION_MANAGEMENT;
using rti1516e::SUPPORT_SERVICES;

using rti1516e::SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE;
using rti1516e::SYNCHRONIZATION_SET_MEMBER_NOT_JOINED;

using rti1516e::RELIABLE;
using rti1516e::BEST_EFFORT;

// ---------- Row 5.1: CallbackModel — values match spec ----------
static_assert(std::is_enum_v<rti1516e::CallbackModel>);
static_assert(std::is_same_v<decltype(HLA_IMMEDIATE), rti1516e::CallbackModel>);
static_assert(static_cast<int>(HLA_IMMEDIATE) == 0);
static_assert(static_cast<int>(HLA_EVOKED) == 1);
// FR-DLC-16 form-lock: unscoped enums implicitly convert to int. `enum class`
// would NOT. So if someone tries to "fix" CallbackModel to scoped, this fails.
static_assert(std::is_convertible_v<rti1516e::CallbackModel, int>);

// ---------- Row 5.2: OrderType — RECEIVE=1, TIMESTAMP=2 ----------
static_assert(std::is_enum_v<rti1516e::OrderType>);
static_assert(static_cast<int>(RECEIVE) == 1);
static_assert(static_cast<int>(TIMESTAMP) == 2);
static_assert(std::is_convertible_v<rti1516e::OrderType, int>);

// ---------- Row 5.3: ResignAction — 6 values, default ordering from 0 ----------
static_assert(std::is_enum_v<rti1516e::ResignAction>);
static_assert(static_cast<int>(UNCONDITIONALLY_DIVEST_ATTRIBUTES) == 0);
static_assert(static_cast<int>(DELETE_OBJECTS) == 1);
static_assert(static_cast<int>(CANCEL_PENDING_OWNERSHIP_ACQUISITIONS) == 2);
static_assert(static_cast<int>(DELETE_OBJECTS_THEN_DIVEST) == 3);
static_assert(static_cast<int>(CANCEL_THEN_DELETE_THEN_DIVEST) == 4);
static_assert(static_cast<int>(NO_ACTION) == 5);
static_assert(std::is_convertible_v<rti1516e::ResignAction, int>);

// ---------- Row 5.4: SaveFailureReason — 6 values ----------
static_assert(std::is_enum_v<rti1516e::SaveFailureReason>);
static_assert(std::is_convertible_v<rti1516e::SaveFailureReason, int>);

// ---------- Row 5.5: RestoreFailureReason — 5 values ----------
static_assert(std::is_enum_v<rti1516e::RestoreFailureReason>);
static_assert(std::is_convertible_v<rti1516e::RestoreFailureReason, int>);

// ---------- Row 5.6: SaveStatus — 4 values ----------
static_assert(std::is_enum_v<rti1516e::SaveStatus>);
static_assert(std::is_convertible_v<rti1516e::SaveStatus, int>);

// ---------- Row 5.7: RestoreStatus — 6 values ----------
static_assert(std::is_enum_v<rti1516e::RestoreStatus>);
static_assert(std::is_convertible_v<rti1516e::RestoreStatus, int>);

// ---------- Row 5.8: ServiceGroup — 7 values ----------
static_assert(std::is_enum_v<rti1516e::ServiceGroup>);
static_assert(std::is_convertible_v<rti1516e::ServiceGroup, int>);

// ---------- Row 5.9: SynchronizationPointFailureReason — 2 values ----------
static_assert(std::is_enum_v<rti1516e::SynchronizationPointFailureReason>);
static_assert(std::is_convertible_v<rti1516e::SynchronizationPointFailureReason, int>);

// ---------- Row 5.10: TransportationType — RELIABLE=1, BEST_EFFORT=2 ----------
static_assert(std::is_enum_v<rti1516e::TransportationType>);
static_assert(static_cast<int>(RELIABLE) == 1);
static_assert(static_cast<int>(BEST_EFFORT) == 2);
static_assert(std::is_convertible_v<rti1516e::TransportationType, int>);

}  // namespace
