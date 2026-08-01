// IEEE 1516.1-2010 Annex A — Typedefs helper class implementations.
//
// gorti M32. Catalogue rows 15.4-15.6.

#include <RTI/Typedefs.h>

namespace rti1516e {

// § 4.32 — FederateRestoreStatus (row 15.6).
FederateRestoreStatus::FederateRestoreStatus(FederateHandle const& thePreHandle,
                                             FederateHandle const& thePostHandle,
                                             RestoreStatus theStatus)
    : preRestoreHandle(thePreHandle),
      postRestoreHandle(thePostHandle),
      status(theStatus) {}

// § 4.8 — FederationExecutionInformation (row 15.5).
FederationExecutionInformation::FederationExecutionInformation(
    std::wstring const& theFederationExecutionName,
    std::wstring const& theLogicalTimeImplementationName)
    : federationExecutionName(theFederationExecutionName),
      logicalTimeImplementationName(theLogicalTimeImplementationName) {}

// § 6.11 — SupplementalReflectInfo (row 15.4a).
SupplementalReflectInfo::SupplementalReflectInfo()
    : hasProducingFederate(false), hasSentRegions(false) {}
SupplementalReflectInfo::SupplementalReflectInfo(
    FederateHandle const& theFederateHandle)
    : hasProducingFederate(true),
      hasSentRegions(false),
      producingFederate(theFederateHandle) {}
SupplementalReflectInfo::SupplementalReflectInfo(
    RegionHandleSet const& theRegionHandleSet)
    : hasProducingFederate(false),
      hasSentRegions(true),
      sentRegions(theRegionHandleSet) {}
SupplementalReflectInfo::SupplementalReflectInfo(
    FederateHandle const& theFederateHandle,
    RegionHandleSet const& theRegionHandleSet)
    : hasProducingFederate(true),
      hasSentRegions(true),
      producingFederate(theFederateHandle),
      sentRegions(theRegionHandleSet) {}

// § 6.13 — SupplementalReceiveInfo (row 15.4b).
SupplementalReceiveInfo::SupplementalReceiveInfo()
    : hasProducingFederate(false), hasSentRegions(false) {}
SupplementalReceiveInfo::SupplementalReceiveInfo(
    FederateHandle const& theFederateHandle)
    : hasProducingFederate(true),
      hasSentRegions(false),
      producingFederate(theFederateHandle) {}
SupplementalReceiveInfo::SupplementalReceiveInfo(
    RegionHandleSet const& theRegionHandleSet)
    : hasProducingFederate(false),
      hasSentRegions(true),
      sentRegions(theRegionHandleSet) {}
SupplementalReceiveInfo::SupplementalReceiveInfo(
    FederateHandle const& theFederateHandle,
    RegionHandleSet const& theRegionHandleSet)
    : hasProducingFederate(true),
      hasSentRegions(true),
      producingFederate(theFederateHandle),
      sentRegions(theRegionHandleSet) {}

// § 6.15 — SupplementalRemoveInfo (row 15.4c).
SupplementalRemoveInfo::SupplementalRemoveInfo()
    : hasProducingFederate(false) {}
SupplementalRemoveInfo::SupplementalRemoveInfo(
    FederateHandle const& theFederateHandle)
    : hasProducingFederate(true), producingFederate(theFederateHandle) {}

}  // namespace rti1516e
