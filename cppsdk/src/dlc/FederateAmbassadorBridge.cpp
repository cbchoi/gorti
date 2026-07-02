// M34 (Agent AD) — DLCFederateAmbassadorBridge impl.
//
// See FederateAmbassadorBridge.h for the design rationale.

#include "FederateAmbassadorBridge.h"

#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>
#include <RTI/time/HLAfloat64Time.h>

#include <cstdint>
#include <cstring>
#include <optional>
#include <set>
#include <string>
#include <vector>

namespace gorti {
namespace dlc {
namespace conv {

std::wstring s2ws(const std::string& s) {
  // Byte-widening only. Matches the M17 -> DLC name/label round-trip used by
  // the M17 impl and the DLC RTIambassadorImpl helpers (see M33's wstring↔
  // string shim skeleton). If proper UTF-8 decoding is needed later, upgrade
  // this + s2ws_set + all other callers.
  std::wstring w;
  w.reserve(s.size());
  for (unsigned char c : s) {
    w.push_back(static_cast<wchar_t>(c));
  }
  return w;
}

std::set<std::wstring> s2ws_set(const std::vector<std::string>& v) {
  std::set<std::wstring> out;
  for (const auto& s : v) out.insert(s2ws(s));
  return out;
}

}  // namespace conv
}  // namespace dlc
}  // namespace gorti

// The DEFINE_HANDLE_CLASS macro in <RTI/Handle.h> declares
// `friend class HandleKind##Friend;` inside every handle class, granting
// access to the protected `HandleKind(VariableLengthData const&)` ctor. We
// define those friend classes here in `namespace rti1516e` so their names
// match the friend declaration. Each Friend exposes a `build(uint64)` that
// BE-encodes the raw value into a VariableLengthData and calls the protected
// VLD ctor — matching the encode/decode roundtrip in Handle.cpp.
namespace rti1516e {

#define DEFINE_HANDLE_FRIEND_BUILDER(HandleKind)                              \
  class HandleKind##Friend {                                                  \
   public:                                                                    \
    static HandleKind build(std::uint64_t raw) {                              \
      unsigned char buf[8];                                                   \
      for (int i = 7; i >= 0; --i) {                                          \
        buf[i] = static_cast<unsigned char>(raw & 0xFFu);                     \
        raw >>= 8;                                                            \
      }                                                                       \
      VariableLengthData vld(buf, sizeof(buf));                               \
      return HandleKind(vld);                                                 \
    }                                                                         \
  };

DEFINE_HANDLE_FRIEND_BUILDER(FederateHandle)
DEFINE_HANDLE_FRIEND_BUILDER(ObjectClassHandle)
DEFINE_HANDLE_FRIEND_BUILDER(InteractionClassHandle)
DEFINE_HANDLE_FRIEND_BUILDER(ObjectInstanceHandle)
DEFINE_HANDLE_FRIEND_BUILDER(AttributeHandle)
DEFINE_HANDLE_FRIEND_BUILDER(ParameterHandle)
DEFINE_HANDLE_FRIEND_BUILDER(DimensionHandle)
DEFINE_HANDLE_FRIEND_BUILDER(MessageRetractionHandle)
DEFINE_HANDLE_FRIEND_BUILDER(RegionHandle)

#undef DEFINE_HANDLE_FRIEND_BUILDER

}  // namespace rti1516e

