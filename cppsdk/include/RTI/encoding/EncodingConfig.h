// IEEE 1516.1-2010 / 1516.2 Annex B — RTI/encoding/EncodingConfig.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Platform-bridge integer typedefs used by BasicDataElements
// (catalogue 14.9 / FR-DLC-7).

#ifndef RTI_EncodingConfig_h_
#define RTI_EncodingConfig_h_

#include <utility>
#include <vector>

namespace rti1516e {

#if defined(_WIN32)
typedef char Integer8;
typedef short Integer16;
typedef int Integer32;
typedef long long Integer64;
#else
#if defined(RTI_USE_64BIT_LONGS)
typedef char Integer8;
typedef short Integer16;
typedef int Integer32;
typedef long Integer64;
#else
typedef char Integer8;
typedef short Integer16;
typedef int Integer32;
typedef long long Integer64;
#endif
#endif

typedef Integer8 Octet;
typedef std::pair<Octet, Octet> OctetPair;

}  // namespace rti1516e

#endif  // RTI_EncodingConfig_h_
