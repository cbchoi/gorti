// IEEE 1516.2-2010 Annex B — HLAfixedRecord composite encoder.
//
// gorti M34 (Agent AF). Catalogue 14.6 / FR-DLC-7.
//
// Wire format: fields concatenated in appendElement order, each padded to
// its own octet boundary. Record's octet boundary = max(field boundaries),
// so a record embedded in a larger composite pads correctly. Nested
// records reset the padding origin (see vector
// `fixed-record-nested-octet-record-octet-float64`).

#include <RTI/encoding/HLAfixedRecord.h>
#include <RTI/encoding/EncodingExceptions.h>
#include <RTI/VariableLengthData.h>

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

}  // namespace

class HLAfixedRecordImplementation {
 public:
  std::vector<rti1516e::auto_ptr<DataElement>> fields;

  HLAfixedRecordImplementation() = default;

  HLAfixedRecordImplementation(HLAfixedRecordImplementation const& rhs) {
    fields.reserve(rhs.fields.size());
    for (auto const& f : rhs.fields) fields.push_back(f->clone());
  }
};

HLAfixedRecord::HLAfixedRecord() : _impl(new HLAfixedRecordImplementation()) {}

HLAfixedRecord::HLAfixedRecord(HLAfixedRecord const& rhs)
    : _impl(new HLAfixedRecordImplementation(*rhs._impl)) {}

HLAfixedRecord::~HLAfixedRecord() { delete _impl; }

rti1516e::auto_ptr<DataElement> HLAfixedRecord::clone() const {
  return rti1516e::auto_ptr<DataElement>(new HLAfixedRecord(*this));
}

VariableLengthData HLAfixedRecord::encode() const {
  std::vector<Octet> buf;
  encodeInto(buf);
  return VariableLengthData(buf.data(), buf.size());
}

void HLAfixedRecord::encode(VariableLengthData& inData) const {
  std::vector<Octet> buf;
  encodeInto(buf);
  inData.setData(buf.data(), buf.size());
}

void HLAfixedRecord::encodeInto(std::vector<Octet>& buffer) const {
  for (auto const& f : _impl->fields) {
    pad_to(buffer, f->getOctetBoundary());
    f->encodeInto(buffer);
  }
}

void HLAfixedRecord::decode(VariableLengthData const& inData) {
  std::vector<Octet> buf(inData.size());
  auto const* src = static_cast<unsigned char const*>(inData.data());
  for (size_t i = 0; i < inData.size(); ++i) buf[i] = static_cast<Octet>(src[i]);
  decodeFrom(buf, 0);
}

size_t HLAfixedRecord::decodeFrom(std::vector<Octet> const& buffer,
                                  size_t index) {
  for (auto& f : _impl->fields) {
    index = align_index(index, f->getOctetBoundary());
    index = f->decodeFrom(buffer, index);
  }
  return index;
}

size_t HLAfixedRecord::getEncodedLength() const {
  std::vector<Octet> buf;
  encodeInto(buf);
  return buf.size();
}

unsigned int HLAfixedRecord::getOctetBoundary() const {
  unsigned int max_b = 1;
  for (auto const& f : _impl->fields) {
    unsigned int b = f->getOctetBoundary();
    if (b > max_b) max_b = b;
  }
  return max_b;
}

bool HLAfixedRecord::hasElementSameTypeAs(size_t index,
                                          DataElement const& inData) const {
  if (index >= _impl->fields.size()) return false;
  return _impl->fields[index]->isSameTypeAs(inData);
}

size_t HLAfixedRecord::size() const { return _impl->fields.size(); }

void HLAfixedRecord::appendElement(DataElement const& dataElement) {
  _impl->fields.push_back(dataElement.clone());
}

void HLAfixedRecord::appendElementPointer(DataElement* dataElement) {
  if (dataElement == nullptr)
    throw EncoderException(L"HLAfixedRecord::appendElementPointer null");
  _impl->fields.emplace_back(dataElement);
}

void HLAfixedRecord::set(size_t index, DataElement const& dataElement) {
  if (index >= _impl->fields.size())
    throw EncoderException(L"HLAfixedRecord::set index out of range");
  if (!_impl->fields[index]->isSameTypeAs(dataElement))
    throw EncoderException(L"HLAfixedRecord::set type mismatch");
  _impl->fields[index] = dataElement.clone();
}

void HLAfixedRecord::setElementPointer(size_t index, DataElement* dataElement) {
  if (index >= _impl->fields.size())
    throw EncoderException(L"HLAfixedRecord::setElementPointer index");
  if (dataElement == nullptr)
    throw EncoderException(L"HLAfixedRecord::setElementPointer null");
  if (!_impl->fields[index]->isSameTypeAs(*dataElement))
    throw EncoderException(L"HLAfixedRecord::setElementPointer type mismatch");
  _impl->fields[index] = rti1516e::auto_ptr<DataElement>(dataElement);
}

DataElement const& HLAfixedRecord::get(size_t index) const {
  if (index >= _impl->fields.size())
    throw EncoderException(L"HLAfixedRecord::get index out of range");
  return *_impl->fields[index];
}

DataElement const& HLAfixedRecord::operator[](size_t index) const {
  return get(index);
}

}  // namespace rti1516e
