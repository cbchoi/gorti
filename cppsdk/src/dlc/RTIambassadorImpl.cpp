// IEEE 1516.1-2010 §10.6 / Annex A — DLC RTIambassador stub impl.
//
// gorti M32. Every method throws RTIinternalError("M32 stub — impl in M33+").
// M32 GREEN target is LINK-only; runtime bodies land M33+ as wstring-adapters
// over gorti's M17 gRPC surface.

#include "RTIambassadorImpl.h"

#include "FederateAmbassadorBridge.h"
#include "M17Bridge.h"

#include <RTI/Exception.h>
#include <RTI/LogicalTime.h>
#include <RTI/RangeBounds.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64Interval.h>

#include <cstdint>
#include <sstream>
#include <stdexcept>
#include <string>
#include <utility>

namespace rti1516e {

namespace {
[[noreturn]] void m32_stub(char const* method) {
  std::wstring msg = L"M32 stub — RTIambassador::";
  // narrow → wide append (ASCII-only method names).
  for (char const* p = method; *p; ++p)
    msg.push_back(static_cast<wchar_t>(static_cast<unsigned char>(*p)));
  msg += L"() impl deferred to M33+.";
  throw RTIinternalError(msg);
}

// wstring → narrow. §4 identifiers (federation name, federate name, sync
// label, FOM path) are ASCII per Pitch fixtures; the shim tolerates
// high-byte content by widening character-by-character, then the M17 layer
// rejects invalid UTF-8 at the wire.
std::string ws2s(std::wstring const& w) {
  std::string s;
  s.reserve(w.size());
  for (wchar_t c : w) {
    // Truncate to single byte; §4 identifiers are ASCII per FOM XML +
    // Pitch-doc convention. Full UTF-8 encode is FR-DLC-17 M35 territory.
    s.push_back(static_cast<char>(c & 0xff));
  }
  return s;
}
std::wstring s2ws(std::string const& s) {
  std::wstring w;
  w.reserve(s.size());
  for (char c : s) w.push_back(static_cast<wchar_t>(static_cast<unsigned char>(c)));
  return w;
}

// Parse §4.2 `localSettingsDesignator`. Pitch's Portable-Options form is
// "crcAddress=host:port"; gorti also accepts a bare "grpc://host:port" or
// "grpcs://host:port" pass-through. Returns a URL suitable for the M17
// `RTIambassador::connect(url)` call. When `settings` is empty or the
// scheme is missing, defaults to "grpc://127.0.0.1:8989" (matches the M17
// documented default; the fixture wire tests write ":8989" too).
std::string parseLocalSettings(std::wstring const& settings) {
  std::string s = ws2s(settings);
  // Trim leading/trailing whitespace (Pitch tolerates it).
  auto lstrip = s.find_first_not_of(" \t\r\n");
  auto rstrip = s.find_last_not_of(" \t\r\n");
  if (lstrip == std::string::npos) return "grpc://127.0.0.1:8989";
  s = s.substr(lstrip, rstrip - lstrip + 1);

  // Direct grpc scheme — pass through.
  if (s.rfind("grpc://", 0) == 0 || s.rfind("grpcs://", 0) == 0) return s;

  // Portable-Options: look for "crcAddress=" (case-insensitive is Pitch,
  // but the fixtures always use camelCase; keep it exact for now).
  auto const kKey = std::string("crcAddress=");
  auto pos = s.find(kKey);
  if (pos != std::string::npos) {
    // Value runs from after "=" to the next comma or end (Pitch's
    // Portable-Options is comma-separated key=value pairs).
    auto start = pos + kKey.size();
    auto end = s.find(',', start);
    auto host = s.substr(start, end == std::string::npos
                                    ? std::string::npos
                                    : end - start);
    // Trim inner whitespace.
    auto hl = host.find_first_not_of(" \t");
    auto hr = host.find_last_not_of(" \t");
    if (hl == std::string::npos) return "grpc://127.0.0.1:8989";
    host = host.substr(hl, hr - hl + 1);
    return std::string("grpc://") + host;
  }

  // Fallback: treat the whole thing as a host:port.
  return std::string("grpc://") + s;
}

// Convert a runtime_error carrying an M17 exception prefix into the
// matching <RTI/Exception.h> type. See M17Bridge.cpp guard() for the
// prefix vocabulary. The DLC caller wraps every m17_->* call with this
// so exceptions surface as spec types the fixture catch blocks expect.
[[noreturn]] void translateBridgeError(std::runtime_error const& e) {
  std::string what = e.what();
  std::wstring msg = s2ws(what);
  // Prefix-matched translation. Order matters — some prefixes are
  // sub-strings of others' contexts.
  if (what.rfind("AlreadyConnected:", 0) == 0) throw AlreadyConnected(msg);
  if (what.rfind("ConnectionFailed:", 0) == 0) throw ConnectionFailed(msg);
  if (what.rfind("NotConnected:", 0) == 0) throw NotConnected(msg);
  if (what.rfind("FederationExecutionAlreadyExists:", 0) == 0)
    throw FederationExecutionAlreadyExists(msg);
  if (what.rfind("FederationExecutionDoesNotExist:", 0) == 0)
    throw FederationExecutionDoesNotExist(msg);
  if (what.rfind("FederateAlreadyExecutionMember:", 0) == 0)
    throw FederateAlreadyExecutionMember(msg);
  if (what.rfind("FederateNotExecutionMember:", 0) == 0)
    throw FederateNotExecutionMember(msg);
  if (what.rfind("NameNotFound:", 0) == 0) throw NameNotFound(msg);

  // ===== M37 Agent EC-3 — detail-string sniffs =====
  //
  // The M17 client folds most server rejections into RTIinternalError with
  // the gRPC status message as detail. The DLC fixtures expect the precise
  // spec exception, so we sniff gorti's error strings here at the DLC
  // boundary (the M17 client's throwFromStatus is EA/DA-owned). STOPGAP:
  // these substrings are pinned to rti/internal/core/errors.go — see
  // cppsdk/src/dlc/README.md for the contract. Order matters: the most
  // specific phrasings match first.
  auto contains = [&what](char const* needle) {
    return what.find(needle) != std::string::npos;
  };
  // §5/§6 publication gates (errors.go: "object class not published by
  // federate" / "interaction class not published").
  if (contains("interaction class not published"))
    throw InteractionClassNotPublished(msg);
  if (contains("not published")) throw ObjectClassNotPublished(msg);
  // §8 time gates. errors.go: "lookahead must be non-negative and finite"
  // (bad enableTimeRegulation/modifyLookahead argument) → InvalidLookahead;
  // "requested time is not greater than current logical time" (advance to
  // the past) → LogicalTimeAlreadyPassed; "invalid logical time: TSO
  // timestamp precedes current time plus lookahead" (and any other
  // lookahead-floor phrasing) → InvalidLogicalTime.
  if (contains("lookahead must be non-negative"))
    throw InvalidLookahead(msg);
  if (contains("requested time is not greater than current logical time"))
    throw LogicalTimeAlreadyPassed(msg);
  if (contains("invalid logical time") || contains("lookahead"))
    throw InvalidLogicalTime(msg);

  // Every other case (including bare RTIinternalError) folds to the
  // spec-legal RTIinternalError catch-all.
  throw RTIinternalError(msg);
}

// FR-DLC-14 (M36 Agent DA) — §10.4 re-entrancy gate. A federate must not
// re-enter the ambassador from inside a callback; the RTI throws the
// spec-mandated CallNotAllowedFromWithinCallback (catalogue 17.2). The
// witness flag is set by gorti::dlc::CallbackScope around every
// DLCFederateAmbassadorBridge dispatch. §10.4-exempt services
// (evokeCallback / evokeMultipleCallbacks / enableCallbacks /
// disableCallbacks) call the *Unguarded forms below.
void requireNotInCallback() {
  if (gorti::dlc::tls_in_callback) {
    throw CallNotAllowedFromWithinCallback(
        L"gorti DLC §10.4: RTI service invoked from within a federate "
        L"callback (FR-DLC-14, catalogue 17.2).");
  }
}

// Bridge-call wrapper: runs `fn`, catches std::runtime_error from the M17
// bridge, translates to spec exception. Void-returning form.
template <typename Fn>
void bridgeUnguarded(Fn&& fn) {
  try {
    fn();
  } catch (std::runtime_error const& e) {
    translateBridgeError(e);
  }
}
template <typename Fn>
void bridge(Fn&& fn) {
  requireNotInCallback();
  bridgeUnguarded(std::forward<Fn>(fn));
}
// Bridge-call wrapper with a return value. Uses decltype-auto to preserve
// the caller-side value type (e.g. FederateHandle).
template <typename Fn>
auto bridgeRUnguarded(Fn&& fn) -> decltype(fn()) {
  try {
    return fn();
  } catch (std::runtime_error const& e) {
    translateBridgeError(e);
  }
}
template <typename Fn>
auto bridgeR(Fn&& fn) -> decltype(fn()) {
  requireNotInCallback();
  return bridgeRUnguarded(std::forward<Fn>(fn));
}

}  // namespace

// Bridge-side FederateHandle uint64 ⇄ typed-handle adapter (declarations).
//
// The DLC handle types (declared in <RTI/Handle.h> via DEFINE_HANDLE_CLASS)
// keep both the VLD-ctor and getImplementation() `protected`. The macro
// declares `FederateHandleFriend` as a friend, and this TU already defines
// that Friend at file-bottom to expose the VLD decode() path.
//
// Bodies are defined AFTER the file-bottom DEFINE_HANDLE_FRIEND macro
// expansion (which produces the friend classes at namespace scope). The
// round-trip uses the handle's spec-defined 8-byte big-endian encoding
// (§10.5). Declared at `namespace rti1516e` scope (NOT anon) so the calls
// from the §4 impls below resolve to the same symbols that get bodies at
// file bottom.
FederateHandle makeFederateHandleFromUint64(std::uint64_t v);
std::uint64_t rawFederateHandle(FederateHandle const& h);

// M35 Agent BC — §6 handle adapters. Same shape as the FederateHandle
// pair above: bodies defined after DEFINE_HANDLE_FRIEND expansion at
// file bottom so the friend classes are in scope for the VLD ctor.
// Only the return-of-registerObjectInstance path needs a Make helper
// (M17 hands back a raw uint64); every input handle type only needs
// the Raw extractor which uses the public `encode()` API.
ObjectInstanceHandle makeObjectInstanceHandleFromUint64(std::uint64_t v);
std::uint64_t rawObjectClassHandle(ObjectClassHandle const& h);
std::uint64_t rawObjectInstanceHandle(ObjectInstanceHandle const& h);
std::uint64_t rawInteractionClassHandle(InteractionClassHandle const& h);
std::uint64_t rawAttributeHandle(AttributeHandle const& h);
std::uint64_t rawParameterHandle(ParameterHandle const& h);

// Base class ctor/dtor — must have out-of-line definitions for the vtable.
// M35 Agent BH — §10 support-service adapters. `make*FromUint64` widens
// M17's raw uint64 return into the DLC typed handle via the Friend shim.
// `raw*` extracts M17's uint64 from a DLC typed handle. Same 8-byte
// big-endian VLD shape as FederateHandle. Bodies at file bottom.
ObjectClassHandle       makeObjectClassHandleFromUint64(std::uint64_t v);
AttributeHandle         makeAttributeHandleFromUint64(std::uint64_t v);
InteractionClassHandle  makeInteractionClassHandleFromUint64(std::uint64_t v);
ParameterHandle         makeParameterHandleFromUint64(std::uint64_t v);
std::uint64_t rawParameterHandle_(ParameterHandle const& h);

// M36 Agent CA — §8/§9 handle adapters. Same 8-byte big-endian VLD shape.
// Bodies at file bottom after the DEFINE_HANDLE_FRIEND expansion.
DimensionHandle makeDimensionHandleFromUint64(std::uint64_t v);
RegionHandle    makeRegionHandleFromUint64(std::uint64_t v);
std::uint64_t rawDimensionHandle(DimensionHandle const& h);
std::uint64_t rawRegionHandle(RegionHandle const& h);
std::uint64_t rawMessageRetractionHandle(MessageRetractionHandle const& h);
// M37 Agent EC-2 — widen M17's retractable-send return (raw uint64) into
// the DLC typed handle. Body at file bottom.
MessageRetractionHandle makeMessageRetractionHandleFromUint64(std::uint64_t v);

RTIambassador::RTIambassador() RTI_NOEXCEPT {}
RTIambassador::~RTIambassador() {}

DLCRTIambassadorImpl::DLCRTIambassadorImpl()
    : m17_(std::make_unique<M17Bridge>()) {}
DLCRTIambassadorImpl::~DLCRTIambassadorImpl() = default;

// ===== M37 Agent EC-4 — deferred synthesized-callback queue =====
//
// See the header block comment for the design. Enqueue is cheap (mutex +
// deque push); dispatch pops under the lock, then runs the callback OUTSIDE
// the lock inside a CallbackScope so FR-DLC-14 re-entrancy detection fires
// if the federate re-enters the ambassador from the synthesized callback.

void DLCRTIambassadorImpl::enqueueSynthesized_(std::function<void()> cb) {
  std::lock_guard<std::mutex> lk(synth_mu_);
  synth_queue_.push_back(std::move(cb));
}

void DLCRTIambassadorImpl::testEnqueueSynthesizedCallback(
    std::function<void()> cb) {
  enqueueSynthesized_(std::move(cb));
}

bool DLCRTIambassadorImpl::drainOneSynthesized_() {
  std::function<void()> cb;
  {
    std::lock_guard<std::mutex> lk(synth_mu_);
    if (synth_queue_.empty()) return false;
    cb = std::move(synth_queue_.front());
    synth_queue_.pop_front();
  }
  gorti::dlc::CallbackScope scope;  // FR-DLC-14 — mark the callback context
  cb();
  return true;
}

std::size_t DLCRTIambassadorImpl::drainAllSynthesized_() {
  std::size_t n = 0;
  while (drainOneSynthesized_()) ++n;
  return n;
}

// ===== §4 Federation Management =====
//
// M34 Agents AA + AD — wstring-adapter shim over the M17 concrete
// rti1516e::RTIambassador. Every entry point converts DLC types
// (wstring, VariableLengthData, FederateHandle) to M17 types (string,
// vector<uint8_t>, uint64) via ws2s/tag2bytes/FederateHandleFriend, then
// delegates through m17_ (an opaque M17Bridge that hides M17's header from
// this TU — see M17Bridge.h). M17 typed exceptions get re-raised as
// spec-defined <RTI/Exception.h> types by translateBridgeError().

// §4.2 — connect. Parses `localSettingsDesignator` (Pitch Portable-Options
// "crcAddress=host:port" or bare "grpc://host:port") into an M17 dial URL.
// Stores the FederateAmbassador reference + CallbackModel for Agent AD's
// callback dispatch bridge. Callback binding is Agent AD's responsibility;
// this method only records the FA and calls m17_->connect(url) so the
// wire channel is up before the fixture's create/join sequence.
//
// TODO(M35): wire in DLCFederateAmbassadorBridge to install `fed_amb_` as
// the M17 ambassador for callback dispatch. Bridge lives at
// cppsdk/src/dlc/FederateAmbassadorBridge.{h,cpp}.
void DLCRTIambassadorImpl::connect(FederateAmbassador& fed,
                                   CallbackModel callbackModel,
                                   std::wstring const& localSettings) {
  fed_amb_ = &fed;
  callback_model_ = callbackModel;
  // Construct the bridge first so if M17 connect fails the bridge dtor
  // (harmless — no M17 backref yet) runs during disconnect() cleanup.
  callback_bridge_ =
      std::make_unique<gorti::dlc::DLCFederateAmbassadorBridge>(&fed);
  std::string const url = parseLocalSettings(localSettings);
  bridge([&] {
    m17_->connect(url);
    m17_->bind_federate_ambassador(callback_bridge_.get());
  });
}

// §4.3 — disconnect. Clears the bound FederateAmbassador so late
// callback deliveries can no-op safely (Agent AD checks fed_amb_ before
// dispatching).
void DLCRTIambassadorImpl::disconnect() {
  bridge([&] {
    if (callback_bridge_) m17_->bind_federate_ambassador(nullptr);
    m17_->disconnect();
  });
  callback_bridge_.reset();
  fed_amb_ = nullptr;
  ddm_default_space_ = 0;  // M36 CA-3 — re-resolve after a reconnect
  // M37 EC-4 — drop undelivered synthesized callbacks; they captured the
  // (now unbound) federate ambassador pointer.
  {
    std::lock_guard<std::mutex> lk(synth_mu_);
    synth_queue_.clear();
  }
}

// §4.5 overload 1 — single FOM module. Widens to the vector-form for
// uniform M17 dispatch. `logicalTimeImplementationName` is accepted by
// the DLC surface but M17 Cut-1 uses HLAfloat64 unconditionally; the
// param is ignored (documented divergence, DLC catalogue §3).
void DLCRTIambassadorImpl::createFederationExecution(
    std::wstring const& federationExecutionName, std::wstring const& fomModule,
    std::wstring const& /*logicalTimeImplementationName*/) {
  std::vector<std::string> mods{ws2s(fomModule)};
  bridge([&] {
    m17_->createFederationExecution(ws2s(federationExecutionName), mods);
  });
}

// §4.5 overload 2 — vector of FOM modules. Same M17 target as overload 1.
void DLCRTIambassadorImpl::createFederationExecution(
    std::wstring const& federationExecutionName,
    std::vector<std::wstring> const& fomModules,
    std::wstring const& /*logicalTimeImplementationName*/) {
  std::vector<std::string> mods;
  mods.reserve(fomModules.size());
  for (auto const& w : fomModules) mods.push_back(ws2s(w));
  bridge([&] {
    m17_->createFederationExecution(ws2s(federationExecutionName), mods);
  });
}

// §4.5 overload 3 — with a MIM (Management Object Model) module. M17
// Cut-1 has no separate MIM slot; the shim prepends the MIM to the
// module list so the manager loads it alongside the FOM. Documented
// divergence in DLC catalogue §3 row "createFederationExecutionWithMIM".
void DLCRTIambassadorImpl::createFederationExecutionWithMIM(
    std::wstring const& federationExecutionName,
    std::vector<std::wstring> const& fomModules,
    std::wstring const& mimModule,
    std::wstring const& /*logicalTimeImplementationName*/) {
  std::vector<std::string> mods;
  mods.reserve(fomModules.size() + 1);
  mods.push_back(ws2s(mimModule));
  for (auto const& w : fomModules) mods.push_back(ws2s(w));
  bridge([&] {
    m17_->createFederationExecution(ws2s(federationExecutionName), mods);
  });
}

// §4.6 — destroy a federation execution.
void DLCRTIambassadorImpl::destroyFederationExecution(
    std::wstring const& federationExecutionName) {
  bridge([&] {
    m17_->destroyFederationExecution(ws2s(federationExecutionName));
  });
}

// §4.7 — list federation executions. M36 Agent CA-4: the wire's
// rti.v1.FederationService/ListFederations is called synchronously through
// the bridge's direct-stub path (M17's ambassador never surfaced a client
// method), then the spec's §4.9 reportFederationExecutions callback is
// synthesized on fed_amb_ (spec-legal: callback delivery mechanics are
// RTI-defined). gorti's wire carries no per-federation logical-time
// implementation name; every entry reports L"HLAfloat64Time" — the only
// binding gorti negotiates (documented divergence, catalogue §3 row 3.6).
void DLCRTIambassadorImpl::listFederationExecutions() {
  auto const names =
      bridgeR([&] { return m17_->listFederationExecutions(); });
  if (fed_amb_ == nullptr) return;
  FederationExecutionInformationVector infos;
  infos.reserve(names.size());
  for (auto const& n : names) {
    infos.emplace_back(s2ws(n), L"HLAfloat64Time");
  }
  // M37 EC-4 — deferred §4.9 report; delivered on the next evoke.
  auto* fed = fed_amb_;
  enqueueSynthesized_([fed, infos = std::move(infos)] {
    fed->reportFederationExecutions(infos);
  });
}

// §4.9 overload 1 — join with `federateType` implying `federateName`
// (Pitch legacy 2-arg shape). M17 accepts (federate_name, federation).
// Per spec §4.9 note 1 the RTI generates a federate name when only
// `federateType` is given; gorti passes federateType as name and lets
// the manager treat identical types as distinct federates.
FederateHandle DLCRTIambassadorImpl::joinFederationExecution(
    std::wstring const& federateType,
    std::wstring const& federationExecutionName,
    std::vector<std::wstring> const& /*additionalFomModules*/) {
  return bridgeR([&] {
    auto raw = m17_->joinFederationExecution(ws2s(federateType),
                                             ws2s(federationExecutionName));
    return makeFederateHandleFromUint64(raw);
  });
}

// §4.9 overload 2 — join with both federateName and federateType. M17
// gets `federateName` (its `federate_name` slot); `federateType` is
// currently unused by the M17 wire (a documented divergence — M17
// treats name/type as fused).
FederateHandle DLCRTIambassadorImpl::joinFederationExecution(
    std::wstring const& federateName,
    std::wstring const& /*federateType*/,
    std::wstring const& federationExecutionName,
    std::vector<std::wstring> const& /*additionalFomModules*/) {
  return bridgeR([&] {
    auto raw = m17_->joinFederationExecution(ws2s(federateName),
                                             ws2s(federationExecutionName));
    return makeFederateHandleFromUint64(raw);
  });
}

// §4.10 — resign. Spec mandates one of the 6 ResignAction values; M17
// Cut-1's resignFederationExecution() is arg-less and semantically fixed
// at UNCONDITIONALLY_DIVEST_ATTRIBUTES. The shim accepts all 6 values
// and folds them to the M17 default. Callers that rely on
// DELETE_OBJECTS / CANCEL_* semantics need M17 Cut-2. Documented
// divergence: DLC catalogue §3 row "resignFederationExecution".
void DLCRTIambassadorImpl::resignFederationExecution(
    ResignAction /*resignAction*/) {
  bridge([&] { m17_->resignFederationExecution(); });
}

// §4.11 overload 1 — sync-point register, no explicit member set (means
// "all currently joined federates").
void DLCRTIambassadorImpl::registerFederationSynchronizationPoint(
    std::wstring const& label,
    VariableLengthData const& theUserSuppliedTag) {
  auto const* p = static_cast<std::uint8_t const*>(theUserSuppliedTag.data());
  std::vector<std::uint8_t> tag(p, p + theUserSuppliedTag.size());
  bridge([&] {
    m17_->registerFederationSynchronizationPoint(ws2s(label), tag, {});
  });
}

// §4.11 overload 2 — sync-point register with an explicit federate set.
void DLCRTIambassadorImpl::registerFederationSynchronizationPoint(
    std::wstring const& label,
    VariableLengthData const& theUserSuppliedTag,
    FederateHandleSet const& syncSet) {
  auto const* p = static_cast<std::uint8_t const*>(theUserSuppliedTag.data());
  std::vector<std::uint8_t> tag(p, p + theUserSuppliedTag.size());
  std::vector<std::uint64_t> fs;
  fs.reserve(syncSet.size());
  for (auto const& h : syncSet) fs.push_back(rawFederateHandle(h));
  bridge([&] {
    m17_->registerFederationSynchronizationPoint(ws2s(label), tag, fs);
  });
}

// §4.14 — sync-point achieved. M37 EC-2: `successfully` now rides M17's
// flag-carrying overload (M37 Agent EA wire) — false still counts toward
// completion but reports the federate in the §4.15 failed-to-sync set.
void DLCRTIambassadorImpl::synchronizationPointAchieved(
    std::wstring const& label, bool successfully) {
  bridge([&] {
    m17_->synchronizationPointAchieved(ws2s(label), successfully);
  });
}

// §4.16 overload 1 — request save "now" (no logical-time pin).
void DLCRTIambassadorImpl::requestFederationSave(std::wstring const& label) {
  bridge([&] { m17_->requestFederationSave(ws2s(label), std::nullopt); });
}

// §4.16 overload 2 — request save at a specific logical time. The
// DLC LogicalTime is abstract; the shim currently supports only
// HLAfloat64Time (M17's exclusive time impl). Callers passing any
// other LogicalTime subclass hit a static_cast that treats it as
// float64; the safe fallback is "save now" if the encoded double is
// not finite. Documented divergence.
void DLCRTIambassadorImpl::requestFederationSave(std::wstring const& label,
                                                 LogicalTime const& theTime) {
  double t = 0.0;
  // LogicalTime supports `toString()` per <RTI/LogicalTime.h>; parse
  // the numeric portion. Non-numeric strings fall through as t=0.
  try {
    t = std::stod(ws2s(theTime.toString()));
  } catch (...) {
    // Fall through — treat as "save now".
    bridge([&] { m17_->requestFederationSave(ws2s(label), std::nullopt); });
    return;
  }
  bridge([&] {
    m17_->requestFederationSave(ws2s(label), std::optional<double>(t));
  });
}

// §4.17 — federate save begun. M17 has no explicit "begun" wire event
// (save begins on the requester's request); the shim is a no-op. The
// method exists on the DLC surface because the spec requires it.
void DLCRTIambassadorImpl::federateSaveBegun() {
  // No-op: M17 begin-save is implicit in the RequestFederationSave RPC.
}

// §4.18 — federate save complete.
void DLCRTIambassadorImpl::federateSaveComplete() {
  bridge([&] { m17_->federateSaveComplete(); });
}

// §4.19 — federate save NOT complete (this federate failed).
void DLCRTIambassadorImpl::federateSaveNotComplete() {
  bridge([&] { m17_->federateSaveNotComplete(); });
}

// §4.28 — abort federation save.
void DLCRTIambassadorImpl::abortFederationSave() {
  bridge([&] { m17_->abortFederationSave(); });
}

// §4.23 — query save status. Result arrives on the
// `federationSaveStatusResponse` FederateAmbassador callback. M17 exposes
// a synchronous polling `querySaveState(label)` but not the callback
// yet; the shim polls once (using an empty label = "current save")
// then discards the result (no callback path). Documented divergence:
// DLC callback signal is deferred to Agent AD's dispatch bridge.
void DLCRTIambassadorImpl::queryFederationSaveStatus() {
  bridge([&] { (void)m17_->querySaveState(""); });
}

// §4.24 — request federation restore.
void DLCRTIambassadorImpl::requestFederationRestore(std::wstring const& label) {
  bridge([&] { m17_->requestFederationRestore(ws2s(label)); });
}

// §4.26 — federate restore complete.
void DLCRTIambassadorImpl::federateRestoreComplete() {
  bridge([&] { m17_->federateRestoreComplete(); });
}

// §4.27 — federate restore NOT complete (this federate failed).
void DLCRTIambassadorImpl::federateRestoreNotComplete() {
  // M17 lacks a NotComplete restore RPC (Cut-3 gap). Treat as abort so
  // the federation-wide restore fails deterministically instead of
  // hanging. Documented divergence.
  bridge([&] { m17_->abortFederationRestore(); });
}

// §4.30 — abort federation restore.
void DLCRTIambassadorImpl::abortFederationRestore() {
  bridge([&] { m17_->abortFederationRestore(); });
}

// §4.32 — query restore status. Same "no callback path yet" story as
// §4.23; the shim polls M17 and discards the result.
void DLCRTIambassadorImpl::queryFederationRestoreStatus() {
  bridge([&] { (void)m17_->queryRestoreState(""); });
}

// ===== §5 Declaration Management =====
//
// M35 Agent BB: real M17 delegation replacing M34 Agent AC's shim.
//
// Design (parity with §4 shim above):
//   - Spec §5 signatures per catalogue rows 11.9-11.11 (DLC-only extras
//     `bool active` + `wstring updateRateDesignator`) are accepted at the
//     DLC surface but stripped before invoking M17 (Cut-1 doesn't model
//     passive subscription or per-subscription update-rate policies —
//     documented in docs/DLC_DIVERGENCE_CATALOGUE.md §11 rows 11.9/11.11).
//   - Whole-class forms (`unpublishObjectClass`, `unsubscribeObjectClass`,
//     catalogue row 11.10) have no direct M17 wire target; the shim
//     delegates via the attribute-set form passing an EMPTY set. On the
//     M17 side an empty set is treated as "publish/subscribe the class
//     without any attribute bindings" (see rti1516e/RtiAmbassador.h §5
//     header comment). The whole-class semantics of "drop every attribute
//     of the class" is not what an empty set yields — the divergence is
//     documented; a proper implementation needs either an M17 batch
//     "unpublishAll" RPC or the federate to remember its own publication
//     set (M35+ follow-up, tracked in the dispatch plan).
//   - Handle adaptation: DLC's typed handles store an 8-byte big-endian
//     VLD blob (§10.5). The file-bottom `rawObjectClassHandle` /
//     `rawAttributeHandle` / `rawInteractionClassHandle` helpers extract
//     the underlying uint64 for the M17 bridge, mirroring `rawFederateHandle`
//     from §4. The bridge (M17Bridge.h) accepts raw uint64s so this TU
//     stays free of the M17 header.
//   - Exception translation follows §4 — every M17 call is wrapped in
//     `bridge([&] { ... })` which catches std::runtime_error from the
//     bridge and re-throws the matching <RTI/Exception.h> type via
//     `translateBridgeError()`. `InvalidObjectClassHandle` /
//     `InvalidInteractionClassHandle` fall through to `RTIinternalError`
//     (the guard()'s catch-all path).

void DLCRTIambassadorImpl::publishObjectClassAttributes(
    ObjectClassHandle theClass, AttributeHandleSet const& attributeList) {
  // §5.2 → M17 publishObjectClassAttributes.
  bridge([&] {
    std::vector<std::uint64_t> attrs;
    attrs.reserve(attributeList.size());
    for (auto const& a : attributeList) attrs.push_back(rawAttributeHandle(a));
    m17_->publishObjectClassAttributes(rawObjectClassHandle(theClass), attrs);
  });
}

void DLCRTIambassadorImpl::unpublishObjectClass(ObjectClassHandle theClass) {
  // §5.3 whole-class form (catalogue row 11.10). M17 lacks a batch
  // "drop-all" RPC; delegate via the attribute-set form with an empty
  // vector. See file-header comment on the divergence.
  bridge([&] {
    m17_->unpublishObjectClassAttributes(rawObjectClassHandle(theClass), {});
  });
}

void DLCRTIambassadorImpl::unpublishObjectClassAttributes(
    ObjectClassHandle theClass, AttributeHandleSet const& attributeList) {
  // §5.3 subset form → M17 unpublishObjectClassAttributes.
  bridge([&] {
    std::vector<std::uint64_t> attrs;
    attrs.reserve(attributeList.size());
    for (auto const& a : attributeList) attrs.push_back(rawAttributeHandle(a));
    m17_->unpublishObjectClassAttributes(rawObjectClassHandle(theClass), attrs);
  });
}

void DLCRTIambassadorImpl::publishInteractionClass(
    InteractionClassHandle theInteraction) {
  // §5.4 → M17 publishInteractionClass.
  bridge([&] {
    m17_->publishInteractionClass(rawInteractionClassHandle(theInteraction));
  });
}

void DLCRTIambassadorImpl::unpublishInteractionClass(
    InteractionClassHandle theInteraction) {
  // §5.5 → M17 unpublishInteractionClass.
  bridge([&] {
    m17_->unpublishInteractionClass(rawInteractionClassHandle(theInteraction));
  });
}

void DLCRTIambassadorImpl::subscribeObjectClassAttributes(
    ObjectClassHandle theClass, AttributeHandleSet const& attributeList,
    bool /*active*/, std::wstring const& /*updateRateDesignator*/) {
  // §5.6 (catalogue row 11.9). `active` and `updateRateDesignator` are
  // DLC-only extras M17 Cut-1 doesn't model — silently dropped. The
  // §5.10 startRegistration callback advisories and FOM-driven update-
  // rate throttling that these params gate remain M35+ follow-ups.
  bridge([&] {
    std::vector<std::uint64_t> attrs;
    attrs.reserve(attributeList.size());
    for (auto const& a : attributeList) attrs.push_back(rawAttributeHandle(a));
    m17_->subscribeObjectClassAttributes(rawObjectClassHandle(theClass), attrs);
  });
}

void DLCRTIambassadorImpl::unsubscribeObjectClass(ObjectClassHandle theClass) {
  // §5.7 whole-class form. Same divergence note as unpublishObjectClass.
  bridge([&] {
    m17_->unsubscribeObjectClassAttributes(rawObjectClassHandle(theClass), {});
  });
}

void DLCRTIambassadorImpl::unsubscribeObjectClassAttributes(
    ObjectClassHandle theClass, AttributeHandleSet const& attributeList) {
  // §5.7 subset form → M17 unsubscribeObjectClassAttributes.
  bridge([&] {
    std::vector<std::uint64_t> attrs;
    attrs.reserve(attributeList.size());
    for (auto const& a : attributeList) attrs.push_back(rawAttributeHandle(a));
    m17_->unsubscribeObjectClassAttributes(rawObjectClassHandle(theClass),
                                           attrs);
  });
}

void DLCRTIambassadorImpl::subscribeInteractionClass(
    InteractionClassHandle theClass, bool /*active*/) {
  // §5.8 (catalogue row 11.11). `active` gates §5.12 turnInteractionsOn/Off
  // callbacks; DLC-only extra silently dropped (M17 Cut-1 gap).
  bridge([&] {
    m17_->subscribeInteractionClass(rawInteractionClassHandle(theClass));
  });
}

void DLCRTIambassadorImpl::unsubscribeInteractionClass(
    InteractionClassHandle theClass) {
  // §5.9 → M17 unsubscribeInteractionClass.
  bridge([&] {
    m17_->unsubscribeInteractionClass(rawInteractionClassHandle(theClass));
  });
}


// ===== §6 Object Management =====
//
// M35 Agent BC: real M17 delegation via M17Bridge (replaces M33 Agent L's
// shim throws). Same wstring-adapter shape as §4/§5/§8:
//   - wstring → narrow via ws2s (top-of-TU helper); DLC accepts wide, M17
//     speaks UTF-8 narrow. Names are ASCII in FOM XML per Pitch convention.
//   - AttributeHandleValueMap → std::map<uint64, bytes> by extracting each
//     handle's raw big-endian uint64 encoding (see decodeHandleVLD_ /
//     rawAttributeHandle at file bottom). ParameterHandleValueMap likewise.
//   - VariableLengthData tag → std::vector<uint8_t>. Tag is DLC-mandatory
//     per catalogue §17.1; M17 Cut-1 does NOT carry it on the wire — the
//     bridge drops it (documented divergence, DLC catalogue §11 rows
//     "updateAttributeValues/tag" + "sendInteraction/tag"). A future M17
//     wire extension picks up the field.
//   - registerObjectInstance return: M17 hands back a raw uint64
//     ObjectInstanceHandle; we widen via makeObjectInstanceHandleFromUint64
//     (defined at file bottom after the DEFINE_HANDLE_FRIEND expansion).
//
// TSO overloads (§6.10/12/14 overload 2 taking LogicalTime): M17 Cut-1's
// §6 surface is RO-only (see RtiAmbassador.h "Cut-1 ships the RO variants").
// The DLC shim currently forwards to the RO wire — the LogicalTime binding
// is silently dropped and the MessageRetractionHandle is a zero-valued
// placeholder. Federates that rely on TSO semantics + retract() need
// M17 Cut-2. Documented divergence: DLC catalogue §11 "§6 TSO overloads".
//
// M17 wire gaps (deleteObjectInstance / localDeleteObjectInstance /
// requestAttributeValueUpdate): the M17 Cut-1 ambassador has no matching
// RPC for these methods. They are implemented as spec-legal no-ops with a
// TODO comment — this matches the §4.7 listFederationExecutions pattern
// (no M17 target → silent no-op). Documented divergences per DLC catalogue
// §11 rows.

namespace {

// wstring → narrow string. Object instance names, interaction/object
// class names and attribute names are ASCII per FOM XML convention;
// FR-DLC-17 requires the shim to be lossy-tolerant. When high-Unicode
// characters land in a name the M17 layer will fail on gRPC validation
// — the DLC contract is "spec-legal wide input; narrow-string M17 gate".
std::string ws2s_(std::wstring const& w) {
  return std::string(w.begin(), w.end());
}

// VariableLengthData → byte vector for the M17 tag field. §17.1 makes
// the tag mandatory; passing an empty VLD yields an empty vector, which
// M17 wire-encodes as a zero-length bytes field (spec-legal).
std::vector<uint8_t> tag2bytes_(VariableLengthData const& tag) {
  auto const* p = static_cast<uint8_t const*>(tag.data());
  return std::vector<uint8_t>(p, p + tag.size());
}

// Forward decl — defined in the §8 anon-namespace block below (same
// unnamed namespace TU-wide). The §6 TSO overloads narrow the abstract
// LogicalTime to the HLAfloat64Time double the M17 wire speaks.
double narrowTime_(LogicalTime const& t, char const* method);

}  // namespace

void DLCRTIambassadorImpl::reserveObjectInstanceName(
    std::wstring const& theObjectInstanceName) {
  // §6.5 — async. Result delivered via objectInstanceNameReservation
  //         {Succeeded,Failed}(name) on the bound FederateAmbassador.
  //         The M17 event-stream drives the callback; the RPC itself
  //         returns promptly.
  bridge([&] {
    m17_->reserveObjectInstanceName(ws2s_(theObjectInstanceName));
  });
}

void DLCRTIambassadorImpl::releaseObjectInstanceName(
    std::wstring const& theObjectInstanceName) {
  // §6.6 — synchronous release of a previously reserved name.
  bridge([&] {
    m17_->releaseObjectInstanceName(ws2s_(theObjectInstanceName));
  });
}

void DLCRTIambassadorImpl::reserveMultipleObjectInstanceName(
    std::set<std::wstring> const& theObjectInstanceNames) {
  // §6.5 (multi) — atomic batch. Delivered via
  //   multipleObjectInstanceNameReservation{Succeeded,Failed}(names).
  // M17 layer: single ReserveMultipleObjectInstanceNames RPC.
  std::vector<std::string> names;
  names.reserve(theObjectInstanceNames.size());
  for (auto const& w : theObjectInstanceNames) {
    names.push_back(ws2s_(w));
  }
  bridge([&] { m17_->reserveMultipleObjectInstanceNames(names); });
}

void DLCRTIambassadorImpl::releaseMultipleObjectInstanceName(
    std::set<std::wstring> const& theObjectInstanceNames) {
  // §6.7 (multi) — spec allows non-atomic release. M17 has no batch
  // release RPC, so we loop the singular release. If any single release
  // throws (e.g. NameNotFound for an already-released name), the loop
  // aborts and the DLC caller sees the mapped exception; earlier
  // releases stay committed — matches the "non-atomic" spec permission.
  bridge([&] {
    for (auto const& w : theObjectInstanceNames) {
      m17_->releaseObjectInstanceName(ws2s_(w));
    }
  });
}

ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstance(
    ObjectClassHandle theClass) {
  // §6.8 overload 1 — RTI-generated name. Empty string tells M17 to
  // generate one. Return is widened uint64 → typed handle via the
  // ObjectInstanceHandleFriend shim.
  return bridgeR([&] {
    auto raw = m17_->registerObjectInstance(
        rawObjectClassHandle(theClass), "");
    return makeObjectInstanceHandleFromUint64(raw);
  });
}

ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstance(
    ObjectClassHandle theClass,
    std::wstring const& theObjectInstanceName) {
  // §6.8 overload 2 — federate-supplied name. Per spec §6.8 the name
  // must have been reserved via §6.5 first; M17 enforces this at the
  // manager and throws ObjectInstanceNameNotReserved (folded into
  // RTIinternalError by translateBridgeError) if not.
  return bridgeR([&] {
    auto raw = m17_->registerObjectInstance(
        rawObjectClassHandle(theClass), ws2s_(theObjectInstanceName));
    return makeObjectInstanceHandleFromUint64(raw);
  });
}

void DLCRTIambassadorImpl::updateAttributeValues(
    ObjectInstanceHandle theObject,
    AttributeHandleValueMap const& theAttributeValues,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.10 overload 1 (RO). Tag is MANDATORY per catalogue §17.1 —
  // extracted here into the M17-shape byte vector; the M17 wire drops
  // it (see file-top block comment). Attribute map is rekeyed uint64.
  std::map<std::uint64_t, std::vector<std::uint8_t>> values;
  for (auto const& kv : theAttributeValues) {
    auto const* p = static_cast<uint8_t const*>(kv.second.data());
    values.emplace(rawAttributeHandle(kv.first),
                   std::vector<uint8_t>(p, p + kv.second.size()));
  }
  auto const tag = tag2bytes_(theUserSuppliedTag);
  bridge([&] {
    m17_->updateAttributeValues(
        rawObjectInstanceHandle(theObject), values, tag);
  });
}

MessageRetractionHandle DLCRTIambassadorImpl::updateAttributeValues(
    ObjectInstanceHandle theObject,
    AttributeHandleValueMap const& theAttributeValues,
    VariableLengthData const& theUserSuppliedTag,
    LogicalTime const& theTime) {
  // §6.10 overload 2 (TSO) — M36 Agent DA: real timed wire; M37 EC-2:
  // routed via M17's retractable variant which allocates a REAL
  // MessageRetractionHandle (per-federate monotonic counter). Pass it to
  // retract() (§8.21) to cancel while buffered server-side. The
  // LogicalTime narrows to the HLAfloat64Time double the proto's
  // `optional double logical_time` carries; the server TSO gate buffers
  // and releases per §8.
  double const t = narrowTime_(theTime, "updateAttributeValues");
  std::map<std::uint64_t, std::vector<std::uint8_t>> values;
  for (auto const& kv : theAttributeValues) {
    auto const* p = static_cast<uint8_t const*>(kv.second.data());
    values.emplace(rawAttributeHandle(kv.first),
                   std::vector<uint8_t>(p, p + kv.second.size()));
  }
  auto const tag = tag2bytes_(theUserSuppliedTag);
  return bridgeR([&] {
    auto const raw = m17_->updateAttributeValuesRetractable(
        rawObjectInstanceHandle(theObject), values, tag, t);
    return makeMessageRetractionHandleFromUint64(raw);
  });
}

void DLCRTIambassadorImpl::sendInteraction(
    InteractionClassHandle theInteraction,
    ParameterHandleValueMap const& theParameterValues,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.12 overload 1 (RO). Parameter map is rekeyed uint64. Tag
  // extracted; M17 wire drops it.
  std::map<std::uint64_t, std::vector<std::uint8_t>> params;
  for (auto const& kv : theParameterValues) {
    auto const* p = static_cast<uint8_t const*>(kv.second.data());
    params.emplace(rawParameterHandle(kv.first),
                   std::vector<uint8_t>(p, p + kv.second.size()));
  }
  auto const tag = tag2bytes_(theUserSuppliedTag);
  bridge([&] {
    m17_->sendInteraction(
        rawInteractionClassHandle(theInteraction), params, tag);
  });
}

MessageRetractionHandle DLCRTIambassadorImpl::sendInteraction(
    InteractionClassHandle theInteraction,
    ParameterHandleValueMap const& theParameterValues,
    VariableLengthData const& theUserSuppliedTag,
    LogicalTime const& theTime) {
  // §6.12 overload 2 (TSO) — M36 Agent DA: real timed wire; M37 EC-2:
  // retractable variant returns the real MessageRetractionHandle. Same
  // narrow-and-send pattern as updateAttributeValues overload 2.
  double const t = narrowTime_(theTime, "sendInteraction");
  std::map<std::uint64_t, std::vector<std::uint8_t>> params;
  for (auto const& kv : theParameterValues) {
    auto const* p = static_cast<uint8_t const*>(kv.second.data());
    params.emplace(rawParameterHandle(kv.first),
                   std::vector<uint8_t>(p, p + kv.second.size()));
  }
  auto const tag = tag2bytes_(theUserSuppliedTag);
  return bridgeR([&] {
    auto const raw = m17_->sendInteractionRetractable(
        rawInteractionClassHandle(theInteraction), params, tag, t);
    return makeMessageRetractionHandleFromUint64(raw);
  });
}

void DLCRTIambassadorImpl::deleteObjectInstance(
    ObjectInstanceHandle theObject,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.14 overload 1 (RO) — M36 Agent DA: real M23 DeleteObjectInstance
  // wire (owner-only; server fans out RemoveObjectInstance to
  // subscribers with the tag).
  auto const tag = tag2bytes_(theUserSuppliedTag);
  bridge([&] {
    m17_->deleteObjectInstance(rawObjectInstanceHandle(theObject), tag);
  });
}

MessageRetractionHandle DLCRTIambassadorImpl::deleteObjectInstance(
    ObjectInstanceHandle theObject,
    VariableLengthData const& theUserSuppliedTag,
    LogicalTime const& theTime) {
  // §6.14 overload 2 (TSO) — M36 Agent DA: timed wire; subscribers see
  // the §6.15 TSO removeObjectInstance overload. Retraction handle
  // stays an invalid placeholder (see updateAttributeValues note).
  double const t = narrowTime_(theTime, "deleteObjectInstance");
  auto const tag = tag2bytes_(theUserSuppliedTag);
  bridge([&] {
    m17_->deleteObjectInstanceTimed(rawObjectInstanceHandle(theObject),
                                    tag, t);
  });
  return MessageRetractionHandle();
}

void DLCRTIambassadorImpl::localDeleteObjectInstance(
    ObjectInstanceHandle theObject) {
  // §6.16 — remove the local reflection only; no wire traffic per spec.
  // With no local reflection state to prune yet (added M35+), this is
  // a spec-legal no-op.
  (void)theObject;
}

void DLCRTIambassadorImpl::requestAttributeValueUpdate(
    ObjectInstanceHandle theObject,
    AttributeHandleSet const& theAttributes,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.19 overload 1 — by object instance. M36 Agent DA: real M23
  // RequestAttributeValueUpdate wire; the owner receives the §6.20
  // provideAttributeValueUpdate callback with the tag.
  std::vector<std::uint64_t> attrs;
  attrs.reserve(theAttributes.size());
  for (auto const& a : theAttributes) attrs.push_back(rawAttributeHandle(a));
  auto const tag = tag2bytes_(theUserSuppliedTag);
  bridge([&] {
    m17_->requestAttributeValueUpdate(rawObjectInstanceHandle(theObject),
                                      attrs, tag);
  });
}

void DLCRTIambassadorImpl::requestAttributeValueUpdate(
    ObjectClassHandle theClass,
    AttributeHandleSet const& theAttributes,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.19 overload 2 — by object class. M36 Agent DA: real M23
  // RequestClassAttributeValueUpdate wire; every owner of any instance
  // of the class receives the callback.
  std::vector<std::uint64_t> attrs;
  attrs.reserve(theAttributes.size());
  for (auto const& a : theAttributes) attrs.push_back(rawAttributeHandle(a));
  auto const tag = tag2bytes_(theUserSuppliedTag);
  bridge([&] {
    m17_->requestClassAttributeValueUpdate(rawObjectClassHandle(theClass),
                                           attrs, tag);
  });
}

// ===== §7 Ownership Management =====
//
// M36 Agent CA-1: real M17 delegation via M17Bridge (replaces M33-K's
// signature-parity no-ops). Mapping notes (docs/DLC_DIVERGENCE_CATALOGUE.md
// §12):
//   - AttributeHandleSet flattens to vector<uint64> via attrs2raw_.
//   - attributeOwnershipAcquisition: the DLC-mandatory tag (row 12.2) is
//     dropped — M17's acquisition wire carries no tag (only the negotiated
//     divestiture does). Same documented-drop pattern as §6.
//   - queryAttributeOwnership: M17 answers synchronously; the DLC spec
//     delivers via §7.18 callbacks. The impl calls M17 then invokes
//     informAttributeOwnership / attributeIsNotOwned on the bound
//     fed_amb_ directly (spec-legal: callback delivery mechanics are
//     RTI-defined; matches the listFederationExecutions pattern).
//   - acquisitionIfAvailable: no M17 RPC. Emulated: per-attribute query
//     splits the set into unowned (delegated to acquisition — the grant
//     arrives via the OwnershipAcquired event) and owned (synthesized
//     §7.10 attributeOwnershipUnavailable on fed_amb_).
//   - confirmDivestiture (row 12.1) + attributeOwnershipReleaseDenied
//     (row 12.4): no M17 wire — spec-legal no-ops, documented divergence.

namespace {
// AttributeHandleSet → raw uint64 vector for the bridge boundary.
std::vector<std::uint64_t> attrs2raw_(AttributeHandleSet const& attrs) {
  std::vector<std::uint64_t> out;
  out.reserve(attrs.size());
  for (auto const& a : attrs) out.push_back(rawAttributeHandle(a));
  return out;
}
}  // namespace

// §7.2 — unconditional divest.
void DLCRTIambassadorImpl::unconditionalAttributeOwnershipDivestiture(
    ObjectInstanceHandle theObject, AttributeHandleSet const& theAttributes) {
  bridge([&] {
    m17_->unconditionalAttributeOwnershipDivestiture(
        rawObjectInstanceHandle(theObject), attrs2raw_(theAttributes));
  });
}

// §7.3 — negotiated divest (offer to subscribers). Subscribers see
// requestAttributeOwnershipAssumption via the M17 event stream. M37 EC-2:
// two_phase=true engages the REAL §7.3/§7.6 protocol — the divester gets
// requestDivestitureConfirmation and completes with confirmDivestiture().
void DLCRTIambassadorImpl::negotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle theObject, AttributeHandleSet const& theAttributes,
    VariableLengthData const& theUserSuppliedTag) {
  bridge([&] {
    m17_->negotiatedAttributeOwnershipDivestiture(
        rawObjectInstanceHandle(theObject), attrs2raw_(theAttributes),
        tag2bytes_(theUserSuppliedTag), /*two_phase=*/true);
  });
}

