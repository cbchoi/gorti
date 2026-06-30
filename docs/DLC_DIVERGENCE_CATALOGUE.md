# DLC Divergence Catalogue — gorti cppsdk vs IEEE 1516.1-2010 (via Pitch reference headers)

**Companion to:** `docs/DLC_COMPLIANCE_PROGRAM.md` (parent track) and `docs/M31_DISPATCH_PLAN.md` (lockfile milestone).

Survey conducted 2026-06-30 against **Pitch pRTI Free 5.5.10 build 9905** (`~/prti1516e/api/cpp/HLA_1516-2010/RTI/` — 30 header files: 15 top-level + 9 in `encoding/` + 6 in `time/`). Target: `/home/cbchoi/project/gorti/cppsdk/include/rti1516e/`. All file:line references in this doc are anchored to Pitch 5.5.10; future Pitch releases may shift line numbers but the spec-mandated content should not move.

Each section catalogues every API-surface difference between gorti and the IEEE 1516.1-2010 DLC C++ API surface. Severity legend:
- **BLOCKING** — federate source written for one cannot compile against the other.
- **MAJOR** — compiles but observable behavior changes.
- **MINOR** — edge-case semantics differ.
- **COSMETIC** — naming / style only.

Each row drives at least one lockfile test in `cppsdk/tests/dlc/lockfile/` (see M31 dispatch plan).

---

## 1. Header Layout & Namespace Surface

| # | Spec § | Pitch (file:line + form) | gorti (file:line + form) | Severity | Strict fix |
|---|---|---|---|---|---|
| 1.1 | Annex A | Headers in `RTI/` dir; `#include <RTI/RTIambassador.h>` (1516-2010/RTI/RTIambassador.h:11) | Headers in `rti1516e/`; `#include <rti1516e/RtiAmbassador.h>` (cppsdk/include/rti1516e/RtiAmbassador.h:1) | BLOCKING | Mirror `RTI/` layout with spec filenames (capital RTI). |
| 1.2 | Annex A | `RTI/RTI1516.h` one-stop include (RTI1516.h:19-64) | No equivalent — 5 manual includes | MAJOR | Add `RTI/RTI1516.h` + `rtiName()`/`rtiVersion()` free functions. |
| 1.3 | Annex A | Namespace `rti1516e` | Namespace `rti1516e` | COSMETIC | OK. |
| 1.4 | Annex A | `RTI/encoding/*.h` (9 files), `RTI/time/*.h` (6 files) | One flat `Encoding.h`, no time subdir | BLOCKING | Create `RTI/encoding/` (9 files) and `RTI/time/` (6 files). |
| 1.5 | Annex A | `RTI/SpecificConfig.h` defines `RTI_EXPORT`, `RTI_NOEXCEPT`, `RTI_THROW` (SpecificConfig.h:39-76) | No analog | BLOCKING | Add the three macros so `RTI_THROW(...)` parses. |
| 1.6 | Annex A | DLC variant has no encoding/time subdirs (HLA_1516-DLC/RTI/) | Asymmetric the wrong way | MAJOR | Ship HLA_1516-2010 layout (with subdirs). |

---

## 2. Construction & Factory

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 2.1 | §10 / Annex A | `class RTIambassador` ctor **protected**, dtor virtual (RTIambassador.h:37-42); federates cannot construct directly | `RTIambassador()` public concrete (RtiAmbassador.h:52) | BLOCKING | Pure-abstract `RTIambassador`; concrete pimpl behind the factory. |
| 2.2 | §10 / RTIambassadorFactory.h | `RTIambassadorFactory::createRTIambassador()` returns `std::auto_ptr<RTIambassador>` (RTIambassadorFactory.h:39-41) | No factory exists | BLOCKING | Add `RTI/RTIambassadorFactory.h`. **C++17 resolution (gorti target):** spec text says `std::auto_ptr` (1516.1 was written for C++03; `auto_ptr` was *removed* in C++17). Resolution: `RTI/SpecificConfig.h` provides `template <typename T> using auto_ptr = std::unique_ptr<T>;` in namespace `rti1516e` under C++17+, with an opt-in `-DGORTI_DLC_USE_REAL_AUTO_PTR` build flag for C++14 source ports that need literal `std::auto_ptr`. The factory signature returns `rti1516e::auto_ptr<RTIambassador>`. |
| 2.3 | §10 | Every method `virtual ... = 0` (RTIambassador.h:45-1846) | All non-virtual concrete with `pimpl` (RtiAmbassador.h:645) | BLOCKING | Pure-virtual everywhere; pimpl in .cpp. |
| 2.4 | §10 | dtor virtual (RTIambassador.h:42) | dtor non-virtual (RtiAmbassador.h:53) | BLOCKING | Make virtual. |
| 2.5 | §10 | Pitch does not delete copy/move (abstract base) | gorti deletes copy + defaults move (RtiAmbassador.h:57-60) | COSMETIC | Once abstract, moot. |

---

