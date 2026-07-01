// IEEE 1516.2-2010 Annex B — HLAvariableArray composite encoder.
//
// gorti M34 (Agent AF). Catalogue 14.5 / FR-DLC-7.
//
// Wire format: [4-byte BE cardinality] [padding to element boundary]
// [element_0] [pad_1] [element_1] ... The 4-byte prefix's own boundary is
// 4; the array's overall octet boundary is max(4, prototype boundary) so
// downstream padding aligns correctly in composite contexts.
//
// The pad between the length prefix and element_0 lets the elements sit at
// their natural boundary even when it exceeds 4 (float64 case). Golden
// vector `variable-array-float64-3` in tests/conformance/encoding_vectors.json
// exercises this pad: prefix (0x00000003) + 4-byte pad (0x00000000) + 3 doubles.

#include <RTI/encoding/HLAvariableArray.h>
#include <RTI/encoding/EncodingExceptions.h>
#include <RTI/VariableLengthData.h>

#include <cstdint>
#include <cstring>
#include <memory>
#include <vector>

namespace rti1516e {

namespace {

static void pad_to(std::vector<Octet>& buf, unsigned int boundary) {
  if (boundary <= 1) return;
  while (buf.size() % boundary != 0) buf.push_back(Octet{0});
}

static size_t align_index(size_t index, unsigned int boundary) {
  if (boundary <= 1) return index;
  return ((index + boundary - 1) / boundary) * boundary;
}

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

class HLAvariableArrayImplementation {
 public:
  rti1516e::auto_ptr<DataElement> prototype;
  std::vector<rti1516e::auto_ptr<DataElement>> elements;

  explicit HLAvariableArrayImplementation(DataElement const& proto)
      : prototype(proto.clone()) {}

  HLAvariableArrayImplementation(HLAvariableArrayImplementation const& rhs)
      : prototype(rhs.prototype->clone()) {
    elements.reserve(rhs.elements.size());
    for (auto const& e : rhs.elements) {
      elements.push_back(e ? e->clone() : rhs.prototype->clone());
    }
  }
};

HLAvariableArray::HLAvariableArray(DataElement const& protoType)
    : _impl(new HLAvariableArrayImplementation(protoType)) {}

HLAvariableArray::HLAvariableArray(HLAvariableArray const& rhs)
    : _impl(new HLAvariableArrayImplementation(*rhs._impl)) {}

HLAvariableArray::~HLAvariableArray() { delete _impl; }

rti1516e::auto_ptr<DataElement> HLAvariableArray::clone() const {
  return rti1516e::auto_ptr<DataElement>(new HLAvariableArray(*this));
}

VariableLengthData HLAvariableArray::encode() const {
  std::vector<Octet> buf;
  encodeInto(buf);
  return VariableLengthData(buf.data(), buf.size());
}

void HLAvariableArray::encode(VariableLengthData& inData) const {
  std::vector<Octet> buf;
  encodeInto(buf);
  inData.setData(buf.data(), buf.size());
}

void HLAvariableArray::encodeInto(std::vector<Octet>& buffer) const {
  unsigned int const elt_boundary = _impl->prototype->getOctetBoundary();
  append_u32_be(buffer, static_cast<std::uint32_t>(_impl->elements.size()));
  // Pad to element boundary before first element (spec §B: length prefix has
  // boundary 4; if element boundary is larger, insert padding).
  pad_to(buffer, elt_boundary);
  for (auto const& e : _impl->elements) {
    pad_to(buffer, elt_boundary);
    e->encodeInto(buffer);
  }
}

void HLAvariableArray::decode(VariableLengthData const& inData) {
  std::vector<Octet> buf(inData.size());
  auto const* src = static_cast<unsigned char const*>(inData.data());
  for (size_t i = 0; i < inData.size(); ++i) buf[i] = static_cast<Octet>(src[i]);
  decodeFrom(buf, 0);
}

size_t HLAvariableArray::decodeFrom(std::vector<Octet> const& buffer,
                                    size_t index) {
  if (buffer.size() < index + 4)
    throw DecoderException(L"HLAvariableArray decodeFrom: header truncated");
  std::uint32_t n = read_u32_be(buffer, index);
  index += 4;
  unsigned int const elt_boundary = _impl->prototype->getOctetBoundary();
  index = align_index(index, elt_boundary);
  _impl->elements.clear();
  _impl->elements.reserve(n);
  for (std::uint32_t i = 0; i < n; ++i) {
    index = align_index(index, elt_boundary);
    auto elem = _impl->prototype->clone();
    index = elem->decodeFrom(buffer, index);
    _impl->elements.push_back(std::move(elem));
  }
  return index;
}

size_t HLAvariableArray::getEncodedLength() const {
  std::vector<Octet> buf;
  encodeInto(buf);
  return buf.size();
}

unsigned int HLAvariableArray::getOctetBoundary() const {
  unsigned int const eb = _impl->prototype->getOctetBoundary();
  return eb > 4 ? eb : 4;
}

bool HLAvariableArray::isSameTypeAs(DataElement const& inData) const {
  auto const* other = dynamic_cast<HLAvariableArray const*>(&inData);
  if (other == nullptr) return false;
  return _impl->prototype->isSameTypeAs(*other->_impl->prototype);
}

bool HLAvariableArray::hasPrototypeSameTypeAs(
    DataElement const& dataElement) const {
  return _impl->prototype->isSameTypeAs(dataElement);
}

size_t HLAvariableArray::size() const { return _impl->elements.size(); }

void HLAvariableArray::addElement(DataElement const& dataElement) {
  if (!_impl->prototype->isSameTypeAs(dataElement))
    throw EncoderException(L"HLAvariableArray::addElement type mismatch");
  _impl->elements.push_back(dataElement.clone());
}

void HLAvariableArray::addElementPointer(DataElement* dataElement) {
  if (dataElement == nullptr)
    throw EncoderException(L"HLAvariableArray::addElementPointer null");
  if (!_impl->prototype->isSameTypeAs(*dataElement))
    throw EncoderException(L"HLAvariableArray::addElementPointer type mismatch");
  // Take ownership per Pitch header:99-103.
  _impl->elements.emplace_back(dataElement);
}

void HLAvariableArray::set(size_t index, DataElement const& dataElement) {
  if (index >= _impl->elements.size())
    throw EncoderException(L"HLAvariableArray::set index out of range");
  if (!_impl->prototype->isSameTypeAs(dataElement))
    throw EncoderException(L"HLAvariableArray::set type mismatch");
  _impl->elements[index] = dataElement.clone();
}

DataElement const& HLAvariableArray::get(size_t index) const {
  if (index >= _impl->elements.size())
    throw EncoderException(L"HLAvariableArray::get index out of range");
  return *_impl->elements[index];
}

DataElement const& HLAvariableArray::operator[](size_t index) const {
  return get(index);
}

}  // namespace rti1516e
