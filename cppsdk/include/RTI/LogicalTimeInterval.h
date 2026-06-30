// IEEE 1516.1-2010 §8 / Annex A — RTI/LogicalTimeInterval.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Abstract base for federation-defined time intervals (catalogue 9.2).
// Concretes live under RTI/time/.

#ifndef RTI_LogicalTimeInterval_h
#define RTI_LogicalTimeInterval_h

#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>
#include <RTI/VariableLengthData.h>
#include <string>

namespace rti1516e {

class LogicalTime;  // forward decl — used by reference below.

class RTI_EXPORT LogicalTimeInterval {
 public:
  virtual ~LogicalTimeInterval() RTI_NOEXCEPT = 0;

  virtual void setZero() = 0;
  virtual bool isZero() const = 0;
  virtual void setEpsilon() = 0;
  virtual bool isEpsilon() const = 0;

  virtual LogicalTimeInterval& operator=(LogicalTimeInterval const& value) = 0;

  virtual void setToDifference(LogicalTime const& minuend,
                               LogicalTime const& subtrahend) = 0;

  virtual LogicalTimeInterval& operator+=(LogicalTimeInterval const& addend) = 0;
  virtual LogicalTimeInterval& operator-=(LogicalTimeInterval const& subtrahend) = 0;

  virtual bool operator>(LogicalTimeInterval const& value) const = 0;
  virtual bool operator<(LogicalTimeInterval const& value) const = 0;
  virtual bool operator==(LogicalTimeInterval const& value) const = 0;
  virtual bool operator>=(LogicalTimeInterval const& value) const = 0;
  virtual bool operator<=(LogicalTimeInterval const& value) const = 0;

  virtual VariableLengthData encode() const = 0;
  virtual size_t encode(void* buffer, size_t bufferSize) const = 0;
  virtual size_t encodedLength() const = 0;

  virtual std::wstring toString() const = 0;

  virtual std::wstring implementationName() const = 0;
};

}  // namespace rti1516e

#endif  // RTI_LogicalTimeInterval_h