## 3. Connect & Federation Lifecycle

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 3.1 | §4.2 connect | `connect(FederateAmbassador&, CallbackModel, std::wstring const& localSettings=L"")` (RTIambassador.h:45-55) | `connect(const std::string& url)` (RtiAmbassador.h:76) | BLOCKING | (a) Take `FederateAmbassador&`. (b) Add `CallbackModel`. (c) `localSettings` is address vector not URL. (d) `std::wstring`. (e) Remove `setFederateAmbassador()`. |
| 3.2 | Enums | `enum CallbackModel { HLA_IMMEDIATE, HLA_EVOKED };` (Enums.h:21-25) | Absent | BLOCKING | Add to `RTI/Enums.h`. |
| 3.3 | §4.3 disconnect | spec signature (RTIambassador.h:58-62) | `disconnect()` + non-spec `isConnected()` (RtiAmbassador.h:81-85) | MAJOR | Drop `isConnected()`; match exception set. |
| 3.4 | §4.5 createFederationExecution | 3 overloads, wstring + vector<wstring> + MIM (RTIambassador.h:65-106) | 1 overload, string + FomModuleList (RtiAmbassador.h:98) | BLOCKING | Add 3 overloads + `createFederationExecutionWithMIM`. |
| 3.5 | §4.5 | `logicalTimeImplementationName=L""` last param of every create (RTIambassador.h:68, 81, 95) | absent | BLOCKING | Add param. |
| 3.6 | §4.6 destroyFederationExecution | wstring (RTIambassador.h:109-115) | string (RtiAmbassador.h:109) | BLOCKING | wstring + `FederatesCurrentlyJoined`. |
| 3.7 | §4.7 listFederationExecutions | `listFederationExecutions()` (RTIambassador.h:118-121) — result via callback | absent | MAJOR | Add (result via `reportFederationExecutions`). |
| 3.8 | §4.9 joinFederationExecution | 2 overloads (RTIambassador.h:124-158) | 1 overload (RtiAmbassador.h:122) | BLOCKING | 2 overloads + `additionalFomModules`; wstring. |
| 3.9 | §4.10 resignFederationExecution | `resignFederationExecution(ResignAction)` mandatory (RTIambassador.h:161-170) | `resignFederationExecution()` no param (RtiAmbassador.h:136) | BLOCKING | Require `ResignAction`. |
| 3.10 | §4.7 registerFederationSync | 2 overloads, wstring + `FederateHandleSet` (RTIambassador.h:173-193) | 1 overload, string + vector (RtiAmbassador.h:535) | BLOCKING | 2 overloads + set + wstring. |
| 3.11 | §4.14 synchronizationPointAchieved | `(wstring, bool successfully=true)` (RTIambassador.h:196-205) | `(string)` (RtiAmbassador.h:542) | BLOCKING | Add `successfully`; wstring. |
| 3.12 | §4.16-22 save | 5 methods, callback-driven (RTIambassador.h:208-270) | 4 methods, polling-based `querySaveState` (RtiAmbassador.h:434-448) | BLOCKING | Add `federateSaveBegun`; switch to callback flow. |
| 3.13 | §4.24-31 restore | 5 methods (RTIambassador.h:273-313) | 4 methods (RtiAmbassador.h:451-463) | BLOCKING | Add `federateRestoreNotComplete`; switch to callback flow. |

---

## 4. FederateAmbassador Callback Surface

