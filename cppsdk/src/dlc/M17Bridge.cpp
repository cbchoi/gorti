// M17Bridge — opaque delegate to gorti's M17 rti1516e::RTIambassador.
//
// M34 Agent AA (§4 Federation Management).
//
// This TU intentionally does NOT include <RTI/RTIambassador.h>. Only M17's
// <rti1516e/RtiAmbassador.h> is visible, so `rti1516e::RTIambassador` here
// unambiguously means the M17 concrete class. Every method catches M17's
// typed exceptions and re-throws as std::runtime_error with a
// method-prefixed message so the DLC caller (RTIambassadorImpl.cpp) can
// re-throw as the matching <RTI/Exception.h> type.

#include "M17Bridge.h"

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

// M36 Agent CA-4 — listFederationExecutions dials its own channel (the M17
// ambassador never surfaced the ListFederations RPC; see header comment).
#include <grpcpp/grpcpp.h>
#include "rti/v1/federation.grpc.pb.h"
#include "rti/v1/federation.pb.h"

#include <stdexcept>
#include <string>

namespace rti1516e {

// ---------- Impl -----------------------------------------------------------

struct M17Bridge::Impl {
  // The M17 Cut-1 concrete class from <rti1516e/RtiAmbassador.h>. M34
  // Agent AA renamed it from `RTIambassador` to `M17RTIambassador` so the
  // linker symbols don't collide with the DLC spec-abstract
  // `rti1516e::RTIambassador` in <RTI/RTIambassador.h>. Existing consumers
  // still see `rti1516e::RTIambassador` via the `using` alias in
  // <rti1516e/RtiAmbassador.h>, but alias uses don't emit new mangled
  // symbols — only `M17RTIambassador::*` shows up at link time.
  //
  // Held via unique_ptr to keep the M17 impl's own PIMPL (a gRPC channel)
  // stable across DLC lifetime — the outer DLCRTIambassadorImpl's move
  // semantics don't attempt to move/copy the M17 amb.
  std::unique_ptr<::rti1516e::M17RTIambassador> amb;
  // M36 Agent CA-4 — dial URL recorded at connect() so
  // listFederationExecutions can open its own stub (the M17 pimpl's
  // channel is not reachable from here). Empty before connect().
  std::string connect_url;
  Impl() : amb(std::make_unique<::rti1516e::M17RTIambassador>()) {}
};

M17Bridge::M17Bridge() : impl_(new Impl()) {}
M17Bridge::~M17Bridge() = default;

// ---------- exception adapter ---------------------------------------------

namespace {
// Wrap the M17 typed exception hierarchy into std::runtime_error so the
// DLC caller can translate to <RTI/Exception.h> types without seeing the
// M17 exception header.
template <typename Fn>
auto guard(char const* op, Fn&& f) -> decltype(f()) {
  try {
    return f();
  } catch (::rti1516e::m17::SpecException const& e) {
    // M39 Agent HB — structured spec-exception carrier. The M17 client
    // (throwFromStatus) parked the server-declared Annex C class name
    // (rti-spec-exception trailer) here; re-emit it as the message
    // prefix so the DLC's translateBridgeError prefix table throws the
    // precise <RTI/Exception.h> type. MUST be the first catch — the
    // carrier derives from m17::RTIinternalError.
    throw std::runtime_error(e.specName() + ": " + e.what() +
                             " [op=" + op + "]");
  } catch (::rti1516e::m17::AlreadyConnected const& e) {
    throw std::runtime_error(std::string("AlreadyConnected: ") + e.what() +
                             " [op=" + op + "]");
  } catch (::rti1516e::m17::ConnectionFailed const& e) {
    throw std::runtime_error(std::string("ConnectionFailed: ") + e.what() +
                             " [op=" + op + "]");
  } catch (::rti1516e::m17::NotConnected const& e) {
    throw std::runtime_error(std::string("NotConnected: ") + e.what() +
                             " [op=" + op + "]");
  } catch (::rti1516e::m17::FederationExecutionAlreadyExists const& e) {
    throw std::runtime_error(
        std::string("FederationExecutionAlreadyExists: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::m17::FederationExecutionDoesNotExist const& e) {
    throw std::runtime_error(
        std::string("FederationExecutionDoesNotExist: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::m17::FederateAlreadyExecutionMember const& e) {
    throw std::runtime_error(
        std::string("FederateAlreadyExecutionMember: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::m17::FederateNotExecutionMember const& e) {
    throw std::runtime_error(
        std::string("FederateNotExecutionMember: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::m17::NameNotFound const& e) {
    throw std::runtime_error(std::string("NameNotFound: ") + e.what() +
                             " [op=" + op + "]");
  } catch (::rti1516e::m17::RTIinternalError const& e) {
    throw std::runtime_error(std::string("RTIinternalError: ") + e.what() +
                             " [op=" + op + "]");
  } catch (std::exception const& e) {
    // Catch-all for any other std::exception (e.g. std::system_error from
    // gRPC). Re-throw as std::runtime_error so the DLC caller's bridge()
    // translates it via the RTIinternalError fallback path.
    throw std::runtime_error(std::string("StdException: ") + e.what() +
                             " [op=" + op + "]");
  }
}
}  // namespace

// ---------- §4.2 / §4.3 connect / disconnect ------------------------------

void M17Bridge::connect(const std::string& url) {
  guard("connect", [&] { impl_->amb->connect(url); });
  impl_->connect_url = url;  // only on success — see guard() throw above
}
void M17Bridge::disconnect() {
  guard("disconnect", [&] { impl_->amb->disconnect(); });
}
bool M17Bridge::isConnected() const noexcept {
  return impl_->amb->isConnected();
}

// ---------- §4.5 / §4.6 create / destroy ----------------------------------

void M17Bridge::createFederationExecution(
    const std::string& name, const std::vector<std::string>& fom_modules) {
  guard("createFederationExecution",
        [&] { impl_->amb->createFederationExecution(name, fom_modules); });
}
void M17Bridge::destroyFederationExecution(const std::string& name) {
  guard("destroyFederationExecution",
        [&] { impl_->amb->destroyFederationExecution(name); });
}

// ---------- §4.9 / §4.10 join / resign ------------------------------------

std::uint64_t M17Bridge::joinFederationExecution(
    const std::string& federate_name, const std::string& federation_name) {
  return guard("joinFederationExecution", [&] {
    auto h = impl_->amb->joinFederationExecution(federate_name, federation_name);
    return static_cast<std::uint64_t>(h.raw());
  });
}
void M17Bridge::resignFederationExecution() {
  // Cut-1 M17 default: UNCONDITIONALLY_DIVEST_ATTRIBUTES. The DLC caller
  // accepts the wider ResignAction and folds all 6 spec values onto this
  // single M17 default; the divergence is tracked in
  // docs/DLC_DIVERGENCE_CATALOGUE.md §3.
  guard("resignFederationExecution",
        [&] { impl_->amb->resignFederationExecution(); });
}

// ---------- §4.11 / §4.14 sync points -------------------------------------

void M17Bridge::registerFederationSynchronizationPoint(
    const std::string& label, const std::vector<std::uint8_t>& tag,
    const std::vector<std::uint64_t>& required_federates) {
  guard("registerFederationSynchronizationPoint", [&] {
    // Re-wrap the raw uint64s as M17 FederateHandles.
    std::vector<::rti1516e::FederateHandle> fs;
    fs.reserve(required_federates.size());
    for (auto v : required_federates) {
      fs.push_back(::rti1516e::FederateHandle{v});
    }
    impl_->amb->registerFederationSynchronizationPoint(label, tag, fs);
  });
}
void M17Bridge::synchronizationPointAchieved(const std::string& label,
                                             bool successfully) {
  // M37 EC-2 — route through M17's §4.14 flag-carrying overload so
  // successfully=false lands in the §4.15 failed-to-sync set.
  guard("synchronizationPointAchieved", [&] {
    impl_->amb->synchronizationPointAchieved(label, successfully);
  });
}

// ---------- §4.16-28 save family ------------------------------------------

void M17Bridge::requestFederationSave(const std::string& label,
                                       std::optional<double> save_time) {
  guard("requestFederationSave",
        [&] { impl_->amb->requestFederationSave(label, save_time); });
}
void M17Bridge::federateSaveComplete() {
  guard("federateSaveComplete", [&] { impl_->amb->federateSaveComplete(); });
}
void M17Bridge::federateSaveNotComplete() {
  guard("federateSaveNotComplete",
        [&] { impl_->amb->federateSaveNotComplete(); });
}
void M17Bridge::abortFederationSave() {
  guard("abortFederationSave", [&] { impl_->amb->abortFederationSave(); });
}
M17SaveState M17Bridge::querySaveState(const std::string& label) {
  return guard("querySaveState", [&] {
    auto st = impl_->amb->querySaveState(label);
    return static_cast<M17SaveState>(static_cast<int>(st));
  });
}

// ---------- §4.24-30 restore family ---------------------------------------

void M17Bridge::requestFederationRestore(const std::string& label) {
  guard("requestFederationRestore",
        [&] { impl_->amb->requestFederationRestore(label); });
}
void M17Bridge::federateRestoreComplete() {
  guard("federateRestoreComplete",
        [&] { impl_->amb->federateRestoreComplete(); });
}
void M17Bridge::abortFederationRestore() {
  guard("abortFederationRestore",
        [&] { impl_->amb->abortFederationRestore(); });
}
M17RestoreState M17Bridge::queryRestoreState(const std::string& label) {
  return guard("queryRestoreState", [&] {
    auto st = impl_->amb->queryRestoreState(label);
    return static_cast<M17RestoreState>(static_cast<int>(st));
  });
}

// ---------- §5 Declaration Management (M35 Agent BB) ----------------------
//
// Each shim rewraps raw uint64 handles as M17 typed handles (StrongHandle
// wrapped over uint64 per include/rti1516e/Types.h). The AttributeHandleSet
// is std::set<AttributeHandle>; we materialize it from the DLC-supplied
// vector so callers don't have to know M17's set-based shape.

namespace {
::rti1516e::AttributeHandleSet toM17AttrSet(
    const std::vector<std::uint64_t>& attrs) {
  ::rti1516e::AttributeHandleSet out;
  for (auto v : attrs) out.insert(::rti1516e::AttributeHandle{v});
  return out;
}
}  // namespace

void M17Bridge::publishObjectClassAttributes(
    std::uint64_t cls, const std::vector<std::uint64_t>& attrs) {
  guard("publishObjectClassAttributes", [&] {
    impl_->amb->publishObjectClassAttributes(
        ::rti1516e::ObjectClassHandle{cls}, toM17AttrSet(attrs));
  });
}
void M17Bridge::unpublishObjectClassAttributes(
    std::uint64_t cls, const std::vector<std::uint64_t>& attrs) {
  guard("unpublishObjectClassAttributes", [&] {
    impl_->amb->unpublishObjectClassAttributes(
        ::rti1516e::ObjectClassHandle{cls}, toM17AttrSet(attrs));
  });
}
void M17Bridge::subscribeObjectClassAttributes(
    std::uint64_t cls, const std::vector<std::uint64_t>& attrs) {
  guard("subscribeObjectClassAttributes", [&] {
    impl_->amb->subscribeObjectClassAttributes(
        ::rti1516e::ObjectClassHandle{cls}, toM17AttrSet(attrs));
  });
}
void M17Bridge::unsubscribeObjectClassAttributes(
    std::uint64_t cls, const std::vector<std::uint64_t>& attrs) {
  guard("unsubscribeObjectClassAttributes", [&] {
    impl_->amb->unsubscribeObjectClassAttributes(
        ::rti1516e::ObjectClassHandle{cls}, toM17AttrSet(attrs));
  });
}

void M17Bridge::publishInteractionClass(std::uint64_t cls) {
  guard("publishInteractionClass", [&] {
    impl_->amb->publishInteractionClass(
        ::rti1516e::InteractionClassHandle{cls});
  });
}
void M17Bridge::unpublishInteractionClass(std::uint64_t cls) {
  guard("unpublishInteractionClass", [&] {
    impl_->amb->unpublishInteractionClass(
        ::rti1516e::InteractionClassHandle{cls});
  });
}
void M17Bridge::subscribeInteractionClass(std::uint64_t cls) {
  guard("subscribeInteractionClass", [&] {
    impl_->amb->subscribeInteractionClass(
        ::rti1516e::InteractionClassHandle{cls});
  });
}
void M17Bridge::unsubscribeInteractionClass(std::uint64_t cls) {
  guard("unsubscribeInteractionClass", [&] {
    impl_->amb->unsubscribeInteractionClass(
        ::rti1516e::InteractionClassHandle{cls});
  });
}

// ---------- §6 Object Management (M35 Agent BC) ---------------------------
//
// The DLC caller passes raw uint64 handles + std::map<uint64, bytes> value
// containers. Inside this TU `rti1516e::AttributeHandle` and
// `rti1516e::VariableLengthData` resolve to the M17 types (from
// <rti1516e/Types.h>) — the DLC and M17 headers cannot co-exist in one TU,
// so we rebuild the M17 shape here.

// §6.5 reserveObjectInstanceName — async; the reply arrives on the bound
// FederateAmbassador via objectInstanceNameReservationSucceeded/Failed
// (Cut-1 wire; M17 owns the callback dispatch).
void M17Bridge::reserveObjectInstanceName(const std::string& name) {
  guard("reserveObjectInstanceName",
        [&] { impl_->amb->reserveObjectInstanceName(name); });
}

// §6.5 reserveMultipleObjectInstanceNames — atomic batch reservation.
void M17Bridge::reserveMultipleObjectInstanceNames(
    const std::vector<std::string>& names) {
  guard("reserveMultipleObjectInstanceNames",
        [&] { impl_->amb->reserveMultipleObjectInstanceNames(names); });
}

// §6.6 releaseObjectInstanceName — synchronous.
void M17Bridge::releaseObjectInstanceName(const std::string& name) {
  guard("releaseObjectInstanceName",
        [&] { impl_->amb->releaseObjectInstanceName(name); });
}

// §6.8 registerObjectInstance — synchronous; returns the assigned
// ObjectInstanceHandle raw uint64. Empty `instance_name` requests an
// RTI-generated name.
std::uint64_t M17Bridge::registerObjectInstance(
    std::uint64_t object_class, const std::string& instance_name) {
  return guard("registerObjectInstance", [&] {
    auto h = impl_->amb->registerObjectInstance(
        ::rti1516e::ObjectClassHandle{object_class}, instance_name);
    return static_cast<std::uint64_t>(h.raw());
  });
}

// §6.10 updateAttributeValues (RO) — rebuild the M17
// AttributeHandleValueMap from the raw uint64 keyed map and delegate.
// M17 Cut-1 does not carry the DLC-mandatory userSuppliedTag on the
// wire (DLC catalogue §11); the `tag` param is accepted at the bridge
// boundary and dropped here to make the divergence explicit — a future
// M17 wire extension will pass it through.
void M17Bridge::updateAttributeValues(
    std::uint64_t object,
    const std::map<std::uint64_t, std::vector<std::uint8_t>>& values,
    const std::vector<std::uint8_t>& tag) {
  (void)tag;  // Divergence: M17 wire has no tag field yet.
  guard("updateAttributeValues", [&] {
    ::rti1516e::AttributeHandleValueMap m17_values;
    for (auto const& kv : values) {
      // M17's VariableLengthData == std::vector<std::uint8_t>, so bytes
      // are moved through the shape unchanged.
      m17_values.emplace(::rti1516e::AttributeHandle{kv.first},
                         ::rti1516e::VariableLengthData{kv.second});
    }
    impl_->amb->updateAttributeValues(
        ::rti1516e::ObjectInstanceHandle{object}, m17_values);
  });
}

// §6.12 sendInteraction (RO) — same shape as updateAttributeValues but
// on the ParameterHandle map. Same tag caveat.
void M17Bridge::sendInteraction(
    std::uint64_t interaction_class,
    const std::map<std::uint64_t, std::vector<std::uint8_t>>& parameters,
    const std::vector<std::uint8_t>& tag) {
  (void)tag;  // Divergence: M17 wire has no tag field yet.
  guard("sendInteraction", [&] {
    ::rti1516e::ParameterHandleValueMap m17_params;
    for (auto const& kv : parameters) {
      m17_params.emplace(::rti1516e::ParameterHandle{kv.first},
                         ::rti1516e::VariableLengthData{kv.second});
    }
    impl_->amb->sendInteraction(
        ::rti1516e::InteractionClassHandle{interaction_class}, m17_params);
  });
}

// ---------- §6 delete / request-update + TSO variants (M36 Agent DA) -------

namespace {
::rti1516e::AttributeHandleValueMap toM17ValueMap(
    const std::map<std::uint64_t, std::vector<std::uint8_t>>& values) {
  ::rti1516e::AttributeHandleValueMap out;
  for (auto const& kv : values) {
    out.emplace(::rti1516e::AttributeHandle{kv.first},
                ::rti1516e::VariableLengthData{kv.second});
  }
  return out;
}
::rti1516e::ParameterHandleValueMap toM17ParamMap(
    const std::map<std::uint64_t, std::vector<std::uint8_t>>& params) {
  ::rti1516e::ParameterHandleValueMap out;
  for (auto const& kv : params) {
    out.emplace(::rti1516e::ParameterHandle{kv.first},
                ::rti1516e::VariableLengthData{kv.second});
  }
  return out;
}
}  // namespace

// §6.14 deleteObjectInstance (RO) — tag rides the wire to subscribers'
// removeObjectInstance callbacks.
void M17Bridge::deleteObjectInstance(std::uint64_t object,
                                     const std::vector<std::uint8_t>& tag) {
  guard("deleteObjectInstance", [&] {
    impl_->amb->deleteObjectInstance(::rti1516e::ObjectInstanceHandle{object},
                                     tag, std::nullopt);
  });
}

// §6.14 deleteObjectInstance (TSO).
void M17Bridge::deleteObjectInstanceTimed(std::uint64_t object,
                                          const std::vector<std::uint8_t>& tag,
                                          double logical_time) {
  guard("deleteObjectInstance", [&] {
    impl_->amb->deleteObjectInstance(::rti1516e::ObjectInstanceHandle{object},
                                     tag,
                                     std::optional<double>(logical_time));
  });
}

// §6.19 requestAttributeValueUpdate — instance-scoped.
void M17Bridge::requestAttributeValueUpdate(
    std::uint64_t object, const std::vector<std::uint64_t>& attrs,
    const std::vector<std::uint8_t>& tag) {
  guard("requestAttributeValueUpdate", [&] {
    impl_->amb->requestAttributeValueUpdate(
        ::rti1516e::ObjectInstanceHandle{object}, toM17AttrSet(attrs), tag);
  });
}

// §6.19 requestAttributeValueUpdate — class-scoped.
void M17Bridge::requestClassAttributeValueUpdate(
    std::uint64_t object_class, const std::vector<std::uint64_t>& attrs,
    const std::vector<std::uint8_t>& tag) {
  guard("requestClassAttributeValueUpdate", [&] {
    impl_->amb->requestAttributeValueUpdate(
        ::rti1516e::ObjectClassHandle{object_class}, toM17AttrSet(attrs),
        tag);
  });
}

// §6.10 updateAttributeValues (TSO) — logical_time engages the server
// TSO gate. Tag divergence identical to the RO path.
void M17Bridge::updateAttributeValuesTimed(
    std::uint64_t object,
    const std::map<std::uint64_t, std::vector<std::uint8_t>>& values,
    const std::vector<std::uint8_t>& tag, double logical_time) {
  (void)tag;  // Divergence: M17 update wire has no tag field yet.
  guard("updateAttributeValues", [&] {
    impl_->amb->updateAttributeValues(
        ::rti1516e::ObjectInstanceHandle{object}, toM17ValueMap(values),
        logical_time);
  });
}

// §6.12 sendInteraction (TSO).
void M17Bridge::sendInteractionTimed(
    std::uint64_t interaction_class,
    const std::map<std::uint64_t, std::vector<std::uint8_t>>& params,
    const std::vector<std::uint8_t>& tag, double logical_time) {
  (void)tag;  // Divergence: M17 send wire has no tag field yet.
  guard("sendInteraction", [&] {
    impl_->amb->sendInteraction(
        ::rti1516e::InteractionClassHandle{interaction_class},
        toM17ParamMap(params), logical_time);
  });
}

// ---------- §8.21/§8.22 retractable TSO sends (M37 Agent EC-2) -------------

std::uint64_t M17Bridge::updateAttributeValuesRetractable(
    std::uint64_t object,
    const std::map<std::uint64_t, std::vector<std::uint8_t>>& values,
    const std::vector<std::uint8_t>& tag, double logical_time) {
  (void)tag;  // Divergence: M17 update wire has no tag field yet.
  return guard("updateAttributeValuesRetractable", [&] {
    return static_cast<std::uint64_t>(
        impl_->amb->updateAttributeValuesRetractable(
            ::rti1516e::ObjectInstanceHandle{object}, toM17ValueMap(values),
            logical_time));
  });
}

std::uint64_t M17Bridge::sendInteractionRetractable(
    std::uint64_t interaction_class,
    const std::map<std::uint64_t, std::vector<std::uint8_t>>& params,
    const std::vector<std::uint8_t>& tag, double logical_time) {
  (void)tag;  // Divergence: M17 send wire has no tag field yet.
  return guard("sendInteractionRetractable", [&] {
    return static_cast<std::uint64_t>(impl_->amb->sendInteractionRetractable(
        ::rti1516e::InteractionClassHandle{interaction_class},
        toM17ParamMap(params), logical_time));
  });
}


// shim name `rti1516e_m17::FederateAmbassador*` in the header (see
// M17Bridge.h), but this TU sees the M17 header without the shim, so the
// same class lives at `::rti1516e::FederateAmbassador`. Both names refer to
// the exact same class definition (identical layout + vtable); a
// reinterpret_cast is safe and lets us hand the pointer to M17's
// setFederateAmbassador. setFederateAmbassador is nothrow in M17 Cut-1;
// still routed through `guard` for uniformity.
void M17Bridge::bind_federate_ambassador(rti1516e_m17::FederateAmbassador* fed) {
  guard("bind_federate_ambassador", [&] {
    impl_->amb->setFederateAmbassador(
        reinterpret_cast<::rti1516e::FederateAmbassador*>(fed));
  });
}


// ---------- §10 Support Services (M35 Agent BH) ---------------------------
//
// Straight delegation to the M17 ambassador. Uint64 in/out; the M17 typed
// handle round-trip happens inside this TU where <rti1516e/Types.h> is
// visible. Handle values are M17's raw() form; DLC callers wrap via the
// makeXHandleFromUint64 helpers in RTIambassadorImpl.cpp.

std::uint64_t M17Bridge::getObjectClassHandle(const std::string& name) {
  return guard("getObjectClassHandle", [&] {
    auto h = impl_->amb->getObjectClassHandle(name);
    return static_cast<std::uint64_t>(h.raw());
  });
}
std::string M17Bridge::getObjectClassName(std::uint64_t handle) {
  return guard("getObjectClassName", [&] {
    return impl_->amb->getObjectClassName(
        ::rti1516e::ObjectClassHandle{handle});
  });
}
std::uint64_t M17Bridge::getAttributeHandle(std::uint64_t cls,
                                            const std::string& name) {
  return guard("getAttributeHandle", [&] {
    auto h = impl_->amb->getAttributeHandle(
        ::rti1516e::ObjectClassHandle{cls}, name);
    return static_cast<std::uint64_t>(h.raw());
  });
}
std::string M17Bridge::getAttributeName(std::uint64_t cls,
                                        std::uint64_t attr) {
  return guard("getAttributeName", [&] {
    return impl_->amb->getAttributeName(
        ::rti1516e::ObjectClassHandle{cls},
        ::rti1516e::AttributeHandle{attr});
  });
}
std::uint64_t M17Bridge::getInteractionClassHandle(const std::string& name) {
  return guard("getInteractionClassHandle", [&] {
    auto h = impl_->amb->getInteractionClassHandle(name);
    return static_cast<std::uint64_t>(h.raw());
  });
}
std::string M17Bridge::getInteractionClassName(std::uint64_t handle) {
  return guard("getInteractionClassName", [&] {
    return impl_->amb->getInteractionClassName(
        ::rti1516e::InteractionClassHandle{handle});
  });
}
std::uint64_t M17Bridge::getParameterHandle(std::uint64_t cls,
                                            const std::string& name) {
  return guard("getParameterHandle", [&] {
    auto h = impl_->amb->getParameterHandle(
        ::rti1516e::InteractionClassHandle{cls}, name);
    return static_cast<std::uint64_t>(h.raw());
  });
}
std::string M17Bridge::getParameterName(std::uint64_t cls,
                                        std::uint64_t param) {
  return guard("getParameterName", [&] {
    return impl_->amb->getParameterName(
        ::rti1516e::InteractionClassHandle{cls},
        ::rti1516e::ParameterHandle{param});
  });
}

std::uint64_t M17Bridge::getFederateHandle(const std::string& federate_name) {
  return guard("getFederateHandle", [&] {
    auto h = impl_->amb->getFederateHandle(federate_name);
    return static_cast<std::uint64_t>(h.raw());
  });
}
std::string M17Bridge::getFederateName(std::uint64_t handle) {
  return guard("getFederateName", [&] {
    return impl_->amb->getFederateName(::rti1516e::FederateHandle{handle});
  });
}

void M17Bridge::enableCallbacks() {
  guard("enableCallbacks", [&] { impl_->amb->enableCallbacks(); });
}
void M17Bridge::disableCallbacks() {
  guard("disableCallbacks", [&] { impl_->amb->disableCallbacks(); });
}



bool M17Bridge::evokeCallback(double approx_min_time, double approx_max_time) {
  return guard("evokeCallback", [&] {
    return impl_->amb->evokeCallback(approx_min_time, approx_max_time);
  });
}
bool M17Bridge::evokeMultipleCallbacks(double approx_min_time,
                                       double approx_max_time) {
  return guard("evokeMultipleCallbacks", [&] {
    return impl_->amb->evokeMultipleCallbacks(approx_min_time,
                                              approx_max_time);
  });
}

// ---------- §7 Ownership Management (M36 Agent CA-1) -----------------------
//
// Rewrap raw uint64s as M17 typed handles (same toM17AttrSet helper as §5).

void M17Bridge::unconditionalAttributeOwnershipDivestiture(
    std::uint64_t object, const std::vector<std::uint64_t>& attrs) {
  guard("unconditionalAttributeOwnershipDivestiture", [&] {
    impl_->amb->unconditionalAttributeOwnershipDivestiture(
        ::rti1516e::ObjectInstanceHandle{object}, toM17AttrSet(attrs));
  });
}
void M17Bridge::negotiatedAttributeOwnershipDivestiture(
    std::uint64_t object, const std::vector<std::uint64_t>& attrs,
    const std::vector<std::uint8_t>& tag, bool two_phase) {
  guard("negotiatedAttributeOwnershipDivestiture", [&] {
    impl_->amb->negotiatedAttributeOwnershipDivestiture(
        ::rti1516e::ObjectInstanceHandle{object}, toM17AttrSet(attrs),
        ::rti1516e::VariableLengthData{tag}, two_phase);
  });
}
void M17Bridge::confirmDivestiture(std::uint64_t object,
                                   const std::vector<std::uint64_t>& attrs) {
  // M37 EC-2 — real §7.6 ConfirmDivestiture RPC (M37 Agent EA wire).
  guard("confirmDivestiture", [&] {
    impl_->amb->confirmDivestiture(::rti1516e::ObjectInstanceHandle{object},
                                   toM17AttrSet(attrs));
  });
}
void M17Bridge::attributeOwnershipAcquisition(
    std::uint64_t object, const std::vector<std::uint64_t>& attrs) {
  guard("attributeOwnershipAcquisition", [&] {
    impl_->amb->attributeOwnershipAcquisition(
        ::rti1516e::ObjectInstanceHandle{object}, toM17AttrSet(attrs));
  });
}
void M17Bridge::attributeOwnershipAcquisitionIfAvailable(
    std::uint64_t object, const std::vector<std::uint64_t>& attrs) {
  // M37 EC-2 — real §7.9 if_available wire; the server answers the owned
  // remainder with AttributeOwnershipUnavailable (§7.10).
  guard("attributeOwnershipAcquisitionIfAvailable", [&] {
    impl_->amb->attributeOwnershipAcquisitionIfAvailable(
        ::rti1516e::ObjectInstanceHandle{object}, toM17AttrSet(attrs));
  });
}
void M17Bridge::cancelNegotiatedAttributeOwnershipDivestiture(
    std::uint64_t object, const std::vector<std::uint64_t>& attrs) {
  guard("cancelNegotiatedAttributeOwnershipDivestiture", [&] {
    impl_->amb->cancelNegotiatedAttributeOwnershipDivestiture(
        ::rti1516e::ObjectInstanceHandle{object}, toM17AttrSet(attrs));
  });
}
void M17Bridge::cancelAttributeOwnershipAcquisition(
    std::uint64_t object, const std::vector<std::uint64_t>& attrs) {
  guard("cancelAttributeOwnershipAcquisition", [&] {
    impl_->amb->cancelAttributeOwnershipAcquisition(
        ::rti1516e::ObjectInstanceHandle{object}, toM17AttrSet(attrs));
  });
}
void M17Bridge::attributeOwnershipDivestitureIfWanted(
    std::uint64_t object, const std::vector<std::uint64_t>& attrs) {
  guard("attributeOwnershipDivestitureIfWanted", [&] {
    impl_->amb->attributeOwnershipDivestitureIfWanted(
        ::rti1516e::ObjectInstanceHandle{object}, toM17AttrSet(attrs));
  });
}
M17Bridge::OwnershipQuery M17Bridge::queryAttributeOwnership(
    std::uint64_t object, std::uint64_t attribute) {
  return guard("queryAttributeOwnership", [&] {
    auto r = impl_->amb->queryAttributeOwnership(
        ::rti1516e::ObjectInstanceHandle{object},
        ::rti1516e::AttributeHandle{attribute});
    return OwnershipQuery{static_cast<std::uint64_t>(r.owner.raw()), r.owned};
  });
}
bool M17Bridge::isAttributeOwnedByFederate(std::uint64_t object,
                                           std::uint64_t attribute) {
  return guard("isAttributeOwnedByFederate", [&] {
    return impl_->amb->isAttributeOwnedByFederate(
        ::rti1516e::ObjectInstanceHandle{object},
        ::rti1516e::AttributeHandle{attribute});
  });
}

// ---------- §8 Time Management (M36 Agent CA-2) ----------------------------

void M17Bridge::enableTimeRegulation(double lookahead) {
  guard("enableTimeRegulation",
        [&] { impl_->amb->enableTimeRegulation(lookahead); });
}
void M17Bridge::disableTimeRegulation() {
  guard("disableTimeRegulation",
        [&] { impl_->amb->disableTimeRegulation(); });
}
void M17Bridge::enableTimeConstrained() {
  guard("enableTimeConstrained",
        [&] { impl_->amb->enableTimeConstrained(); });
}
void M17Bridge::disableTimeConstrained() {
  guard("disableTimeConstrained",
        [&] { impl_->amb->disableTimeConstrained(); });
}
void M17Bridge::modifyLookahead(double lookahead) {
  guard("modifyLookahead", [&] { impl_->amb->modifyLookahead(lookahead); });
}
void M17Bridge::timeAdvanceRequest(double time) {
  guard("timeAdvanceRequest", [&] { impl_->amb->timeAdvanceRequest(time); });
}
void M17Bridge::timeAdvanceRequestAvailable(double time) {
  guard("timeAdvanceRequestAvailable",
        [&] { impl_->amb->timeAdvanceRequestAvailable(time); });
}
void M17Bridge::nextMessageRequest(double time) {
  guard("nextMessageRequest", [&] { impl_->amb->nextMessageRequest(time); });
}
void M17Bridge::nextMessageRequestAvailable(double time) {
  guard("nextMessageRequestAvailable",
        [&] { impl_->amb->nextMessageRequestAvailable(time); });
}
void M17Bridge::flushQueueRequest(double time) {
  guard("flushQueueRequest", [&] { impl_->amb->flushQueueRequest(time); });
}
double M17Bridge::queryLogicalTime() {
  return guard("queryLogicalTime",
               [&] { return impl_->amb->queryLogicalTime(); });
}
double M17Bridge::queryLookahead() {
  return guard("queryLookahead",
               [&] { return impl_->amb->queryLookahead(); });
}
M17Bridge::TimeQuery M17Bridge::queryGALT() {
  return guard("queryGALT", [&] {
    auto r = impl_->amb->queryGALT();
    return TimeQuery{r.time, r.finite};
  });
}
M17Bridge::TimeQuery M17Bridge::queryLITS() {
  return guard("queryLITS", [&] {
    auto r = impl_->amb->queryLITS();
    return TimeQuery{r.time, r.finite};
  });
}
void M17Bridge::enableAsynchronousDelivery() {
  guard("enableAsynchronousDelivery",
        [&] { impl_->amb->enableAsynchronousDelivery(); });
}
void M17Bridge::disableAsynchronousDelivery() {
  guard("disableAsynchronousDelivery",
        [&] { impl_->amb->disableAsynchronousDelivery(); });
}
void M17Bridge::retract(std::uint64_t retraction_handle) {
  guard("retract", [&] {
    impl_->amb->retract(
        ::rti1516e::MessageRetractionHandle{retraction_handle});
  });
}

// ---------- §9 Data Distribution Management (M36 Agent CA-3) ---------------

namespace {
std::vector<::rti1516e::RegionHandle> toM17Regions(
    const std::vector<std::uint64_t>& regions) {
  std::vector<::rti1516e::RegionHandle> out;
  out.reserve(regions.size());
  for (auto v : regions) out.push_back(::rti1516e::RegionHandle{v});
  return out;
}
::rti1516e::RegionHandleSet toM17RegionSet(
    const std::vector<std::uint64_t>& regions) {
  ::rti1516e::RegionHandleSet out;
  for (auto v : regions) out.insert(::rti1516e::RegionHandle{v});
  return out;
}
::rti1516e::AttributeRegionMap toM17AttrRegionMap(
    const std::map<std::uint64_t, std::vector<std::uint64_t>>& m) {
  ::rti1516e::AttributeRegionMap out;
  for (auto const& kv : m) {
    out.emplace(::rti1516e::AttributeHandle{kv.first},
                toM17RegionSet(kv.second));
  }
  return out;
}
}  // namespace

std::uint64_t M17Bridge::getRoutingSpaceHandle(const std::string& name) {
  return guard("getRoutingSpaceHandle", [&] {
    auto h = impl_->amb->getRoutingSpaceHandle(name);
    return static_cast<std::uint64_t>(h.raw());
  });
}
std::uint64_t M17Bridge::getDimensionHandle(std::uint64_t routing_space,
                                            const std::string& name) {
  return guard("getDimensionHandle", [&] {
    auto h = impl_->amb->getDimensionHandle(
        ::rti1516e::RoutingSpaceHandle{routing_space}, name);
    return static_cast<std::uint64_t>(h.raw());
  });
}
std::uint64_t M17Bridge::createRegion(
    std::uint64_t routing_space, const std::vector<std::uint64_t>& dimensions) {
  return guard("createRegion", [&] {
    std::vector<::rti1516e::DimensionHandle> dims;
    dims.reserve(dimensions.size());
    for (auto v : dimensions) dims.push_back(::rti1516e::DimensionHandle{v});
    auto h = impl_->amb->createRegion(
        ::rti1516e::RoutingSpaceHandle{routing_space}, dims);
    return static_cast<std::uint64_t>(h.raw());
  });
}
void M17Bridge::setRangeBounds(std::uint64_t region, std::uint64_t dimension,
                               std::uint64_t lower, std::uint64_t upper) {
  guard("setRangeBounds", [&] {
    impl_->amb->setRangeBounds(::rti1516e::RegionHandle{region},
                               ::rti1516e::DimensionHandle{dimension},
                               ::rti1516e::DimensionRange{lower, upper});
  });
}
M17Bridge::RegionBounds M17Bridge::queryRangeBounds(std::uint64_t region,
                                                    std::uint64_t dimension) {
  return guard("queryRangeBounds", [&] {
    auto r = impl_->amb->queryBounds(::rti1516e::RegionHandle{region},
                                     ::rti1516e::DimensionHandle{dimension});
    return RegionBounds{r.bounds.lower, r.bounds.upper, r.found};
  });
}
void M17Bridge::commitRegionModifications(
    const std::vector<std::uint64_t>& regions) {
  guard("commitRegionModifications", [&] {
    impl_->amb->commitRegionModifications(toM17Regions(regions));
  });
}
void M17Bridge::deleteRegion(std::uint64_t region) {
  guard("deleteRegion",
        [&] { impl_->amb->deleteRegion(::rti1516e::RegionHandle{region}); });
}

void M17Bridge::subscribeObjectClassAttributesWithRegions(
    std::uint64_t cls, const std::vector<std::uint64_t>& attrs,
    const std::vector<std::uint64_t>& regions) {
  guard("subscribeObjectClassAttributesWithRegions", [&] {
    impl_->amb->subscribeObjectClassAttributesWithRegions(
        ::rti1516e::ObjectClassHandle{cls}, toM17AttrSet(attrs),
        toM17RegionSet(regions));
  });
}
void M17Bridge::unsubscribeObjectClassAttributesWithRegions(
    std::uint64_t cls, const std::vector<std::uint64_t>& attrs,
    const std::vector<std::uint64_t>& regions) {
  guard("unsubscribeObjectClassAttributesWithRegions", [&] {
    impl_->amb->unsubscribeObjectClassAttributesWithRegions(
        ::rti1516e::ObjectClassHandle{cls}, toM17AttrSet(attrs),
        toM17RegionSet(regions));
  });
}
void M17Bridge::subscribeInteractionClassWithRegions(
    std::uint64_t cls, const std::vector<std::uint64_t>& regions) {
  guard("subscribeInteractionClassWithRegions", [&] {
    impl_->amb->subscribeInteractionClassWithRegions(
        ::rti1516e::InteractionClassHandle{cls}, toM17RegionSet(regions));
  });
}
void M17Bridge::unsubscribeInteractionClassWithRegions(
    std::uint64_t cls, const std::vector<std::uint64_t>& regions) {
  guard("unsubscribeInteractionClassWithRegions", [&] {
    impl_->amb->unsubscribeInteractionClassWithRegions(
        ::rti1516e::InteractionClassHandle{cls}, toM17RegionSet(regions));
  });
}

std::uint64_t M17Bridge::registerObjectInstanceWithRegions(
    std::uint64_t cls,
    const std::map<std::uint64_t, std::vector<std::uint64_t>>&
        attribute_regions,
    const std::string& object_name) {
  return guard("registerObjectInstanceWithRegions", [&] {
    auto r = impl_->amb->registerObjectInstanceWithRegions(
        ::rti1516e::ObjectClassHandle{cls},
        toM17AttrRegionMap(attribute_regions), object_name);
    return static_cast<std::uint64_t>(r.object.raw());
  });
}
void M17Bridge::associateRegionsForUpdates(
    std::uint64_t object,
    const std::map<std::uint64_t, std::vector<std::uint64_t>>&
        attribute_regions) {
  guard("associateRegionsForUpdates", [&] {
    impl_->amb->associateRegionsForUpdates(
        ::rti1516e::ObjectInstanceHandle{object},
        toM17AttrRegionMap(attribute_regions));
  });
}
void M17Bridge::unassociateRegionsForUpdates(
    std::uint64_t object,
    const std::map<std::uint64_t, std::vector<std::uint64_t>>&
        attribute_regions) {
  guard("unassociateRegionsForUpdates", [&] {
    impl_->amb->unassociateRegionsForUpdates(
        ::rti1516e::ObjectInstanceHandle{object},
        toM17AttrRegionMap(attribute_regions));
  });
}

void M17Bridge::sendInteractionWithRegions(
    std::uint64_t interaction_class,
    const std::map<std::uint64_t, std::vector<std::uint8_t>>& params,
    const std::vector<std::uint64_t>& regions,
    std::optional<double> logical_time) {
  guard("sendInteractionWithRegions", [&] {
    ::rti1516e::ParameterHandleValueMap m17_params;
    for (auto const& kv : params) {
      m17_params.emplace(::rti1516e::ParameterHandle{kv.first},
                         ::rti1516e::VariableLengthData{kv.second});
    }
    impl_->amb->sendInteractionWithRegions(
        ::rti1516e::InteractionClassHandle{interaction_class}, m17_params,
        toM17RegionSet(regions), logical_time);
  });
}
void M17Bridge::requestAttributeValueUpdateWithRegions(
    std::uint64_t object_class, const std::vector<std::uint64_t>& attrs,
    const std::vector<std::uint64_t>& regions,
    const std::vector<std::uint8_t>& tag) {
  guard("requestAttributeValueUpdateWithRegions", [&] {
    impl_->amb->requestAttributeValueUpdateWithRegions(
        ::rti1516e::ObjectClassHandle{object_class}, toM17AttrSet(attrs),
        toM17RegionSet(regions), ::rti1516e::VariableLengthData{tag});
  });
}

// ---------- §4.8 listFederationExecutions (M36 Agent CA-4) -----------------
//
// Direct-stub path: the M17 ambassador has no listFederations client method,
// and its gRPC channel lives behind its private pimpl. A short-lived channel
// to the recorded connect URL is cheap (one unary RPC) and keeps the M17
// surface untouched. Failure modes fold into the guard() vocabulary.

std::vector<std::string> M17Bridge::listFederationExecutions() {
  return guard("listFederationExecutions", [&]() -> std::vector<std::string> {
    if (impl_->connect_url.empty()) {
      throw ::rti1516e::m17::NotConnected(
          "listFederationExecutions: connect() not called");
    }
    // Strip the grpc:// / grpcs:// scheme (same accepted forms as M17's
    // connect; TLS falls back to plaintext exactly like M17 Cut-1).
    std::string target = impl_->connect_url;
    if (target.rfind("grpc://", 0) == 0) target = target.substr(7);
    else if (target.rfind("grpcs://", 0) == 0) target = target.substr(8);
    auto channel =
        grpc::CreateChannel(target, grpc::InsecureChannelCredentials());
    auto stub = ::rti::v1::FederationService::NewStub(channel);
    ::rti::v1::ListFederationsRequest req;
    req.set_wire_version(::rti::v1::WIRE_VERSION_V1);
    ::rti::v1::ListFederationsResponse resp;
    grpc::ClientContext ctx;
    const auto s = stub->ListFederations(&ctx, req, &resp);
    if (!s.ok()) {
      throw std::runtime_error(
          std::string("RTIinternalError: ListFederations RPC failed: ") +
          s.error_message());
    }
    std::vector<std::string> names;
    names.reserve(static_cast<std::size_t>(resp.federations_size()));
    for (auto const& f : resp.federations()) names.push_back(f.name());
    return names;
  });
}

}  // namespace rti1516e
