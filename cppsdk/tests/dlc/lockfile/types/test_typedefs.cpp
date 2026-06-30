// Lockfile: IEEE 1516.1-2010 RTI/Typedefs.h — handle-set / map / pair-vector + supplemental info.
// Catalogue §15 rows 15.1-15.6.
//
// M31 RED — fails until M32 lands `RTI/Typedefs.h` with the spec aliases AND
// the SupplementalReflectInfo / SupplementalReceiveInfo / SupplementalRemoveInfo
// + FederateRestoreStatus + FederationExecutionInformation classes.
//
// Pitch ref: ~/prti1516e/api/cpp/HLA_1516-2010/RTI/Typedefs.h:33-148

#include <RTI/Typedefs.h>
#include <RTI/Handle.h>
#include <RTI/Enums.h>
#include <RTI/VariableLengthData.h>
#include <type_traits>
#include <set>
#include <map>
#include <vector>
#include <utility>
#include <string>

namespace {

// ---------- Row 15.1: handle-set typedefs are std::set of the right handle ----------
static_assert(std::is_same_v<rti1516e::AttributeHandleSet,
                             std::set<rti1516e::AttributeHandle>>);
static_assert(std::is_same_v<rti1516e::ParameterHandleSet,
                             std::set<rti1516e::ParameterHandle>>);
static_assert(std::is_same_v<rti1516e::FederateHandleSet,
                             std::set<rti1516e::FederateHandle>>);
static_assert(std::is_same_v<rti1516e::DimensionHandleSet,
                             std::set<rti1516e::DimensionHandle>>);
static_assert(std::is_same_v<rti1516e::RegionHandleSet,
                             std::set<rti1516e::RegionHandle>>);

// ---------- Row 15.2: AttributeHandleValueMap / ParameterHandleValueMap ----------
static_assert(std::is_same_v<rti1516e::AttributeHandleValueMap,
                             std::map<rti1516e::AttributeHandle,
                                      rti1516e::VariableLengthData>>);
static_assert(std::is_same_v<rti1516e::ParameterHandleValueMap,
                             std::map<rti1516e::ParameterHandle,
                                      rti1516e::VariableLengthData>>);

// ---------- Row 15.3: AttributeHandleSetRegionHandleSetPair + Vector ----------
// PAIR (not map) so one region-set can apply across multiple attribute groups.
// gorti's M17 AttributeRegionMap is the BLOCKING divergence.
static_assert(std::is_same_v<rti1516e::AttributeHandleSetRegionHandleSetPair,
                             std::pair<rti1516e::AttributeHandleSet,
                                       rti1516e::RegionHandleSet>>);
static_assert(std::is_same_v<rti1516e::AttributeHandleSetRegionHandleSetPairVector,
                             std::vector<rti1516e::AttributeHandleSetRegionHandleSetPair>>);

// ---------- Row 15.6 / §4.23: FederateHandleSaveStatusPair + Vector ----------
static_assert(std::is_same_v<rti1516e::FederateHandleSaveStatusPair,
                             std::pair<rti1516e::FederateHandle,
                                       rti1516e::SaveStatus>>);
static_assert(std::is_same_v<rti1516e::FederateHandleSaveStatusPairVector,
                             std::vector<rti1516e::FederateHandleSaveStatusPair>>);

// ---------- Row 15.6: FederateRestoreStatus class ----------
static_assert(std::is_class_v<rti1516e::FederateRestoreStatus>);
// Ctor takes (FederateHandle const&, FederateHandle const&, RestoreStatus)
static_assert(std::is_constructible_v<rti1516e::FederateRestoreStatus,
                                      rti1516e::FederateHandle const&,
                                      rti1516e::FederateHandle const&,
                                      rti1516e::RestoreStatus>);
// Members per Typedefs.h:78-80
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::FederateRestoreStatus&>().preRestoreHandle),
    rti1516e::FederateHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::FederateRestoreStatus&>().postRestoreHandle),
    rti1516e::FederateHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::FederateRestoreStatus&>().status),
    rti1516e::RestoreStatus>);
// Row 15.6: vector typedef
static_assert(std::is_same_v<rti1516e::FederateRestoreStatusVector,
                             std::vector<rti1516e::FederateRestoreStatus>>);

// ---------- Row 15.5: FederationExecutionInformation class ----------
static_assert(std::is_class_v<rti1516e::FederationExecutionInformation>);
static_assert(std::is_constructible_v<rti1516e::FederationExecutionInformation,
                                      std::wstring const&,
                                      std::wstring const&>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::FederationExecutionInformation&>()
                 .federationExecutionName),
    std::wstring>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::FederationExecutionInformation&>()
                 .logicalTimeImplementationName),
    std::wstring>);
static_assert(std::is_same_v<rti1516e::FederationExecutionInformationVector,
                             std::vector<rti1516e::FederationExecutionInformation>>);

// ---------- Row 15.4: SupplementalReflectInfo / ReceiveInfo / RemoveInfo ----------
static_assert(std::is_class_v<rti1516e::SupplementalReflectInfo>);
static_assert(std::is_default_constructible_v<rti1516e::SupplementalReflectInfo>);
static_assert(std::is_constructible_v<rti1516e::SupplementalReflectInfo,
                                      rti1516e::FederateHandle const&>);
static_assert(std::is_constructible_v<rti1516e::SupplementalReflectInfo,
                                      rti1516e::RegionHandleSet const&>);
static_assert(std::is_constructible_v<rti1516e::SupplementalReflectInfo,
                                      rti1516e::FederateHandle const&,
                                      rti1516e::RegionHandleSet const&>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::SupplementalReflectInfo&>().hasProducingFederate),
    bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::SupplementalReflectInfo&>().hasSentRegions),
    bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::SupplementalReflectInfo&>().producingFederate),
    rti1516e::FederateHandle>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::SupplementalReflectInfo&>().sentRegions),
    rti1516e::RegionHandleSet>);

static_assert(std::is_class_v<rti1516e::SupplementalReceiveInfo>);
static_assert(std::is_default_constructible_v<rti1516e::SupplementalReceiveInfo>);
static_assert(std::is_constructible_v<rti1516e::SupplementalReceiveInfo,
                                      rti1516e::FederateHandle const&>);
static_assert(std::is_constructible_v<rti1516e::SupplementalReceiveInfo,
                                      rti1516e::RegionHandleSet const&>);

static_assert(std::is_class_v<rti1516e::SupplementalRemoveInfo>);
static_assert(std::is_default_constructible_v<rti1516e::SupplementalRemoveInfo>);
static_assert(std::is_constructible_v<rti1516e::SupplementalRemoveInfo,
                                      rti1516e::FederateHandle const&>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::SupplementalRemoveInfo&>().hasProducingFederate),
    bool>);
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::SupplementalRemoveInfo&>().producingFederate),
    rti1516e::FederateHandle>);

}  // namespace
