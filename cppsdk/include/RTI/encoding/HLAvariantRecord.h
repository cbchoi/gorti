// IEEE 1516.2-2010 Annex B — RTI/encoding/HLAvariantRecord.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Catalogue 14.7 / FR-DLC-7.

#ifndef RTI_HLAvariantRecord_h_
#define RTI_HLAvariantRecord_h_

#include <RTI/SpecificConfig.h>
#include <RTI/encoding/DataElement.h>

namespace rti1516e {

class HLAvariantRecordImplementation;

class RTI_EXPORT HLAvariantRecord : public DataElement {
 public:
  HLAvariantRecord(DataElement const& discriminantPrototype);
  HLAvariantRecord(HLAvariantRecord const& rhs);
  virtual ~HLAvariantRecord();

  virtual rti1516e::auto_ptr<DataElement> clone() const override;

  virtual VariableLengthData encode() const override;
  virtual void encode(VariableLengthData& inData) const override;
  virtual void encodeInto(std::vector<Octet>& buffer) const override;

  virtual void decode(VariableLengthData const& inData) override;
  virtual size_t decodeFrom(std::vector<Octet> const& buffer,
                            size_t index) override;

  virtual size_t getEncodedLength() const override;
  virtual unsigned int getOctetBoundary() const override;

  virtual void addVariant(DataElement const& discriminant,
                          DataElement const& valuePrototype);
  virtual void addVariantPointer(DataElement const& discriminant,
                                 DataElement* valuePtr);
  virtual void setDiscriminant(DataElement const& discriminant);
  virtual void setVariant(DataElement const& discriminant,
                          DataElement const& value);
  virtual void setVariantPointer(DataElement const& discriminant,
                                 DataElement* valuePtr);

  virtual DataElement const& getDiscriminant() const;
  virtual DataElement const& getVariant() const;

 private:
  HLAvariantRecord& operator=(HLAvariantRecord const&) = delete;
  HLAvariantRecordImplementation* _impl;
};

}  // namespace rti1516e

#endif  // RTI_HLAvariantRecord_h_
