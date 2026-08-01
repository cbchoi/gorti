// IEEE 1516.1-2010 §10.42 / Annex A — FederateAmbassador base ctor/dtor.
//
// gorti M33. Pure-virtual class — only the protected ctor and
// virtual dtor need bodies here so the vtable link resolves. Signatures
// match the RTI_THROW(FederateInternalError) shape adopted in M33 per
// catalogue row 4.37.

#include <RTI/FederateAmbassador.h>

namespace rti1516e {

FederateAmbassador::FederateAmbassador() RTI_THROW(FederateInternalError) {}

FederateAmbassador::~FederateAmbassador() RTI_NOEXCEPT {}

}  // namespace rti1516e
