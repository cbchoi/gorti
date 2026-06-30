// IEEE 1516.1-2010 §8 / Annex A — RTI/time/HLAfloat64Time.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Concrete float64 LogicalTime impl (catalogue 9.4 / FR-DLC-8).

#ifndef RTI_HLAfloat64Time_h
#define RTI_HLAfloat64Time_h

#include <RTI/SpecificConfig.h>
#include <RTI/LogicalTime.h>

namespace rti1516e {

class HLAfloat64TimeImpl;
class LogicalTimeInterval;

class RTI_EXPORT_FEDTIME HLAfloat64Time : public LogicalTime {
 public:
  HLAfloat64Time();
  HLAfloat64Time(double value);
  HLAfloat64Time(LogicalTime const& value);
  HLAfloat64Time(HLAfloat64Time const& value);
  virtual ~HLAfloat64Time() RTI_NOEXCEPT;

  virtual void setInitial() override;
  virtual bool isInitial() const override;
  virtual void setFinal() override;
  virtual bool isFinal() const override;

  virtual LogicalTime& operator=(LogicalTime const& value) override;
  virtual LogicalTime& operator+=(LogicalTimeInterval const& addend) override;
  virtual LogicalTime& operator-=(LogicalTimeInterval const& subtrahend) override;

  virtual bool operator>(LogicalTime const& value) const override;
  virtual bool operator<(LogicalTime const& value) const override;
  virtual bool operator==(LogicalTime const& value) const override;
  virtual bool operator>=(LogicalTime const& value) const override;
  virtual bool operator<=(LogicalTime const& value) const override;

  HLAfloat64Time& operator=(HLAfloat64Time const& value);
  virtual double getTime() const;
  virtual void setTime(double value);

  virtual VariableLengthData encode() const override;
  virtual size_t encode(void* buffer, size_t bufferSize) const override;
  virtual size_t encodedLength() const override;
  virtual std::wstring toString() const override;
  virtual std::wstring implementationName() const override;

 private:
  HLAfloat64TimeImpl* _impl;
};

}  // namespace rti1516e

#endif  // RTI_HLAfloat64Time_h