This is where the 2026-06-30 parity test hit its biggest cost: 14 callback overloads to bridge.

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 4.1 | §10 | `FederateAmbassador` pure abstract, ctor protected (FederateAmbassador.h:32-41) | Concrete, all callbacks no-op defaults (FederateAmbassador.h:27-29) | MAJOR | Ship `FederateAmbassador` (pure) + `NullFederateAmbassador` (concrete no-op subclass). |
| 4.2 | §4 | `NullFederateAmbassador` (NullFederateAmbassador.h:25-489) | absent | BLOCKING | Add `RTI/NullFederateAmbassador.h`. |
| 4.3 | §4.4 connectionLost | `(wstring faultDescription)` (FederateAmbassador.h:44-47) | absent | MAJOR | Add. |
| 4.4 | §4.8 reportFederationExecutions | `(FederationExecutionInformationVector)` (FederateAmbassador.h:50-54) | absent | MAJOR | Add. |
| 4.5 | §4.12 sync registration | `synchronizationPointRegistrationSucceeded/Failed(wstring, SynchronizationPointFailureReason)` (FederateAmbassador.h:57-66) | absent | MAJOR | Add both + enum. |
| 4.6 | §4.13 announceSynchronizationPoint | `(wstring, VariableLengthData)` (FederateAmbassador.h:69-73) | `(string, VariableLengthData)` (FederateAmbassador.h:86-88) | BLOCKING | wstring. |
| 4.7 | §4.15 federationSynchronized | `(wstring, FederateHandleSet failedToSyncSet)` (FederateAmbassador.h:76-80) | `(string)` (FederateAmbassador.h:92) | BLOCKING | Add `failedToSyncSet`; wstring. |
| 4.8 | §4.17 initiateFederateSave | 2 overloads (FederateAmbassador.h:83-92) | 1 overload with `std::optional<double>` (FederateAmbassador.h:124-126) | BLOCKING | 2 overloads + `LogicalTime const&`. |
| 4.9 | §4.20 federationSaved / NotSaved | `federationSaved()`, `federationNotSaved(SaveFailureReason)` — **no label arg** (FederateAmbassador.h:95-102) | both take string label (FederateAmbassador.h:130-134) | BLOCKING | Drop label; add `SaveFailureReason`. |
| 4.10 | §4.23 federationSaveStatusResponse | `(FederateHandleSaveStatusPairVector)` (FederateAmbassador.h:106-110) | absent | MAJOR | Add + supporting types. |
| 4.11 | §4.25 restore request ack | `requestFederationRestoreSucceeded/Failed(wstring)` (FederateAmbassador.h:113-121) | absent | MAJOR | Add both. |
| 4.12 | §4.26 federationRestoreBegun | (FederateAmbassador.h:124-126) | absent | MAJOR | Add. |
| 4.13 | §4.27 initiateFederateRestore | `(wstring label, wstring federateName, FederateHandle)` (FederateAmbassador.h:129-134) | `(string label, FederateHandle)` (FederateAmbassador.h:142-144) | BLOCKING | Add federateName; wstring. |
| 4.14 | §4.29 federationRestored / NotRestored | `federationRestored()`, `federationNotRestored(RestoreFailureReason)` (FederateAmbassador.h:137-144) | both take label (FederateAmbassador.h:148-153) | BLOCKING | Drop label; enum. |
| 4.15 | §4.32 federationRestoreStatusResponse | `(FederateRestoreStatusVector)` (FederateAmbassador.h:147-151) | absent | MAJOR | Add + types. |
| 4.16 | §5.10-13 declaration callbacks | 4 callbacks: startReg/stopReg/turnInteractionsOn/Off (FederateAmbassador.h:158-179) | absent | MAJOR | Add 4. |
| 4.17 | §6.3 nameReservationSucceeded/Failed | `(wstring)` (FederateAmbassador.h:186-194) | `(string)` (FederateAmbassador.h:56-61) | BLOCKING | wstring. |
| 4.18 | §6.6 multipleNameReservation | `(std::set<wstring>)` for both succeeded/failed (FederateAmbassador.h:197-205) | `(vector<string>)` + 2-arg failed (FederateAmbassador.h:65-73) | BLOCKING | set<wstring>; 1-arg failed. |
| 4.19 | §6.9 discoverObjectInstance | **2 overloads** — 3-arg + 4-arg with `FederateHandle producingFederate` (FederateAmbassador.h:209-222) | 1 overload, string (FederateAmbassador.h:34-37) | BLOCKING | Add 4-arg; wstring. |
| 4.20 | §6.11 reflectAttributeValues | **3 overloads** — RO / TSO / TSO+retract with full param sets (FederateAmbassador.h:225-258) | 1 overload, `std::optional<double>` (FederateAmbassador.h:42-45) | BLOCKING | Ship all 3 overloads. **Central parity-test blocker.** |
| 4.21 | §6.13 receiveInteraction | **3 overloads** (FederateAmbassador.h:261-294) | 1 overload (FederateAmbassador.h:48-51) | BLOCKING | Ship 3 overloads. |
| 4.22 | §6.15 removeObjectInstance | 3 overloads (FederateAmbassador.h:297-324) | absent | MAJOR | Add 3 overloads. |
| 4.23 | §6.17-18 attribute scope | 2 callbacks (FederateAmbassador.h:327-338) | absent | MAJOR | Add. |
| 4.24 | §6.20 provideAttributeValueUpdate | (FederateAmbassador.h:341-346) | absent | MAJOR | Add. |
| 4.25 | §6.21-22 updates on/off | 2 overloads + opposite (FederateAmbassador.h:349-367) | absent | MAJOR | Add. |
| 4.26 | §6.24-30 transportation | 4 callbacks (FederateAmbassador.h:370-398) | absent | MAJOR | Add 4. |
| 4.27 | §7.4 requestAttributeOwnershipAssumption | 3-arg (no divestingFederate) (FederateAmbassador.h:406-411) | 4-arg with divestingFederate (FederateAmbassador.h:98-102) | BLOCKING | Drop divestingFederate. |
| 4.28 | §7.7 attributeOwnershipAcquisitionNotification | (object, attrs, tag) (FederateAmbassador.h:421-426) | (object, attrs, owningFederate) (FederateAmbassador.h:107-110) | BLOCKING | Add tag; drop owningFederate. |
| 4.29 | §7.10 attributeOwnershipUnavailable | (FederateAmbassador.h:429-433) | absent | MAJOR | Add. |
| 4.30 | §7.11 requestAttributeOwnershipRelease | (FederateAmbassador.h:436-441) | absent | MAJOR | Add. |
| 4.31 | §7.16 confirmAttributeOwnershipAcquisitionCancellation | (FederateAmbassador.h:444-448) | absent | MAJOR | Add. |
| 4.32 | §7.18 informAttributeOwnership / etc. | 3 callbacks (FederateAmbassador.h:451-468) | absent (gorti has sync query) | MAJOR | Add 3. |
| 4.33 | §8.3 timeRegulationEnabled | `(LogicalTime)` (FederateAmbassador.h:475-478) | absent (sync) | BLOCKING | Add — enable call is async. |
| 4.34 | §8.6 timeConstrainedEnabled | (FederateAmbassador.h:481-484) | absent | BLOCKING | Add. |
| 4.35 | §8.13 timeAdvanceGrant | `(LogicalTime)` (FederateAmbassador.h:487-490) | `(double)` (FederateAmbassador.h:79) | BLOCKING | LogicalTime ref. |
| 4.36 | §8.22 requestRetraction | `(MessageRetractionHandle)` (FederateAmbassador.h:493-496) | absent | MAJOR | Add. |
| 4.37 | §10 RTI_THROW | Every callback declared `RTI_THROW(FederateInternalError) = 0` | gorti: noexcept defaults | MAJOR | Adopt RTI_THROW. |

