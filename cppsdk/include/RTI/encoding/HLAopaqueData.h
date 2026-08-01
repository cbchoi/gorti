// IEEE 1516.2-2010 Annex B — RTI/encoding/HLAopaqueData.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Catalogue 14.8 / FR-DLC-7.

#ifndef RTI_HLAopaqueData_h_
#define RTI_HLAopaqueData_h_

#include <RTI/SpecificConfig.h>
#include <RTI/encoding/DataElement.h>

namespace rti1516e {

class HLAopaqueDataImplementation;

class RTI_EXPORT HLAopaqueData : public DataElement {
 public:
  HLAopaqueData();
  HLAopaqueData(Octet const* inData, size_t dataSize);
  HLAopaqueData(Octet** inData, size_t bufferSize, size_t dataSize);
  HLAopaqueData(HLAopaqueData const& rhs);
  virtual ~HLAopaqueData();

  virtual rti1516e::auto_ptr<DataElement> clone() const override;

  virtual VariableLengthData encode() const override;
  virtual void encode(VariableLengthData& inData) const override;
  virtual void encodeInto(std::vector<Octet>& buffer) const override;

  virtual void decode(VariableLengthData const& inData) override;
  virtual size_t decodeFrom(std::vector<Octet> const& buffer,
                            size_t index) override;

  virtual size_t getEncodedLength() const override;
  virtual unsigned int getOctetBoundary() const override;

  virtual size_t bufferLength() const;
  virtual size_t dataLength() const;
  virtual void setDataPointer(Octet** inData, size_t bufferSize,
                              size_t dataSize);
  virtual void set(Octet const* inData, size_t dataSize);

  virtual Octet const* get() const;
  virtual operator Octet const*() const;

 private:
  HLAopaqueData& operator=(HLAopaqueData const&) = delete;
  HLAopaqueDataImplementation* _impl;
};

}  // namespace rti1516e

#endif  // RTI_HLAopaqueData_h_