namespace gorti {
namespace dlc {
namespace conv {

// Specialize to_dlc_handle for each DLC handle class by dispatching to the
// matching friend builder. Each specialization is trivially inlineable but
// we leave them out-of-line so the header stays free of the friend hooks.
template <>
rti1516e::ObjectInstanceHandle
to_dlc_handle<rti1516e::ObjectInstanceHandle>(std::uint64_t raw) {
  return rti1516e::ObjectInstanceHandleFriend::build(raw);
}
template <>
rti1516e::ObjectClassHandle
to_dlc_handle<rti1516e::ObjectClassHandle>(std::uint64_t raw) {
  return rti1516e::ObjectClassHandleFriend::build(raw);
}
template <>
rti1516e::InteractionClassHandle
to_dlc_handle<rti1516e::InteractionClassHandle>(std::uint64_t raw) {
  return rti1516e::InteractionClassHandleFriend::build(raw);
}
template <>
rti1516e::AttributeHandle
to_dlc_handle<rti1516e::AttributeHandle>(std::uint64_t raw) {
  return rti1516e::AttributeHandleFriend::build(raw);
}
template <>
rti1516e::ParameterHandle
to_dlc_handle<rti1516e::ParameterHandle>(std::uint64_t raw) {
  return rti1516e::ParameterHandleFriend::build(raw);
}
template <>
rti1516e::FederateHandle
to_dlc_handle<rti1516e::FederateHandle>(std::uint64_t raw) {
  return rti1516e::FederateHandleFriend::build(raw);
}

rti1516e::VariableLengthData to_dlc_vld(
    const std::vector<std::uint8_t>& bytes) {
  return rti1516e::VariableLengthData(
      bytes.empty() ? nullptr : bytes.data(), bytes.size());
}

rti1516e::AttributeHandleValueMap to_dlc_attr_map(
    const rti1516e_m17::AttributeHandleValueMap& m) {
  rti1516e::AttributeHandleValueMap out;
  for (const auto& kv : m) {
    rti1516e::AttributeHandle h =
        to_dlc_handle<rti1516e::AttributeHandle>(kv.first.raw());
    out.emplace(std::move(h), to_dlc_vld(kv.second));
  }
  return out;
}

rti1516e::ParameterHandleValueMap to_dlc_param_map(
    const rti1516e_m17::ParameterHandleValueMap& m) {
  rti1516e::ParameterHandleValueMap out;
  for (const auto& kv : m) {
    rti1516e::ParameterHandle h =
        to_dlc_handle<rti1516e::ParameterHandle>(kv.first.raw());
    out.emplace(std::move(h), to_dlc_vld(kv.second));
  }
  return out;
}

rti1516e::AttributeHandleSet to_dlc_attr_set(
    const rti1516e_m17::AttributeHandleSet& s) {
  rti1516e::AttributeHandleSet out;
  for (const auto& h : s) {
    out.insert(to_dlc_handle<rti1516e::AttributeHandle>(h.raw()));
  }
  return out;
}

}  // namespace conv

// ===== FR-DLC-14 re-entrancy witness (M36 Agent DA) =====

thread_local bool tls_in_callback = false;

// ===== Bridge impl =====

DLCFederateAmbassadorBridge::DLCFederateAmbassadorBridge(
    rti1516e::FederateAmbassador* dlc_fed)
    : dlc_fed_(dlc_fed) {}

void DLCFederateAmbassadorBridge::discoverObjectInstance(
    rti1516e_m17::ObjectInstanceHandle object,
    rti1516e_m17::ObjectClassHandle object_class,
    const std::string& object_name) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  // §6.9 two-arg overload (no producingFederate on M17). Delegate to the
  // 3-arg DLC overload.
  dlc_fed_->discoverObjectInstance(
      conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(object.raw()),
      conv::to_dlc_handle<rti1516e::ObjectClassHandle>(object_class.raw()),
      conv::s2ws(object_name));
}

void DLCFederateAmbassadorBridge::reflectAttributeValues(
    rti1516e_m17::ObjectInstanceHandle object,
    const rti1516e_m17::AttributeHandleValueMap& values,
    std::optional<double> timestamp) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  auto oh = conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(object.raw());
  auto vm = conv::to_dlc_attr_map(values);
  rti1516e::VariableLengthData tag;  // M17 does not carry user tag on reflect
  rti1516e::SupplementalReflectInfo supp;
  if (timestamp.has_value()) {
    // With-time overload (§6.11). Build an HLAfloat64Time on the stack —
    // the DLC surface takes the ref only for the duration of the call and
    // does not retain a pointer. M36 Agent DA: Pitch delivers TSO through
    // the RETRACTION-handle variant (the wire carries no retraction
    // designator yet, so a default-invalid handle is passed) — the TSO
    // conformance fixtures override that 9-arg form.
    rti1516e::HLAfloat64Time t(*timestamp);
    dlc_fed_->reflectAttributeValues(
        oh, vm, tag, rti1516e::TIMESTAMP, rti1516e::RELIABLE, t,
        rti1516e::TIMESTAMP, rti1516e::MessageRetractionHandle(), supp);
  } else {
    // No-time overload.
    dlc_fed_->reflectAttributeValues(
        oh, vm, tag, rti1516e::RECEIVE, rti1516e::RELIABLE, supp);
  }
}

