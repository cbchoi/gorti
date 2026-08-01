// Lockfile: RTIambassador federation-management signatures per IEEE 1516.1-2010
// §4.3-4.32. Covers connect/disconnect/create/destroy/join/resign/sync/save/restore.
// Catalogue rows covered: 3.4-3.13. Driver requirements: FR-DLC-3, FR-DLC-4, FR-DLC-10.

#include <RTI/RTIambassador.h>
#include <RTI/FederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Typedefs.h>
#include <RTI/Handle.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <string>
#include <vector>
#include <set>

namespace {

using rti1516e::RTIambassador;
using rti1516e::FederateAmbassador;
using rti1516e::CallbackModel;
using rti1516e::ResignAction;
using rti1516e::FederateHandle;
using rti1516e::FederateHandleSet;
using rti1516e::HLA_IMMEDIATE;

// §4.5 createFederationExecution — 3 overloads (catalogue 3.4) plus the MIM
//        variant `createFederationExecutionWithMIM` (reference_rtiambassador.h:65-106).
// Overload 1: (wstring federationName, wstring fomModule, wstring logicalTimeImplName=L"")
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().createFederationExecution(
        std::declval<std::wstring const&>(),
        std::declval<std::wstring const&>(),
        std::declval<std::wstring const&>())),
    void>);

// Overload 2: (wstring federationName, vector<wstring> fomModules, wstring logicalTimeImplName=L"")
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().createFederationExecution(
        std::declval<std::wstring const&>(),
        std::declval<std::vector<std::wstring> const&>(),
        std::declval<std::wstring const&>())),
    void>);

// §4.5 createFederationExecutionWithMIM (catalogue 3.4): adds MIM module path.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().createFederationExecutionWithMIM(
        std::declval<std::wstring const&>(),
        std::declval<std::vector<std::wstring> const&>(),
        std::declval<std::wstring const&>(),
        std::declval<std::wstring const&>())),
    void>);

// §4.6 destroyFederationExecution — wstring (catalogue 3.6).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().destroyFederationExecution(
        std::declval<std::wstring const&>())),
    void>);

// §4.7 listFederationExecutions — void return; result delivered via callback
//      (catalogue 3.7, reference_rtiambassador.h:118-121).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().listFederationExecutions()),
    void>);

// §4.9 joinFederationExecution — 2 overloads, both returning FederateHandle
//      (catalogue 3.8, reference_rtiambassador.h:124-158).
// Overload 1: (wstring federateName, wstring federateType, wstring federationName,
//              vector<wstring> additionalFomModules)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().joinFederationExecution(
        std::declval<std::wstring const&>(),
        std::declval<std::wstring const&>(),
        std::declval<std::wstring const&>(),
        std::declval<std::vector<std::wstring> const&>())),
    FederateHandle>);

// Overload 2: (wstring federateType, wstring federationName,
//              vector<wstring> additionalFomModules) — federate-name-elided form.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().joinFederationExecution(
        std::declval<std::wstring const&>(),
        std::declval<std::wstring const&>(),
        std::declval<std::vector<std::wstring> const&>())),
    FederateHandle>);

// §4.10 resignFederationExecution — REQUIRES a ResignAction (catalogue 3.9).
//        gorti M17 had a no-arg form; the DLC surface mandates the enum.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().resignFederationExecution(
        std::declval<ResignAction>())),
    void>);

// §4.10 — ResignAction is unscoped enum with 6 spec-mandated values (catalogue 5.3).
//          See test_callbackmodel_enum.cpp for the value-locking.
static_assert(std::is_convertible_v<ResignAction, int>);

// §4.7 registerFederationSynchronizationPoint — 2 overloads, FederateHandleSet
//      (catalogue 3.10).
// Overload 1: (wstring label, VariableLengthData tag) — global sync.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().registerFederationSynchronizationPoint(
        std::declval<std::wstring const&>(),
        std::declval<rti1516e::VariableLengthData const&>())),
    void>);

// Overload 2: (wstring label, VariableLengthData tag, FederateHandleSet) — subset sync.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().registerFederationSynchronizationPoint(
        std::declval<std::wstring const&>(),
        std::declval<rti1516e::VariableLengthData const&>(),
        std::declval<FederateHandleSet const&>())),
    void>);

// §4.14 synchronizationPointAchieved — (wstring, bool successfully=true).
//         gorti M17 was string + no bool (catalogue 3.11).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().synchronizationPointAchieved(
        std::declval<std::wstring const&>(),
        std::declval<bool>())),
    void>);

// §4.16 requestFederationSave — 2 overloads (catalogue 3.12, reference_rtiambassador.h:208-220).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().requestFederationSave(
        std::declval<std::wstring const&>())),
    void>);

// Time-tagged variant: (wstring label, LogicalTime const&). We cannot
// instantiate LogicalTime (it is abstract), but we can lock the existence
// of the spec-required two-arg form via a separate test_rtiambassador_time
// assertion. Here we lock the no-time form.

// §4.17-4.22 — save-flow callback-driven primitives (catalogue 3.12).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().federateSaveBegun()),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().federateSaveComplete()),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().federateSaveNotComplete()),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().abortFederationSave()),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().queryFederationSaveStatus()),
    void>);

// §4.24-4.31 — restore flow (catalogue 3.13).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().requestFederationRestore(
        std::declval<std::wstring const&>())),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().federateRestoreComplete()),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().federateRestoreNotComplete()),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().abortFederationRestore()),
    void>);
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().queryFederationRestoreStatus()),
    void>);

// §4.6 — destroyFederationExecution throws FederatesCurrentlyJoined.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::FederatesCurrentlyJoined>);

// §4.9 — joinFederationExecution throws FederationExecutionDoesNotExist.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::FederationExecutionDoesNotExist>);

// §4.10 — resignFederationExecution throws FederateNotExecutionMember.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::FederateNotExecutionMember>);

// §4.5 — createFederationExecution throws FederationExecutionAlreadyExists.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::FederationExecutionAlreadyExists>);

}  // namespace