// §7.6 — confirm divest after §7.5 requestDivestitureConfirmation. M37
// EC-2: real ConfirmDivestiture RPC via M17 (M37 Agent EA wire) — replaces
// the catalogue-row-12.1 no-op. The DLC-mandatory tag is dropped at this
// boundary (the M17 confirm wire carries none — same documented-drop
// pattern as §7.8 acquisition, row 12.2).
void DLCRTIambassadorImpl::confirmDivestiture(
    ObjectInstanceHandle theObject, AttributeHandleSet const& theAttributes,
    VariableLengthData const& /*theUserSuppliedTag*/) {
  bridge([&] {
    m17_->confirmDivestiture(rawObjectInstanceHandle(theObject),
                             attrs2raw_(theAttributes));
  });
}

// §7.8 — request ownership acquisition. Tag dropped (row 12.2, see
// section comment). Acquisition outcome arrives via the OwnershipAcquired
// event → attributeOwnershipAcquisitionNotification on fed_amb_.
void DLCRTIambassadorImpl::attributeOwnershipAcquisition(
    ObjectInstanceHandle theObject, AttributeHandleSet const& desiredAttributes,
    VariableLengthData const& /*theUserSuppliedTag*/) {
  bridge([&] {
    m17_->attributeOwnershipAcquisition(rawObjectInstanceHandle(theObject),
                                        attrs2raw_(desiredAttributes));
  });
}

