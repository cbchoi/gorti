// IEEE 1516.1-2010 §8 / Annex A — RTI/time/HLAfloat64TimeFactory.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.

#ifndef RTI_HLAfloat64TimeFactory_h
#define RTI_HLAfloat64TimeFactory_h

#include <RTI/SpecificConfig.h>
#include <RTI/LogicalTimeFactory.h>
#include <string>

namespace rti1516e {

// IEEE 1516.1-2010 namespace-scope selector constant used by
// federates pass to LogicalTimeFactoryFactory::makeLogicalTimeFactory to
// select this concrete factory. `const` at namespace scope gives internal
// linkage by default, so the header-only definition is ODR-safe.
const std::wstring HLAfloat64TimeName(L"HLAfloat64Time");

class HLAfloat64Time;
class HLAfloat64Interval;

class RTI_EXPORT_FEDTIME HLAfloat64TimeFactory : public LogicalTimeFactory {
 public:
  HLAfloat64TimeFactory();
  virtual ~HLAfloat64TimeFactory() RTI_NOEXCEPT;

  virtual rti1516e::auto_ptr<LogicalTime> makeInitial() override;
  virtual rti1516e::auto_ptr<LogicalTime> makeFinal() override;

  // Convenience non-virtual makers returning the concrete type.
  virtual rti1516e::auto_ptr<HLAfloat64Time> makeLogicalTime(double value);
  virtual rti1516e::auto_ptr<HLAfloat64Interval> makeLogicalTimeInterval(
      double value);

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

#endif  // RTI_HLAfloat64TimeFactory_h