---

## 5. Enums

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 5.1 | Enums.h | `CallbackModel { HLA_IMMEDIATE, HLA_EVOKED }` (Enums.h:21-25) | absent | BLOCKING | Add. |
| 5.2 | Enums.h | `OrderType { RECEIVE=1, TIMESTAMP=2 }` (Enums.h:27-31) | absent | BLOCKING | Add. |
| 5.3 | §4.10 | `ResignAction { UNCONDITIONALLY_DIVEST_ATTRIBUTES, DELETE_OBJECTS, CANCEL_PENDING_OWNERSHIP_ACQUISITIONS, DELETE_OBJECTS_THEN_DIVEST, CANCEL_THEN_DELETE_THEN_DIVEST, NO_ACTION }` (Enums.h:33-41) | absent | BLOCKING | Add. |
| 5.4 | §4.20 | `SaveFailureReason` 6-value enum (Enums.h:62-70) | gorti has inline `SaveState` instead | BLOCKING | Add. |
| 5.5 | §4.29 | `RestoreFailureReason` 5-value enum (Enums.h:43-50) | absent | BLOCKING | Add. |
| 5.6 | §4.23 | `SaveStatus` 4-value (Enums.h:72-78) | absent | MAJOR | Add. |
| 5.7 | §4.32 | `RestoreStatus` 6-value (Enums.h:52-60) | absent | MAJOR | Add. |
| 5.8 | §10.32 | `ServiceGroup` 7-value (Enums.h:80-89) | absent | MAJOR | Add. |
| 5.9 | §4.12 | `SynchronizationPointFailureReason` 2-value (Enums.h:91-95) | absent | MAJOR | Add. |
| 5.10 | §6 | `TransportationType { RELIABLE=1, BEST_EFFORT=2 }` (Enums.h:97-101) | absent | BLOCKING | Add. |

---

## 6. Exceptions

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 6.1 | Annex C | `class Exception` pure abstract; `what()` returns `wstring` (Exception.h:26-46) | `class RTIinternalError : public std::runtime_error` (Exceptions.h:17-20) | BLOCKING | Replace base with pure abstract `Exception`, wstring what(). |
| 6.2 | Annex C | ~120 exception classes via `RTI_EXCEPTION(name)` macro (Exception.h:54-191) | 14 classes derived from `runtime_error` | BLOCKING | Replicate all 120. |
| 6.3 | Annex C must-haves | 120 named exceptions — see full list in agent survey output, Section 6 row 6.3 | gorti has 14 (Exceptions.h:17-74) | BLOCKING | Add the missing 106 one-line `RTI_EXCEPTION(Name)` macros. |
| 6.4 | Annex C | `RTIinternalError` is a leaf (Exception.h:171) | `RTIinternalError` is base of everything (Exceptions.h:17) | BLOCKING | Restructure: `Exception` base; `RTIinternalError` sibling of others. |
| 6.5 | §C.2 | `class EncoderException : public Exception` (EncodingExceptions.h:23-34) | `class EncodingError : public std::runtime_error` (Encoding.h:31-33) | BLOCKING | Rename; rebase on Exception; wstring. |

---

## 7. Typed Handles

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 7.1 | §10.5 | `DEFINE_HANDLE_CLASS(Name)` macro — real class with `_impl` pointer, hash(), encode(), encodedLength(), toString(), copy ctor, dtor, op<< (Handle.h:33-120) | `using ObjectClassHandle = detail::StrongHandle<...>` strong typedef of uint64 (Types.h:73) | BLOCKING | Replace with `DEFINE_HANDLE_CLASS` pattern. |
| 7.2 | §10.5 | 9 handle classes (Handle.h:128-136) | 9 same names + extra `RoutingSpaceHandle` + `MessageRetractionHandle` is uint64 alias (Types.h:81, 112) | MINOR / BLOCKING | Drop RoutingSpaceHandle (not 1516); class-ify MessageRetractionHandle. |
| 7.3 | §10.5 | `long hash() const` (Handle.h:77) | absent | MAJOR | Add. |
| 7.4 | §10.5 | `VariableLengthData encode()`, `encodedLength()`, `size_t encode(void*, size_t)` (Handle.h:81-92); RTIambassador also has `decode*Handle()` (RTIambassador.h:1776-1846) | only `raw()` returning uint64 | MAJOR | Add encode/decode chain. |
| 7.5 | §10.5 | `std::wstring toString()` (Handle.h:94) | absent | MAJOR | Add. |
| 7.6 | §10.5 | `operator<<(wostream&, ...)` (Handle.h:117-120) | absent | MINOR | Add. |
| 7.7 | §10.5 | RegionHandle passed by const ref | also by value (RtiAmbassador.h:359) | COSMETIC | Match. |

---

