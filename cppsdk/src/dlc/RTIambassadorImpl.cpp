// IEEE 1516.1-2010 §10.6 / Annex A — DLC RTIambassador stub impl.
//
// gorti M32. Every method throws RTIinternalError("M32 stub — impl in M33+").
// M32 GREEN target is LINK-only; runtime bodies land M33+ as wstring-adapters
// over gorti's M17 gRPC surface.

#include "RTIambassadorImpl.h"

#include "M17Bridge.h"

#include <RTI/Exception.h>
#include <RTI/LogicalTime.h>
#include <RTI/RangeBounds.h>
#include <RTI/RTIambassadorFactory.h>

#include <cstdint>
#include <sstream>
#include <stdexcept>
#include <string>

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
  // Every other case (including bare RTIinternalError) folds to the
  // spec-legal RTIinternalError catch-all.
  throw RTIinternalError(msg);
}

// Bridge-call wrapper: runs `fn`, catches std::runtime_error from the M17
// bridge, translates to spec exception. Void-returning form.
template <typename Fn>
void bridge(Fn&& fn) {
  try {
    fn();
  } catch (std::runtime_error const& e) {
    translateBridgeError(e);
  }
}
// Bridge-call wrapper with a return value. Uses decltype-auto to preserve
// the caller-side value type (e.g. FederateHandle).
template <typename Fn>
auto bridgeR(Fn&& fn) -> decltype(fn()) {
  try {
    return fn();
  } catch (std::runtime_error const& e) {
    translateBridgeError(e);
  }
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

// Base class ctor/dtor — must have out-of-line definitions for the vtable.
RTIambassador::RTIambassador() RTI_NOEXCEPT {}
RTIambassador::~RTIambassador() {}

DLCRTIambassadorImpl::DLCRTIambassadorImpl()
    : m17_(std::make_unique<M17Bridge>()) {}
DLCRTIambassadorImpl::~DLCRTIambassadorImpl() = default;

// ===== §4 Federation Management =====
//
// M34 Agent AA — wstring-adapter shim over the M17 concrete
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
void DLCRTIambassadorImpl::connect(FederateAmbassador& fed,
                                   CallbackModel callbackModel,
                                   std::wstring const& localSettings) {
  std::string const url = parseLocalSettings(localSettings);
  bridge([&] { m17_->connect(url); });
  fed_amb_ = &fed;
  callback_model_ = callbackModel;
}

// §4.3 — disconnect. Clears the bound FederateAmbassador so late
// callback deliveries can no-op safely (Agent AD checks fed_amb_ before
// dispatching).
void DLCRTIambassadorImpl::disconnect() {
  bridge([&] { m17_->disconnect(); });
  fed_amb_ = nullptr;
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

// §4.7 — asynchronous list. Result arrives via the
// `reportFederationExecutions` FederateAmbassador callback. M17 does not
// yet expose ListFederationExecutions on the wire; the shim is a no-op
// so fixtures that call this method don't error, but no callback fires
// until M17 gains the service. Documented divergence: DLC catalogue §3
// row 3.6 (deferred to M17 Cut-4).
void DLCRTIambassadorImpl::listFederationExecutions() {
  // No-op — no M17 wire target yet.
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

// §4.14 — sync-point achieved. `successfully=false` maps to M17's
// synchronizationPointAchieved followed by... M17 Cut-1 has no "failed"
// variant; the shim always announces achieved. Divergence catalogued
// (M17 lacks the "failed" achieve wire message).
void DLCRTIambassadorImpl::synchronizationPointAchieved(
    std::wstring const& label, bool /*successfully*/) {
  bridge([&] { m17_->synchronizationPointAchieved(ws2s(label)); });
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
void DLCRTIambassadorImpl::publishObjectClassAttributes(
    ObjectClassHandle, AttributeHandleSet const&) {
  m32_stub("publishObjectClassAttributes");
}
void DLCRTIambassadorImpl::unpublishObjectClass(ObjectClassHandle) {
  m32_stub("unpublishObjectClass");
}
void DLCRTIambassadorImpl::unpublishObjectClassAttributes(
    ObjectClassHandle, AttributeHandleSet const&) {
  m32_stub("unpublishObjectClassAttributes");
}
void DLCRTIambassadorImpl::publishInteractionClass(InteractionClassHandle) {
  m32_stub("publishInteractionClass");
}
void DLCRTIambassadorImpl::unpublishInteractionClass(InteractionClassHandle) {
  m32_stub("unpublishInteractionClass");
}
void DLCRTIambassadorImpl::subscribeObjectClassAttributes(
    ObjectClassHandle, AttributeHandleSet const&, bool,
    std::wstring const&) {
  m32_stub("subscribeObjectClassAttributes");
}
void DLCRTIambassadorImpl::unsubscribeObjectClass(ObjectClassHandle) {
  m32_stub("unsubscribeObjectClass");
}
void DLCRTIambassadorImpl::unsubscribeObjectClassAttributes(
    ObjectClassHandle, AttributeHandleSet const&) {
  m32_stub("unsubscribeObjectClassAttributes");
}
void DLCRTIambassadorImpl::subscribeInteractionClass(InteractionClassHandle,
                                                     bool) {
  m32_stub("subscribeInteractionClass");
}
void DLCRTIambassadorImpl::unsubscribeInteractionClass(InteractionClassHandle) {
  m32_stub("unsubscribeInteractionClass");
}

// ===== §6 Object Management =====
//
// M33 Agent L: §6 shim over gorti's M17 RtiAmbassador gRPC surface.
//
// Design:
//   - Spec (§6) uses std::wstring for object/interaction names; M17 uses
//     std::string. Each shim narrows wstring→string via ws2s_() before
//     calling M17. The DLC catalogue §17.1 promotes `theUserSuppliedTag`
//     to MANDATORY on every §6 call; the shim binds it into a
//     std::vector<uint8_t> (m17_tag_) so the M17 wire request always
//     carries the federate-supplied bytes.
//   - The DLCRTIambassadorImpl class currently owns no M17 state member;
//     wiring an M17 RTIambassador into a `pImpl` requires editing
//     RTIambassadorImpl.h, which is outside M33 Agent L's file scope
//     ("Only touch RTIambassadorImpl.cpp lines 159-227"). Until that
//     PIMPL lands (tracked as an M33-follow-up), each shim throws
//     rti1516e::NotConnected — the spec-legal reply for "no CRC bound"
//     that all §6 methods declare in their RTI_THROW clause. This keeps
//     the symbols present (so the 7 om_* conformance fixtures LINK) and
//     is the correct runtime signal for a not-yet-wired federate.
//   - The multi-name variants (§6.5) sort into a std::vector<std::string>
//     preserving the atomic semantics: reserveMultipleObjectInstanceName
//     is one gRPC call (M17: ReserveMultipleObjectInstanceNames);
//     releaseMultipleObjectInstanceName loops the singular M17 release
//     (M17 has no batch release RPC yet).
//   - Async callback shape (§6.5 reservation → objectInstanceNameReservation
//     Succeeded/Failed) is delivered on the federate's FederateAmbassador,
//     wired via the M17 event-stream. That plumbing is unchanged; the
//     spec-facing DLC method returns void and the callback fires when
//     the M17 stream lands the reservation reply.

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

// Central point for the "M17 not yet wired" reply. All §6 methods
// declare NotConnected in their RTI_THROW clause (see Pitch spec header
// §6.5-6.19); this is the spec-legal way to signal "no CRC bound".
[[noreturn]] void m17NotWired_(char const* method) {
  std::wstring msg = L"gorti DLC §6 ";
  for (char const* p = method; *p; ++p)
    msg.push_back(static_cast<wchar_t>(static_cast<unsigned char>(*p)));
  msg += L": M17 RtiAmbassador pImpl not yet wired into "
         L"DLCRTIambassadorImpl (M33 follow-up — needs a private member on "
         L"RTIambassadorImpl.h; tracked as \"M33 header PIMPL\" in the "
         L"dispatch plan). Federate is not connected to any CRC.";
  throw NotConnected(msg);
}

}  // namespace

void DLCRTIambassadorImpl::reserveObjectInstanceName(
    std::wstring const& theObjectInstanceName) {
  // §6.5 — async. Result delivered via objectInstanceNameReservation
  //         {Succeeded,Failed}(name) on the bound FederateAmbassador.
  std::string const name = ws2s_(theObjectInstanceName);
  (void)name;  // Used once M17 pImpl lands.
  m17NotWired_("reserveObjectInstanceName");
}

void DLCRTIambassadorImpl::releaseObjectInstanceName(
    std::wstring const& theObjectInstanceName) {
  // §6.6 — synchronous release of a previously reserved name.
  std::string const name = ws2s_(theObjectInstanceName);
  (void)name;
  m17NotWired_("releaseObjectInstanceName");
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
  (void)names;
  m17NotWired_("reserveMultipleObjectInstanceName");
}

void DLCRTIambassadorImpl::releaseMultipleObjectInstanceName(
    std::set<std::wstring> const& theObjectInstanceNames) {
  // §6.7 (multi) — spec allows non-atomic release. M17 has no batch
  // release RPC, so the shim will loop the singular release once
  // pImpl lands.
  std::vector<std::string> names;
  names.reserve(theObjectInstanceNames.size());
  for (auto const& w : theObjectInstanceNames) {
    names.push_back(ws2s_(w));
  }
  (void)names;
  m17NotWired_("releaseMultipleObjectInstanceName");
}

ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstance(
    ObjectClassHandle theClass) {
  // §6.8 overload 1 — RTI-generated name. Distinct from the 2-arg
  // overload per catalogue row 11.2 (2 spec overloads, not one with
  // default-arg — the vtable slot must be distinct).
  (void)theClass;
  m17NotWired_("registerObjectInstance");
}

ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstance(
    ObjectClassHandle theClass,
    std::wstring const& theObjectInstanceName) {
  // §6.8 overload 2 — federate-supplied name. Name must have been
  // reserved via §6.5 first; otherwise ObjectInstanceNameNotReserved.
  std::string const name = ws2s_(theObjectInstanceName);
  (void)theClass;
  (void)name;
  m17NotWired_("registerObjectInstance");
}

void DLCRTIambassadorImpl::updateAttributeValues(
    ObjectInstanceHandle theObject,
    AttributeHandleValueMap const& theAttributeValues,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.10 overload 1 (RO). Tag is MANDATORY per catalogue §17.1.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)theAttributeValues;
  (void)m17_tag_;
  m17NotWired_("updateAttributeValues");
}

MessageRetractionHandle DLCRTIambassadorImpl::updateAttributeValues(
    ObjectInstanceHandle theObject,
    AttributeHandleValueMap const& theAttributeValues,
    VariableLengthData const& theUserSuppliedTag,
    LogicalTime const& theTime) {
  // §6.10 overload 2 (TSO). Returns MessageRetractionHandle for
  // §8.21 retract(). Tag mandatory; theTime binds to a §8 logical
  // time from the federation's HLAlogicalTimeFactory.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)theAttributeValues;
  (void)m17_tag_;
  (void)theTime;
  m17NotWired_("updateAttributeValues");
}

void DLCRTIambassadorImpl::sendInteraction(
    InteractionClassHandle theInteraction,
    ParameterHandleValueMap const& theParameterValues,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.12 overload 1 (RO). Tag mandatory.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theInteraction;
  (void)theParameterValues;
  (void)m17_tag_;
  m17NotWired_("sendInteraction");
}

MessageRetractionHandle DLCRTIambassadorImpl::sendInteraction(
    InteractionClassHandle theInteraction,
    ParameterHandleValueMap const& theParameterValues,
    VariableLengthData const& theUserSuppliedTag,
    LogicalTime const& theTime) {
  // §6.12 overload 2 (TSO). Returns MessageRetractionHandle. Used by
  // om_message_retraction/federate_publisher.cpp.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theInteraction;
  (void)theParameterValues;
  (void)m17_tag_;
  (void)theTime;
  m17NotWired_("sendInteraction");
}

void DLCRTIambassadorImpl::deleteObjectInstance(
    ObjectInstanceHandle theObject,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.14 overload 1 (RO). Tag mandatory. Federate must own at least
  // the privilegeToDelete attribute of the target instance.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)m17_tag_;
  m17NotWired_("deleteObjectInstance");
}

MessageRetractionHandle DLCRTIambassadorImpl::deleteObjectInstance(
    ObjectInstanceHandle theObject,
    VariableLengthData const& theUserSuppliedTag,
    LogicalTime const& theTime) {
  // §6.14 overload 2 (TSO). Returns MessageRetractionHandle.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)m17_tag_;
  (void)theTime;
  m17NotWired_("deleteObjectInstance");
}

void DLCRTIambassadorImpl::localDeleteObjectInstance(
    ObjectInstanceHandle theObject) {
  // §6.16 — remove the local reflection only; no wire traffic. Used
  // when the federate no longer needs to reflect updates for the
  // instance. Fires no callbacks.
  (void)theObject;
  m17NotWired_("localDeleteObjectInstance");
}

void DLCRTIambassadorImpl::requestAttributeValueUpdate(
    ObjectInstanceHandle theObject,
    AttributeHandleSet const& theAttributes,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.19 overload 1 — by object instance. Fires provideAttribute-
  // ValueUpdate callback on publisher(s) with matching attributes.
  // Tag mandatory.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theObject;
  (void)theAttributes;
  (void)m17_tag_;
  m17NotWired_("requestAttributeValueUpdate");
}

void DLCRTIambassadorImpl::requestAttributeValueUpdate(
    ObjectClassHandle theClass,
    AttributeHandleSet const& theAttributes,
    VariableLengthData const& theUserSuppliedTag) {
  // §6.19 overload 2 — by object class. Fires provideAttribute-
  // ValueUpdate for every instance of the class the federate knows
  // about that publishes any of the requested attributes.
  auto const m17_tag_ = tag2bytes_(theUserSuppliedTag);
  (void)theClass;
  (void)theAttributes;
  (void)m17_tag_;
  m17NotWired_("requestAttributeValueUpdate");
}

// ===== §7 Ownership Management =====
//
// M33-K: signature-parity impls per IEEE 1516.1-2010 §7.2-7.19 and
// docs/DLC_DIVERGENCE_CATALOGUE.md §12 (rows 12.1-12.7). Bodies here
// are deliberately minimal (no persistent ownership state; no
// FederateAmbassador is bound on this impl until §4 connect() is
// implemented in M34+). They satisfy the vtable + spec signature +
// out-param contracts so §7 conformance fixtures LINK cleanly.
//
// Runtime callback delivery (requestDivestitureConfirmation,
// attributeOwnershipAcquisitionNotification, informAttributeOwnership,
// attributeIsNotOwned, attributeIsOwnedByRTI, attributeOwnershipUnavailable)
// is deferred to M34+ once §4 `connect()` stores the bound
// FederateAmbassador reference on the impl.

// §7.2 — unconditional divest. Void return; no side-effects modeled.
void DLCRTIambassadorImpl::unconditionalAttributeOwnershipDivestiture(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op: real ownership bookkeeping deferred to M34+.
}

// §7.3 — negotiated divest (offer to subscribers). Callback delivery of
// requestAttributeOwnershipAssumption on subscribers is deferred to M34+.
void DLCRTIambassadorImpl::negotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {
  // no-op.
}

// §7.6 — confirm divest after §7.5 requestDivestitureConfirmation.
// Catalogue row 12.1: M17 was absent; DLC adds it.
void DLCRTIambassadorImpl::confirmDivestiture(ObjectInstanceHandle,
                                              AttributeHandleSet const&,
                                              VariableLengthData const&) {
  // no-op.
}

// §7.8 — request ownership acquisition with a spec-mandatory tag.
// Catalogue row 12.2: M17 lacked the tag; DLC adds it (BLOCKING).
void DLCRTIambassadorImpl::attributeOwnershipAcquisition(
    ObjectInstanceHandle, AttributeHandleSet const&,
    VariableLengthData const&) {
  // no-op.
}

// §7.9 — acquire-if-available. Per spec this is (object, attributes)
// with NO tag. Catalogue row 12.3: M17 absent; DLC adds it (MAJOR).
void DLCRTIambassadorImpl::attributeOwnershipAcquisitionIfAvailable(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op.
}

// §7.12 — release-denied. Catalogue row 12.4: M17 absent; DLC adds
// it (MAJOR).
void DLCRTIambassadorImpl::attributeOwnershipReleaseDenied(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op.
}

// §7.13 — divest-if-wanted. Spec has an out-param filled with the
// attributes the RTI actually divested. Catalogue row 12.5: M17 had no
// out-param; DLC adds it (BLOCKING). We clear the out-set — no owner
// state means nothing was divested this call.
void DLCRTIambassadorImpl::attributeOwnershipDivestitureIfWanted(
    ObjectInstanceHandle, AttributeHandleSet const&,
    AttributeHandleSet& theDivestedAttributes) {
  theDivestedAttributes.clear();
}

// §7.14 — cancel a pending negotiated divest.
void DLCRTIambassadorImpl::cancelNegotiatedAttributeOwnershipDivestiture(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op.
}

// §7.15 — cancel a pending acquisition.
void DLCRTIambassadorImpl::cancelAttributeOwnershipAcquisition(
    ObjectInstanceHandle, AttributeHandleSet const&) {
  // no-op.
}

// §7.17 — query ownership. Void return per spec; the answer arrives on
// a §7.18 callback (informAttributeOwnership / attributeIsNotOwned /
// attributeIsOwnedByRTI). Catalogue row 12.6 (BLOCKING): M17 returned a
// synchronous OwnershipQueryResult; DLC drops the return + defers the
// answer to the FederateAmbassador. M34+ will dispatch the callback on
// the FederateAmbassador stored by connect().
void DLCRTIambassadorImpl::queryAttributeOwnership(ObjectInstanceHandle,
                                                   AttributeHandle) {
  // no-op: callback delivery deferred to M34+.
}

// §7.19 — is this attribute owned by THIS federate? Direct bool per
// spec. Catalogue row 12.7 (COSMETIC): DLC matches M17 shape. Without
// stored ownership state we return false conservatively — the fixture
// won't observe true until M34+ wires real state.
bool DLCRTIambassadorImpl::isAttributeOwnedByFederate(ObjectInstanceHandle,
                                                      AttributeHandle) {
  return false;
}

// ===== §8 Time Management =====
void DLCRTIambassadorImpl::enableTimeRegulation(LogicalTimeInterval const&) {
  m32_stub("enableTimeRegulation");
}
void DLCRTIambassadorImpl::disableTimeRegulation() {
  m32_stub("disableTimeRegulation");
}
void DLCRTIambassadorImpl::enableTimeConstrained() {
  m32_stub("enableTimeConstrained");
}
void DLCRTIambassadorImpl::disableTimeConstrained() {
  m32_stub("disableTimeConstrained");
}
void DLCRTIambassadorImpl::timeAdvanceRequest(LogicalTime const&) {
  m32_stub("timeAdvanceRequest");
}
void DLCRTIambassadorImpl::timeAdvanceRequestAvailable(LogicalTime const&) {
  m32_stub("timeAdvanceRequestAvailable");
}
void DLCRTIambassadorImpl::nextMessageRequest(LogicalTime const&) {
  m32_stub("nextMessageRequest");
}
void DLCRTIambassadorImpl::nextMessageRequestAvailable(LogicalTime const&) {
  m32_stub("nextMessageRequestAvailable");
}
void DLCRTIambassadorImpl::flushQueueRequest(LogicalTime const&) {
  m32_stub("flushQueueRequest");
}
void DLCRTIambassadorImpl::enableAsynchronousDelivery() {
  m32_stub("enableAsynchronousDelivery");
}
void DLCRTIambassadorImpl::disableAsynchronousDelivery() {
  m32_stub("disableAsynchronousDelivery");
}
bool DLCRTIambassadorImpl::queryGALT(LogicalTime&) { m32_stub("queryGALT"); }
void DLCRTIambassadorImpl::queryLogicalTime(LogicalTime&) {
  m32_stub("queryLogicalTime");
}
bool DLCRTIambassadorImpl::queryLITS(LogicalTime&) { m32_stub("queryLITS"); }
void DLCRTIambassadorImpl::modifyLookahead(LogicalTimeInterval const&) {
  m32_stub("modifyLookahead");
}
void DLCRTIambassadorImpl::queryLookahead(LogicalTimeInterval&) {
  m32_stub("queryLookahead");
}
void DLCRTIambassadorImpl::retract(MessageRetractionHandle) {
  m32_stub("retract");
}
void DLCRTIambassadorImpl::changeAttributeOrderType(ObjectInstanceHandle,
                                                    AttributeHandleSet const&,
                                                    OrderType) {
  m32_stub("changeAttributeOrderType");
}
void DLCRTIambassadorImpl::changeInteractionOrderType(InteractionClassHandle,
                                                      OrderType) {
  m32_stub("changeInteractionOrderType");
}

// ===== §9 DDM =====
RegionHandle DLCRTIambassadorImpl::createRegion(DimensionHandleSet const&) {
  m32_stub("createRegion");
}
void DLCRTIambassadorImpl::commitRegionModifications(RegionHandleSet const&) {
  m32_stub("commitRegionModifications");
}
void DLCRTIambassadorImpl::deleteRegion(RegionHandle const&) {
  m32_stub("deleteRegion");
}

ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstanceWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&) {
  m32_stub("registerObjectInstanceWithRegions");
}
ObjectInstanceHandle DLCRTIambassadorImpl::registerObjectInstanceWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&,
    std::wstring const&) {
  m32_stub("registerObjectInstanceWithRegions");
}

