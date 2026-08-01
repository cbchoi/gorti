// IEEE 1516.2-2010 Annex B — HLAvariantRecord composite encoder.
//
// gorti M34. Catalogue 14.7 / FR-DLC-7.
//
// Wire format: [discriminant] [pad to alternative boundary] [alternative
// value]. Record boundary = max(discriminant boundary, all alternative
// boundaries). The active alternative is selected via setDiscriminant /
// setVariant. On decode, the discriminant is read first, then used as a
// key into the variant map to select which prototype to decode into.
//
// Discriminant equality: gorti keys the variant map by the discriminant's
// hash() output. That works for all leaf types in BasicDataElements (each
// hash() body is deterministic in value) and is stable across encode/decode
// round-trips since decode restores value → same hash.

#include <RTI/encoding/HLAvariantRecord.h>
#include <RTI/encoding/EncodingExceptions.h>
#include <RTI/VariableLengthData.h>

#include <memory>
#include <unordered_map>
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

class HLAvariantRecordImplementation {
 public:
  rti1516e::auto_ptr<DataElement> discriminantPrototype;
  rti1516e::auto_ptr<DataElement> activeDiscriminant;
  // Keyed by discriminant hash — see file-level comment.
  std::unordered_map<Integer64, rti1516e::auto_ptr<DataElement>> variants;
  // Preserve insertion order for stable iteration (not spec-mandated but
  // useful for debugging).
  std::vector<Integer64> keys;

  explicit HLAvariantRecordImplementation(DataElement const& proto)
      : discriminantPrototype(proto.clone()) {}

  HLAvariantRecordImplementation(HLAvariantRecordImplementation const& rhs)
      : discriminantPrototype(rhs.discriminantPrototype->clone()),
        activeDiscriminant(rhs.activeDiscriminant
                               ? rhs.activeDiscriminant->clone()
                               : rti1516e::auto_ptr<DataElement>{}),
        keys(rhs.keys) {
    for (auto const& kv : rhs.variants) {
      variants[kv.first] = kv.second->clone();
    }
  }
};

HLAvariantRecord::HLAvariantRecord(DataElement const& discriminantPrototype)
    : _impl(new HLAvariantRecordImplementation(discriminantPrototype)) {}

HLAvariantRecord::HLAvariantRecord(HLAvariantRecord const& rhs)
    : _impl(new HLAvariantRecordImplementation(*rhs._impl)) {}

HLAvariantRecord::~HLAvariantRecord() { delete _impl; }

rti1516e::auto_ptr<DataElement> HLAvariantRecord::clone() const {
  return rti1516e::auto_ptr<DataElement>(new HLAvariantRecord(*this));
}

VariableLengthData HLAvariantRecord::encode() const {
  std::vector<Octet> buf;
  encodeInto(buf);
  return VariableLengthData(buf.data(), buf.size());
}

void HLAvariantRecord::encode(VariableLengthData& inData) const {
  std::vector<Octet> buf;
  encodeInto(buf);
  inData.setData(buf.data(), buf.size());
}

void HLAvariantRecord::encodeInto(std::vector<Octet>& buffer) const {
  if (!_impl->activeDiscriminant)
    throw EncoderException(L"HLAvariantRecord::encode: no active discriminant");
  Integer64 key = _impl->activeDiscriminant->hash();
  auto it = _impl->variants.find(key);
  if (it == _impl->variants.end())
    throw EncoderException(L"HLAvariantRecord::encode: no variant for disc");
  _impl->activeDiscriminant->encodeInto(buffer);
  pad_to(buffer, it->second->getOctetBoundary());
  it->second->encodeInto(buffer);
}

void HLAvariantRecord::decode(VariableLengthData const& inData) {
  std::vector<Octet> buf(inData.size());
  auto const* src = static_cast<unsigned char const*>(inData.data());
  for (size_t i = 0; i < inData.size(); ++i) buf[i] = static_cast<Octet>(src[i]);
  decodeFrom(buf, 0);
}

