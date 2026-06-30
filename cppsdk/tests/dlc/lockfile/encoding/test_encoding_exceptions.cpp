// Lockfile: IEEE 1516.1-2010 RTI/encoding/EncodingExceptions.h.
// Catalogue §6 row 6.5.
//
// M31 RED — fails until M32 lands `RTI/encoding/EncodingExceptions.h` with
// `EncoderException` derived from `rti1516e::Exception` (NOT std::runtime_error).
//
// Pitch only ships `EncoderException` in this header (line 23 of the reference
// EncodingExceptions.h). DecoderException is mentioned in the M31 brief but is
// not in the Pitch surface — locking only EncoderException keeps this TU
// faithful to the Pitch-port contract.

#include <RTI/encoding/EncodingExceptions.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <string>
#include <stdexcept>  // for std::runtime_error used in static_assert below

namespace {

// EncoderException is a class deriving from rti1516e::Exception.
static_assert(std::is_class_v<rti1516e::EncoderException>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::EncoderException>);

// NOT a derived class of std::runtime_error — that's the gorti M17 form.
static_assert(!std::is_base_of_v<std::runtime_error, rti1516e::EncoderException>);

// Constructible from std::wstring const&.
static_assert(std::is_constructible_v<rti1516e::EncoderException, std::wstring const&>);

// what() returns std::wstring (NOT const char*).
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::EncoderException const&>().what()),
    std::wstring>);

}  // namespace
