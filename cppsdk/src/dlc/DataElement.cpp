// IEEE 1516.2-2010 Annex B — DataElement pure-virtual base.
//
// gorti M32. Only the dtor + the two non-pure defaults (isSameTypeAs, hash)
// need bodies. The rest is pure-virtual, satisfied by the BasicDataElements
// leaf impls.

#include <RTI/encoding/DataElement.h>

namespace rti1516e {

DataElement::~DataElement() = default;

bool DataElement::isSameTypeAs(DataElement const& inData) const {
  // Default: same-typeness == same dynamic type.
  return typeid(*this) == typeid(inData);
}

Integer64 DataElement::hash() const {
  // Default: zero. Concrete leaves override.
  return 0;
}

}  // namespace rti1516e