## 8. VariableLengthData

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 8.1 | Annex A | `class VariableLengthData { ctor, ctor(void*, size_t), copy ctor, data(), size(), setData(void*, size_t), setDataPointer(void*, size_t), takeDataPointer(void*, size_t, deleter=0) };` (VariableLengthData.h:34-93) | `using VariableLengthData = std::vector<uint8_t>` (Types.h:105) | BLOCKING | Replace alias with the Pitch class. **One of the parity-test's blocking divergences.** |
| 8.2 | Annex A | copy vs borrow vs take ownership (VariableLengthData.h:64-84) | vector always copies | MAJOR | Replicate three modes. |
| 8.3 | Annex A | `typedef void (*VariableLengthDataDeleteFunction)(void*)` (VariableLengthData.h:32) | absent | MAJOR | Add. |

---

## 9. Time

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 9.1 | §8 | `class LogicalTime` abstract base (LogicalTime.h:35-131) | absent (federates pass `double`) | BLOCKING | Add. |
| 9.2 | §8 | `class LogicalTimeInterval` abstract base (LogicalTimeInterval.h:35-139) | absent | BLOCKING | Add. |
| 9.3 | §8 | `class LogicalTimeFactory` abstract + `LogicalTimeFactoryFactory` static maker (LogicalTimeFactory.h:46-132) | absent | BLOCKING | Add. |
| 9.4 | §8 | `HLAfloat64Time/Interval/Factory` (time/HLAfloat64Time.h, etc.) | absent | BLOCKING | Add all three. Factory returns `rti1516e::auto_ptr<LogicalTime>` per #2.2 C++17 resolution. |
| 9.5 | §8 | `HLAinteger64Time/Interval/Factory` | absent | MAJOR | Add (needed if federation chooses int64 time). |
| 9.6 | §8.8-12 | `timeAdvanceRequest(LogicalTime const&)` + 4 sibling methods (RTIambassador.h:925-997) | `timeAdvanceRequest(double)` (RtiAmbassador.h:279-290) | BLOCKING | Switch param. |
| 9.7 | §8.16 queryGALT | `bool queryGALT(LogicalTime& outTime)` (RTIambassador.h:1020-1026) | `GALTResult queryGALT()` struct (RtiAmbassador.h:308-309) | BLOCKING | bool + out-param. |
| 9.8 | §8.17 queryLogicalTime | `void queryLogicalTime(LogicalTime&)` (RTIambassador.h:1029-1035) | `double queryLogicalTime()` (RtiAmbassador.h:293) | BLOCKING | out-param. |
| 9.9 | §8.18 queryLITS | `bool queryLITS(LogicalTime&)` (RTIambassador.h:1038-1044) | `LITSResult queryLITS()` (RtiAmbassador.h:316) | BLOCKING | bool + out-param. |
| 9.10 | §8.20 queryLookahead | `void queryLookahead(LogicalTimeInterval&)` (RTIambassador.h:1060-1067) | `double queryLookahead()` (RtiAmbassador.h:296) | BLOCKING | out-param. |
| 9.11 | §8.2 enableTimeRegulation | `(LogicalTimeInterval const&)`, async, ack via callback (RTIambassador.h:879-890) | `(double)`, sync (RtiAmbassador.h:266) | BLOCKING | LogicalTimeInterval + async. |
| 9.12 | §8.19 modifyLookahead | `(LogicalTimeInterval const&)` (RTIambassador.h:1047-1057) | `(double)` (RtiAmbassador.h:275) | BLOCKING | LogicalTimeInterval. |
| 9.13 | §8.21 retract | `retract(MessageRetractionHandle)` (RTIambassador.h:1070-1080) | uint64 alias (RtiAmbassador.h:249) | BLOCKING | Aligns when #7.2 fixed. |
| 9.14 | §8.23-24 changeOrderType | `changeAttributeOrderType`, `changeInteractionOrderType` (RTIambassador.h:1083-1108) | absent | MAJOR | Add. |

---

## 10. DDM (§9)

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 10.1 | §9.2 createRegion | `(DimensionHandleSet)` (RTIambassador.h:1115-1123) | `(RoutingSpaceHandle, vector<DimensionHandle>)` (RtiAmbassador.h:348) | BLOCKING | Drop routing space; pass set. |
| 10.2 | §10.29-30 | `class RangeBounds` with getters/setters (RangeBounds.h:21-52) + `getRangeBounds()` | `struct DimensionRange { uint64 lower, upper; }` (Types.h:93-96); no get | BLOCKING | Rename + class-ify + add get. |
| 10.3 | §9.5 registerObjectInstanceWithRegions | 2 overloads with `AttributeHandleSetRegionHandleSetPairVector` (RTIambassador.h:1151-1188) | 1 overload returning struct, uses `AttributeRegionMap` (RtiAmbassador.h:385-388) | BLOCKING | Use spec types; 2 overloads. |
| 10.4 | §9.6 | `associate/unassociateRegionsForUpdates` with pair-vector (RTIambassador.h:1191-1221) | uses `AttributeRegionMap` (RtiAmbassador.h:396-401) | BLOCKING | Switch to vector-of-pairs. |
| 10.5 | §9.8 subscribeObjectClassAttributesWithRegions | (cls, pair-vector, bool active=true, wstring updateRate=L"") (RTIambassador.h:1224-1241) | (cls, set, set) (RtiAmbassador.h:366-369) | BLOCKING | Add params. |
| 10.6 | §9.12 sendInteractionWithRegions | 2 overloads RO/TSO+retract (RTIambassador.h:1292-1328) | 1 overload with `std::optional<double>` (RtiAmbassador.h:404-408) | BLOCKING | 2 overloads + tag mandatory. |
| 10.7 | §9.13 requestAttributeValueUpdateWithRegions | pair-vector (RTIambassador.h:1331-1345) | (cls, set, set, tag) (RtiAmbassador.h:411-415) | BLOCKING | Switch to pair-vector. |
| 10.8 | §9 RoutingSpaceHandle | absent in 1516 entirely | gorti has it (RtiAmbassador.h:340) | MAJOR | Remove. |
| 10.9 | §9.5 commitRegionModifications | `(RegionHandleSet)` (RTIambassador.h:1126-1135) | `(vector<RegionHandle>)` (RtiAmbassador.h:356) | BLOCKING | Use set. |

