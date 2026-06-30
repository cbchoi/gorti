// IEEE 1516.2-2010 Annex B — RTI/encoding/DataElement.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Abstract base for encoded HLA data types (catalogue 14.1 / FR-DLC-7).
// `clone()` returns `rti1516e::auto_ptr<DataElement>` per the C++17
// resolution (catalogue 14.10).

#ifndef RTI_DataElement_h_
#define RTI_DataElement_h_

#include <RTI/SpecificConfig.h>
#include <RTI/encoding/EncodingConfig.h>
#include <RTI/encoding/EncodingExceptions.h>
#include <memory>
#include <vector>

namespace rti1516e {

class VariableLengthData;

class RTI_EXPORT DataElement {
 public:
  virtual ~DataElement() = 0;

  // Catalogue 14.10 — auto_ptr alias used per FR-DLC-2.
  virtual rti1516e::auto_ptr<DataElement> clone() const = 0;

  virtual VariableLengthData encode() const = 0;
  virtual void encode(VariableLengthData& inData) const = 0;
  virtual void encodeInto(std::vector<Octet>& buffer) const = 0;

  virtual void decode(VariableLengthData const& inData) = 0;
  virtual size_t decodeFrom(std::vector<Octet> const& buffer,
                            size_t index) = 0;

  virtual size_t getEncodedLength() const = 0;
  virtual unsigned int getOctetBoundary() const = 0;

  virtual bool isSameTypeAs(DataElement const& inData) const;
  virtual Integer64 hash() const;
};

}  // namespace rti1516e

#endif  // RTI_DataElement_h_