// §7.9 — acquire-if-available. M37 EC-2: real if_available wire (M37
// Agent EA) — replaces CA's query-then-acquire emulation. The server
// atomically grants the unowned subset and emits AttributeOwnershipUnavailable
// (§7.10) for the owned remainder; the bridge delivers it like any other
// wire callback (no client-side synthesis).
void DLCRTIambassadorImpl::attributeOwnershipAcquisitionIfAvailable(
    ObjectInstanceHandle theObject,
    AttributeHandleSet const& desiredAttributes) {
  bridge([&] {
    m17_->attributeOwnershipAcquisitionIfAvailable(
        rawObjectInstanceHandle(theObject), attrs2raw_(desiredAttributes));
  });
}

// §7.12 — release-denied. Catalogue row 12.4: no M17 wire (gorti's manager
// has no release-request/deny leg — acquisition against an owned attribute
// simply does not transfer). Spec-legal no-op, documented divergence.
void DLCRTIambassadorImpl::attributeOwnershipReleaseDenied(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op: M17 wire gap (catalogue row 12.4).
}

// §7.13 — divest-if-wanted. M17's RPC returns no divested set (row 12.5);
// the out-param is reconstructed by post-querying ownership: any requested
// attribute this federate no longer owns after the call was divested.
void DLCRTIambassadorImpl::attributeOwnershipDivestitureIfWanted(
    ObjectInstanceHandle theObject, AttributeHandleSet const& theAttributes,
    AttributeHandleSet& theDivestedAttributes) {
  theDivestedAttributes.clear();
  auto const raw_obj = rawObjectInstanceHandle(theObject);
  bridge([&] {
    m17_->attributeOwnershipDivestitureIfWanted(raw_obj,
                                                attrs2raw_(theAttributes));
    for (auto const& a : theAttributes) {
      if (!m17_->isAttributeOwnedByFederate(raw_obj, rawAttributeHandle(a))) {
        theDivestedAttributes.insert(a);
      }
    }
  });
}

