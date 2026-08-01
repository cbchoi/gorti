// IEEE 1516.1-2010 §8 / Annex A — RTI/LogicalTimeFactory.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Abstract factory + `LogicalTimeFactoryFactory` static maker
// (catalogue 9.3 / FR-DLC-8). Returns `rti1516e::auto_ptr<...>` per the
// C++17 resolution in docs/DLC_COMPLIANCE_PROGRAM.md §3.1.0.

#ifndef RTI_LogicalTimeFactory_h
#define RTI_LogicalTimeFactory_h

namespace rti1516e {
class LogicalTime;
class LogicalTimeInterval;
class VariableLengthData;
}  // namespace rti1516e

#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>
#include <memory>
#include <string>

namespace rti1516e {

class RTI_EXPORT LogicalTimeFactory {
 public:
  virtual ~LogicalTimeFactory() RTI_NOEXCEPT = 0;

  virtual rti1516e::auto_ptr<LogicalTime> makeInitial() = 0;
  virtual rti1516e::auto_ptr<LogicalTime> makeFinal() = 0;

  virtual rti1516e::auto_ptr<LogicalTimeInterval> makeZero() = 0;
  virtual rti1516e::auto_ptr<LogicalTimeInterval> makeEpsilon() = 0;

  virtual rti1516e::auto_ptr<LogicalTime> decodeLogicalTime(
      VariableLengthData const& encodedLogicalTime) = 0;
  virtual rti1516e::auto_ptr<LogicalTime> decodeLogicalTime(
      void* buffer, size_t bufferSize) = 0;

  virtual rti1516e::auto_ptr<LogicalTimeInterval> decodeLogicalTimeInterval(
      VariableLengthData const& encodedValue) = 0;
  virtual rti1516e::auto_ptr<LogicalTimeInterval> decodeLogicalTimeInterval(
      void* buffer, size_t bufferSize) = 0;

  virtual std::wstring getName() const = 0;
};

// IEEE 1516.1-2010 convenience factory for standard HLA time representations.
// factory. Provides HLAfloat64Time / HLAinteger64Time out of the box; the
// federate-time-side LogicalTimeFactoryFactory below forwards to this.
class RTI_EXPORT HLAlogicalTimeFactoryFactory {
 public:
  static rti1516e::auto_ptr<LogicalTimeFactory> makeLogicalTimeFactory(
      std::wstring const& implementationName);
};

// §8 — static maker: choose factory by impl-name string at runtime.
// Exported under RTI_EXPORT_FEDTIME by the IEEE 1516.1-2010 DLC API.
// (the fed-time library), and its impl typically forwards to
// HLAlogicalTimeFactoryFactory::makeLogicalTimeFactory.
class RTI_EXPORT_FEDTIME LogicalTimeFactoryFactory {
 public:
  static rti1516e::auto_ptr<LogicalTimeFactory> makeLogicalTimeFactory(
      std::wstring const& implementationName);
};

}  // namespace rti1516e

#endif  // RTI_LogicalTimeFactory_h
