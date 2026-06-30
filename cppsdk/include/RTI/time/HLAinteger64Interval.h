// IEEE 1516.1-2010 §8 / Annex A — RTI/time/HLAinteger64Interval.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.

#ifndef RTI_HLAinteger64Interval_h
#define RTI_HLAinteger64Interval_h

#include <RTI/SpecificConfig.h>
#include <RTI/LogicalTimeInterval.h>
#include <stdint.h>

namespace rti1516e {

class HLAinteger64IntervalImpl;
class LogicalTime;

class RTI_EXPORT_FEDTIME HLAinteger64Interval : public LogicalTimeInterval {
 public:
  HLAinteger64Interval();
  HLAinteger64Interval(int64_t value);
  HLAinteger64Interval(LogicalTimeInterval const& value);
  HLAinteger64Interval(HLAinteger64Interval const& value);
  virtual ~HLAinteger64Interval() RTI_NOEXCEPT;

  virtual void setZero() override;
  virtual bool isZero() const override;
  virtual void setEpsilon() override;
  virtual bool isEpsilon() const override;

  virtual LogicalTimeInterval& operator=(LogicalTimeInterval const& value) override;
  virtual void setToDifference(LogicalTime const& minuend,
                               LogicalTime const& subtrahend) override;

  virtual LogicalTimeInterval& operator+=(LogicalTimeInterval const& addend) override;
  virtual LogicalTimeInterval& operator-=(LogicalTimeInterval const& subtrahend) override;

  virtual bool operator>(LogicalTimeInterval const& value) const override;
  virtual bool operator<(LogicalTimeInterval const& value) const override;
  virtual bool operator==(LogicalTimeInterval const& value) const override;
  virtual bool operator>=(LogicalTimeInterval const& value) const override;
  virtual bool operator<=(LogicalTimeInterval const& value) const override;

  HLAinteger64Interval& operator=(HLAinteger64Interval const& value);
  virtual int64_t getInterval() const;
  virtual void setInterval(int64_t value);

  virtual VariableLengthData encode() const override;
  virtual size_t encode(void* buffer, size_t bufferSize) const override;
  virtual size_t encodedLength() const override;
  virtual std::wstring toString() const override;
  virtual std::wstring implementationName() const override;

 private:
  HLAinteger64IntervalImpl* _impl;
};

}  // namespace rti1516e

#endif  // RTI_HLAinteger64Interval_h
