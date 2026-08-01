// Lockfile: CallbackModel enum + the OTHER unscoped enums in RTI/Enums.h
// per IEEE 1516.1-2010 §4.2 + FR-DLC-16.
//
// This is the canonical FR-DLC-16 lockfile TU: it proves the spec-mandated
// enums are UNSCOPED (plain `enum`, not `enum class`). The semantic load:
//   - reference_rti federate source writes `connect(fa, HLA_IMMEDIATE)` with the
//     bare enumerator name. If gorti shipped `enum class CallbackModel`,
//     federate source would not compile — it would need
//     `CallbackModel::HLA_IMMEDIATE`.
//   - Unscoped enums are implicitly convertible to int; scoped enums are not.
//     The cast-to-int and is_convertible_v<E,int> assertions below FAIL on
//     `enum class`. That is the per-assertion RED signal for FR-DLC-16.
//
// Catalogue rows covered: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8, 5.9, 5.10.

#include <RTI/Enums.h>
#include <type_traits>

namespace {

using rti1516e::CallbackModel;
using rti1516e::HLA_IMMEDIATE;
using rti1516e::HLA_EVOKED;

using rti1516e::OrderType;
using rti1516e::RECEIVE;
using rti1516e::TIMESTAMP;

using rti1516e::TransportationType;
using rti1516e::RELIABLE;
using rti1516e::BEST_EFFORT;

using rti1516e::ResignAction;
using rti1516e::UNCONDITIONALLY_DIVEST_ATTRIBUTES;
using rti1516e::DELETE_OBJECTS;
using rti1516e::CANCEL_PENDING_OWNERSHIP_ACQUISITIONS;
using rti1516e::DELETE_OBJECTS_THEN_DIVEST;
using rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST;
using rti1516e::NO_ACTION;

using rti1516e::ServiceGroup;
using rti1516e::FEDERATION_MANAGEMENT;
using rti1516e::DECLARATION_MANAGEMENT;
using rti1516e::OBJECT_MANAGEMENT;
using rti1516e::OWNERSHIP_MANAGEMENT;
using rti1516e::TIME_MANAGEMENT;
using rti1516e::DATA_DISTRIBUTION_MANAGEMENT;
using rti1516e::SUPPORT_SERVICES;

// === FR-DLC-16: CallbackModel (catalogue 5.1) ===
// §4.2 enum CallbackModel { HLA_IMMEDIATE, HLA_EVOKED };
static_assert(std::is_enum_v<CallbackModel>);
static_assert(std::is_convertible_v<CallbackModel, int>);   // unscoped form
static_assert(std::is_same_v<decltype(HLA_IMMEDIATE), CallbackModel>);
static_assert(std::is_same_v<decltype(HLA_EVOKED), CallbackModel>);
static_assert(static_cast<int>(HLA_IMMEDIATE) == 0);
static_assert(static_cast<int>(HLA_EVOKED) == 1);

// === OrderType (catalogue 5.2, reference_rti Enums.h:27-31) ===
// `RECEIVE = 1, TIMESTAMP = 2` — note OrderType skips 0; this matters
// because some federate code branches on `(orderType == 0)` to mean "no
// order".
static_assert(std::is_enum_v<OrderType>);
static_assert(std::is_convertible_v<OrderType, int>);
static_assert(std::is_same_v<decltype(RECEIVE), OrderType>);
static_assert(std::is_same_v<decltype(TIMESTAMP), OrderType>);
static_assert(static_cast<int>(RECEIVE) == 1);
static_assert(static_cast<int>(TIMESTAMP) == 2);

// === TransportationType (catalogue 5.10) ===
// `RELIABLE = 1, BEST_EFFORT = 2`.
static_assert(std::is_enum_v<TransportationType>);
static_assert(std::is_convertible_v<TransportationType, int>);
static_assert(std::is_same_v<decltype(RELIABLE), TransportationType>);
static_assert(std::is_same_v<decltype(BEST_EFFORT), TransportationType>);
static_assert(static_cast<int>(RELIABLE) == 1);
static_assert(static_cast<int>(BEST_EFFORT) == 2);

// === ResignAction (catalogue 5.3) ===
// 6-value enum starting at 0. UNCONDITIONALLY_DIVEST_ATTRIBUTES = 0 because
// it is the most-conservative default in federate-shutdown code.
static_assert(std::is_enum_v<ResignAction>);
static_assert(std::is_convertible_v<ResignAction, int>);
static_assert(std::is_same_v<decltype(UNCONDITIONALLY_DIVEST_ATTRIBUTES), ResignAction>);
static_assert(static_cast<int>(UNCONDITIONALLY_DIVEST_ATTRIBUTES) == 0);
static_assert(static_cast<int>(DELETE_OBJECTS) == 1);
static_assert(static_cast<int>(CANCEL_PENDING_OWNERSHIP_ACQUISITIONS) == 2);
static_assert(static_cast<int>(DELETE_OBJECTS_THEN_DIVEST) == 3);
static_assert(static_cast<int>(CANCEL_THEN_DELETE_THEN_DIVEST) == 4);
static_assert(static_cast<int>(NO_ACTION) == 5);

// === SaveFailureReason / SaveStatus (catalogue 5.4, 5.6) ===
static_assert(std::is_enum_v<rti1516e::SaveFailureReason>);
static_assert(std::is_convertible_v<rti1516e::SaveFailureReason, int>);
static_assert(std::is_enum_v<rti1516e::SaveStatus>);
static_assert(std::is_convertible_v<rti1516e::SaveStatus, int>);

// === RestoreFailureReason / RestoreStatus (catalogue 5.5, 5.7) ===
static_assert(std::is_enum_v<rti1516e::RestoreFailureReason>);
static_assert(std::is_convertible_v<rti1516e::RestoreFailureReason, int>);
static_assert(std::is_enum_v<rti1516e::RestoreStatus>);
static_assert(std::is_convertible_v<rti1516e::RestoreStatus, int>);

// === ServiceGroup (catalogue 5.8) ===
// 7-value enum starting at 0. Used by normalizeServiceGroup (§10.32).
static_assert(std::is_enum_v<ServiceGroup>);
static_assert(std::is_convertible_v<ServiceGroup, int>);
static_assert(std::is_same_v<decltype(FEDERATION_MANAGEMENT), ServiceGroup>);
static_assert(static_cast<int>(FEDERATION_MANAGEMENT) == 0);
static_assert(static_cast<int>(DECLARATION_MANAGEMENT) == 1);
static_assert(static_cast<int>(OBJECT_MANAGEMENT) == 2);
static_assert(static_cast<int>(OWNERSHIP_MANAGEMENT) == 3);
static_assert(static_cast<int>(TIME_MANAGEMENT) == 4);
static_assert(static_cast<int>(DATA_DISTRIBUTION_MANAGEMENT) == 5);
static_assert(static_cast<int>(SUPPORT_SERVICES) == 6);

// === SynchronizationPointFailureReason (catalogue 5.9) ===
static_assert(std::is_enum_v<rti1516e::SynchronizationPointFailureReason>);
static_assert(std::is_convertible_v<rti1516e::SynchronizationPointFailureReason, int>);

// === FR-DLC-16 NEGATIVE LOCK ===
// Sanity-check that what we have is NOT scoped. The implicit conversion
// to int already proves this, but we capture it as a load-bearing assertion
// that fires per-row if anyone ever switches to enum class:
//
//   `is_convertible_v<scoped, int>` is FALSE for `enum class`.
//   `is_convertible_v<unscoped, int>` is TRUE.
//
// If a future commit changes `enum CallbackModel { ... }` to
// `enum class CallbackModel { ... }`, every line above fires red.
//
// (No additional assertion needed — the asserts above ARE the negative lock.)

}  // namespace
