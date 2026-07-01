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
  // The `rti1516e::RTIambassador` here is the M17 Cut-1 concrete class from
  // <rti1516e/RtiAmbassador.h> — a DIFFERENT type from the DLC spec-abstract
  // class of the same name in <RTI/RTIambassador.h>. Only one is visible in
  // any given TU; this file sees only the M17 one.
  ::rti1516e::RTIambassador amb;
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
  } catch (::rti1516e::AlreadyConnected const& e) {
    throw std::runtime_error(std::string("AlreadyConnected: ") + e.what() +
                             " [op=" + op + "]");
  } catch (::rti1516e::ConnectionFailed const& e) {
    throw std::runtime_error(std::string("ConnectionFailed: ") + e.what() +
                             " [op=" + op + "]");
  } catch (::rti1516e::NotConnected const& e) {
    throw std::runtime_error(std::string("NotConnected: ") + e.what() +
                             " [op=" + op + "]");
  } catch (::rti1516e::FederationExecutionAlreadyExists const& e) {
    throw std::runtime_error(
        std::string("FederationExecutionAlreadyExists: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::FederationExecutionDoesNotExist const& e) {
    throw std::runtime_error(
        std::string("FederationExecutionDoesNotExist: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::FederateAlreadyExecutionMember const& e) {
    throw std::runtime_error(
        std::string("FederateAlreadyExecutionMember: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::FederateNotExecutionMember const& e) {
    throw std::runtime_error(
        std::string("FederateNotExecutionMember: ") + e.what() +
        " [op=" + op + "]");
  } catch (::rti1516e::RTIinternalError const& e) {
    throw std::runtime_error(std::string("RTIinternalError: ") + e.what() +
                             " [op=" + op + "]");
  }
}
}  // namespace

// ---------- §4.2 / §4.3 connect / disconnect ------------------------------

void M17Bridge::connect(const std::string& url) {
  guard("connect", [&] { impl_->amb.connect(url); });
}
void M17Bridge::disconnect() {
  guard("disconnect", [&] { impl_->amb.disconnect(); });
}
bool M17Bridge::isConnected() const noexcept {
  return impl_->amb.isConnected();
}

// ---------- §4.5 / §4.6 create / destroy ----------------------------------

void M17Bridge::createFederationExecution(
    const std::string& name, const std::vector<std::string>& fom_modules) {
  guard("createFederationExecution",
        [&] { impl_->amb.createFederationExecution(name, fom_modules); });
}
void M17Bridge::destroyFederationExecution(const std::string& name) {
  guard("destroyFederationExecution",
        [&] { impl_->amb.destroyFederationExecution(name); });
}

// ---------- §4.9 / §4.10 join / resign ------------------------------------

std::uint64_t M17Bridge::joinFederationExecution(
    const std::string& federate_name, const std::string& federation_name) {
  return guard("joinFederationExecution", [&] {
    auto h = impl_->amb.joinFederationExecution(federate_name, federation_name);
    return static_cast<std::uint64_t>(h.raw());
  });
}
void M17Bridge::resignFederationExecution() {
  // Cut-1 M17 default: UNCONDITIONALLY_DIVEST_ATTRIBUTES. The DLC caller
  // accepts the wider ResignAction and folds all 6 spec values onto this
  // single M17 default; the divergence is tracked in
  // docs/DLC_DIVERGENCE_CATALOGUE.md §3.
  guard("resignFederationExecution",
        [&] { impl_->amb.resignFederationExecution(); });
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
    impl_->amb.registerFederationSynchronizationPoint(label, tag, fs);
  });
}
void M17Bridge::synchronizationPointAchieved(const std::string& label) {
  guard("synchronizationPointAchieved",
        [&] { impl_->amb.synchronizationPointAchieved(label); });
}

// ---------- §4.16-28 save family ------------------------------------------

void M17Bridge::requestFederationSave(const std::string& label,
                                       std::optional<double> save_time) {
  guard("requestFederationSave",
        [&] { impl_->amb.requestFederationSave(label, save_time); });
}
void M17Bridge::federateSaveComplete() {
  guard("federateSaveComplete", [&] { impl_->amb.federateSaveComplete(); });
}
void M17Bridge::federateSaveNotComplete() {
  guard("federateSaveNotComplete",
        [&] { impl_->amb.federateSaveNotComplete(); });
}
void M17Bridge::abortFederationSave() {
  guard("abortFederationSave", [&] { impl_->amb.abortFederationSave(); });
}
M17SaveState M17Bridge::querySaveState(const std::string& label) {
  return guard("querySaveState", [&] {
    auto st = impl_->amb.querySaveState(label);
    return static_cast<M17SaveState>(static_cast<int>(st));
  });
}

// ---------- §4.24-30 restore family ---------------------------------------

void M17Bridge::requestFederationRestore(const std::string& label) {
  guard("requestFederationRestore",
        [&] { impl_->amb.requestFederationRestore(label); });
}
void M17Bridge::federateRestoreComplete() {
  guard("federateRestoreComplete",
        [&] { impl_->amb.federateRestoreComplete(); });
}
void M17Bridge::abortFederationRestore() {
  guard("abortFederationRestore",
        [&] { impl_->amb.abortFederationRestore(); });
}
M17RestoreState M17Bridge::queryRestoreState(const std::string& label) {
  return guard("queryRestoreState", [&] {
    auto st = impl_->amb.queryRestoreState(label);
    return static_cast<M17RestoreState>(static_cast<int>(st));
  });
}

}  // namespace rti1516e
