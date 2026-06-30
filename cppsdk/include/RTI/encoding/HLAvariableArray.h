// IEEE 1516.2-2010 Annex B — RTI/encoding/HLAvariableArray.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Catalogue 14.5 / FR-DLC-7.

#ifndef RTI_HLAvariableArray_h_
#define RTI_HLAvariableArray_h_

#include <RTI/SpecificConfig.h>
#include <RTI/encoding/DataElement.h>

namespace rti1516e {

class HLAvariableArrayImplementation;

class RTI_EXPORT HLAvariableArray : public DataElement {
 public:
  HLAvariableArray(DataElement const& protoType);
  HLAvariableArray(HLAvariableArray const& rhs);
  virtual ~HLAvariableArray();

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
  virtual void addElement(DataElement const& dataElement);
  virtual void addElementPointer(DataElement* dataElement);
  virtual DataElement const& get(size_t index) const;
  virtual DataElement const& operator[](size_t index) const;

 private:
  HLAvariableArray& operator=(HLAvariableArray const&) = delete;
  HLAvariableArrayImplementation* _impl;
};

}  // namespace rti1516e

#endif  // RTI_HLAvariableArray_h_