void DLCFederateAmbassadorBridge::receiveInteraction(
    rti1516e_m17::InteractionClassHandle interaction_class,
    const rti1516e_m17::ParameterHandleValueMap& parameters,
    std::optional<double> timestamp) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  auto ih = conv::to_dlc_handle<rti1516e::InteractionClassHandle>(
      interaction_class.raw());
  auto pm = conv::to_dlc_param_map(parameters);
  rti1516e::VariableLengthData tag;
  rti1516e::SupplementalReceiveInfo supp;
  if (timestamp.has_value()) {
    // M36 Agent DA: TSO delivery uses the 9-arg retraction-handle
    // overload (Pitch shape) — see reflectAttributeValues note above.
    rti1516e::HLAfloat64Time t(*timestamp);
    dlc_fed_->receiveInteraction(
        ih, pm, tag, rti1516e::TIMESTAMP, rti1516e::RELIABLE, t,
        rti1516e::TIMESTAMP, rti1516e::MessageRetractionHandle(), supp);
  } else {
    dlc_fed_->receiveInteraction(
        ih, pm, tag, rti1516e::RECEIVE, rti1516e::RELIABLE, supp);
  }
}

void DLCFederateAmbassadorBridge::removeObjectInstance(
    rti1516e_m17::ObjectInstanceHandle object,
    std::optional<double> timestamp,
    const rti1516e_m17::VariableLengthData& tag) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  auto oh = conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(object.raw());
  auto vtag = conv::to_dlc_vld(tag);
  rti1516e::SupplementalRemoveInfo supp;
  if (timestamp.has_value()) {
    // §6.15 TSO — retraction-handle overload (Pitch shape; the wire has
    // no retraction designator yet, so a default-invalid handle rides).
    rti1516e::HLAfloat64Time t(*timestamp);
    dlc_fed_->removeObjectInstance(oh, vtag, rti1516e::TIMESTAMP, t,
                                   rti1516e::TIMESTAMP,
                                   rti1516e::MessageRetractionHandle(), supp);
  } else {
    // §6.15 RO 4-arg overload.
    dlc_fed_->removeObjectInstance(oh, vtag, rti1516e::RECEIVE, supp);
  }
}

void DLCFederateAmbassadorBridge::provideAttributeValueUpdate(
    rti1516e_m17::ObjectInstanceHandle object,
    const rti1516e_m17::AttributeHandleSet& attributes,
    const rti1516e_m17::VariableLengthData& tag) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  dlc_fed_->provideAttributeValueUpdate(
      conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(object.raw()),
      conv::to_dlc_attr_set(attributes),
      conv::to_dlc_vld(tag));
}

void DLCFederateAmbassadorBridge::objectInstanceNameReservationSucceeded(
    const std::string& object_name) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  dlc_fed_->objectInstanceNameReservationSucceeded(conv::s2ws(object_name));
}

void DLCFederateAmbassadorBridge::objectInstanceNameReservationFailed(
    const std::string& object_name) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  dlc_fed_->objectInstanceNameReservationFailed(conv::s2ws(object_name));
}

void DLCFederateAmbassadorBridge::
    multipleObjectInstanceNameReservationSucceeded(
        const std::vector<std::string>& object_names) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  dlc_fed_->multipleObjectInstanceNameReservationSucceeded(
      conv::s2ws_set(object_names));
}

void DLCFederateAmbassadorBridge::
    multipleObjectInstanceNameReservationFailed(
        const std::vector<std::string>& /*requested_names*/,
        const std::vector<std::string>& colliding_names) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  // §6.6 DLC callback takes the requested-names set. Per header comment we
  // forward the COLLIDING set — which is what tests observe today. If callers
  // need the full requested set instead, swap arg here.
  dlc_fed_->multipleObjectInstanceNameReservationFailed(
      conv::s2ws_set(colliding_names));
}

