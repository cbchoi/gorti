// IEEE 1516.1-2010 §A — RTI/Typedefs.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Standard-library typedefs for handle sets, value maps, and the
// region/save/restore status containers passed to/from callbacks.
// Catalogue rows 15.1-15.6.

#ifndef RTI_Typedefs_h
#define RTI_Typedefs_h

#include <RTI/SpecificConfig.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/VariableLengthData.h>
#include <set>
#include <map>
#include <vector>
#include <utility>
#include <string>

namespace rti1516e {

// §A — Handle sets (catalogue 15.1).
typedef std::set<AttributeHandle> AttributeHandleSet;
typedef std::set<ParameterHandle> ParameterHandleSet;
typedef std::set<FederateHandle> FederateHandleSet;
typedef std::set<DimensionHandle> DimensionHandleSet;
typedef std::set<RegionHandle> RegionHandleSet;

// §6.10 / §A — Attribute/parameter value maps (catalogue 15.2).
typedef std::map<AttributeHandle, VariableLengthData> AttributeHandleValueMap;
typedef std::map<ParameterHandle, VariableLengthData> ParameterHandleValueMap;

// §9 / §A — DDM pair-vector (catalogue 15.3; FR-DLC-13).
typedef std::pair<AttributeHandleSet, RegionHandleSet>
    AttributeHandleSetRegionHandleSetPair;
typedef std::vector<AttributeHandleSetRegionHandleSetPair>
    AttributeHandleSetRegionHandleSetPairVector;

// §4.23 — Save status per federate (Pitch Typedefs.h:62-67).
typedef std::pair<FederateHandle, SaveStatus> FederateHandleSaveStatusPair;
typedef std::vector<FederateHandleSaveStatusPair>
    FederateHandleSaveStatusPairVector;

// §4.32 — Restore status per federate (catalogue 15.6).
class RTI_EXPORT FederateRestoreStatus {
 public:
  FederateRestoreStatus(FederateHandle const& thePreHandle,
                        FederateHandle const& thePostHandle,
                        RestoreStatus theStatus);

  FederateHandle preRestoreHandle;
  FederateHandle postRestoreHandle;
  RestoreStatus status;
};
typedef std::vector<FederateRestoreStatus> FederateRestoreStatusVector;

// §4.8 — listFederationExecutions reply (catalogue 15.5).
class RTI_EXPORT FederationExecutionInformation {
 public:
  FederationExecutionInformation(
      std::wstring const& theFederationExecutionName,
      std::wstring const& theLogicalTimeImplementationName);

  std::wstring federationExecutionName;
  std::wstring logicalTimeImplementationName;
};
typedef std::vector<FederationExecutionInformation>
    FederationExecutionInformationVector;

// §6.11 / §6.13 / §6.15 — Supplemental-info structs passed by the RTI to
// the reflect/receive/remove callback families (catalogue 15.4).
class RTI_EXPORT SupplementalReflectInfo {
 public:
  SupplementalReflectInfo();
  SupplementalReflectInfo(FederateHandle const& theFederateHandle);
  SupplementalReflectInfo(RegionHandleSet const& theRegionHandleSet);
  SupplementalReflectInfo(FederateHandle const& theFederateHandle,
                          RegionHandleSet const& theRegionHandleSet);

  bool hasProducingFederate;
  bool hasSentRegions;
  FederateHandle producingFederate;
  RegionHandleSet sentRegions;
};

class RTI_EXPORT SupplementalReceiveInfo {
 public:
  SupplementalReceiveInfo();
  SupplementalReceiveInfo(FederateHandle const& theFederateHandle);
  SupplementalReceiveInfo(RegionHandleSet const& theRegionHandleSet);
  SupplementalReceiveInfo(FederateHandle const& theFederateHandle,
                          RegionHandleSet const& theRegionHandleSet);

  bool hasProducingFederate;
  bool hasSentRegions;
  FederateHandle producingFederate;
  RegionHandleSet sentRegions;
};

class RTI_EXPORT SupplementalRemoveInfo {
 public:
  SupplementalRemoveInfo();
  SupplementalRemoveInfo(FederateHandle const& theFederateHandle);

  bool hasProducingFederate;
  FederateHandle producingFederate;
};

}  // namespace rti1516e

#endif  // RTI_Typedefs_h
