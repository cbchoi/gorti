// IEEE 1516.2-2010 Annex B — HLAfixedArray composite encoder.
//
// gorti M34 (Agent AF). Catalogue 14.4 / FR-DLC-7.
//
// Wire format (spec §B): elements packed contiguously, each element padded
// to its own octet boundary. In the fixed-array case, no length prefix is
// emitted — the reader already knows the cardinality from the prototype +
// length passed at ctor. The array's own octet boundary equals the
// prototype's octet boundary (elements are homogeneous by construction).
//
// Storage: vector<unique_ptr<DataElement>>, one per index, populated lazily
// from clone(prototype) on first access.

#include <RTI/encoding/HLAfixedArray.h>
#include <RTI/encoding/EncodingExceptions.h>
#include <RTI/VariableLengthData.h>

#include <memory>
#include <vector>

namespace rti1516e {

namespace {

// Grow buffer to next multiple of `boundary` with zero-fill.
static void pad_to(std::vector<Octet>& buf, unsigned int boundary) {
  if (boundary <= 1) return;
  while (buf.size() % boundary != 0) buf.push_back(Octet{0});
}

// Advance index in `buf` to next multiple of `boundary`.
static size_t align_index(size_t index, unsigned int boundary) {
  if (boundary <= 1) return index;
  return ((index + boundary - 1) / boundary) * boundary;
}

}  // namespace

class HLAfixedArrayImplementation {
 public:
  rti1516e::auto_ptr<DataElement> prototype;
  size_t length{0};
  std::vector<rti1516e::auto_ptr<DataElement>> elements;

  HLAfixedArrayImplementation(DataElement const& proto, size_t len)
      : prototype(proto.clone()), length(len) {
    elements.resize(len);
  }

  HLAfixedArrayImplementation(HLAfixedArrayImplementation const& rhs)
      : prototype(rhs.prototype->clone()), length(rhs.length) {
    elements.resize(length);
    for (size_t i = 0; i < length; ++i) {
      if (rhs.elements[i]) elements[i] = rhs.elements[i]->clone();
    }
  }

  DataElement& ensure(size_t i) {
    if (!elements[i]) elements[i] = prototype->clone();
    return *elements[i];
  }
  DataElement const& view(size_t i) const {
    if (elements[i]) return *elements[i];
    // Lazily clone into a mutable slot — needed because get() returns a
    // reference, and callers may inspect elements they haven't set yet.
    // const_cast on the wrapping vector is safe: HLAfixedArray API allows
    // this mutation as a spec-mandated side effect (see Pitch header:109).
    auto& mut = const_cast<HLAfixedArrayImplementation&>(*this);
    mut.elements[i] = prototype->clone();
    return *mut.elements[i];
  }
};

HLAfixedArray::HLAfixedArray(DataElement const& protoType, size_t length)
    : _impl(new HLAfixedArrayImplementation(protoType, length)) {}

HLAfixedArray::HLAfixedArray(HLAfixedArray const& rhs)
    : _impl(new HLAfixedArrayImplementation(*rhs._impl)) {}

HLAfixedArray::~HLAfixedArray() { delete _impl; }

rti1516e::auto_ptr<DataElement> HLAfixedArray::clone() const {
  return rti1516e::auto_ptr<DataElement>(new HLAfixedArray(*this));
}

VariableLengthData HLAfixedArray::encode() const {
  std::vector<Octet> buf;
  encodeInto(buf);
  return VariableLengthData(buf.data(), buf.size());
}

void HLAfixedArray::encode(VariableLengthData& inData) const {
  std::vector<Octet> buf;
  encodeInto(buf);
  inData.setData(buf.data(), buf.size());
}

void HLAfixedArray::encodeInto(std::vector<Octet>& buffer) const {
  unsigned int const elt_boundary = _impl->prototype->getOctetBoundary();
  for (size_t i = 0; i < _impl->length; ++i) {
    pad_to(buffer, elt_boundary);
    _impl->ensure(i).encodeInto(buffer);
  }
}

void HLAfixedArray::decode(VariableLengthData const& inData) {
  std::vector<Octet> buf(inData.size());
  auto const* src = static_cast<unsigned char const*>(inData.data());
  for (size_t i = 0; i < inData.size(); ++i) {
    buf[i] = static_cast<Octet>(src[i]);
  }
  decodeFrom(buf, 0);
}

size_t HLAfixedArray::decodeFrom(std::vector<Octet> const& buffer,
                                 size_t index) {
  unsigned int const elt_boundary = _impl->prototype->getOctetBoundary();
  for (size_t i = 0; i < _impl->length; ++i) {
    index = align_index(index, elt_boundary);
    if (!_impl->elements[i]) _impl->elements[i] = _impl->prototype->clone();
    index = _impl->elements[i]->decodeFrom(buffer, index);
  }
  return index;
}

size_t HLAfixedArray::getEncodedLength() const {
  std::vector<Octet> buf;
  encodeInto(buf);
  return buf.size();
}

unsigned int HLAfixedArray::getOctetBoundary() const {
  return _impl->prototype->getOctetBoundary();
}

bool HLAfixedArray::isSameTypeAs(DataElement const& inData) const {
  auto const* other = dynamic_cast<HLAfixedArray const*>(&inData);
  if (other == nullptr) return false;
  return _impl->length == other->_impl->length &&
         _impl->prototype->isSameTypeAs(*other->_impl->prototype);
}

bool HLAfixedArray::hasPrototypeSameTypeAs(
    DataElement const& dataElement) const {
  return _impl->prototype->isSameTypeAs(dataElement);
}

size_t HLAfixedArray::size() const { return _impl->length; }

void HLAfixedArray::set(size_t index, DataElement const& dataElement) {
  if (index >= _impl->length)
    throw EncoderException(L"HLAfixedArray::set index out of range");
  if (!_impl->prototype->isSameTypeAs(dataElement))
    throw EncoderException(L"HLAfixedArray::set element type mismatch");
  _impl->elements[index] = dataElement.clone();
}

DataElement const& HLAfixedArray::get(size_t index) const {
  if (index >= _impl->length)
    throw EncoderException(L"HLAfixedArray::get index out of range");
  return _impl->view(index);
}

DataElement const& HLAfixedArray::operator[](size_t index) const {
  return get(index);
}

}  // namespace rti1516e