// §7.14 — cancel a pending negotiated divest.
void DLCRTIambassadorImpl::cancelNegotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle theObject, AttributeHandleSet const& theAttributes) {
  bridge([&] {
    m17_->cancelNegotiatedAttributeOwnershipDivestiture(
        rawObjectInstanceHandle(theObject), attrs2raw_(theAttributes));
  });
}

// §7.15 — cancel a pending acquisition.
void DLCRTIambassadorImpl::cancelAttributeOwnershipAcquisition(
    ObjectInstanceHandle theObject, AttributeHandleSet const& theAttributes) {
  bridge([&] {
    m17_->cancelAttributeOwnershipAcquisition(
        rawObjectInstanceHandle(theObject), attrs2raw_(theAttributes));
  });
}

// §7.17 — query ownership. Synchronous M17 answer converted to the spec's
// §7.18 callback delivery on fed_amb_ (see section comment). owner==0 with
// owned==true would be mid-transfer; both unowned cases map to
// attributeIsNotOwned.
void DLCRTIambassadorImpl::queryAttributeOwnership(
    ObjectInstanceHandle theObject, AttributeHandle theAttribute) {
  auto const q = bridgeR([&] {
    return m17_->queryAttributeOwnership(rawObjectInstanceHandle(theObject),
                                         rawAttributeHandle(theAttribute));
  });
  if (fed_amb_ == nullptr) return;
  // M37 EC-2/EC-4 — the synthesized §7.18 answer delivers on the next
  // evoke so the callback follows the call's return (Pitch ordering).
  auto* fed = fed_amb_;
  if (q.owned && q.owner != 0) {
    auto const owner = q.owner;
    enqueueSynthesized_([fed, theObject, theAttribute, owner] {
      fed->informAttributeOwnership(theObject, theAttribute,
                                    makeFederateHandleFromUint64(owner));
    });
  } else {
    enqueueSynthesized_([fed, theObject, theAttribute] {
      fed->attributeIsNotOwned(theObject, theAttribute);
    });
  }
}