---

## 11. Object Management (§6)

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 11.1 | §6.5 reserveMultipleObjectInstanceName | `set<wstring>` (singular Name) (RTIambassador.h:469-478) | plural "Names" + vector (RtiAmbassador.h:188) | BLOCKING | Singular + set + wstring. |
| 11.2 | §6.8 registerObjectInstance | 2 overloads (RTIambassador.h:492-515) | 1 overload + default arg (RtiAmbassador.h:229-231) | MAJOR | 2 distinct overloads. |
| 11.3 | §6.10 updateAttributeValues | 2 overloads + mandatory tag (RTIambassador.h:518-546) | 1 overload no tag (RtiAmbassador.h:235-236) | BLOCKING | 2 overloads + tag. |
| 11.4 | §6.12 sendInteraction | 2 overloads + tag (RTIambassador.h:549-577) | 1 overload no tag (RtiAmbassador.h:239-240) | BLOCKING | 2 overloads + tag. |
| 11.5 | §6.14 deleteObjectInstance | 2 overloads (RTIambassador.h:580-604) | absent | MAJOR | Add. |
| 11.6 | §6.16 localDeleteObjectInstance | (RTIambassador.h:607-617) | absent | MAJOR | Add. |
| 11.7 | §6.19 requestAttributeValueUpdate | 2 overloads (by instance, by class) (RTIambassador.h:620-644) | only DDM variant | MAJOR | Add 2. |
| 11.8 | §6.23-29 transportation | 4 methods (RTIambassador.h:647-701) | absent | MAJOR | Add 4. |
| 11.9 | §5.6 subscribeObjectClassAttributes | `(cls, set, bool active=true, wstring updateRate=L"")` (RTIambassador.h:380-393) | (cls, set) (RtiAmbassador.h:209-210) | BLOCKING | Add params. |
| 11.10 | §5.3/5.7 | Both `unpublishObjectClass` and `unpublishObjectClassAttributes` (RTIambassador.h:333-355, 396-416) | only the attribute-subset form | MAJOR | Add whole-class form. |
| 11.11 | §5.8 subscribeInteractionClass | `(cls, bool active=true)` (RTIambassador.h:419-429) | `(cls)` (RtiAmbassador.h:216) | MAJOR | Add active flag. |

---

## 12. Ownership Management (§7)

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 12.1 | §7.6 confirmDivestiture | `(object, set, tag)` (RTIambassador.h:739-753) | absent | MAJOR | Add. |
| 12.2 | §7.8 attributeOwnershipAcquisition | `(object, set, tag)` (RTIambassador.h:756-770) | `(object, set)` (RtiAmbassador.h:488-490) | BLOCKING | Add tag. |
| 12.3 | §7.9 attributeOwnershipAcquisitionIfAvailable | (RTIambassador.h:773-787) | absent | MAJOR | Add. |
| 12.4 | §7.12 attributeOwnershipReleaseDenied | (RTIambassador.h:790-801) | absent | MAJOR | Add. |
| 12.5 | §7.13 attributeOwnershipDivestitureIfWanted | out-param (RTIambassador.h:804-816) | no out-param (RtiAmbassador.h:503-505) | BLOCKING | Add out-param. |
| 12.6 | §7.17 queryAttributeOwnership | void (callback-driven) (RTIambassador.h:849-859) | returns `OwnershipQueryResult` (RtiAmbassador.h:513-515) | BLOCKING | void + callbacks. |
| 12.7 | §7.19 isAttributeOwnedByFederate | bool (RTIambassador.h:862-872) | bool (RtiAmbassador.h:518-520) | COSMETIC | Match. Add spec exception set. |

---

