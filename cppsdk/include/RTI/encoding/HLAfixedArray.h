// IEEE 1516.2-2010 Annex B — RTI/encoding/HLAfixedArray.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Catalogue 14.4 / FR-DLC-7.

#ifndef RTI_HLAfixedArray_h_
#define RTI_HLAfixedArray_h_

#include <RTI/SpecificConfig.h>
#include <RTI/encoding/DataElement.h>

namespace rti1516e {

class HLAfixedArrayImplementation;

class RTI_EXPORT HLAfixedArray : public DataElement {
 public:
  HLAfixedArray(DataElement const& protoType, size_t length);
  HLAfixedArray(HLAfixedArray const& rhs);
  virtual ~HLAfixedArray();

  virtual rti1516e::auto_ptr<DataElement> clone() const override;

  virtual VariableLengthData encode() const override;
  virtual void encode(VariableLengthData& inData) const override;
  virtual void encodeInto(std::vector<Octet>& buffer) const override;

  virtual void decode(VariableLengthData const& inData) override;
  virtual size_t decodeFrom(std::vector<Octet> const& buffer,
                            size_t index) override;

  virtual size_t getEncodedLength() const override;
  virtual unsigned int getOctetBoundary() const override;

  virtual bool isSameTypeAs(DataElement const& inData) const override;
  virtual bool hasPrototypeSameTypeAs(DataElement const& dataElement) const;

  virtual size_t size() const;

  virtual void set(size_t index, DataElement const& dataElement);

  virtual DataElement const& get(size_t index) const;
  virtual DataElement const& operator[](size_t index) const;

 private:
  HLAfixedArray& operator=(HLAfixedArray const&) = delete;
  HLAfixedArrayImplementation* _impl;
};

}  // namespace rti1516e

#endif  // RTI_HLAfixedArray_h_
