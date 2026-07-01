// IEEE 1516.1-2010 §C.2 — encoding exception impls.
// gorti M32. Catalogue row 6.5.

#include <RTI/encoding/EncodingExceptions.h>

namespace rti1516e {

EncoderException::EncoderException(std::wstring const& message) RTI_NOEXCEPT
    : _msg(message) {}
std::wstring EncoderException::what() const RTI_NOEXCEPT { return _msg; }

DecoderException::DecoderException(std::wstring const& message) RTI_NOEXCEPT
    : _msg(message) {}
std::wstring DecoderException::what() const RTI_NOEXCEPT { return _msg; }

}  // namespace rti1516e
