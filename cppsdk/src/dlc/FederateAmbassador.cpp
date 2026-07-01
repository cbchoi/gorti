// IEEE 1516.1-2010 §10.42 / Annex A — FederateAmbassador base ctor/dtor.
//
// gorti M32. Pure-virtual class — only the protected ctor and virtual dtor
// need bodies here so the vtable link resolves.

#include <RTI/FederateAmbassador.h>

namespace rti1516e {

FederateAmbassador::FederateAmbassador() RTI_NOEXCEPT {}

FederateAmbassador::~FederateAmbassador() RTI_NOEXCEPT {}

}  // namespace rti1516e