size_t HLAvariantRecord::decodeFrom(std::vector<Octet> const& buffer,
                                    size_t index) {
  auto disc = _impl->discriminantPrototype->clone();
  index = disc->decodeFrom(buffer, index);
  Integer64 key = disc->hash();
  auto it = _impl->variants.find(key);
  if (it == _impl->variants.end())
    throw DecoderException(L"HLAvariantRecord::decode: unknown discriminant");
  _impl->activeDiscriminant = std::move(disc);
  index = align_index(index, it->second->getOctetBoundary());
  return it->second->decodeFrom(buffer, index);
}

size_t HLAvariantRecord::getEncodedLength() const {
  std::vector<Octet> buf;
  encodeInto(buf);
  return buf.size();
}

unsigned int HLAvariantRecord::getOctetBoundary() const {
  unsigned int max_b = _impl->discriminantPrototype->getOctetBoundary();
  for (auto const& kv : _impl->variants) {
    unsigned int b = kv.second->getOctetBoundary();
    if (b > max_b) max_b = b;
  }
  return max_b;
}

void HLAvariantRecord::addVariant(DataElement const& discriminant,
                                  DataElement const& valuePrototype) {
  if (!_impl->discriminantPrototype->isSameTypeAs(discriminant))
    throw EncoderException(L"HLAvariantRecord::addVariant disc type mismatch");
  Integer64 key = discriminant.hash();
  if (_impl->variants.find(key) == _impl->variants.end()) {
    _impl->keys.push_back(key);
  }
  _impl->variants[key] = valuePrototype.clone();
}

void HLAvariantRecord::addVariantPointer(DataElement const& discriminant,
                                         DataElement* valuePtr) {
  if (valuePtr == nullptr)
    throw EncoderException(L"HLAvariantRecord::addVariantPointer null value");
  if (!_impl->discriminantPrototype->isSameTypeAs(discriminant))
    throw EncoderException(L"HLAvariantRecord::addVariantPointer disc mismatch");
  Integer64 key = discriminant.hash();
  if (_impl->variants.find(key) == _impl->variants.end()) {
    _impl->keys.push_back(key);
  }
  _impl->variants[key] = rti1516e::auto_ptr<DataElement>(valuePtr);
}

void HLAvariantRecord::setDiscriminant(DataElement const& discriminant) {
  if (!_impl->discriminantPrototype->isSameTypeAs(discriminant))
    throw EncoderException(L"HLAvariantRecord::setDiscriminant type mismatch");
  Integer64 key = discriminant.hash();
  if (_impl->variants.find(key) == _impl->variants.end())
    throw EncoderException(L"HLAvariantRecord::setDiscriminant unknown");
  _impl->activeDiscriminant = discriminant.clone();
}

void HLAvariantRecord::setVariant(DataElement const& discriminant,
                                  DataElement const& value) {
  Integer64 key = discriminant.hash();
  auto it = _impl->variants.find(key);
  if (it == _impl->variants.end())
    throw EncoderException(L"HLAvariantRecord::setVariant unknown disc");
  if (!it->second->isSameTypeAs(value))
    throw EncoderException(L"HLAvariantRecord::setVariant type mismatch");
  it->second = value.clone();
}

void HLAvariantRecord::setVariantPointer(DataElement const& discriminant,
                                         DataElement* valuePtr) {
  if (valuePtr == nullptr)
    throw EncoderException(L"HLAvariantRecord::setVariantPointer null");
  Integer64 key = discriminant.hash();
  auto it = _impl->variants.find(key);
  if (it == _impl->variants.end())
    throw EncoderException(L"HLAvariantRecord::setVariantPointer unknown");
  if (!it->second->isSameTypeAs(*valuePtr))
    throw EncoderException(L"HLAvariantRecord::setVariantPointer type");
  it->second = rti1516e::auto_ptr<DataElement>(valuePtr);
}

DataElement const& HLAvariantRecord::getDiscriminant() const {
  if (!_impl->activeDiscriminant)
    throw EncoderException(L"HLAvariantRecord::getDiscriminant unset");
  return *_impl->activeDiscriminant;
}

DataElement const& HLAvariantRecord::getVariant() const {
  if (!_impl->activeDiscriminant)
    throw EncoderException(L"HLAvariantRecord::getVariant unset");
  Integer64 key = _impl->activeDiscriminant->hash();
  auto it = _impl->variants.find(key);
  if (it == _impl->variants.end())
    throw EncoderException(L"HLAvariantRecord::getVariant no mapping");
  return *it->second;
}

}  // namespace rti1516e
