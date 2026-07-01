// IEEE 1516.1-2010 §10.5 / Annex A — typed handle implementations.
//
// gorti M32. Catalogue rows 7.1-7.7 (all 9 handles). Each handle wraps
// a `<HandleKind>Implementation*` PIMPL that stores a uint64 value. Two
// handles compare equal iff their stored uint64 matches. Invalid handles
// (default-constructed) have value 0.
//
// The encode/decode roundtrip serializes the uint64 as big-endian 8 bytes
// (deterministic per FR-DLC-4). toString() renders the value as decimal.

#include <RTI/Handle.h>
#include <RTI/VariableLengthData.h>
#include <RTI/Exception.h>

#include <cstdint>
#include <cstring>
#include <ostream>
#include <sstream>

namespace rti1516e {

namespace {

// Big-endian encode uint64 -> 8-byte buffer.
inline void be64_encode(std::uint64_t v, unsigned char* out) {
  for (int i = 7; i >= 0; --i) {
    out[i] = static_cast<unsigned char>(v & 0xFFu);
    v >>= 8;
  }
}

// Big-endian decode 8-byte buffer -> uint64. `n` bytes read; missing bytes
// are treated as zero (defensive against short payloads).
inline std::uint64_t be64_decode(unsigned char const* in, size_t n) {
  std::uint64_t v = 0;
  for (size_t i = 0; i < 8 && i < n; ++i) {
    v = (v << 8) | in[i];
  }
  return v;
}

}  // namespace

}  // namespace rti1516e

// The Implementation classes are declared out-of-class by the DEFINE_HANDLE_CLASS
// macro in Handle.h; define them here as trivial PIMPLs so `getImplementation()`
// can return a stable pointer while keeping the underlying storage opaque.
#define IMPLEMENT_HANDLE_CLASS(HandleKind)                                     \
                                                                               \
  namespace rti1516e {                                                         \
                                                                               \
  class HandleKind##Implementation {                                           \
   public:                                                                     \
    std::uint64_t value{0};                                                    \
    HandleKind##Implementation() = default;                                    \
    explicit HandleKind##Implementation(std::uint64_t v) : value(v) {}         \
  };                                                                           \
                                                                               \
  HandleKind::HandleKind() : _impl(new HandleKind##Implementation()) {}        \
                                                                               \
  HandleKind::~HandleKind() RTI_NOEXCEPT { delete _impl; }                     \
                                                                               \
  HandleKind::HandleKind(HandleKind const& rhs)                                \
      : _impl(new HandleKind##Implementation(*rhs._impl)) {}                   \
                                                                               \
  HandleKind& HandleKind::operator=(HandleKind const& rhs) {                   \
    if (this != &rhs) *_impl = *rhs._impl;                                     \
    return *this;                                                              \
  }                                                                            \
                                                                               \
  bool HandleKind::isValid() const { return _impl->value != 0; }               \
                                                                               \
  bool HandleKind::operator==(HandleKind const& rhs) const {                   \
    return _impl->value == rhs._impl->value;                                   \
  }                                                                            \
  bool HandleKind::operator!=(HandleKind const& rhs) const {                   \
    return _impl->value != rhs._impl->value;                                   \
  }                                                                            \
  bool HandleKind::operator<(HandleKind const& rhs) const {                    \
    return _impl->value < rhs._impl->value;                                    \
  }                                                                            \
                                                                               \
  long HandleKind::hash() const {                                              \
    return static_cast<long>(_impl->value);                                    \
  }                                                                            \
                                                                               \
  VariableLengthData HandleKind::encode() const {                              \
    unsigned char buf[8];                                                      \
    be64_encode(_impl->value, buf);                                            \
    return VariableLengthData(buf, sizeof(buf));                               \
  }                                                                            \
  void HandleKind::encode(VariableLengthData& buffer) const {                  \
    unsigned char buf[8];                                                      \
    be64_encode(_impl->value, buf);                                            \
    buffer.setData(buf, sizeof(buf));                                          \
  }                                                                            \
  size_t HandleKind::encode(void* buffer, size_t bufferSize) const {           \
    if (bufferSize < 8) throw CouldNotEncode(L"buffer too small (need 8)");    \
    be64_encode(_impl->value, static_cast<unsigned char*>(buffer));            \
    return 8;                                                                  \
  }                                                                            \
  size_t HandleKind::encodedLength() const { return 8; }                       \
                                                                               \
  std::wstring HandleKind::toString() const {                                  \
    std::wstringstream ss;                                                     \
    ss << L"" #HandleKind L"(" << _impl->value << L")";                        \
    return ss.str();                                                           \
  }                                                                            \
                                                                               \
  HandleKind##Implementation const* HandleKind::getImplementation() const {    \
    return _impl;                                                              \
  }                                                                            \
  HandleKind##Implementation* HandleKind::getImplementation() { return _impl; }\
                                                                               \
  HandleKind::HandleKind(HandleKind##Implementation* impl) : _impl(impl) {}    \
                                                                               \
  HandleKind::HandleKind(VariableLengthData const& encodedValue)               \
      : _impl(new HandleKind##Implementation()) {                              \
    _impl->value = be64_decode(                                                \
        static_cast<unsigned char const*>(encodedValue.data()),                \
        encodedValue.size());                                                  \
  }                                                                            \
                                                                               \
  std::wostream& operator<<(std::wostream& os, HandleKind const& h) {          \
    return os << h.toString();                                                 \
  }                                                                            \
                                                                               \
  }  // namespace rti1516e

IMPLEMENT_HANDLE_CLASS(FederateHandle)
IMPLEMENT_HANDLE_CLASS(ObjectClassHandle)
IMPLEMENT_HANDLE_CLASS(InteractionClassHandle)
IMPLEMENT_HANDLE_CLASS(ObjectInstanceHandle)
IMPLEMENT_HANDLE_CLASS(AttributeHandle)
IMPLEMENT_HANDLE_CLASS(ParameterHandle)
IMPLEMENT_HANDLE_CLASS(DimensionHandle)
IMPLEMENT_HANDLE_CLASS(MessageRetractionHandle)
IMPLEMENT_HANDLE_CLASS(RegionHandle)