// §7.19 — is this attribute owned by THIS federate? Direct bool per spec
// (row 12.7: DLC matches M17 shape).
bool DLCRTIambassadorImpl::isAttributeOwnedByFederate(
    ObjectInstanceHandle theObject, AttributeHandle theAttribute) {
  return bridgeR([&] {
    return m17_->isAttributeOwnedByFederate(rawObjectInstanceHandle(theObject),
                                            rawAttributeHandle(theAttribute));
  });
}

// ===== §8 Time Management =====
//
// M36 Agent CA-2: real M17 delegation via M17Bridge (replaces M34 Agent
// AB's m17NotWiredTime_ throws). Design:
//   - Spec parameters LogicalTime const& / LogicalTimeInterval const& are
//     pure-abstract; gorti M17 speaks double (HLAfloat64Time wire shape).
//     Each shim narrows via dynamic_cast to the HLAfloat64 concrete and
//     throws InvalidLogicalTime / InvalidLookahead when a foreign
//     LogicalTime binding is passed (catalogue §9 rows 9.1/9.4).
//   - Query out-params widen by assigning into the caller's LogicalTime&
//     via dynamic_cast to HLAfloat64Time& + setTime (resp. setInterval).
//   - Async ack synthesis: M17's Enable{Regulation,Constrained} RPCs are
//     synchronous with NO ack event on the stream. The spec's §8.3/§8.6
//     acks (timeRegulationEnabled / timeConstrainedEnabled) are therefore
//     synthesized on fed_amb_ directly after the RPC returns OK, carrying
//     the federate's current logical time from queryLogicalTime. Spec-legal:
//     callback delivery mechanics are RTI-defined (same pattern as
//     queryAttributeOwnership / listFederationExecutions).
//   - TAR/TARA/NER/NMRA/FQR grants stay fully async: the manager emits
//     TimeAdvanceGrant on the event stream → DLCFederateAmbassadorBridge
//     wraps as HLAfloat64Time → fed_amb_->timeAdvanceGrant.
//   - changeAttributeOrderType / changeInteractionOrderType: no M17 wire
//     (order type is FOM-declared in gorti). Spec-legal no-ops, documented
//     divergence (catalogue §9 rows 9.13/9.14 pattern — same as §6 gaps).
//
// Catalogue §9 rows 9.1-9.14 (FR-DLC-8).

namespace {

// LogicalTime → double. Throws InvalidLogicalTime on a non-HLAfloat64Time
// binding (gorti federations negotiate HLAfloat64Time unconditionally).
double narrowTime_(LogicalTime const& t, char const* method) {
  auto const* p = dynamic_cast<HLAfloat64Time const*>(&t);
  if (p == nullptr) {
    std::wstring msg = L"gorti DLC §8 ";
    for (char const* c = method; *c; ++c)
      msg.push_back(static_cast<wchar_t>(static_cast<unsigned char>(*c)));
    msg += L": LogicalTime binding must be HLAfloat64Time.";
    throw InvalidLogicalTime(msg);
  }
  return p->getTime();
}

// LogicalTimeInterval → double. Throws InvalidLookahead on a foreign type.
double narrowInterval_(LogicalTimeInterval const& i, char const* method) {
  auto const* p = dynamic_cast<HLAfloat64Interval const*>(&i);
  if (p == nullptr) {
    std::wstring msg = L"gorti DLC §8 ";
    for (char const* c = method; *c; ++c)
      msg.push_back(static_cast<wchar_t>(static_cast<unsigned char>(*c)));
    msg += L": LogicalTimeInterval binding must be HLAfloat64Interval.";
    throw InvalidLookahead(msg);
  }
  return p->getInterval();
}

// double → caller's LogicalTime& out-param. Same binding constraint.
void widenTime_(LogicalTime& t, double value, char const* method) {
  auto* p = dynamic_cast<HLAfloat64Time*>(&t);
  if (p == nullptr) {
    std::wstring msg = L"gorti DLC §8 ";
    for (char const* c = method; *c; ++c)
      msg.push_back(static_cast<wchar_t>(static_cast<unsigned char>(*c)));
    msg += L": LogicalTime binding must be HLAfloat64Time.";
    throw InvalidLogicalTime(msg);
  }
  p->setTime(value);
}

}  // namespace

void DLCRTIambassadorImpl::enableTimeRegulation(
    LogicalTimeInterval const& theLookahead) {
  // §8.2 — M17's EnableTimeRegulation RPC is synchronous; the §8.3
  // timeRegulationEnabled(federateTime) ack is synthesized (see section
  // comment).
  double const lookahead = narrowInterval_(theLookahead,
                                           "enableTimeRegulation");
  double federate_time = 0.0;
  bridge([&] {
    m17_->enableTimeRegulation(lookahead);
    federate_time = m17_->queryLogicalTime();
  });
  if (fed_amb_ != nullptr) {
    // M37 EC-4 — deliver the synthesized ack on the next evoke (Pitch
    // delivers §8.3 after the call returns, not inside it).
    auto* fed = fed_amb_;
    enqueueSynthesized_([fed, federate_time] {
      HLAfloat64Time const t(federate_time);
      fed->timeRegulationEnabled(t);
    });
  }
}

void DLCRTIambassadorImpl::disableTimeRegulation() {
  // §8.4 — synchronous per spec (no async ack).
  bridge([&] { m17_->disableTimeRegulation(); });
}

void DLCRTIambassadorImpl::enableTimeConstrained() {
  // §8.5 — §8.6 timeConstrainedEnabled(federateTime) ack synthesized.
  double federate_time = 0.0;
  bridge([&] {
    m17_->enableTimeConstrained();
    federate_time = m17_->queryLogicalTime();
  });
  if (fed_amb_ != nullptr) {
    // M37 EC-4 — deferred §8.6 ack; see enableTimeRegulation note.
    auto* fed = fed_amb_;
    enqueueSynthesized_([fed, federate_time] {
      HLAfloat64Time const t(federate_time);
      fed->timeConstrainedEnabled(t);
    });
  }
}

void DLCRTIambassadorImpl::disableTimeConstrained() {
  // §8.7 — synchronous.
  bridge([&] { m17_->disableTimeConstrained(); });
}

void DLCRTIambassadorImpl::timeAdvanceRequest(LogicalTime const& theTime) {
  // §8.8 — async; grant via §8.13 timeAdvanceGrant(theTime).
  double const t = narrowTime_(theTime, "timeAdvanceRequest");
  bridge([&] { m17_->timeAdvanceRequest(t); });
}

void DLCRTIambassadorImpl::timeAdvanceRequestAvailable(
    LogicalTime const& theTime) {
  // §8.9 — async; grant via §8.13.
  double const t = narrowTime_(theTime, "timeAdvanceRequestAvailable");
  bridge([&] { m17_->timeAdvanceRequestAvailable(t); });
}

void DLCRTIambassadorImpl::nextMessageRequest(LogicalTime const& theTime) {
  // §8.10 — async; grant via §8.13. NER anchors the tm_ner_pair fixture.
  double const t = narrowTime_(theTime, "nextMessageRequest");
  bridge([&] { m17_->nextMessageRequest(t); });
}

void DLCRTIambassadorImpl::nextMessageRequestAvailable(
    LogicalTime const& theTime) {
  // §8.11 — async; grant via §8.13.
  double const t = narrowTime_(theTime, "nextMessageRequestAvailable");
  bridge([&] { m17_->nextMessageRequestAvailable(t); });
}

void DLCRTIambassadorImpl::flushQueueRequest(LogicalTime const& theTime) {
  // §8.12 — async; grant via §8.13.
  double const t = narrowTime_(theTime, "flushQueueRequest");
  bridge([&] { m17_->flushQueueRequest(t); });
}

void DLCRTIambassadorImpl::enableAsynchronousDelivery() {
  // §8.14 — synchronous toggle.
  bridge([&] { m17_->enableAsynchronousDelivery(); });
}

void DLCRTIambassadorImpl::disableAsynchronousDelivery() {
  // §8.15 — synchronous toggle.
  bridge([&] { m17_->disableAsynchronousDelivery(); });
}

bool DLCRTIambassadorImpl::queryGALT(LogicalTime& theTime) {
  // §8.16 — out-param + bool. false when GALT is undefined (no other
  // regulating federate); theTime untouched in that case per spec.
  auto const r = bridgeR([&] { return m17_->queryGALT(); });
  if (!r.finite) return false;
  widenTime_(theTime, r.time, "queryGALT");
  return true;
}

void DLCRTIambassadorImpl::queryLogicalTime(LogicalTime& theTime) {
  // §8.17 — assigns federate's current logical time.
  auto const t = bridgeR([&] { return m17_->queryLogicalTime(); });
  widenTime_(theTime, t, "queryLogicalTime");
}

bool DLCRTIambassadorImpl::queryLITS(LogicalTime& theTime) {
  // §8.18 — out-param + bool. Least incoming time stamp; false if none.
  auto const r = bridgeR([&] { return m17_->queryLITS(); });
  if (!r.finite) return false;
  widenTime_(theTime, r.time, "queryLITS");
  return true;
}

void DLCRTIambassadorImpl::modifyLookahead(
    LogicalTimeInterval const& theLookahead) {
  // §8.19 — synchronous. Requires TimeRegulation enabled (M17 enforces).
  double const lookahead = narrowInterval_(theLookahead, "modifyLookahead");
  bridge([&] { m17_->modifyLookahead(lookahead); });
}

void DLCRTIambassadorImpl::queryLookahead(LogicalTimeInterval& interval) {
  // §8.20 — out-param. Requires TimeRegulation enabled (M17 enforces).
  auto const v = bridgeR([&] { return m17_->queryLookahead(); });
  auto* p = dynamic_cast<HLAfloat64Interval*>(&interval);
  if (p == nullptr) {
    throw InvalidLookahead(
        L"gorti DLC §8 queryLookahead: LogicalTimeInterval binding must be "
        L"HLAfloat64Interval.");
  }
  p->setInterval(v);
}