## 13. Support Services (§10)

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 13.1 | §10.2 | `getAutomaticResignDirective()` (RTIambassador.h:1352-1356) | absent | MAJOR | Add. |
| 13.2 | §10.3 | `setAutomaticResignDirective(ResignAction)` (RTIambassador.h:1359-1365) | absent | MAJOR | Add. |
| 13.3 | §10.4-5 | `getFederateHandle/Name` (RTIambassador.h:1368-1384) | absent | MAJOR | Add. |
| 13.4 | §10.6-12 | `getObjectClassHandle/Name`, `getKnownObjectClassHandle`, `getObjectInstanceHandle/Name`, `getAttributeHandle/Name` wstring (RTIambassador.h:1387-1452) | std::string (RtiAmbassador.h:147-173) | BLOCKING | wstring + `getKnownObjectClassHandle`. |
| 13.5 | §10.13-14 | `getUpdateRateValue`, `getUpdateRateValueForAttribute` (RTIambassador.h:1455-1472) | absent | MAJOR | Add. |
| 13.6 | §10.15-18 | `getInteractionClassHandle/Name`, `getParameterHandle/Name` wstring (RTIambassador.h:1475-1513) | string (RtiAmbassador.h:155-161) | BLOCKING | wstring. |
| 13.7 | §10.19-22 | `getOrderType/Name`, `getTransportationType/Name` (RTIambassador.h:1516-1549) | absent | MAJOR | Add. |
| 13.8 | §10.23-30 | dimension lookup, region dim set, get/setRangeBounds (RTIambassador.h:1552-1637) | partial; takes RoutingSpaceHandle | BLOCKING | Full set; drop routing-space arg. |
| 13.9 | §10.31-32 | `normalizeFederateHandle`, `normalizeServiceGroup` (RTIambassador.h:1640-1655) | absent | MAJOR | Add. |
| 13.10 | §10.33-40 | 8 advisory-switch enable/disable (RTIambassador.h:1658-1735) | absent | MAJOR | Add 8. |
| 13.11 | §10.41 evokeCallback | **single arg** (RTIambassador.h:1738-1742) | 2 args + defaults (RtiAmbassador.h:628-629) | BLOCKING | Single arg. |
| 13.12 | §10.42 evokeMultipleCallbacks | 2 args, no defaults (RTIambassador.h:1745-1750) | 2 args + defaults + non-spec `tickCallback` (RtiAmbassador.h:610, 633-634) | MAJOR | Drop defaults + tickCallback. |
| 13.13 | §10.43-44 enable/disableCallbacks | (RTIambassador.h:1753-1764) | match (RtiAmbassador.h:640-642) | COSMETIC | OK. |
| 13.14 | spec | `getTimeFactory()` returns auto_ptr (RTIambassador.h:1769-1773) | absent | BLOCKING | Add — returns `rti1516e::auto_ptr<LogicalTimeFactory>` per #2.2 C++17 resolution. |
| 13.15 | spec | 9 decode*Handle methods (RTIambassador.h:1776-1846) | absent | MAJOR | Add 9. |

---

## 14. Encoding (HLA Evolved §10 / IEEE 1516.2 Annex B)

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 14.1 | Annex B | `class DataElement` abstract base (DataElement.h:28-79) | no base — free functions returning `VariableLengthData` (Encoding.h:78-103) | BLOCKING | Add. Federates polymorphically holding `auto_ptr<DataElement>` cannot compile. |
| 14.2 | Annex B | 19 basic types via `DEFINE_ENCODING_HELPER_CLASS`: HLAASCIIchar/string, HLAboolean, HLAbyte, HLAfloat32/64 BE/LE, HLAinteger16/32/64 BE/LE, HLAoctet, HLAoctetPair BE/LE, HLAunicodeChar/String (BasicDataElements.h:151-170) | 5 free functions (Encoding.h:78-115) | BLOCKING | 19 classes. |
| 14.3 | Annex B | `HLAunicodeString` storing `std::wstring` | `encodeHLAunicodeString(u16string_view)` | BLOCKING | class + wstring. |
| 14.4 | Annex B | `HLAfixedArray` class with prototype + length ctor (HLAfixedArray.h:27-134) | template free function (Encoding.h:193-200) | BLOCKING | Class form. |
| 14.5 | Annex B | `HLAvariableArray` class (HLAvariableArray.h:27-146) | 2 template free functions (Encoding.h:229-323) | BLOCKING | Class form. |
| 14.6 | Annex B | `HLAfixedRecord` class with appendElement (HLAfixedRecord.h:27-127) | `encodeHLAfixedRecord` free function (Encoding.h:339-408) | BLOCKING | Class form. |
| 14.7 | Annex B | `HLAvariantRecord` (HLAvariantRecord.h:27-149) | absent | MAJOR | Add. |
| 14.8 | Annex B | `HLAopaqueData` (HLAopaqueData.h:27-126) | uses raw `vector<uint8_t>` | MAJOR | Add. |
| 14.9 | Annex B | `Integer8/16/32/64`, `Octet`, `OctetPair` typedefs (EncodingConfig.h:24-46) | absent | BLOCKING | Add `RTI/encoding/EncodingConfig.h`. |
| 14.10 | Annex B | `clone()` returns auto_ptr<DataElement> | n/a | BLOCKING | gorti targets **C++17** (`cppsdk/CMakeLists.txt:31`); spec text is C++03. Use `rti1516e::auto_ptr` alias (= `std::unique_ptr` under C++17) per #2.2 resolution. Documented in `RTI/SpecificConfig.h`. |

---

