// IEEE 1516.1-2010 §8 / Annex A — RTI/time/HLAfloat64Interval.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Concrete float64 LogicalTimeInterval impl (catalogue 9.4 / FR-DLC-8).

#ifndef RTI_HLAfloat64Interval_h
#define RTI_HLAfloat64Interval_h

#include <RTI/SpecificConfig.h>
#include <RTI/LogicalTimeInterval.h>

namespace rti1516e {

class HLAfloat64IntervalImpl;
class LogicalTime;

class RTI_EXPORT_FEDTIME HLAfloat64Interval : public LogicalTimeInterval {
 public:
  HLAfloat64Interval();
  HLAfloat64Interval(double value);
  HLAfloat64Interval(LogicalTimeInterval const& value);
  HLAfloat64Interval(HLAfloat64Interval const& value);
  virtual ~HLAfloat64Interval() RTI_NOEXCEPT;

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

  HLAfloat64Interval& operator=(HLAfloat64Interval const& value);
  virtual double getInterval() const;
  virtual void setInterval(double value);

  virtual VariableLengthData encode() const override;
  virtual size_t encode(void* buffer, size_t bufferSize) const override;
  virtual size_t encodedLength() const override;
  virtual std::wstring toString() const override;
  virtual std::wstring implementationName() const override;

 private:
  HLAfloat64IntervalImpl* _impl;
};

}  // namespace rti1516e

#endif  // RTI_HLAfloat64Interval_h
