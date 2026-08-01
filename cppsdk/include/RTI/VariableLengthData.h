// IEEE 1516.1-2010 §A — RTI/VariableLengthData.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Replaces gorti's old `using VariableLengthData = std::vector<uint8_t>`
// alias with the IEEE 1516 ambassador-surface class. Catalogue row 8.1 — central
// parity-test blocker.

#ifndef RTI_VariableLengthData_h
#define RTI_VariableLengthData_h

#include <RTI/SpecificConfig.h>
#include <stddef.h>

namespace rti1516e {

// Forward decl — implementation pimpl held opaque from federates.
class VariableLengthDataImplementation;

// Free-function-pointer style deleter (Annex A; row 8.3).
typedef void (*VariableLengthDataDeleteFunction)(void* data);

class RTI_EXPORT VariableLengthData {
 public:
  VariableLengthData();
  VariableLengthData(void const* inData, size_t inSize);
  VariableLengthData(VariableLengthData const& rhs);
  ~VariableLengthData();

  VariableLengthData& operator=(VariableLengthData const& rhs);

  void const* data() const;
  size_t size() const;

  // Caller is free to delete inData after the call (copy mode).
  void setData(void const* inData, size_t inSize);

  // Caller retains lifetime ownership (borrow mode).
  void setDataPointer(void* inData, size_t inSize);

  // Caller transfers lifetime ownership (take mode).
  void takeDataPointer(void* inData,
                       size_t inSize,
                       VariableLengthDataDeleteFunction func = 0);

 private:
  friend class VariableLengthDataFriend;
  VariableLengthDataImplementation* _impl;
};

}  // namespace rti1516e

#endif  // RTI_VariableLengthData_h
