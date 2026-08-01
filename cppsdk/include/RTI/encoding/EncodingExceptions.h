// IEEE 1516.1-2010 §C.2 — RTI/encoding/EncodingExceptions.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Encoding-specific exception leaves (catalogue 6.5 / FR-DLC-6).

#ifndef RTI_EncodingExceptions_h_
#define RTI_EncodingExceptions_h_

#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>
#include <RTI/encoding/EncodingConfig.h>
#include <string>

namespace rti1516e {

class RTI_EXPORT EncoderException : public Exception {
 public:
  EncoderException(std::wstring const& message) RTI_NOEXCEPT;
  std::wstring what() const RTI_NOEXCEPT;

 private:
  std::wstring _msg;
};

class RTI_EXPORT DecoderException : public Exception {
 public:
  DecoderException(std::wstring const& message) RTI_NOEXCEPT;
  std::wstring what() const RTI_NOEXCEPT;

 private:
  std::wstring _msg;
};

}  // namespace rti1516e

#endif  // RTI_EncodingExceptions_h_