void DLCRTIambassadorImpl::retract(MessageRetractionHandle theHandle) {
  // §8.21 — retract by raw handle via M17's Retract RPC. M37 EC-2: the §6
  // TSO update/sendInteraction overloads now return REAL handles from the
  // retractable wire variants, so this cancels a still-buffered TSO
  // message for real; receivers that had it queued get §8.22
  // requestRetraction. An unmatched/already-delivered handle is still OK
  // per M17's header contract.
  auto const raw = rawMessageRetractionHandle(theHandle);
  bridge([&] { m17_->retract(raw); });
}

void DLCRTIambassadorImpl::changeAttributeOrderType(
    ObjectInstanceHandle theObject,
    AttributeHandleSet const& theAttributes,
    OrderType theType) {
  // §8.23 — per-attribute order change. No M17 wire: gorti's delivery
  // order is FOM-declared per attribute (see federation.fom.xml <order>).
  // Spec-legal no-op, documented divergence (see section comment).
  (void)theObject;
  (void)theAttributes;
  (void)theType;
}

void DLCRTIambassadorImpl::changeInteractionOrderType(
    InteractionClassHandle theClass,
    OrderType theType) {
  // §8.24 — per-interaction-class order change. Same M17 wire gap as
  // §8.23; spec-legal no-op, documented divergence.
  (void)theClass;
  (void)theType;
}

// ===== §9 DDM =====
//
// M36 Agent CA-3: real M17 delegation via M17Bridge (replaces the M32
// stubs). Shape notes:
//   - gorti's DDM wire is HLA 1.3-shaped (routing spaces). The FOM parser
//     drops every 1516e <dimension> into the implicit routing space
//     "default" (rti/internal/ddm/state.go populateFromFOM), so the DLC's
//     space-less §9 surface resolves that one space lazily via
//     ddmDefaultSpace_() and threads its handle through createRegion /
//     getDimensionHandle.
//   - AttributeHandleSetRegionHandleSetPairVector flattens per-pair: the
//     subscribe/unsubscribe/requestUpdate shims loop the pairs into
//     M17's (attrs, regions) form; the register/associate shims merge the
//     pairs into M17's AttributeRegionMap (map<attr, region-set>).
//   - `bool active` / `wstring updateRateDesignator` on the subscribe
//     overloads are dropped exactly like §5 (documented divergence,
//     catalogue §11 rows 11.9/11.11).
//   - sendInteractionWithRegions: DLC-mandatory tag dropped (M17 wire has
//     no tag on the DDM send — same as §6); the TSO overload narrows
//     LogicalTime to M17's optional<double> and returns an invalid
//     placeholder MessageRetractionHandle (same contract as §6 TSO).

namespace {
// RegionHandleSet → raw uint64 vector for the bridge boundary.
std::vector<std::uint64_t> regions2raw_(RegionHandleSet const& regions) {
  std::vector<std::uint64_t> out;
  out.reserve(regions.size());
  for (auto const& r : regions) out.push_back(rawRegionHandle(r));
  return out;
}
// Pair-vector → M17-shaped map<attr, region-vector>. Later pairs merge
// into earlier ones (spec allows one attribute in multiple pairs; the
// region sets union).
std::map<std::uint64_t, std::vector<std::uint64_t>> pairs2attrRegions_(
    AttributeHandleSetRegionHandleSetPairVector const& pairs) {
  std::map<std::uint64_t, std::vector<std::uint64_t>> out;
  for (auto const& pr : pairs) {
    auto const raw_regions = regions2raw_(pr.second);
    for (auto const& a : pr.first) {
      auto& v = out[rawAttributeHandle(a)];
      v.insert(v.end(), raw_regions.begin(), raw_regions.end());
    }
  }
  return out;
}
}  // namespace

// Lazily resolves gorti's implicit "default" routing space (see section
// comment). Requires a joined federate — M17's LookupRoutingSpace RPC
// enforces that, and the resulting spec exception surfaces unchanged.
std::uint64_t DLCRTIambassadorImpl::ddmDefaultSpace_() {
  if (ddm_default_space_ == 0) {
    ddm_default_space_ =
        bridgeR([&] { return m17_->getRoutingSpaceHandle("default"); });
  }
  return ddm_default_space_;
}

RegionHandle DLCRTIambassadorImpl::createRegion(
    DimensionHandleSet const& theDimensions) {
  // §9.2 — create in the implicit default routing space.
  if (theDimensions.empty())
    throw InvalidDimensionHandle(L"createRegion: empty dimension set.");
  std::vector<std::uint64_t> dims;
  dims.reserve(theDimensions.size());
  for (auto const& d : theDimensions) dims.push_back(rawDimensionHandle(d));
  auto const space = ddmDefaultSpace_();
  return bridgeR([&] {
    return makeRegionHandleFromUint64(m17_->createRegion(space, dims));
  });
}

void DLCRTIambassadorImpl::commitRegionModifications(
    RegionHandleSet const& theRegions) {
  // §9.3 — publish pending range-bound changes federation-wide.
  bridge([&] { m17_->commitRegionModifications(regions2raw_(theRegions)); });
}

void DLCRTIambassadorImpl::deleteRegion(RegionHandle const& theRegion) {
  // §9.4 — delete; M17 auto-unbinds subscribers/publishers.
  if (!theRegion.isValid()) throw InvalidRegion(L"deleteRegion");
  bridge([&] { m17_->deleteRegion(rawRegionHandle(theRegion)); });
}

// §9.5 registerObjectInstanceWithRegions — fused emulation. gorti's
// RegisterObjectInstanceWithRegions RPC is a Cut-3 stub: it returns
// ObjectHandle 0 and records nothing (see rti/internal/transport/grpc/
// ddm.go "Cut-3 simplification" comment — the caller must pair it with a
// plain RegisterObject). The DLC shim therefore registers the object
// through the real §6.8 path first, then binds the per-attribute regions
// via AssociateRegionsForUpdates — the exact fused-call pattern the
// Python SDK's federation.RegisterObjectInstanceWithRegions uses.
ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstanceWithRegions(
    ObjectClassHandle theClass,
    AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions) {
  // Overload 1 — RTI-generated name.
  return bridgeR([&] {
    auto raw = m17_->registerObjectInstance(rawObjectClassHandle(theClass), "");
    m17_->associateRegionsForUpdates(raw,
                                     pairs2attrRegions_(attributesAndRegions));
    return makeObjectInstanceHandleFromUint64(raw);
  });
}
ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstanceWithRegions(
    ObjectClassHandle theClass,
    AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions,
    std::wstring const& theObjectInstanceName) {
  // Overload 2 — federate-supplied (pre-reserved) name.
  return bridgeR([&] {
    auto raw = m17_->registerObjectInstance(rawObjectClassHandle(theClass),
                                            ws2s(theObjectInstanceName));
    m17_->associateRegionsForUpdates(raw,
                                     pairs2attrRegions_(attributesAndRegions));
    return makeObjectInstanceHandleFromUint64(raw);
  });
}

void DLCRTIambassadorImpl::associateRegionsForUpdates(
    ObjectInstanceHandle theObject,
    AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions) {
  // §9.6 — bind per-attribute regions on an existing object.
  bridge([&] {
    m17_->associateRegionsForUpdates(rawObjectInstanceHandle(theObject),
                                     pairs2attrRegions_(attributesAndRegions));
  });
}
void DLCRTIambassadorImpl::unassociateRegionsForUpdates(
    ObjectInstanceHandle theObject,
    AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions) {
  // §9.7 — drop per-attribute region bindings.
  bridge([&] {
    m17_->unassociateRegionsForUpdates(
        rawObjectInstanceHandle(theObject),
        pairs2attrRegions_(attributesAndRegions));
  });
}

void DLCRTIambassadorImpl::subscribeObjectClassAttributesWithRegions(
    ObjectClassHandle theClass,
    AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions,
    bool /*active*/, std::wstring const& /*updateRateDesignator*/) {
  // §9.8 — per-pair delegation; active/updateRate dropped (see section
  // comment).
  bridge([&] {
    for (auto const& pr : attributesAndRegions) {
      m17_->subscribeObjectClassAttributesWithRegions(
          rawObjectClassHandle(theClass), attrs2raw_(pr.first),
          regions2raw_(pr.second));
    }
  });
}
void DLCRTIambassadorImpl::unsubscribeObjectClassAttributesWithRegions(
    ObjectClassHandle theClass,
    AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions) {
  // §9.9 — per-pair delegation.
  bridge([&] {
    for (auto const& pr : attributesAndRegions) {
      m17_->unsubscribeObjectClassAttributesWithRegions(
          rawObjectClassHandle(theClass), attrs2raw_(pr.first),
          regions2raw_(pr.second));
    }
  });
}

void DLCRTIambassadorImpl::subscribeInteractionClassWithRegions(
    InteractionClassHandle theClass, RegionHandleSet const& theRegions,
    bool /*active*/) {
  // §9.10 — active dropped (see section comment).
  bridge([&] {
    m17_->subscribeInteractionClassWithRegions(
        rawInteractionClassHandle(theClass), regions2raw_(theRegions));
  });
}
void DLCRTIambassadorImpl::unsubscribeInteractionClassWithRegions(
    InteractionClassHandle theClass, RegionHandleSet const& theRegions) {
  // §9.11.
  bridge([&] {
    m17_->unsubscribeInteractionClassWithRegions(
        rawInteractionClassHandle(theClass), regions2raw_(theRegions));
  });
}

void DLCRTIambassadorImpl::sendInteractionWithRegions(
    InteractionClassHandle theInteraction,
    ParameterHandleValueMap const& theParameterValues,
    RegionHandleSet const& theRegions,
    VariableLengthData const& /*theUserSuppliedTag*/) {
  // §9.12 overload 1 (RO). Tag dropped (M17 DDM send carries none).
  std::map<std::uint64_t, std::vector<std::uint8_t>> params;
  for (auto const& kv : theParameterValues) {
    auto const* p = static_cast<uint8_t const*>(kv.second.data());
    params.emplace(rawParameterHandle(kv.first),
                   std::vector<uint8_t>(p, p + kv.second.size()));
  }
  bridge([&] {
    m17_->sendInteractionWithRegions(rawInteractionClassHandle(theInteraction),
                                     params, regions2raw_(theRegions),
                                     std::nullopt);
  });
}
MessageRetractionHandle DLCRTIambassadorImpl::sendInteractionWithRegions(
    InteractionClassHandle theInteraction,
    ParameterHandleValueMap const& theParameterValues,
    RegionHandleSet const& theRegions,
    VariableLengthData const& /*theUserSuppliedTag*/,
    LogicalTime const& theTime) {
  // §9.12 overload 2 (TSO). Time narrows to M17's optional<double>;
  // placeholder retraction handle (same contract as §6 TSO overloads).
  double const t = narrowTime_(theTime, "sendInteractionWithRegions");
  std::map<std::uint64_t, std::vector<std::uint8_t>> params;
  for (auto const& kv : theParameterValues) {
    auto const* p = static_cast<uint8_t const*>(kv.second.data());
    params.emplace(rawParameterHandle(kv.first),
                   std::vector<uint8_t>(p, p + kv.second.size()));
  }
  bridge([&] {
    m17_->sendInteractionWithRegions(rawInteractionClassHandle(theInteraction),
                                     params, regions2raw_(theRegions),
                                     std::optional<double>(t));
  });
  return MessageRetractionHandle();  // spec-legal invalid placeholder
}

void DLCRTIambassadorImpl::requestAttributeValueUpdateWithRegions(
    ObjectClassHandle theClass,
    AttributeHandleSetRegionHandleSetPairVector const& attributesAndRegions,
    VariableLengthData const& theUserSuppliedTag) {
  // §9.13 — per-pair delegation; tag passes through (M17 carries it).
  auto const tag = tag2bytes_(theUserSuppliedTag);
  bridge([&] {
    for (auto const& pr : attributesAndRegions) {
      m17_->requestAttributeValueUpdateWithRegions(
          rawObjectClassHandle(theClass), attrs2raw_(pr.first),
          regions2raw_(pr.second), tag);
    }
  });
}

// ===== §10 Support Services =====
//
// M33 Agent M impl. Design: the DLC RTIambassador has no gRPC binding yet —
// it's the front-end for a future M35 dispatch layer. So handle-lookup
// methods that need federation state throw NotConnected (spec-legal error),
// while stateless conversions (order/transport enum ↔ name, normalize) are
// fully implemented. Advisory-switch on/off pairs no-op (silent success —
// there's no advisory infrastructure to gate yet; the DLC contract only
// requires void return, no throw). evokeCallback / evokeMultipleCallbacks
// no-op returning false (no queued events). getTimeFactory returns a
// null-throwing placeholder (Task M-4: factory-factory chain deferred).
// The 9 decode*Handle methods use per-handle Friend shims (defined below).
//
// Catalogue rows 13.1-13.15 all touched. FR-DLC-4 (wstring), FR-DLC-14
// (callback re-entry) exercised by test_rtiambassador_handle_services.cpp
// static_asserts.

