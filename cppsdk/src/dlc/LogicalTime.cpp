// IEEE 1516.1-2010 §8 / Annex A — LogicalTime + LogicalTimeInterval + Float64.
//
// gorti M32. LogicalTime & LogicalTimeInterval are pure-abstract classes;
// their pure-virtual dtors still need out-of-line definitions. Concrete
// HLAfloat64Time + HLAfloat64Interval wrap a `double` and implement the
// spec's full arithmetic + wire-encoding surface.
//
// Catalogue rows 9.1-9.4 / FR-DLC-8.

#include <RTI/LogicalTime.h>
#include <RTI/LogicalTimeInterval.h>
#include <RTI/time/HLAfloat64Time.h>
#include <RTI/time/HLAfloat64Interval.h>
#include <RTI/Exception.h>
#include <RTI/VariableLengthData.h>

#include <cstring>
#include <limits>
#include <sstream>

namespace rti1516e {

// Pure-virtual dtor definitions (required by the C++ ABI).
LogicalTime::~LogicalTime() RTI_NOEXCEPT {}
LogicalTimeInterval::~LogicalTimeInterval() RTI_NOEXCEPT {}

namespace {

// Big-endian encode of an IEEE-754 double.
inline void be_encode_double(double v, unsigned char* out) {
  static_assert(sizeof(double) == 8, "double must be 8 bytes");
  unsigned char const* src = reinterpret_cast<unsigned char const*>(&v);
  for (size_t i = 0; i < 8; ++i) out[i] = src[7 - i];
}

inline double be_decode_double(unsigned char const* in) {
  double v = 0;
  unsigned char* dst = reinterpret_cast<unsigned char*>(&v);
  for (size_t i = 0; i < 8; ++i) dst[i] = in[7 - i];
  return v;
}

}  // namespace

// ---------------------------------------------------------------------------
// HLAfloat64Time
// ---------------------------------------------------------------------------

class HLAfloat64TimeImpl {
 public:
  double value{0.0};
};

HLAfloat64Time::HLAfloat64Time() : _impl(new HLAfloat64TimeImpl()) {}
HLAfloat64Time::HLAfloat64Time(double v) : _impl(new HLAfloat64TimeImpl()) {
  _impl->value = v;
}
HLAfloat64Time::HLAfloat64Time(LogicalTime const& v)
    : _impl(new HLAfloat64TimeImpl()) {
  HLAfloat64Time const* p = dynamic_cast<HLAfloat64Time const*>(&v);
  if (p) _impl->value = p->_impl->value;
}
HLAfloat64Time::HLAfloat64Time(HLAfloat64Time const& v)
    : _impl(new HLAfloat64TimeImpl()) {
  _impl->value = v._impl->value;
}
HLAfloat64Time::~HLAfloat64Time() RTI_NOEXCEPT { delete _impl; }

void HLAfloat64Time::setInitial() { _impl->value = 0.0; }
bool HLAfloat64Time::isInitial() const { return _impl->value == 0.0; }
void HLAfloat64Time::setFinal() {
  _impl->value = std::numeric_limits<double>::infinity();
}
bool HLAfloat64Time::isFinal() const {
  return _impl->value == std::numeric_limits<double>::infinity();
}

LogicalTime& HLAfloat64Time::operator=(LogicalTime const& v) {
  HLAfloat64Time const* p = dynamic_cast<HLAfloat64Time const*>(&v);
  if (!p) throw InvalidLogicalTime(L"HLAfloat64Time = non-HLAfloat64Time");
  _impl->value = p->_impl->value;
  return *this;
}
LogicalTime& HLAfloat64Time::operator+=(LogicalTimeInterval const& v) {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  if (!p)
    throw IllegalTimeArithmetic(L"HLAfloat64Time += non-HLAfloat64Interval");
  _impl->value += p->getInterval();
  return *this;
}
LogicalTime& HLAfloat64Time::operator-=(LogicalTimeInterval const& v) {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  if (!p)
    throw IllegalTimeArithmetic(L"HLAfloat64Time -= non-HLAfloat64Interval");
  _impl->value -= p->getInterval();
  return *this;
}
bool HLAfloat64Time::operator>(LogicalTime const& v) const {
  HLAfloat64Time const* p = dynamic_cast<HLAfloat64Time const*>(&v);
  return p && _impl->value > p->_impl->value;
}
bool HLAfloat64Time::operator<(LogicalTime const& v) const {
  HLAfloat64Time const* p = dynamic_cast<HLAfloat64Time const*>(&v);
  return p && _impl->value < p->_impl->value;
}
bool HLAfloat64Time::operator==(LogicalTime const& v) const {
  HLAfloat64Time const* p = dynamic_cast<HLAfloat64Time const*>(&v);
  return p && _impl->value == p->_impl->value;
}
bool HLAfloat64Time::operator>=(LogicalTime const& v) const {
  HLAfloat64Time const* p = dynamic_cast<HLAfloat64Time const*>(&v);
  return p && _impl->value >= p->_impl->value;
}
bool HLAfloat64Time::operator<=(LogicalTime const& v) const {
  HLAfloat64Time const* p = dynamic_cast<HLAfloat64Time const*>(&v);
  return p && _impl->value <= p->_impl->value;
}

HLAfloat64Time& HLAfloat64Time::operator=(HLAfloat64Time const& v) {
  if (this != &v) _impl->value = v._impl->value;
  return *this;
}
double HLAfloat64Time::getTime() const { return _impl->value; }
void HLAfloat64Time::setTime(double v) { _impl->value = v; }

VariableLengthData HLAfloat64Time::encode() const {
  unsigned char buf[8];
  be_encode_double(_impl->value, buf);
  return VariableLengthData(buf, 8);
}
size_t HLAfloat64Time::encode(void* buffer, size_t bufferSize) const {
  if (bufferSize < 8)
    throw CouldNotEncode(L"HLAfloat64Time encode: buffer too small");
  be_encode_double(_impl->value, static_cast<unsigned char*>(buffer));
  return 8;
}
size_t HLAfloat64Time::encodedLength() const { return 8; }

std::wstring HLAfloat64Time::toString() const {
  std::wstringstream ss;
  ss << _impl->value;
  return ss.str();
}
std::wstring HLAfloat64Time::implementationName() const {
  return L"HLAfloat64Time";
}

// ---------------------------------------------------------------------------
// HLAfloat64Interval
// ---------------------------------------------------------------------------

class HLAfloat64IntervalImpl {
 public:
  double value{0.0};
};

HLAfloat64Interval::HLAfloat64Interval()
    : _impl(new HLAfloat64IntervalImpl()) {}
HLAfloat64Interval::HLAfloat64Interval(double v)
    : _impl(new HLAfloat64IntervalImpl()) {
  _impl->value = v;
}
HLAfloat64Interval::HLAfloat64Interval(LogicalTimeInterval const& v)
    : _impl(new HLAfloat64IntervalImpl()) {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  if (p) _impl->value = p->_impl->value;
}
HLAfloat64Interval::HLAfloat64Interval(HLAfloat64Interval const& v)
    : _impl(new HLAfloat64IntervalImpl()) {
  _impl->value = v._impl->value;
}
HLAfloat64Interval::~HLAfloat64Interval() RTI_NOEXCEPT { delete _impl; }

void HLAfloat64Interval::setZero() { _impl->value = 0.0; }
bool HLAfloat64Interval::isZero() const { return _impl->value == 0.0; }
void HLAfloat64Interval::setEpsilon() {
  _impl->value = std::numeric_limits<double>::epsilon();
}
bool HLAfloat64Interval::isEpsilon() const {
  return _impl->value == std::numeric_limits<double>::epsilon();
}

LogicalTimeInterval& HLAfloat64Interval::operator=(
    LogicalTimeInterval const& v) {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  if (!p)
    throw InvalidLogicalTimeInterval(
        L"HLAfloat64Interval = non-HLAfloat64Interval");
  _impl->value = p->_impl->value;
  return *this;
}
void HLAfloat64Interval::setToDifference(LogicalTime const& a,
                                         LogicalTime const& b) {
  HLAfloat64Time const* pa = dynamic_cast<HLAfloat64Time const*>(&a);
  HLAfloat64Time const* pb = dynamic_cast<HLAfloat64Time const*>(&b);
  if (!pa || !pb)
    throw IllegalTimeArithmetic(L"HLAfloat64Interval::setToDifference");
  _impl->value = pa->getTime() - pb->getTime();
}
LogicalTimeInterval& HLAfloat64Interval::operator+=(
    LogicalTimeInterval const& v) {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  if (!p) throw IllegalTimeArithmetic(L"HLAfloat64Interval +=");
  _impl->value += p->_impl->value;
  return *this;
}
LogicalTimeInterval& HLAfloat64Interval::operator-=(
    LogicalTimeInterval const& v) {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  if (!p) throw IllegalTimeArithmetic(L"HLAfloat64Interval -=");
  _impl->value -= p->_impl->value;
  return *this;
}
bool HLAfloat64Interval::operator>(LogicalTimeInterval const& v) const {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  return p && _impl->value > p->_impl->value;
}
bool HLAfloat64Interval::operator<(LogicalTimeInterval const& v) const {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  return p && _impl->value < p->_impl->value;
}
bool HLAfloat64Interval::operator==(LogicalTimeInterval const& v) const {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  return p && _impl->value == p->_impl->value;
}
bool HLAfloat64Interval::operator>=(LogicalTimeInterval const& v) const {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  return p && _impl->value >= p->_impl->value;
}
bool HLAfloat64Interval::operator<=(LogicalTimeInterval const& v) const {
  HLAfloat64Interval const* p = dynamic_cast<HLAfloat64Interval const*>(&v);
  return p && _impl->value <= p->_impl->value;
}

HLAfloat64Interval& HLAfloat64Interval::operator=(
    HLAfloat64Interval const& v) {
  if (this != &v) _impl->value = v._impl->value;
  return *this;
}
double HLAfloat64Interval::getInterval() const { return _impl->value; }
void HLAfloat64Interval::setInterval(double v) { _impl->value = v; }

VariableLengthData HLAfloat64Interval::encode() const {
  unsigned char buf[8];
  be_encode_double(_impl->value, buf);
  return VariableLengthData(buf, 8);
}
size_t HLAfloat64Interval::encode(void* buffer, size_t bufferSize) const {
  if (bufferSize < 8)
    throw CouldNotEncode(L"HLAfloat64Interval encode: buffer too small");
  be_encode_double(_impl->value, static_cast<unsigned char*>(buffer));
  return 8;
}
size_t HLAfloat64Interval::encodedLength() const { return 8; }

std::wstring HLAfloat64Interval::toString() const {
  std::wstringstream ss;
  ss << _impl->value;
  return ss.str();
}
std::wstring HLAfloat64Interval::implementationName() const {
  return L"HLAfloat64Interval";
}

}  // namespace rti1516e
