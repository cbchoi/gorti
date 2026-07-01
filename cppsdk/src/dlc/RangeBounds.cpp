// IEEE 1516.1-2010 §10.29-30 / Annex A — RangeBounds impl.
// gorti M32. Catalogue row 15.6 sibling / FR-DLC-13 sibling.

#include <RTI/RangeBounds.h>

namespace rti1516e {

RangeBounds::RangeBounds() : _lower(0), _upper(0) {}

RangeBounds::RangeBounds(unsigned long lower, unsigned long upper)
    : _lower(lower), _upper(upper) {}

RangeBounds::RangeBounds(RangeBounds const& rhs) = default;

RangeBounds::~RangeBounds() RTI_NOEXCEPT = default;

RangeBounds& RangeBounds::operator=(RangeBounds const& rhs) {
  if (this != &rhs) {
    _lower = rhs._lower;
    _upper = rhs._upper;
  }
  return *this;
}

unsigned long RangeBounds::getLowerBound() const { return _lower; }
unsigned long RangeBounds::getUpperBound() const { return _upper; }

void RangeBounds::setLowerBound(unsigned long lower) { _lower = lower; }
void RangeBounds::setUpperBound(unsigned long upper) { _upper = upper; }

}  // namespace rti1516e