void DLCFederateAmbassadorBridge::timeAdvanceGrant(double time) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  rti1516e::HLAfloat64Time t(time);
  dlc_fed_->timeAdvanceGrant(t);
}

void DLCFederateAmbassadorBridge::announceSynchronizationPoint(
    const std::string& label,
    const rti1516e_m17::VariableLengthData& tag) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  dlc_fed_->announceSynchronizationPoint(conv::s2ws(label),
                                         conv::to_dlc_vld(tag));
}

void DLCFederateAmbassadorBridge::federationSynchronized(
    const std::string& label) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  // §4.15 DLC callback: (label, failedToSyncSet). M17 carries no such set;
  // pass empty.
  rti1516e::FederateHandleSet empty;
  dlc_fed_->federationSynchronized(conv::s2ws(label), empty);
}

void DLCFederateAmbassadorBridge::requestAttributeOwnershipAssumption(
    rti1516e_m17::ObjectInstanceHandle object,
    const rti1516e_m17::AttributeHandleSet& attributes,
    rti1516e_m17::FederateHandle /*divesting_federate*/,
    const rti1516e_m17::VariableLengthData& tag) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  dlc_fed_->requestAttributeOwnershipAssumption(
      conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(object.raw()),
      conv::to_dlc_attr_set(attributes),
      conv::to_dlc_vld(tag));
}

void DLCFederateAmbassadorBridge::attributeOwnershipAcquisitionNotification(
    rti1516e_m17::ObjectInstanceHandle object,
    const rti1516e_m17::AttributeHandleSet& attributes,
    rti1516e_m17::FederateHandle /*owning_federate*/) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  rti1516e::VariableLengthData empty_tag;
  dlc_fed_->attributeOwnershipAcquisitionNotification(
      conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(object.raw()),
      conv::to_dlc_attr_set(attributes),
      empty_tag);
}

void DLCFederateAmbassadorBridge::requestDivestitureConfirmation(
    rti1516e_m17::ObjectInstanceHandle object,
    const rti1516e_m17::AttributeHandleSet& attributes) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  dlc_fed_->requestDivestitureConfirmation(
      conv::to_dlc_handle<rti1516e::ObjectInstanceHandle>(object.raw()),
      conv::to_dlc_attr_set(attributes));
}

void DLCFederateAmbassadorBridge::initiateFederateSave(
    const std::string& label,
    std::optional<double> save_time) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  if (save_time.has_value()) {
    rti1516e::HLAfloat64Time t(*save_time);
    dlc_fed_->initiateFederateSave(conv::s2ws(label), t);
  } else {
    dlc_fed_->initiateFederateSave(conv::s2ws(label));
  }
}

void DLCFederateAmbassadorBridge::federationSaved(
    const std::string& /*label*/) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  // §4.20 DLC callback takes no label.
  dlc_fed_->federationSaved();
}

void DLCFederateAmbassadorBridge::federationNotSaved(
    const std::string& /*label*/) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  // §4.20 DLC callback takes a SaveFailureReason; M17 does not carry one.
  dlc_fed_->federationNotSaved(rti1516e::RTI_UNABLE_TO_SAVE);
}

void DLCFederateAmbassadorBridge::initiateFederateRestore(
    const std::string& label,
    rti1516e_m17::FederateHandle federate_handle) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  // §4.27 DLC callback: (label, federateName, handle). M17 does not carry a
  // name; forward empty.
  std::wstring empty_name;
  dlc_fed_->initiateFederateRestore(
      conv::s2ws(label), empty_name,
      conv::to_dlc_handle<rti1516e::FederateHandle>(federate_handle.raw()));
}

void DLCFederateAmbassadorBridge::federationRestored(
    const std::string& /*label*/) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  dlc_fed_->federationRestored();
}

void DLCFederateAmbassadorBridge::federationNotRestored(
    const std::string& /*label*/) {
  if (!dlc_fed_) return;
  CallbackScope scope;  // FR-DLC-14 — mark the callback context
  dlc_fed_->federationNotRestored(rti1516e::RTI_UNABLE_TO_RESTORE);
}

}  // namespace dlc
}  // namespace gorti
