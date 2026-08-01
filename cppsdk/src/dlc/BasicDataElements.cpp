// IEEE 1516.2-2010 Annex B — 19 basic HLA data encoders.
//
// gorti M32. Catalogue 14.2 / FR-DLC-7. This file provides the minimum
// impl surface so the DEFINE_ENCODING_HELPER_CLASS macro's 19 vtables
// resolve at link time.
//
// The encoding fidelity is spec-correct for the fixed-width numeric types
// (HLAinteger*, HLAfloat*, HLAoctet, HLAbyte, HLAboolean, HLAASCIIchar,
// HLAunicodeChar, HLAoctetPair). Variable-width types (HLAASCIIstring,
// HLAunicodeString) use the spec's HLA-varString wire form
// (uint32BE length + raw bytes). Downstream milestones may replace with
// stricter reference_rti-parity impls; M32 acceptance is link-only.

#include <RTI/encoding/BasicDataElements.h>
#include <RTI/encoding/EncodingExceptions.h>
#include <RTI/VariableLengthData.h>

#include <cstdint>
#include <cstring>
#include <memory>
#include <string>
#include <vector>

namespace rti1516e {

namespace {

// Byte-order helpers. Assume host is little-endian (x86/ARM64) — matches
// gorti's build targets.

template <typename T>
void copy_be(T v, unsigned char* out) {
  unsigned char const* src = reinterpret_cast<unsigned char const*>(&v);
  for (size_t i = 0; i < sizeof(T); ++i) {
    out[i] = src[sizeof(T) - 1 - i];
  }
}

template <typename T>
void copy_le(T v, unsigned char* out) {
  std::memcpy(out, &v, sizeof(T));
}

template <typename T>
T load_be(unsigned char const* in) {
  T v = 0;
  unsigned char* dst = reinterpret_cast<unsigned char*>(&v);
  for (size_t i = 0; i < sizeof(T); ++i) {
    dst[i] = in[sizeof(T) - 1 - i];
  }
  return v;
}

template <typename T>
T load_le(unsigned char const* in) {
  T v = 0;
  std::memcpy(&v, in, sizeof(T));
  return v;
}

}  // namespace

// -----------------------------------------------------------------------
// Numeric leaf: fixed-size, BE/LE, no dynamic state.
// The macro-declared *Implementation class holds the value.
// -----------------------------------------------------------------------

#define DEFINE_NUMERIC_IMPL(Cls, SimpleT, Endian)                              \
  class Cls##Implementation {                                                  \
   public:                                                                     \
    SimpleT value{};                                                           \
    Cls##Implementation() = default;                                           \
    explicit Cls##Implementation(SimpleT v) : value(v) {}                      \
  };                                                                           \
                                                                               \
  Cls::Cls() : _impl(new Cls##Implementation()) {}                             \
  Cls::Cls(SimpleT const& inData) : _impl(new Cls##Implementation(inData)) {}  \
  Cls::Cls(SimpleT* inData)                                                    \
      : _impl(new Cls##Implementation(inData ? *inData : SimpleT{})) {}        \
  Cls::Cls(Cls const& rhs) : _impl(new Cls##Implementation(*rhs._impl)) {}     \
  Cls::~Cls() { delete _impl; }                                                \
                                                                               \
  rti1516e::auto_ptr<DataElement> Cls::clone() const {                         \
    return rti1516e::auto_ptr<DataElement>(new Cls(*this));                    \
  }                                                                            \
                                                                               \
  VariableLengthData Cls::encode() const {                                     \
    unsigned char buf[sizeof(SimpleT)];                                        \
    copy_##Endian(_impl->value, buf);                                          \
    return VariableLengthData(buf, sizeof(buf));                               \
  }                                                                            \
  void Cls::encode(VariableLengthData& inData) const {                         \
    unsigned char buf[sizeof(SimpleT)];                                        \
    copy_##Endian(_impl->value, buf);                                          \
    inData.setData(buf, sizeof(buf));                                          \
  }                                                                            \
  void Cls::encodeInto(std::vector<Octet>& buffer) const {                     \
    unsigned char tmp[sizeof(SimpleT)];                                        \
    copy_##Endian(_impl->value, tmp);                                          \
    for (size_t i = 0; i < sizeof(tmp); ++i)                                   \
      buffer.push_back(static_cast<Octet>(tmp[i]));                            \
  }                                                                            \
  void Cls::decode(VariableLengthData const& inData) {                         \
    if (inData.size() < sizeof(SimpleT))                                       \
      throw DecoderException(L"" #Cls L" decode: buffer too small");           \
    _impl->value = load_##Endian<SimpleT>(                                     \
        static_cast<unsigned char const*>(inData.data()));                     \
  }                                                                            \
  size_t Cls::decodeFrom(std::vector<Octet> const& buffer, size_t index) {     \
    if (buffer.size() < index + sizeof(SimpleT))                               \
      throw DecoderException(L"" #Cls L" decodeFrom: buffer too small");       \
    unsigned char tmp[sizeof(SimpleT)];                                        \
    for (size_t i = 0; i < sizeof(SimpleT); ++i)                               \
      tmp[i] = static_cast<unsigned char>(buffer[index + i]);                  \
    _impl->value = load_##Endian<SimpleT>(tmp);                                \
    return index + sizeof(SimpleT);                                            \
  }                                                                            \
  size_t Cls::getEncodedLength() const { return sizeof(SimpleT); }             \
  unsigned int Cls::getOctetBoundary() const { return sizeof(SimpleT); }       \
                                                                               \
  Integer64 Cls::hash() const {                                                \
    return static_cast<Integer64>(_impl->value);                               \
  }                                                                            \
                                                                               \
  Cls& Cls::operator=(Cls const& rhs) {                                        \
    if (this != &rhs) *_impl = *rhs._impl;                                     \
    return *this;                                                              \
  }                                                                            \
  Cls& Cls::operator=(SimpleT const& rhs) {                                    \
    _impl->value = rhs;                                                        \
    return *this;                                                              \
  }                                                                            \
  Cls::operator SimpleT() const { return _impl->value; }                       \
  SimpleT Cls::get() const { return _impl->value; }                            \
  void Cls::set(SimpleT inData) { _impl->value = inData; }

// HLAboolean is NOT a naive bool serialization — per Annex B it uses
// HLAinteger32BE wire form (0 = false, 1 = true). sizeof(bool) is
// platform-defined (1 on x86-64) so DEFINE_NUMERIC_IMPL wouldn't produce
// spec-canonical bytes. Hand-rolled below.
DEFINE_NUMERIC_IMPL(HLAbyte, Octet, be)
DEFINE_NUMERIC_IMPL(HLAfloat32BE, float, be)
DEFINE_NUMERIC_IMPL(HLAfloat32LE, float, le)
DEFINE_NUMERIC_IMPL(HLAfloat64BE, double, be)
DEFINE_NUMERIC_IMPL(HLAfloat64LE, double, le)
DEFINE_NUMERIC_IMPL(HLAinteger16BE, Integer16, be)
DEFINE_NUMERIC_IMPL(HLAinteger16LE, Integer16, le)
DEFINE_NUMERIC_IMPL(HLAinteger32BE, Integer32, be)
DEFINE_NUMERIC_IMPL(HLAinteger32LE, Integer32, le)
DEFINE_NUMERIC_IMPL(HLAinteger64BE, Integer64, be)
DEFINE_NUMERIC_IMPL(HLAinteger64LE, Integer64, le)
DEFINE_NUMERIC_IMPL(HLAoctet, Octet, be)
DEFINE_NUMERIC_IMPL(HLAASCIIchar, char, be)
DEFINE_NUMERIC_IMPL(HLAunicodeChar, wchar_t, be)

// -----------------------------------------------------------------------
// HLAboolean — Annex B: uint32BE, 0/1. sizeof(bool) is 1 on x86-64 so we
// cannot reuse DEFINE_NUMERIC_IMPL. Cross-check: gorti's golden vector
// `boolean-true` = 00000001, `boolean-false` = 00000000
// (tests/conformance/encoding_vectors.json).
// -----------------------------------------------------------------------

class HLAbooleanImplementation {
 public:
  bool value{false};
  HLAbooleanImplementation() = default;
  explicit HLAbooleanImplementation(bool v) : value(v) {}
};

HLAboolean::HLAboolean() : _impl(new HLAbooleanImplementation()) {}
HLAboolean::HLAboolean(bool const& v) : _impl(new HLAbooleanImplementation(v)) {}
HLAboolean::HLAboolean(bool* v)
    : _impl(new HLAbooleanImplementation(v ? *v : false)) {}
HLAboolean::HLAboolean(HLAboolean const& rhs)
    : _impl(new HLAbooleanImplementation(*rhs._impl)) {}
HLAboolean::~HLAboolean() { delete _impl; }

rti1516e::auto_ptr<DataElement> HLAboolean::clone() const {
  return rti1516e::auto_ptr<DataElement>(new HLAboolean(*this));
}

VariableLengthData HLAboolean::encode() const {
  unsigned char buf[4];
  std::uint32_t n = _impl->value ? 1u : 0u;
  copy_be<std::uint32_t>(n, buf);
  return VariableLengthData(buf, sizeof(buf));
}
void HLAboolean::encode(VariableLengthData& inData) const {
  unsigned char buf[4];
  std::uint32_t n = _impl->value ? 1u : 0u;
  copy_be<std::uint32_t>(n, buf);
  inData.setData(buf, sizeof(buf));
}
void HLAboolean::encodeInto(std::vector<Octet>& buffer) const {
  unsigned char buf[4];
  std::uint32_t n = _impl->value ? 1u : 0u;
  copy_be<std::uint32_t>(n, buf);
  for (int i = 0; i < 4; ++i) buffer.push_back(static_cast<Octet>(buf[i]));
}
void HLAboolean::decode(VariableLengthData const& inData) {
  if (inData.size() < 4)
    throw DecoderException(L"HLAboolean decode: buffer too small");
  std::uint32_t n = load_be<std::uint32_t>(
      static_cast<unsigned char const*>(inData.data()));
  _impl->value = (n != 0);
}
size_t HLAboolean::decodeFrom(std::vector<Octet> const& buffer, size_t index) {
  if (buffer.size() < index + 4)
    throw DecoderException(L"HLAboolean decodeFrom: buffer too small");
  unsigned char tmp[4];
  for (int i = 0; i < 4; ++i)
    tmp[i] = static_cast<unsigned char>(buffer[index + i]);
  std::uint32_t n = load_be<std::uint32_t>(tmp);
  _impl->value = (n != 0);
  return index + 4;
}
size_t HLAboolean::getEncodedLength() const { return 4; }
unsigned int HLAboolean::getOctetBoundary() const { return 4; }
Integer64 HLAboolean::hash() const {
  return _impl->value ? Integer64{1} : Integer64{0};
}
HLAboolean& HLAboolean::operator=(HLAboolean const& rhs) {
  if (this != &rhs) *_impl = *rhs._impl;
  return *this;
}
HLAboolean& HLAboolean::operator=(bool const& rhs) {
  _impl->value = rhs;
  return *this;
}
HLAboolean::operator bool() const { return _impl->value; }
bool HLAboolean::get() const { return _impl->value; }
void HLAboolean::set(bool inData) { _impl->value = inData; }

// -----------------------------------------------------------------------
// HLAoctetPair (2 bytes) — BE + LE. SimpleT is std::pair<Octet, Octet>.
// -----------------------------------------------------------------------

#define DEFINE_OCTET_PAIR_IMPL(Cls, Endian)                                    \
  class Cls##Implementation {                                                  \
   public:                                                                     \
    OctetPair value{};                                                         \
    Cls##Implementation() = default;                                           \
    explicit Cls##Implementation(OctetPair v) : value(v) {}                    \
  };                                                                           \
                                                                               \
  Cls::Cls() : _impl(new Cls##Implementation()) {}                             \
  Cls::Cls(OctetPair const& v) : _impl(new Cls##Implementation(v)) {}          \
  Cls::Cls(OctetPair* v)                                                       \
      : _impl(new Cls##Implementation(v ? *v : OctetPair{})) {}                \
  Cls::Cls(Cls const& rhs) : _impl(new Cls##Implementation(*rhs._impl)) {}     \
  Cls::~Cls() { delete _impl; }                                                \
                                                                               \
  rti1516e::auto_ptr<DataElement> Cls::clone() const {                         \
    return rti1516e::auto_ptr<DataElement>(new Cls(*this));                    \
  }                                                                            \
                                                                               \
  VariableLengthData Cls::encode() const {                                     \
    unsigned char buf[2];                                                      \
    if (std::string(#Endian) == "be") {                                        \
      buf[0] = static_cast<unsigned char>(_impl->value.first);                 \
      buf[1] = static_cast<unsigned char>(_impl->value.second);                \
    } else {                                                                   \
      buf[0] = static_cast<unsigned char>(_impl->value.second);                \
      buf[1] = static_cast<unsigned char>(_impl->value.first);                 \
    }                                                                          \
    return VariableLengthData(buf, 2);                                         \
  }                                                                            \
  void Cls::encode(VariableLengthData& inData) const {                         \
    VariableLengthData v = encode();                                           \
    inData.setData(v.data(), v.size());                                        \
  }                                                                            \
  void Cls::encodeInto(std::vector<Octet>& buffer) const {                     \
    VariableLengthData v = encode();                                           \
    unsigned char const* p =                                                   \
        static_cast<unsigned char const*>(v.data());                           \
    buffer.push_back(static_cast<Octet>(p[0]));                                \
    buffer.push_back(static_cast<Octet>(p[1]));                                \
  }                                                                            \
  void Cls::decode(VariableLengthData const& inData) {                         \
    if (inData.size() < 2)                                                     \
      throw DecoderException(L"" #Cls L" decode: buffer too small");           \
    unsigned char const* p =                                                   \
        static_cast<unsigned char const*>(inData.data());                      \
    if (std::string(#Endian) == "be") {                                        \
      _impl->value.first = static_cast<Octet>(p[0]);                           \
      _impl->value.second = static_cast<Octet>(p[1]);                          \
    } else {                                                                   \
      _impl->value.first = static_cast<Octet>(p[1]);                           \
      _impl->value.second = static_cast<Octet>(p[0]);                          \
    }                                                                          \
  }                                                                            \
  size_t Cls::decodeFrom(std::vector<Octet> const& buffer, size_t index) {     \
    if (buffer.size() < index + 2)                                             \
      throw DecoderException(L"" #Cls L" decodeFrom: buffer too small");       \
    if (std::string(#Endian) == "be") {                                        \
      _impl->value.first = buffer[index];                                      \
      _impl->value.second = buffer[index + 1];                                 \
    } else {                                                                   \
      _impl->value.first = buffer[index + 1];                                  \
      _impl->value.second = buffer[index];                                     \
    }                                                                          \
    return index + 2;                                                          \
  }                                                                            \
  size_t Cls::getEncodedLength() const { return 2; }                           \
  unsigned int Cls::getOctetBoundary() const { return 2; }                     \
                                                                               \
  Integer64 Cls::hash() const {                                                \
    return (static_cast<Integer64>(                                            \
                static_cast<unsigned char>(_impl->value.first))                \
            << 8) |                                                            \
           static_cast<Integer64>(                                             \
               static_cast<unsigned char>(_impl->value.second));               \
  }                                                                            \
                                                                               \
  Cls& Cls::operator=(Cls const& rhs) {                                        \
    if (this != &rhs) *_impl = *rhs._impl;                                     \
    return *this;                                                              \
  }                                                                            \
  Cls& Cls::operator=(OctetPair const& rhs) {                                  \
    _impl->value = rhs;                                                        \
    return *this;                                                              \
  }                                                                            \
  Cls::operator OctetPair() const { return _impl->value; }                     \
  OctetPair Cls::get() const { return _impl->value; }                          \
  void Cls::set(OctetPair inData) { _impl->value = inData; }

DEFINE_OCTET_PAIR_IMPL(HLAoctetPairBE, be)
DEFINE_OCTET_PAIR_IMPL(HLAoctetPairLE, le)

// -----------------------------------------------------------------------
// HLAASCIIstring — HLA-varString: uint32BE length + N ASCII bytes.
// -----------------------------------------------------------------------

class HLAASCIIstringImplementation {
 public:
  std::string value;
  HLAASCIIstringImplementation() = default;
  explicit HLAASCIIstringImplementation(std::string v) : value(std::move(v)) {}
};

HLAASCIIstring::HLAASCIIstring() : _impl(new HLAASCIIstringImplementation()) {}
HLAASCIIstring::HLAASCIIstring(std::string const& v)
    : _impl(new HLAASCIIstringImplementation(v)) {}
HLAASCIIstring::HLAASCIIstring(std::string* v)
    : _impl(new HLAASCIIstringImplementation(v ? *v : std::string{})) {}
HLAASCIIstring::HLAASCIIstring(HLAASCIIstring const& rhs)
    : _impl(new HLAASCIIstringImplementation(*rhs._impl)) {}
HLAASCIIstring::~HLAASCIIstring() { delete _impl; }

rti1516e::auto_ptr<DataElement> HLAASCIIstring::clone() const {
  return rti1516e::auto_ptr<DataElement>(new HLAASCIIstring(*this));
}

VariableLengthData HLAASCIIstring::encode() const {
  std::vector<unsigned char> buf;
  buf.resize(4 + _impl->value.size());
  std::uint32_t n = static_cast<std::uint32_t>(_impl->value.size());
  copy_be<std::uint32_t>(n, buf.data());
  std::memcpy(buf.data() + 4, _impl->value.data(), _impl->value.size());
  return VariableLengthData(buf.data(), buf.size());
}
void HLAASCIIstring::encode(VariableLengthData& inData) const {
  VariableLengthData v = encode();
  inData.setData(v.data(), v.size());
}
void HLAASCIIstring::encodeInto(std::vector<Octet>& buffer) const {
  VariableLengthData v = encode();
  unsigned char const* p = static_cast<unsigned char const*>(v.data());
  for (size_t i = 0; i < v.size(); ++i)
    buffer.push_back(static_cast<Octet>(p[i]));
}
void HLAASCIIstring::decode(VariableLengthData const& inData) {
  if (inData.size() < 4)
    throw DecoderException(L"HLAASCIIstring decode: header truncated");
  unsigned char const* p = static_cast<unsigned char const*>(inData.data());
  std::uint32_t n = load_be<std::uint32_t>(p);
  if (inData.size() < 4 + n)
    throw DecoderException(L"HLAASCIIstring decode: payload truncated");
  _impl->value.assign(reinterpret_cast<char const*>(p + 4), n);
}
size_t HLAASCIIstring::decodeFrom(std::vector<Octet> const& buffer,
                                  size_t index) {
  if (buffer.size() < index + 4)
    throw DecoderException(L"HLAASCIIstring decodeFrom: header truncated");
  unsigned char hdr[4];
  for (size_t i = 0; i < 4; ++i)
    hdr[i] = static_cast<unsigned char>(buffer[index + i]);
  std::uint32_t n = load_be<std::uint32_t>(hdr);
  if (buffer.size() < index + 4 + n)
    throw DecoderException(L"HLAASCIIstring decodeFrom: payload truncated");
  _impl->value.assign(reinterpret_cast<char const*>(buffer.data() + index + 4),
                      n);
  return index + 4 + n;
}
size_t HLAASCIIstring::getEncodedLength() const {
  return 4 + _impl->value.size();
}
unsigned int HLAASCIIstring::getOctetBoundary() const { return 4; }

Integer64 HLAASCIIstring::hash() const {
  // Simple FNV-1a for a stable hash.
  std::uint64_t h = 1469598103934665603ULL;
  for (char c : _impl->value) {
    h ^= static_cast<std::uint64_t>(static_cast<unsigned char>(c));
    h *= 1099511628211ULL;
  }
  return static_cast<Integer64>(h);
}

HLAASCIIstring& HLAASCIIstring::operator=(HLAASCIIstring const& rhs) {
  if (this != &rhs) *_impl = *rhs._impl;
  return *this;
}
HLAASCIIstring& HLAASCIIstring::operator=(std::string const& rhs) {
  _impl->value = rhs;
  return *this;
}
HLAASCIIstring::operator std::string() const { return _impl->value; }
std::string HLAASCIIstring::get() const { return _impl->value; }
void HLAASCIIstring::set(std::string inData) { _impl->value = std::move(inData); }

// -----------------------------------------------------------------------
// HLAunicodeString — HLA-varString of uint16BE code units (UTF-16 BE).
// -----------------------------------------------------------------------

class HLAunicodeStringImplementation {
 public:
  std::wstring value;
  HLAunicodeStringImplementation() = default;
  explicit HLAunicodeStringImplementation(std::wstring v)
      : value(std::move(v)) {}
};

HLAunicodeString::HLAunicodeString()
    : _impl(new HLAunicodeStringImplementation()) {}
HLAunicodeString::HLAunicodeString(std::wstring const& v)
    : _impl(new HLAunicodeStringImplementation(v)) {}
HLAunicodeString::HLAunicodeString(std::wstring* v)
    : _impl(new HLAunicodeStringImplementation(v ? *v : std::wstring{})) {}
HLAunicodeString::HLAunicodeString(HLAunicodeString const& rhs)
    : _impl(new HLAunicodeStringImplementation(*rhs._impl)) {}
HLAunicodeString::~HLAunicodeString() { delete _impl; }

rti1516e::auto_ptr<DataElement> HLAunicodeString::clone() const {
  return rti1516e::auto_ptr<DataElement>(new HLAunicodeString(*this));
}

VariableLengthData HLAunicodeString::encode() const {
  std::vector<unsigned char> buf;
  std::uint32_t n = static_cast<std::uint32_t>(_impl->value.size());
  buf.resize(4 + 2 * n);
  copy_be<std::uint32_t>(n, buf.data());
  for (std::uint32_t i = 0; i < n; ++i) {
    std::uint16_t code = static_cast<std::uint16_t>(_impl->value[i] & 0xFFFF);
    copy_be<std::uint16_t>(code, buf.data() + 4 + 2 * i);
  }
  return VariableLengthData(buf.data(), buf.size());
}
void HLAunicodeString::encode(VariableLengthData& inData) const {
  VariableLengthData v = encode();
  inData.setData(v.data(), v.size());
}
void HLAunicodeString::encodeInto(std::vector<Octet>& buffer) const {
  VariableLengthData v = encode();
  unsigned char const* p = static_cast<unsigned char const*>(v.data());
  for (size_t i = 0; i < v.size(); ++i)
    buffer.push_back(static_cast<Octet>(p[i]));
}
void HLAunicodeString::decode(VariableLengthData const& inData) {
  if (inData.size() < 4)
    throw DecoderException(L"HLAunicodeString decode: header truncated");
  unsigned char const* p = static_cast<unsigned char const*>(inData.data());
  std::uint32_t n = load_be<std::uint32_t>(p);
  if (inData.size() < 4 + 2 * n)
    throw DecoderException(L"HLAunicodeString decode: payload truncated");
  _impl->value.clear();
  _impl->value.reserve(n);
  for (std::uint32_t i = 0; i < n; ++i) {
    std::uint16_t code = load_be<std::uint16_t>(p + 4 + 2 * i);
    _impl->value.push_back(static_cast<wchar_t>(code));
  }
}
size_t HLAunicodeString::decodeFrom(std::vector<Octet> const& buffer,
                                    size_t index) {
  if (buffer.size() < index + 4)
    throw DecoderException(L"HLAunicodeString decodeFrom: header truncated");
  unsigned char hdr[4];
  for (size_t i = 0; i < 4; ++i)
    hdr[i] = static_cast<unsigned char>(buffer[index + i]);
  std::uint32_t n = load_be<std::uint32_t>(hdr);
  if (buffer.size() < index + 4 + 2 * n)
    throw DecoderException(L"HLAunicodeString decodeFrom: payload truncated");
  _impl->value.clear();
  _impl->value.reserve(n);
  for (std::uint32_t i = 0; i < n; ++i) {
    unsigned char code_buf[2];
    code_buf[0] = static_cast<unsigned char>(buffer[index + 4 + 2 * i]);
    code_buf[1] = static_cast<unsigned char>(buffer[index + 4 + 2 * i + 1]);
    _impl->value.push_back(
        static_cast<wchar_t>(load_be<std::uint16_t>(code_buf)));
  }
  return index + 4 + 2 * n;
}
size_t HLAunicodeString::getEncodedLength() const {
  return 4 + 2 * _impl->value.size();
}
unsigned int HLAunicodeString::getOctetBoundary() const { return 4; }

Integer64 HLAunicodeString::hash() const {
  std::uint64_t h = 1469598103934665603ULL;
  for (wchar_t c : _impl->value) {
    h ^= static_cast<std::uint64_t>(c);
    h *= 1099511628211ULL;
  }
  return static_cast<Integer64>(h);
}

HLAunicodeString& HLAunicodeString::operator=(HLAunicodeString const& rhs) {
  if (this != &rhs) *_impl = *rhs._impl;
  return *this;
}
HLAunicodeString& HLAunicodeString::operator=(std::wstring const& rhs) {
  _impl->value = rhs;
  return *this;
}
HLAunicodeString::operator std::wstring() const { return _impl->value; }
std::wstring HLAunicodeString::get() const { return _impl->value; }
void HLAunicodeString::set(std::wstring inData) { _impl->value = std::move(inData); }

}  // namespace rti1516e