void DLCRTIambassadorImpl::associateRegionsForUpdates(
    ObjectInstanceHandle, AttributeHandleSetRegionHandleSetPairVector const&) {
  m32_stub("associateRegionsForUpdates");
}
void DLCRTIambassadorImpl::unassociateRegionsForUpdates(
    ObjectInstanceHandle, AttributeHandleSetRegionHandleSetPairVector const&) {
  m32_stub("unassociateRegionsForUpdates");
}

void DLCRTIambassadorImpl::subscribeObjectClassAttributesWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&,
    bool, std::wstring const&) {
  m32_stub("subscribeObjectClassAttributesWithRegions");
}
void DLCRTIambassadorImpl::unsubscribeObjectClassAttributesWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&) {
  m32_stub("unsubscribeObjectClassAttributesWithRegions");
}

void DLCRTIambassadorImpl::subscribeInteractionClassWithRegions(
    InteractionClassHandle, RegionHandleSet const&, bool) {
  m32_stub("subscribeInteractionClassWithRegions");
}
void DLCRTIambassadorImpl::unsubscribeInteractionClassWithRegions(
    InteractionClassHandle, RegionHandleSet const&) {
  m32_stub("unsubscribeInteractionClassWithRegions");
}

void DLCRTIambassadorImpl::sendInteractionWithRegions(
    InteractionClassHandle, ParameterHandleValueMap const&,
    RegionHandleSet const&, VariableLengthData const&) {
  m32_stub("sendInteractionWithRegions");
}
MessageRetractionHandle DLCRTIambassadorImpl::sendInteractionWithRegions(
    InteractionClassHandle, ParameterHandleValueMap const&,
    RegionHandleSet const&, VariableLengthData const&, LogicalTime const&) {
  m32_stub("sendInteractionWithRegions");
}
void DLCRTIambassadorImpl::requestAttributeValueUpdateWithRegions(
    ObjectClassHandle, AttributeHandleSetRegionHandleSetPairVector const&,
    VariableLengthData const&) {
  m32_stub("requestAttributeValueUpdateWithRegions");
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
FederateHandle DLCRTIambassadorImpl::getFederateHandle(std::wstring const&) {
  // §10.4 — need federation state to resolve name → handle.
  throw NotConnected(L"DLC RTIambassador: getFederateHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getFederateName(FederateHandle theHandle) {
  // §10.5 — need federation state.
  if (!theHandle.isValid()) throw InvalidFederateHandle(L"getFederateName");
  throw NotConnected(L"DLC RTIambassador: getFederateName requires "
                     L"federation connection (M35+).");
}
ObjectClassHandle DLCRTIambassadorImpl::getObjectClassHandle(
    std::wstring const&) {
  // §10.6 — need FOM state to resolve class name.
  throw NotConnected(L"DLC RTIambassador: getObjectClassHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getObjectClassName(
    ObjectClassHandle theHandle) {
  // §10.7 — need FOM state.
  if (!theHandle.isValid())
    throw InvalidObjectClassHandle(L"getObjectClassName");
  throw NotConnected(L"DLC RTIambassador: getObjectClassName requires "
                     L"federation connection (M35+).");
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
AttributeHandle DLCRTIambassadorImpl::getAttributeHandle(ObjectClassHandle,
                                                         std::wstring const&) {
  // §10.11 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getAttributeHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getAttributeName(ObjectClassHandle,
                                                    AttributeHandle) {
  // §10.12 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getAttributeName requires "
                     L"federation connection (M35+).");
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
    std::wstring const&) {
  // §10.15 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getInteractionClassHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getInteractionClassName(
    InteractionClassHandle) {
  // §10.16 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getInteractionClassName requires "
                     L"federation connection (M35+).");
}
ParameterHandle DLCRTIambassadorImpl::getParameterHandle(
    InteractionClassHandle, std::wstring const&) {
  // §10.17 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getParameterHandle requires "
                     L"federation connection (M35+).");
}
std::wstring DLCRTIambassadorImpl::getParameterName(InteractionClassHandle,
                                                    ParameterHandle) {
  // §10.18 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getParameterName requires "
                     L"federation connection (M35+).");
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
DimensionHandle DLCRTIambassadorImpl::getDimensionHandle(std::wstring const&) {
  // §10.25 — need FOM state.
  throw NotConnected(L"DLC RTIambassador: getDimensionHandle requires "
                     L"federation connection (M35+).");
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
  // §10.29 — need region-registry state.
  if (!theRegion.isValid()) throw InvalidRegion(L"getRangeBounds");
  if (!theDim.isValid()) throw InvalidDimensionHandle(L"getRangeBounds");
  throw NotConnected(L"DLC RTIambassador: getRangeBounds requires "
                     L"federation connection (M35+).");
}
void DLCRTIambassadorImpl::setRangeBounds(RegionHandle theRegion,
                                          DimensionHandle theDim,
                                          RangeBounds const& bounds) {
  // §10.30 — need region-registry state. Validate arguments per spec
  // before failing on connectivity.
  if (!theRegion.isValid()) throw InvalidRegion(L"setRangeBounds");
  if (!theDim.isValid()) throw InvalidDimensionHandle(L"setRangeBounds");
  if (bounds.getLowerBound() > bounds.getUpperBound())
    throw InvalidRangeBound(L"setRangeBounds: lower > upper.");
  throw NotConnected(L"DLC RTIambassador: setRangeBounds requires "
                     L"federation connection (M35+).");
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
void DLCRTIambassadorImpl::enableObjectClassRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableObjectClassRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::enableAttributeRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableAttributeRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::enableAttributeScopeAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableAttributeScopeAdvisorySwitch() {}
void DLCRTIambassadorImpl::enableInteractionRelevanceAdvisorySwitch() {}
void DLCRTIambassadorImpl::disableInteractionRelevanceAdvisorySwitch() {}

// §10.41 evokeCallback — single-arg spec form (catalogue 13.11).
// No event queue in DLC M33 yet; returns false to signal "no more callbacks
// pending". Federate can loop on this call safely.
bool DLCRTIambassadorImpl::evokeCallback(double) { return false; }

// §10.42 evokeMultipleCallbacks — 2 args, no defaults (catalogue 13.12).
// Same rationale as evokeCallback: no queued events, return false.
bool DLCRTIambassadorImpl::evokeMultipleCallbacks(double, double) {
  return false;
}

// §10.43-10.44 enable/disableCallbacks — no dispatch loop to gate yet;
// silently succeed.
void DLCRTIambassadorImpl::enableCallbacks() {}
void DLCRTIambassadorImpl::disableCallbacks() {}

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

}  // namespace rti1516e