ResignAction DLCRTIambassadorImpl::getAutomaticResignDirective() {
  // §10.2 — default per Pitch is NO_ACTION; return that until a real
  // federation binding lands.
  return NO_ACTION;
}
void DLCRTIambassadorImpl::setAutomaticResignDirective(ResignAction) {
  // §10.3 — no federation binding; silently accept. Spec-legal (returns
  // void, no throw required in the not-connected case for this setter).
}
FederateHandle DLCRTIambassadorImpl::getFederateHandle(
    std::wstring const& theName) {
  // §10.4 — M36 Agent DA: real resolution via the M24
  // ListFederationMembers wire (M17Bridge::getFederateHandle). Unknown
  // names surface as NameNotFound through translateBridgeError.
  return bridgeR([&] {
    auto raw = m17_->getFederateHandle(ws2s(theName));
    return makeFederateHandleFromUint64(raw);
  });
}
std::wstring DLCRTIambassadorImpl::getFederateName(FederateHandle theHandle) {
  // §10.5 — M36 Agent DA: reverse lookup over the same member list.
  if (!theHandle.isValid()) throw InvalidFederateHandle(L"getFederateName");
  return bridgeR([&] {
    auto name = m17_->getFederateName(rawFederateHandle(theHandle));
    return s2ws(name);
  });
}
// M35 Agent BH — §10 support services now delegate to M17 for real
// FOM name↔handle resolution. The M17 ambassador caches results, so
// repeated lookups don't re-hit the wire. NameNotFound / Invalid*Handle
// surface through translateBridgeError as the matching DLC spec
// exceptions.
ObjectClassHandle DLCRTIambassadorImpl::getObjectClassHandle(
    std::wstring const& theName) {
  // §10.6 — real delegation. Widens M17's uint64 return via the file-
  // bottom ObjectClassHandleFriend shim.
  return bridgeR([&] {
    auto raw = m17_->getObjectClassHandle(ws2s(theName));
    return makeObjectClassHandleFromUint64(raw);
  });
}
std::wstring DLCRTIambassadorImpl::getObjectClassName(
    ObjectClassHandle theHandle) {
  // §10.7 — validate then reverse-lookup.
  if (!theHandle.isValid())
    throw InvalidObjectClassHandle(L"getObjectClassName");
  return bridgeR([&] {
    auto name = m17_->getObjectClassName(rawObjectClassHandle(theHandle));
    return s2ws(name);
  });
}
ObjectClassHandle DLCRTIambassadorImpl::getKnownObjectClassHandle(
    ObjectInstanceHandle) {
  // §10.8 — need federation state to look up instance's known class.
  throw NotConnected(L"DLC RTIambassador: getKnownObjectClassHandle requires "
                     L"federation connection (M35+).");
}
ObjectInstanceHandle DLCRTIambassadorImpl::getObjectInstanceHandle(
    std::wstring const&) {
  // §10.9 — need federation state to resolve instance name.
  throw NotConnected(L"DLC RTIambassador: getObjectInstanceHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getObjectInstanceName(
    ObjectInstanceHandle) {
  // §10.10 — need federation state.
  throw NotConnected(L"DLC RTIambassador: getObjectInstanceName requires "
                     L"federation connection (M35+).");
}
AttributeHandle DLCRTIambassadorImpl::getAttributeHandle(
    ObjectClassHandle theClass, std::wstring const& theName) {
  // §10.11 — M35 Agent BH real delegation. Class-handle validation
  // happens on the M17 side (throws InvalidObjectClassHandle if the
  // class is unknown, NameNotFound if the attribute name isn't in it).
  if (!theClass.isValid())
    throw InvalidObjectClassHandle(L"getAttributeHandle");
  return bridgeR([&] {
    auto raw = m17_->getAttributeHandle(rawObjectClassHandle(theClass),
                                        ws2s(theName));
    return makeAttributeHandleFromUint64(raw);
  });
}
std::wstring DLCRTIambassadorImpl::getAttributeName(
    ObjectClassHandle theClass, AttributeHandle theAttribute) {
  // §10.12 — M35 Agent BH real delegation.
  if (!theClass.isValid())
    throw InvalidObjectClassHandle(L"getAttributeName");
  if (!theAttribute.isValid())
    throw InvalidAttributeHandle(L"getAttributeName");
  return bridgeR([&] {
    auto name = m17_->getAttributeName(rawObjectClassHandle(theClass),
                                       rawAttributeHandle(theAttribute));
    return s2ws(name);
  });
}
double DLCRTIambassadorImpl::getUpdateRateValue(std::wstring const&) {
  // §10.13 — need FOM state (update-rate designators are FOM-declared).
  throw InvalidUpdateRateDesignator(L"DLC RTIambassador: no FOM loaded "
                                    L"(federation connection deferred M35+).");
}
double DLCRTIambassadorImpl::getUpdateRateValueForAttribute(
    ObjectInstanceHandle, AttributeHandle) {
  // §10.14 — need federation state.
  throw NotConnected(L"DLC RTIambassador: getUpdateRateValueForAttribute "
                     L"requires federation connection (M35+).");
}
InteractionClassHandle DLCRTIambassadorImpl::getInteractionClassHandle(
    std::wstring const& theName) {
  // §10.15 — M35 Agent BH real delegation.
  return bridgeR([&] {
    auto raw = m17_->getInteractionClassHandle(ws2s(theName));
    return makeInteractionClassHandleFromUint64(raw);
  });
}
std::wstring DLCRTIambassadorImpl::getInteractionClassName(
    InteractionClassHandle theHandle) {
  // §10.16 — M35 Agent BH real delegation.
  if (!theHandle.isValid())
    throw InvalidInteractionClassHandle(L"getInteractionClassName");
  return bridgeR([&] {
    auto name = m17_->getInteractionClassName(
        rawInteractionClassHandle(theHandle));
    return s2ws(name);
  });
}
ParameterHandle DLCRTIambassadorImpl::getParameterHandle(
    InteractionClassHandle theClass, std::wstring const& theName) {
  // §10.17 — M35 Agent BH real delegation.
  if (!theClass.isValid())
    throw InvalidInteractionClassHandle(L"getParameterHandle");
  return bridgeR([&] {
    auto raw = m17_->getParameterHandle(rawInteractionClassHandle(theClass),
                                        ws2s(theName));
    return makeParameterHandleFromUint64(raw);
  });
}
std::wstring DLCRTIambassadorImpl::getParameterName(
    InteractionClassHandle theClass, ParameterHandle theParameter) {
  // §10.18 — M35 Agent BH real delegation.
  if (!theClass.isValid())
    throw InvalidInteractionClassHandle(L"getParameterName");
  if (!theParameter.isValid())
    throw InvalidParameterHandle(L"getParameterName");
  return bridgeR([&] {
    auto name = m17_->getParameterName(
        rawInteractionClassHandle(theClass),
        rawParameterHandle(theParameter));
    return s2ws(name);
  });
}

// §10.19 getOrderType — spec-defined name ↔ enum mapping. Names per
// IEEE 1516.1-2010 Table 10-1: L"Receive", L"TimeStamp".
OrderType DLCRTIambassadorImpl::getOrderType(std::wstring const& orderName) {
  if (orderName == L"Receive") return RECEIVE;
  if (orderName == L"TimeStamp") return TIMESTAMP;
  throw InvalidOrderName(L"DLC RTIambassador: unknown OrderType name '" +
                         orderName + L"'.");
}
std::wstring DLCRTIambassadorImpl::getOrderName(OrderType theType) {
  switch (theType) {
    case RECEIVE:   return L"Receive";
    case TIMESTAMP: return L"TimeStamp";
  }
  throw InvalidOrderType(L"DLC RTIambassador: OrderType out of range.");
}

// §10.21 getTransportationType — spec-defined name ↔ enum mapping. Names per
// IEEE 1516.1-2010 Table 10-1: L"HLAreliable", L"HLAbestEffort".
TransportationType DLCRTIambassadorImpl::getTransportationType(
    std::wstring const& transportationName) {
  if (transportationName == L"HLAreliable")   return RELIABLE;
  if (transportationName == L"HLAbestEffort") return BEST_EFFORT;
  throw InvalidTransportationName(
      L"DLC RTIambassador: unknown TransportationType name '" +
      transportationName + L"'.");
}
std::wstring DLCRTIambassadorImpl::getTransportationName(
    TransportationType theType) {
  switch (theType) {
    case RELIABLE:    return L"HLAreliable";
    case BEST_EFFORT: return L"HLAbestEffort";
  }
  throw InvalidTransportationType(
      L"DLC RTIambassador: TransportationType out of range.");
}

DimensionHandleSet DLCRTIambassadorImpl::getAvailableDimensionsForClassAttribute(
    ObjectClassHandle, AttributeHandle) {
  // §10.23 — need FOM state (dimensions are FOM-declared).
  throw NotConnected(L"DLC RTIambassador: getAvailableDimensionsForClass"
                     L"Attribute requires federation connection (M35+).");
}
DimensionHandleSet
DLCRTIambassadorImpl::getAvailableDimensionsForInteractionClass(
    InteractionClassHandle) {
  // §10.24 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getAvailableDimensionsForInteraction"
                     L"Class requires federation connection (M35+).");
}
DimensionHandle DLCRTIambassadorImpl::getDimensionHandle(
    std::wstring const& theName) {
  // §10.25 — M36 Agent CA-3 real delegation. 1516e dimensions live in
  // gorti's implicit "default" routing space (see §9 section comment);
  // the space handle resolves lazily and the M17 lookup runs against it.
  auto const space = ddmDefaultSpace_();
  return bridgeR([&] {
    return makeDimensionHandleFromUint64(
        m17_->getDimensionHandle(space, ws2s(theName)));
  });
}
std::wstring DLCRTIambassadorImpl::getDimensionName(DimensionHandle theHandle) {
  // §10.26 — need FOM state.
  if (!theHandle.isValid())
    throw InvalidDimensionHandle(L"getDimensionName");
  throw NotConnected(L"DLC RTIambassador: getDimensionName requires "
                     L"federation connection (M35+).");
}
unsigned long DLCRTIambassadorImpl::getDimensionUpperBound(
    DimensionHandle theHandle) {
  // §10.27 — need FOM state.
  if (!theHandle.isValid())
    throw InvalidDimensionHandle(L"getDimensionUpperBound");
  throw NotConnected(L"DLC RTIambassador: getDimensionUpperBound requires "
                     L"federation connection (M35+).");
}
DimensionHandleSet DLCRTIambassadorImpl::getDimensionHandleSet(
    RegionHandle theRegion) {
  // §10.28 — need region-registry state.
  if (!theRegion.isValid()) throw InvalidRegion(L"getDimensionHandleSet");
  throw NotConnected(L"DLC RTIambassador: getDimensionHandleSet requires "
                     L"federation connection (M35+).");
}
RangeBounds DLCRTIambassadorImpl::getRangeBounds(RegionHandle theRegion,
                                                 DimensionHandle theDim) {
  // §10.29 — M36 Agent CA-3 real delegation via M17 queryBounds. A region
  // with no committed bounds for the dimension reports found=false; the
  // spec maps that to RegionDoesNotContainSpecifiedDimension.
  if (!theRegion.isValid()) throw InvalidRegion(L"getRangeBounds");
  if (!theDim.isValid()) throw InvalidDimensionHandle(L"getRangeBounds");
  auto const r = bridgeR([&] {
    return m17_->queryRangeBounds(rawRegionHandle(theRegion),
                                  rawDimensionHandle(theDim));
  });
  if (!r.found) {
    throw RegionDoesNotContainSpecifiedDimension(
        L"getRangeBounds: region has no bounds for the dimension.");
  }
  return RangeBounds(static_cast<unsigned long>(r.lower),
                     static_cast<unsigned long>(r.upper));
}
void DLCRTIambassadorImpl::setRangeBounds(RegionHandle theRegion,
                                          DimensionHandle theDim,
                                          RangeBounds const& bounds) {
  // §10.30 — M36 Agent CA-3 real delegation. Validate arguments per spec
  // before hitting the wire.
  if (!theRegion.isValid()) throw InvalidRegion(L"setRangeBounds");
  if (!theDim.isValid()) throw InvalidDimensionHandle(L"setRangeBounds");
  if (bounds.getLowerBound() > bounds.getUpperBound())
    throw InvalidRangeBound(L"setRangeBounds: lower > upper.");
  bridge([&] {
    m17_->setRangeBounds(rawRegionHandle(theRegion),
                         rawDimensionHandle(theDim),
                         static_cast<std::uint64_t>(bounds.getLowerBound()),
                         static_cast<std::uint64_t>(bounds.getUpperBound()));
  });
}

// §10.31 normalizeFederateHandle — returns a stable per-federation numeric
// tag suitable as a random-partition seed. The gorti Handle PIMPL stores a
// uint64 internally; hash() exposes the low 32 bits as a long, which suffices
// as a normalization value.
unsigned long DLCRTIambassadorImpl::normalizeFederateHandle(
    FederateHandle theHandle) {
  if (!theHandle.isValid())
    throw InvalidFederateHandle(L"normalizeFederateHandle: invalid handle.");
  return static_cast<unsigned long>(theHandle.hash());
}

