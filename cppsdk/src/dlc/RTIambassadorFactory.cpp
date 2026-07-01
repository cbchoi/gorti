// IEEE 1516.1-2010 §10.35 / Annex A — RTIambassadorFactory impl.
//
// gorti M32. Catalogue row 2.2 / FR-DLC-2. Sole entry point for a federate
// to construct an RTIambassador. Returns rti1516e::auto_ptr (== unique_ptr
// under C++17 per SpecificConfig.h) owning a DLCRTIambassadorImpl.

#include <RTI/RTIambassadorFactory.h>
#include "RTIambassadorImpl.h"

namespace rti1516e {

RTIambassadorFactory::RTIambassadorFactory() = default;
RTIambassadorFactory::~RTIambassadorFactory() RTI_NOEXCEPT = default;

rti1516e::auto_ptr<RTIambassador> RTIambassadorFactory::createRTIambassador() {
  return rti1516e::auto_ptr<RTIambassador>(new DLCRTIambassadorImpl());
}

}  // namespace rti1516e
