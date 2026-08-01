// IEEE 1516.1-2010 §10.29-30 / Annex A — RTI/RangeBounds.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.

#ifndef RTI_RangeBounds_h
#define RTI_RangeBounds_h

#include <RTI/SpecificConfig.h>

namespace rti1516e {

class RTI_EXPORT RangeBounds {
 public:
  RangeBounds();
  RangeBounds(unsigned long lower, unsigned long upper);
  RangeBounds(RangeBounds const& rhs);
  ~RangeBounds() RTI_NOEXCEPT;

  RangeBounds& operator=(RangeBounds const& rhs);

  unsigned long getLowerBound() const;
  unsigned long getUpperBound() const;

  void setLowerBound(unsigned long lower);
  void setUpperBound(unsigned long upper);

 private:
  unsigned long _lower;
  unsigned long _upper;
};

}  // namespace rti1516e

#endif  // RTI_RangeBounds_h
