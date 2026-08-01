// Lockfile: IEEE 1516.1-2010 RTI/encoding/BasicDataElements.h — 19 classes.
// Catalogue §14 row 14.2.
//
// M31 RED — fails until M32 lands `RTI/encoding/BasicDataElements.h`. gorti M17
// has 5 free functions in Encoding.h, not 19 classes — the BLOCKING divergence.
//
// Each class via DEFINE_ENCODING_HELPER_CLASS must:
//   * derive publicly from rti1516e::DataElement
//   * be polymorphic + abstract-free (concretely instantiable)
//   * have default + copy ctor + assignment + dtor
//
// IEEE 1516.1-2010 API reference: RTI/encoding/BasicDataElements.h

#include <RTI/encoding/BasicDataElements.h>
#include <RTI/encoding/DataElement.h>
#include <type_traits>

namespace {

#define LOCK_ENCODING_HELPER(Helper)                                            \
  static_assert(std::is_class_v<rti1516e::Helper>);                             \
  static_assert(std::is_base_of_v<rti1516e::DataElement, rti1516e::Helper>);    \
  static_assert(!std::is_abstract_v<rti1516e::Helper>);                         \
  static_assert(std::is_default_constructible_v<rti1516e::Helper>);             \
  static_assert(std::is_copy_constructible_v<rti1516e::Helper>);                \
  static_assert(std::is_copy_assignable_v<rti1516e::Helper>)

// 19 classes per BasicDataElements.h:151-170
LOCK_ENCODING_HELPER(HLAASCIIchar);
LOCK_ENCODING_HELPER(HLAASCIIstring);
LOCK_ENCODING_HELPER(HLAboolean);
LOCK_ENCODING_HELPER(HLAbyte);
LOCK_ENCODING_HELPER(HLAfloat32BE);
LOCK_ENCODING_HELPER(HLAfloat32LE);
LOCK_ENCODING_HELPER(HLAfloat64BE);
LOCK_ENCODING_HELPER(HLAfloat64LE);
LOCK_ENCODING_HELPER(HLAinteger16LE);
LOCK_ENCODING_HELPER(HLAinteger16BE);
LOCK_ENCODING_HELPER(HLAinteger32BE);
LOCK_ENCODING_HELPER(HLAinteger32LE);
LOCK_ENCODING_HELPER(HLAinteger64BE);
LOCK_ENCODING_HELPER(HLAinteger64LE);
LOCK_ENCODING_HELPER(HLAoctet);
LOCK_ENCODING_HELPER(HLAoctetPairBE);
LOCK_ENCODING_HELPER(HLAoctetPairLE);
LOCK_ENCODING_HELPER(HLAunicodeChar);
LOCK_ENCODING_HELPER(HLAunicodeString);

#undef LOCK_ENCODING_HELPER

// EncodingConfig.h typedefs — Row 14.9.
static_assert(std::is_signed_v<rti1516e::Integer8>);
static_assert(sizeof(rti1516e::Integer8) == 1);
static_assert(sizeof(rti1516e::Integer16) == 2);
static_assert(sizeof(rti1516e::Integer32) == 4);
static_assert(sizeof(rti1516e::Integer64) == 8);
static_assert(std::is_same_v<rti1516e::Octet, rti1516e::Integer8>);
static_assert(std::is_same_v<rti1516e::OctetPair,
                             std::pair<rti1516e::Octet, rti1516e::Octet>>);

}  // namespace