## 15. Misc / Typedefs

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 15.1 | Typedefs.h | `typedef std::set<AttributeHandle> AttributeHandleSet` (Typedefs.h:35-39) | `using` (Types.h:87-90) | COSMETIC | Equivalent. |
| 15.2 | Typedefs.h | `AttributeHandleValueMap` (Typedefs.h:43-44) | match (Types.h:116) | COSMETIC | OK (modulo VLD class). |
| 15.3 | Typedefs.h | `AttributeHandleSetRegionHandleSetPair` = `pair<AttributeHandleSet, RegionHandleSet>` + vector (Typedefs.h:53-57) | `AttributeRegionMap` map (Types.h:100) — different semantics | BLOCKING | Vector of pairs (lets one regionset apply to multiple attrs). |
| 15.4 | Typedefs.h | `SupplementalReflectInfo/ReceiveInfo/RemoveInfo` (Typedefs.h:101-147) | absent | BLOCKING | Add — passed to every reflect/receive/remove callback. |
| 15.5 | Typedefs.h | `FederationExecutionInformation` (Typedefs.h:88-99) | absent | MAJOR | Add. |
| 15.6 | Typedefs.h | `FederateRestoreStatus` (Typedefs.h:70-81) | absent | MAJOR | Add. |
| 15.7 | RTI1516.h | `wstring rtiName(); wstring rtiVersion();` (RTI1516.h:53-54) | absent | MINOR | Add. |
| 15.8 | RTI1516.h | `HLA_SPECIFICATION_NAME "1516"`, version macros (RTI1516.h:23-25) | absent | COSMETIC | Add. |

---

## 16. MOM (§11)

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 16.1 | §11 | MOM via standard object-management subscription to `HLAobjectRoot.HLAmanager.*` — no dedicated API | `queryFederationAttributes`, `queryFederateAttributes`, `enumerateMomInstances` returning bespoke structs (RtiAmbassador.h:552-586) | MAJOR | Remove bespoke API; deliver MOM via standard object-mgmt callbacks. Keep gorti helpers in non-standard namespace if needed. |

---

## 17. Tag / Callback Threading Conventions

| # | Spec § | Pitch | gorti | Severity | Fix |
|---|---|---|---|---|---|
| 17.1 | §6.10 | Mandatory `VariableLengthData const& theUserSuppliedTag` on every object-mgmt / ownership call (RTIambassador.h:518-530 et al.) | tag often optional / absent (RtiAmbassador.h:235-240) | BLOCKING | Promote tag to mandatory where spec requires. |
| 17.2 | §10 | `CallNotAllowedFromWithinCallback` thrown if federate re-enters from callback (RTIambassador.h:54, 1741) | comment notes "must not" but no exception class | MAJOR | Add exception + runtime check. |

---

## Summary

| Category | BLOCKING | MAJOR | MINOR | COSMETIC | Total |
|---|---|---|---|---|---|
| 1. Header layout | 4 | 2 | 0 | 1 | 7 |
| 2. Construction | 4 | 0 | 0 | 1 | 5 |
| 3. Connect / federation | 11 | 2 | 0 | 0 | 13 |
| 4. FederateAmbassador callbacks | 8 | 23 | 0 | 0 | 31 |
| 5. Enums | 5 | 5 | 0 | 0 | 10 |
| 6. Exceptions | 4 | 0 | 0 | 0 | 4 |
| 7. Typed handles | 3 | 2 | 1 | 1 | 7 |
| 8. VariableLengthData | 1 | 2 | 0 | 0 | 3 |
| 9. Time | 11 | 2 | 0 | 0 | 13 |
| 10. DDM | 7 | 1 | 0 | 0 | 8 |
| 11. Object management | 5 | 5 | 0 | 0 | 10 |
| 12. Ownership | 3 | 3 | 0 | 1 | 7 |
| 13. Support services | 5 | 9 | 0 | 1 | 15 |
| 14. Encoding | 7 | 3 | 0 | 0 | 10 |
| 15. Misc typedefs | 2 | 2 | 1 | 2 | 7 |
| 16. MOM | 0 | 1 | 0 | 0 | 1 |
| 17. Tag / threading | 1 | 1 | 0 | 0 | 2 |
| **TOTAL** | **81** | **63** | **2** | **7** | **153** |

Each row drives at least one lockfile assertion. Composite rows (e.g. 4.20 "3 overloads of reflectAttributeValues") drive ≥3. The actual M31 lockfile assertion count is estimated at **~200** — final number recorded in `cppsdk/tests/dlc/lockfile/expected_red_count.txt`.

---

## Critical files (largest impact)

These five gorti headers, once aligned, get a Pitch-ported federate compiling unchanged:

- `cppsdk/include/rti1516e/RtiAmbassador.h` → rename `RTIambassador.h`, make abstract, expose factory — fixes §3 + §11 + §12 + §13
- `cppsdk/include/rti1516e/FederateAmbassador.h` → ship 3 reflect / 3 receive / 3 remove overloads + add `NullFederateAmbassador.h` — fixes §4
- `cppsdk/include/rti1516e/Types.h` → split into `Handle.h` + `Typedefs.h` + `Enums.h` + `VariableLengthData.h`; class-ify VLD + handles — fixes §5 + §7 + §8 + §15
- `cppsdk/include/rti1516e/Exceptions.h` → rebase on abstract `Exception`, add ~106 missing classes — fixes §6
- `cppsdk/include/rti1516e/Encoding.h` → replace free functions with `DataElement`-derived classes; split into `encoding/` subdir — fixes §14

Two headers that don't exist today but MUST be added:

- `cppsdk/include/RTI/LogicalTime.h` + `LogicalTimeInterval.h` + `LogicalTimeFactory.h` + `time/HLAfloat64Time.h` etc. — fixes §9
- `cppsdk/include/RTI/RTIambassadorFactory.h` — fixes §2
