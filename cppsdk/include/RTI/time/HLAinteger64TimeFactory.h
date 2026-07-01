// IEEE 1516.1-2010 §8 / Annex A — RTI/time/HLAinteger64TimeFactory.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.

#ifndef RTI_HLAinteger64TimeFactory_h
#define RTI_HLAinteger64TimeFactory_h

#include <RTI/SpecificConfig.h>
#include <RTI/LogicalTimeFactory.h>
#include <RTI/encoding/EncodingConfig.h>  // for Integer64 typedef
#include <string>

namespace rti1516e {

// Per Pitch HLAinteger64TimeFactory.h:26 — namespace-scope selector constant.
const std::wstring HLAinteger64TimeName(L"HLAinteger64Time");

class HLAinteger64Time;
class HLAinteger64Interval;

class RTI_EXPORT_FEDTIME HLAinteger64TimeFactory : public LogicalTimeFactory {
 public:
  HLAinteger64TimeFactory();
  virtual ~HLAinteger64TimeFactory() RTI_NOEXCEPT;

  virtual rti1516e::auto_ptr<LogicalTime> makeInitial() override;
  virtual rti1516e::auto_ptr<LogicalTime> makeFinal() override;

  // Use rti1516e::Integer64 (matches concrete HLAinteger64Time's surface).
  virtual rti1516e::auto_ptr<HLAinteger64Time> makeLogicalTime(Integer64 value);
  virtual rti1516e::auto_ptr<HLAinteger64Interval> makeLogicalTimeInterval(
      Integer64 value);

  virtual rti1516e::auto_ptr<LogicalTimeInterval> makeZero() override;
  virtual rti1516e::auto_ptr<LogicalTimeInterval> makeEpsilon() override;

  virtual rti1516e::auto_ptr<LogicalTime> decodeLogicalTime(
      VariableLengthData const& encodedLogicalTime) override;
  virtual rti1516e::auto_ptr<LogicalTime> decodeLogicalTime(
      void* buffer, size_t bufferSize) override;
  virtual rti1516e::auto_ptr<LogicalTimeInterval> decodeLogicalTimeInterval(
      VariableLengthData const& encodedValue) override;
  virtual rti1516e::auto_ptr<LogicalTimeInterval> decodeLogicalTimeInterval(
      void* buffer, size_t bufferSize) override;

  virtual std::wstring getName() const override;
};

}  // namespace rti1516e

#endif  // RTI_HLAinteger64TimeFactory_h
