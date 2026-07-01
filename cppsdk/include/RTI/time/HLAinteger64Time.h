// IEEE 1516.1-2010 §8 / Annex A — RTI/time/HLAinteger64Time.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.

#ifndef RTI_HLAinteger64Time_h
#define RTI_HLAinteger64Time_h

#include <RTI/SpecificConfig.h>
#include <RTI/LogicalTime.h>
#include <RTI/encoding/EncodingConfig.h>  // for Integer64 typedef

namespace rti1516e {

class HLAinteger64TimeImpl;
class LogicalTimeInterval;

class RTI_EXPORT_FEDTIME HLAinteger64Time : public LogicalTime {
 public:
  HLAinteger64Time();
  HLAinteger64Time(Integer64 value);
  HLAinteger64Time(LogicalTime const& value);
  HLAinteger64Time(HLAinteger64Time const& value);
  virtual ~HLAinteger64Time() RTI_NOEXCEPT;

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

  HLAinteger64Time& operator=(HLAinteger64Time const& value);
  // Per Pitch HLAinteger64Time.h:129,131,141 — Integer64 typedef, not
  // int64_t; the concrete surface uses rti1516e::Integer64 consistently.
  virtual Integer64 getTime() const;
  virtual void setTime(Integer64 value);
  operator Integer64() const;

  virtual VariableLengthData encode() const override;
  virtual size_t encode(void* buffer, size_t bufferSize) const override;
  virtual size_t encodedLength() const override;
  virtual std::wstring toString() const override;
  virtual std::wstring implementationName() const override;

 private:
  HLAinteger64TimeImpl* _impl;
};

}  // namespace rti1516e

#endif  // RTI_HLAinteger64Time_h
