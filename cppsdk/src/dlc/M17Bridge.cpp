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
    // M35 Agent BH — surface as its own prefix so the DLC caller can
    // translate to §C.3 NameNotFound instead of the RTIinternalError
    // fallback. §10 support-services triggers this on unknown FOM names.
    throw std::runtime_error(std::string("NameNotFound: ") + e.what() +
                             " [op=" + op + "]");
  } catch (::rti1516e::m17::InvalidObjectClassHandle const& e) {
    throw std::runtime_error(
        std::string("InvalidObjectClassHandle: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::m17::InvalidAttributeHandle const& e) {
    throw std::runtime_error(std::string("InvalidAttributeHandle: ") +
                             e.what() + " [op=" + op + "]");
  } catch (::rti1516e::m17::InvalidInteractionClassHandle const& e) {
    throw std::runtime_error(
        std::string("InvalidInteractionClassHandle: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::m17::InvalidParameterHandle const& e) {
    throw std::runtime_error(std::string("InvalidParameterHandle: ") +
                             e.what() + " [op=" + op + "]");
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
}
void M17Bridge::disconnect() {
  guard("disconnect", [&] { impl_->amb->disconnect(); });
}
bool M17Bridge::isConnected() const noexcept {
  return impl_->amb->isConnected();
}

// M35 Agent BD — install the M17 callback sink. `fed` is declared under the
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
void M17Bridge::synchronizationPointAchieved(const std::string& label) {
  guard("synchronizationPointAchieved",
        [&] { impl_->amb->synchronizationPointAchieved(label); });
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

void M17Bridge::enableCallbacks() {
  guard("enableCallbacks", [&] { impl_->amb->enableCallbacks(); });
}
void M17Bridge::disableCallbacks() {
  guard("disableCallbacks", [&] { impl_->amb->disableCallbacks(); });
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

}  // namespace rti1516e
