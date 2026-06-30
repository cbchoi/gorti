// IEEE 1516.1-2010 §8 / Annex A — RTI/LogicalTime.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Abstract base for federation-defined logical times (catalogue 9.1).
// Concrete `HLAfloat64Time` + `HLAinteger64Time` live under RTI/time/.

#ifndef RTI_LogicalTime_h
#define RTI_LogicalTime_h

namespace rti1516e {
class LogicalTimeInterval;
}

#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>
#include <RTI/VariableLengthData.h>
#include <string>

namespace rti1516e {

class RTI_EXPORT LogicalTime {
 public:
  virtual ~LogicalTime() RTI_NOEXCEPT = 0;

  virtual void setInitial() = 0;
  virtual bool isInitial() const = 0;
  virtual void setFinal() = 0;
  virtual bool isFinal() const = 0;

  virtual LogicalTime& operator=(LogicalTime const& value) = 0;
  virtual LogicalTime& operator+=(LogicalTimeInterval const& addend) = 0;
  virtual LogicalTime& operator-=(LogicalTimeInterval const& subtrahend) = 0;

  virtual bool operator>(LogicalTime const& value) const = 0;
  virtual bool operator<(LogicalTime const& value) const = 0;
  virtual bool operator==(LogicalTime const& value) const = 0;
  virtual bool operator>=(LogicalTime const& value) const = 0;
  virtual bool operator<=(LogicalTime const& value) const = 0;

  // Decode/encode (catalogue 9.7-9.10).
  virtual VariableLengthData encode() const = 0;
  virtual size_t encode(void* buffer, size_t bufferSize) const = 0;
  virtual size_t encodedLength() const = 0;

  virtual std::wstring toString() const = 0;

  virtual std::wstring implementationName() const = 0;
};

}  // namespace rti1516e

#endif  // RTI_LogicalTime_h
