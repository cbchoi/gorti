// IEEE 1516.2-2010 Annex B — RTI/encoding/HLAfixedRecord.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Catalogue 14.6 / FR-DLC-7.

#ifndef RTI_HLAfixedRecord_h_
#define RTI_HLAfixedRecord_h_

#include <RTI/SpecificConfig.h>
#include <RTI/encoding/DataElement.h>

namespace rti1516e {

class HLAfixedRecordImplementation;

class RTI_EXPORT HLAfixedRecord : public DataElement {
 public:
  HLAfixedRecord();
  HLAfixedRecord(HLAfixedRecord const& rhs);
  virtual ~HLAfixedRecord();

  virtual rti1516e::auto_ptr<DataElement> clone() const override;

  virtual VariableLengthData encode() const override;
  virtual void encode(VariableLengthData& inData) const override;
  virtual void encodeInto(std::vector<Octet>& buffer) const override;

  virtual void decode(VariableLengthData const& inData) override;
  virtual size_t decodeFrom(std::vector<Octet> const& buffer,
                            size_t index) override;

  virtual size_t getEncodedLength() const override;
  virtual unsigned int getOctetBoundary() const override;

  virtual bool hasElementSameTypeAs(size_t index,
                                    DataElement const& inData) const;

  virtual size_t size() const;
  virtual void appendElement(DataElement const& dataElement);
  virtual void appendElementPointer(DataElement* dataElement);
  virtual void set(size_t index, DataElement const& dataElement);
  virtual void setElementPointer(size_t index, DataElement* dataElement);
  virtual DataElement const& get(size_t index) const;
  virtual DataElement const& operator[](size_t index) const;

 private:
  HLAfixedRecord& operator=(HLAfixedRecord const&) = delete;
  HLAfixedRecordImplementation* _impl;
};

}  // namespace rti1516e

#endif  // RTI_HLAfixedRecord_h_
