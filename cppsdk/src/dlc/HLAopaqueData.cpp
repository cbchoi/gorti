// IEEE 1516.2-2010 Annex B — HLAopaqueData composite encoder.
//
// gorti M34. Catalogue 14.8 / FR-DLC-7.
//
// Wire format: [4-byte BE length] [N raw bytes]. Octet boundary = 4 (from
// the length prefix). See golden vectors `opaque-empty`,
// `opaque-deadbeef`, `opaque-three-bytes`.
//
// Storage: internal byte vector; ownership always internal (setDataPointer
// is a spec convenience for federate code that wants zero-copy handoff, but
// gorti's impl copies to internal storage on every setter to keep lifetime
// invariants simple).

#include <RTI/encoding/HLAopaqueData.h>
#include <RTI/encoding/EncodingExceptions.h>
#include <RTI/VariableLengthData.h>

#include <cstdint>
#include <cstring>
#include <vector>

namespace rti1516e {

namespace {

static void append_u32_be(std::vector<Octet>& buf, std::uint32_t v) {
  buf.push_back(static_cast<Octet>((v >> 24) & 0xff));
  buf.push_back(static_cast<Octet>((v >> 16) & 0xff));
  buf.push_back(static_cast<Octet>((v >> 8) & 0xff));
  buf.push_back(static_cast<Octet>(v & 0xff));
}

static std::uint32_t read_u32_be(std::vector<Octet> const& buf, size_t index) {
  return (static_cast<std::uint32_t>(static_cast<unsigned char>(buf[index])) << 24) |
         (static_cast<std::uint32_t>(static_cast<unsigned char>(buf[index + 1])) << 16) |
         (static_cast<std::uint32_t>(static_cast<unsigned char>(buf[index + 2])) << 8) |
         (static_cast<std::uint32_t>(static_cast<unsigned char>(buf[index + 3])));
}

}  // namespace

class HLAopaqueDataImplementation {
 public:
  std::vector<Octet> data;

  HLAopaqueDataImplementation() = default;

  HLAopaqueDataImplementation(Octet const* p, size_t n) {
    if (p != nullptr && n > 0) data.assign(p, p + n);
  }

  HLAopaqueDataImplementation(HLAopaqueDataImplementation const& rhs) = default;
};

HLAopaqueData::HLAopaqueData() : _impl(new HLAopaqueDataImplementation()) {}

HLAopaqueData::HLAopaqueData(Octet const* inData, size_t dataSize)
    : _impl(new HLAopaqueDataImplementation(inData, dataSize)) {}

HLAopaqueData::HLAopaqueData(Octet** inData, size_t bufferSize, size_t dataSize)
    : _impl(new HLAopaqueDataImplementation()) {
  // Spec §B: `Octet**` triple-pointer form takes ownership semantics. gorti
  // treats it as a copy — the federate keeps its buffer valid, we take a
  // snapshot. `bufferSize` is the underlying buffer capacity; `dataSize` is
  // the payload extent.
  (void)bufferSize;  // opaque buffer capacity not tracked separately here.
  if (inData != nullptr && *inData != nullptr && dataSize > 0) {
    _impl->data.assign(*inData, *inData + dataSize);
  }
}

HLAopaqueData::HLAopaqueData(HLAopaqueData const& rhs)
    : _impl(new HLAopaqueDataImplementation(*rhs._impl)) {}

HLAopaqueData::~HLAopaqueData() { delete _impl; }

rti1516e::auto_ptr<DataElement> HLAopaqueData::clone() const {
  return rti1516e::auto_ptr<DataElement>(new HLAopaqueData(*this));
}

VariableLengthData HLAopaqueData::encode() const {
  std::vector<Octet> buf;
  encodeInto(buf);
  return VariableLengthData(buf.data(), buf.size());
}

void HLAopaqueData::encode(VariableLengthData& inData) const {
  std::vector<Octet> buf;
  encodeInto(buf);
  inData.setData(buf.data(), buf.size());
}

void HLAopaqueData::encodeInto(std::vector<Octet>& buffer) const {
  append_u32_be(buffer, static_cast<std::uint32_t>(_impl->data.size()));
  for (Octet o : _impl->data) buffer.push_back(o);
}

void HLAopaqueData::decode(VariableLengthData const& inData) {
  std::vector<Octet> buf(inData.size());
  auto const* src = static_cast<unsigned char const*>(inData.data());
  for (size_t i = 0; i < inData.size(); ++i) buf[i] = static_cast<Octet>(src[i]);
  decodeFrom(buf, 0);
}

size_t HLAopaqueData::decodeFrom(std::vector<Octet> const& buffer,
                                 size_t index) {
  if (buffer.size() < index + 4)
    throw DecoderException(L"HLAopaqueData decodeFrom: header truncated");
  std::uint32_t n = read_u32_be(buffer, index);
  index += 4;
  if (buffer.size() < index + n)
    throw DecoderException(L"HLAopaqueData decodeFrom: payload truncated");
  _impl->data.assign(buffer.begin() + index, buffer.begin() + index + n);
  return index + n;
}

size_t HLAopaqueData::getEncodedLength() const {
  return 4 + _impl->data.size();
}

unsigned int HLAopaqueData::getOctetBoundary() const { return 4; }

size_t HLAopaqueData::bufferLength() const { return _impl->data.size(); }

size_t HLAopaqueData::dataLength() const { return _impl->data.size(); }

void HLAopaqueData::setDataPointer(Octet** inData, size_t bufferSize,
                                   size_t dataSize) {
  (void)bufferSize;
  if (inData == nullptr || *inData == nullptr || dataSize == 0) {
    _impl->data.clear();
    return;
  }
  _impl->data.assign(*inData, *inData + dataSize);
}

void HLAopaqueData::set(Octet const* inData, size_t dataSize) {
  if (inData == nullptr || dataSize == 0) {
    _impl->data.clear();
    return;
  }
  _impl->data.assign(inData, inData + dataSize);
}

Octet const* HLAopaqueData::get() const {
  return _impl->data.empty() ? nullptr : _impl->data.data();
}

HLAopaqueData::operator Octet const*() const { return get(); }

}  // namespace rti1516e