// §10.32 normalizeServiceGroup — enum → tag. Cast is safe: the enum
// values are 0..6 for the 7 service groups per IEEE 1516.1-2010 §10.32.
unsigned long DLCRTIambassadorImpl::normalizeServiceGroup(ServiceGroup group) {
  switch (group) {
    case FEDERATION_MANAGEMENT:        return 0;
    case DECLARATION_MANAGEMENT:       return 1;
    case OBJECT_MANAGEMENT:            return 2;
    case OWNERSHIP_MANAGEMENT:         return 3;
    case TIME_MANAGEMENT:              return 4;
    case DATA_DISTRIBUTION_MANAGEMENT: return 5;
    case SUPPORT_SERVICES:             return 6;
  }
  throw InvalidServiceGroup(L"normalizeServiceGroup: enum out of range.");
}

// §10.33-10.40 — 8 advisory-switch on/off pairs. No advisory infrastructure
// is wired in DLC M33 yet, so these silently succeed (spec-legal — the
// setter contract only requires void return). Once M35 lands a real
// dispatch layer, the switches will gate advisory-callback dispatch.
//
// M37 EC-2 — attribute SCOPE advisory pair: gorti's server emits
// AttributesInScope/OutOfScope UNCONDITIONALLY whenever DDM region-overlap
// membership changes (rti/internal/object/update.go emitScopeAdvisories;
// there is no per-federate switch RPC on the wire). The DLC switch is
// therefore accept-and-record only — the §6.17/§6.18 callbacks arrive
// regardless of the recorded state. Documented divergence; a wire-level
// per-federate gate is future-server territory.
void DLCRTIambassadorImpl::enableObjectClassRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableObjectClassRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::enableAttributeRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableAttributeRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::enableAttributeScopeAdvisorySwitch() {
  scope_advisory_recorded_ = true;  // accept-and-record (see block comment)
}
void DLCRTIambassadorImpl::disableAttributeScopeAdvisorySwitch() {
  scope_advisory_recorded_ = false;  // accept-and-record (see block comment)
}
void DLCRTIambassadorImpl::enableInteractionRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableInteractionRelevanceAdvisorySwitch() {}

// §10.41 evokeCallback — single-arg spec form (catalogue 13.11).
// No event queue in DLC M33 yet; returns false to signal "no more callbacks
// pending". Federate can loop on this call safely.
bool DLCRTIambassadorImpl::evokeCallback(double approximateMinimumTimeInSeconds) {
  // M37 EC-4 — synthesized callbacks (queued by the §7.17/§8.2/§8.5/§4.8
  // synthesis sites) deliver FIRST, one per evoke, matching Pitch's
  // deliver-after-return ordering.
  if (drainOneSynthesized_()) return true;
  // Delegate to M17's at-most-one drain (M17.22 semantics). §10.4-exempt
  // from the FR-DLC-14 re-entrancy gate (spec allows evoking from within
  // a callback) — unguarded form.
  return bridgeRUnguarded([&] {
    return m17_->evokeCallback(approximateMinimumTimeInSeconds,
                               approximateMinimumTimeInSeconds);
  });
}

// §10.42 evokeMultipleCallbacks — 2 args, no defaults (catalogue 13.12).
// Same rationale as evokeCallback: no queued events, return false.
bool DLCRTIambassadorImpl::evokeMultipleCallbacks(
    double approximateMinimumTimeInSeconds,
    double approximateMaximumTimeInSeconds) {
  // M37 EC-4 — deliver every queued synthesized callback before the wire
  // events (see evokeCallback note).
  auto const drained = drainAllSynthesized_();
  // Delegate to M17's drain-in-window (tickCallback alias). §10.4-exempt
  // from the re-entrancy gate — unguarded form.
  bool const wire_fired = bridgeRUnguarded([&] {
    return m17_->evokeMultipleCallbacks(approximateMinimumTimeInSeconds,
                                        approximateMaximumTimeInSeconds);
  });
  return wire_fired || drained > 0;
}

// §10.43-10.44 enable/disableCallbacks — M35 Agent BH: delegate to M17
// which supports these natively (gates its own dispatch loop). §10.4
// exempts both from the FR-DLC-14 re-entrancy gate.
void DLCRTIambassadorImpl::enableCallbacks() {
  bridgeUnguarded([&] { m17_->enableCallbacks(); });
}
void DLCRTIambassadorImpl::disableCallbacks() {
  bridgeUnguarded([&] { m17_->disableCallbacks(); });
}

// §10 (catalogue 13.14) getTimeFactory — returns the federation's logical-time
// factory. The factory-factory chain (LogicalTimeFactoryFactory → concrete
// HLAfloat64TimeFactory) requires a federation connection to know which
// implementation name was negotiated at join time. Until M35 lands that,
// we throw a spec-legal exception rather than return a null unique_ptr
// (which would trip caller-side UB on the first dereference).
rti1516e::auto_ptr<LogicalTimeFactory>
DLCRTIambassadorImpl::getTimeFactory() const {
  throw CouldNotCreateLogicalTimeFactory(
      L"DLC RTIambassador: getTimeFactory requires federation connection "
      L"(M35+); factory-factory chain deferred.");
}


}  // namespace rti1516e  (temporarily close for friend-shim definitions)

// The DEFINE_HANDLE_CLASS macro declares the VLD-ctor `protected` — we can't
// call it from outside the class hierarchy. Use per-handle Friend shims,
// declared by the macro as `friend class HandleKind##Friend;`.

#define DEFINE_HANDLE_FRIEND(HandleKind)                                \
  namespace rti1516e {                                                  \
  class HandleKind##Friend {                                            \
   public:                                                              \
    static HandleKind decode(VariableLengthData const& v) {             \
      return HandleKind(v);                                             \
    }                                                                   \
  };                                                                    \
  }

DEFINE_HANDLE_FRIEND(FederateHandle)
DEFINE_HANDLE_FRIEND(ObjectClassHandle)
DEFINE_HANDLE_FRIEND(InteractionClassHandle)
DEFINE_HANDLE_FRIEND(ObjectInstanceHandle)
DEFINE_HANDLE_FRIEND(AttributeHandle)
DEFINE_HANDLE_FRIEND(ParameterHandle)
DEFINE_HANDLE_FRIEND(DimensionHandle)
DEFINE_HANDLE_FRIEND(MessageRetractionHandle)
DEFINE_HANDLE_FRIEND(RegionHandle)

namespace rti1516e {

// §10 (catalogue 13.15) — 9 decode*Handle methods. All `const` per Agent F's
// M32 drift fix. Impl: each Friend shim invokes the handle's
// VariableLengthData ctor (declared `protected` by DEFINE_HANDLE_CLASS
// so we can't call it directly from a free function).
FederateHandle DLCRTIambassadorImpl::decodeFederateHandle(
    VariableLengthData const& v) const {
  return FederateHandleFriend::decode(v);
}
ObjectClassHandle DLCRTIambassadorImpl::decodeObjectClassHandle(
    VariableLengthData const& v) const {
  return ObjectClassHandleFriend::decode(v);
}
InteractionClassHandle DLCRTIambassadorImpl::decodeInteractionClassHandle(
    VariableLengthData const& v) const {
  return InteractionClassHandleFriend::decode(v);
}
ObjectInstanceHandle DLCRTIambassadorImpl::decodeObjectInstanceHandle(
    VariableLengthData const& v) const {
  return ObjectInstanceHandleFriend::decode(v);
}
AttributeHandle DLCRTIambassadorImpl::decodeAttributeHandle(
    VariableLengthData const& v) const {
  return AttributeHandleFriend::decode(v);
}
ParameterHandle DLCRTIambassadorImpl::decodeParameterHandle(
    VariableLengthData const& v) const {
  return ParameterHandleFriend::decode(v);
}
DimensionHandle DLCRTIambassadorImpl::decodeDimensionHandle(
    VariableLengthData const& v) const {
  return DimensionHandleFriend::decode(v);
}
MessageRetractionHandle DLCRTIambassadorImpl::decodeMessageRetractionHandle(
    VariableLengthData const& v) const {
  return MessageRetractionHandleFriend::decode(v);
}
RegionHandle DLCRTIambassadorImpl::decodeRegionHandle(
    VariableLengthData const& v) const {
  return RegionHandleFriend::decode(v);
}

// M34 Agent AA — bodies for the FederateHandle adapters declared at the
// top of this TU. Defined here (after DEFINE_HANDLE_FRIEND expansion) so
// the file-bottom `rti1516e::FederateHandleFriend` class is visible.
// The uint64 encoding is the spec's 8-byte big-endian shape (§10.5).
FederateHandle makeFederateHandleFromUint64(std::uint64_t v) {
  unsigned char buf[8];
  for (int i = 0; i < 8; ++i) {
    buf[i] = static_cast<unsigned char>((v >> (56 - i * 8)) & 0xff);
  }
  VariableLengthData vld(buf, 8);
  return FederateHandleFriend::decode(vld);
}
std::uint64_t rawFederateHandle(FederateHandle const& h) {
  VariableLengthData vld = h.encode();
  auto const* p = static_cast<unsigned char const*>(vld.data());
  std::uint64_t v = 0;
  // Defensive: encoded FederateHandle is exactly 8 bytes per §10.5.
  for (size_t i = 0; i < 8 && i < vld.size(); ++i) {
    v = (v << 8) | p[i];
  }
  return v;
}

// M35 Agent BC — §6 handle adapters. Same 8-byte big-endian encoding as
// FederateHandle (§10.5 is uniform across all handle types).
namespace {
std::uint64_t decodeHandleVLD_(VariableLengthData const& vld) {
  auto const* p = static_cast<unsigned char const*>(vld.data());
  std::uint64_t v = 0;
  for (size_t i = 0; i < 8 && i < vld.size(); ++i) {
    v = (v << 8) | p[i];
  }
  return v;
}
VariableLengthData encodeHandleVLD_(std::uint64_t v) {
  unsigned char buf[8];
  for (int i = 0; i < 8; ++i) {
    buf[i] = static_cast<unsigned char>((v >> (56 - i * 8)) & 0xff);
  }
  return VariableLengthData(buf, 8);
}
}  // namespace

ObjectInstanceHandle makeObjectInstanceHandleFromUint64(std::uint64_t v) {
  return ObjectInstanceHandleFriend::decode(encodeHandleVLD_(v));
}
std::uint64_t rawObjectClassHandle(ObjectClassHandle const& h) {
  return decodeHandleVLD_(h.encode());
}
std::uint64_t rawObjectInstanceHandle(ObjectInstanceHandle const& h) {
  return decodeHandleVLD_(h.encode());
}
std::uint64_t rawInteractionClassHandle(InteractionClassHandle const& h) {
  return decodeHandleVLD_(h.encode());
}
std::uint64_t rawAttributeHandle(AttributeHandle const& h) {
  return decodeHandleVLD_(h.encode());
}
std::uint64_t rawParameterHandle(ParameterHandle const& h) {
  return decodeHandleVLD_(h.encode());
}


// M35 Agent BH — §10 support-service adapters bodies. Uses same 8-byte
// big-endian VLD shape as FederateHandle.
namespace {
VariableLengthData encodeHandleBE_(std::uint64_t v) {
  unsigned char buf[8];
  for (int i = 0; i < 8; ++i) {
    buf[i] = static_cast<unsigned char>((v >> (56 - i * 8)) & 0xff);
  }
  return VariableLengthData(buf, 8);
}
}  // namespace
ObjectClassHandle makeObjectClassHandleFromUint64(std::uint64_t v) {
  return ObjectClassHandleFriend::decode(encodeHandleBE_(v));
}
AttributeHandle makeAttributeHandleFromUint64(std::uint64_t v) {
  return AttributeHandleFriend::decode(encodeHandleBE_(v));
}
InteractionClassHandle makeInteractionClassHandleFromUint64(std::uint64_t v) {
  return InteractionClassHandleFriend::decode(encodeHandleBE_(v));
}
ParameterHandle makeParameterHandleFromUint64(std::uint64_t v) {
  return ParameterHandleFriend::decode(encodeHandleBE_(v));
}
std::uint64_t rawParameterHandle_(ParameterHandle const& h) {
  // Convert BC's decodeHandleVLD_ result to BE via encode round-trip.
  return rawParameterHandle(h);  // same uint64 as BC's version
}

// M36 Agent CA — §8/§9 handle adapter bodies.
DimensionHandle makeDimensionHandleFromUint64(std::uint64_t v) {
  return DimensionHandleFriend::decode(encodeHandleBE_(v));
}
RegionHandle makeRegionHandleFromUint64(std::uint64_t v) {
  return RegionHandleFriend::decode(encodeHandleBE_(v));
}
std::uint64_t rawDimensionHandle(DimensionHandle const& h) {
  return decodeHandleVLD_(h.encode());
}
std::uint64_t rawRegionHandle(RegionHandle const& h) {
  return decodeHandleVLD_(h.encode());
}
std::uint64_t rawMessageRetractionHandle(MessageRetractionHandle const& h) {
  return decodeHandleVLD_(h.encode());
}
// M37 Agent EC-2 — retractable-send return widening.
MessageRetractionHandle makeMessageRetractionHandleFromUint64(
    std::uint64_t v) {
  return MessageRetractionHandleFriend::decode(encodeHandleBE_(v));
}

}  // namespace rti1516e
