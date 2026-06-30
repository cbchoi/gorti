// IEEE 1516.2-2010 Annex B — RTI/encoding/BasicDataElements.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// 19 spec-mandated basic encodable types (catalogue 14.2 / FR-DLC-7).
// Each declared via DEFINE_ENCODING_HELPER_CLASS — matches the Pitch
// reprint of Annex B.

#ifndef RTI_BasicDataElements_h_
#define RTI_BasicDataElements_h_

#include <RTI/SpecificConfig.h>
#include <RTI/VariableLengthData.h>
#include <RTI/encoding/DataElement.h>
#include <RTI/encoding/EncodingConfig.h>
#include <string>

#define DEFINE_ENCODING_HELPER_CLASS(EncodableDataType, SimpleDataType)        \
  class EncodableDataType##Implementation;                                     \
                                                                               \
  class RTI_EXPORT EncodableDataType : public rti1516e::DataElement {          \
   public:                                                                     \
    EncodableDataType();                                                       \
    EncodableDataType(SimpleDataType const& inData);                           \
    EncodableDataType(SimpleDataType* inData);                                 \
    EncodableDataType(EncodableDataType const& rhs);                           \
    virtual ~EncodableDataType();                                              \
                                                                               \
    virtual rti1516e::auto_ptr<rti1516e::DataElement> clone() const override;  \
                                                                               \
    virtual rti1516e::VariableLengthData encode() const override;              \
    virtual void encode(rti1516e::VariableLengthData& inData) const override;  \
    virtual void encodeInto(std::vector<rti1516e::Octet>& buffer)              \
        const override;                                                        \
                                                                               \
    virtual void decode(rti1516e::VariableLengthData const& inData) override;  \
    virtual size_t decodeFrom(std::vector<rti1516e::Octet> const& buffer,      \
                              size_t index) override;                          \
                                                                               \
    virtual size_t getEncodedLength() const override;                          \
    virtual unsigned int getOctetBoundary() const override;                    \
                                                                               \
    virtual rti1516e::Integer64 hash() const override;                         \
                                                                               \
    EncodableDataType& operator=(EncodableDataType const& rhs);                \
    EncodableDataType& operator=(SimpleDataType const& rhs);                   \
    operator SimpleDataType() const;                                           \
    SimpleDataType get() const;                                                \
                                                                               \
   private:                                                                    \
    EncodableDataType##Implementation* _impl;                                  \
  };

namespace rti1516e {

DEFINE_ENCODING_HELPER_CLASS(HLAASCIIchar, char)
DEFINE_ENCODING_HELPER_CLASS(HLAASCIIstring, std::string)
DEFINE_ENCODING_HELPER_CLASS(HLAboolean, bool)
DEFINE_ENCODING_HELPER_CLASS(HLAbyte, Octet)
DEFINE_ENCODING_HELPER_CLASS(HLAfloat32BE, float)
DEFINE_ENCODING_HELPER_CLASS(HLAfloat32LE, float)
DEFINE_ENCODING_HELPER_CLASS(HLAfloat64BE, double)
DEFINE_ENCODING_HELPER_CLASS(HLAfloat64LE, double)
DEFINE_ENCODING_HELPER_CLASS(HLAinteger16LE, Integer16)
DEFINE_ENCODING_HELPER_CLASS(HLAinteger16BE, Integer16)
DEFINE_ENCODING_HELPER_CLASS(HLAinteger32BE, Integer32)
DEFINE_ENCODING_HELPER_CLASS(HLAinteger32LE, Integer32)
DEFINE_ENCODING_HELPER_CLASS(HLAinteger64BE, Integer64)
DEFINE_ENCODING_HELPER_CLASS(HLAinteger64LE, Integer64)
DEFINE_ENCODING_HELPER_CLASS(HLAoctet, Octet)
DEFINE_ENCODING_HELPER_CLASS(HLAoctetPairBE, OctetPair)
DEFINE_ENCODING_HELPER_CLASS(HLAoctetPairLE, OctetPair)
DEFINE_ENCODING_HELPER_CLASS(HLAunicodeChar, wchar_t)
DEFINE_ENCODING_HELPER_CLASS(HLAunicodeString, std::wstring)

}  // namespace rti1516e

#endif  // RTI_BasicDataElements_h_
