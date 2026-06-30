// IEEE 1516.1-2010 §10.35 / Annex A — RTI/RTIambassadorFactory.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// FR-DLC-2 / catalogue row 2.2. Returns `rti1516e::auto_ptr<RTIambassador>`
// per the C++17 resolution in docs/DLC_COMPLIANCE_PROGRAM.md §3.1.0.

#ifndef RTI_RTIambassadorFactory_h
#define RTI_RTIambassadorFactory_h

namespace rti1516e {
class RTIambassador;
}

#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>
#include <memory>
#include <vector>
#include <string>

namespace rti1516e {

class RTI_EXPORT RTIambassadorFactory {
 public:
  RTIambassadorFactory();
  virtual ~RTIambassadorFactory() RTI_NOEXCEPT;

  // §10.35 — sole construction path for an RTIambassador.
  rti1516e::auto_ptr<RTIambassador> createRTIambassador();
};

}  // namespace rti1516e

#endif  // RTI_RTIambassadorFactory_h
