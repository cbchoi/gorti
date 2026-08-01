// IEEE 1516.1-2010 §A — RTI/RTI1516.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// One-stop include header (catalogue 1.2 / 15.7-15.8). Pulls in the
// spec-mandated subset of RTI/*.h and exposes rtiName() / rtiVersion()
// free functions.

#ifndef RTI_RTI1516_h
#define RTI_RTI1516_h

#include <RTI/SpecificConfig.h>

// Spec macros (catalogue 15.8).
#define HLA_SPECIFICATION_NAME "1516"
#define HLA_API_MAJOR_VERSION 2010
#define HLA_API_MINOR_VERSION 0

#include <RTI/Enums.h>
#include <RTI/Exception.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>
#include <RTI/RangeBounds.h>
#include <RTI/LogicalTime.h>
#include <RTI/LogicalTimeInterval.h>
#include <RTI/LogicalTimeFactory.h>
#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/FederateAmbassador.h>
#include <RTI/NullFederateAmbassador.h>

#include <string>

namespace rti1516e {

// §A / reference_rti1516.h:53-54.
RTI_EXPORT std::wstring rtiName();
RTI_EXPORT std::wstring rtiVersion();

}  // namespace rti1516e

#endif  // RTI_RTI1516_h
