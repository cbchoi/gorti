// rti1516e::encoding — HLA Evolved Annex B encoding helpers.
//
// IEEE 1516.2-2010 Annex B defines a fixed encoding for the basic
// data types (HLAfloat64BE, HLAinteger32BE, etc.). Cross-language
// federations rely on byte-identical encoding — a C++ federate that
// emits HLAfloat64BE(42.0) must produce bytes a Python or Go
// subscriber decodes back to the same value.
//
// M17.8 ships the "basic" set (Annex B §B.1) most Pitch federates
// use. Variable / fixed records, enumerated types, opaque data, and
// the array families land in a Cut-3 milestone alongside the
// HLAfixedRecord and HLAvariableArray templates.
//
// Header-only because none of the helpers need a translation unit;
// constexpr where possible.

#pragma once

#include <cstdint>
#include <cstring>
#include <stdexcept>
#include <string>

#include "Types.h"

namespace rti1516e::encoding {

class EncodingError : public std::runtime_error {
  using std::runtime_error::runtime_error;
};

namespace detail {

// Pack value's bytes in big-endian order into a freshly allocated
// VariableLengthData. T must be trivially copyable.
template <typename T>
VariableLengthData packBE(T v) {
  static_assert(std::is_trivially_copyable_v<T>, "encoding requires trivial layout");
  VariableLengthData out(sizeof(T));
  std::uint8_t raw[sizeof(T)];
  std::memcpy(raw, &v, sizeof(T));
#if defined(__BYTE_ORDER__) && __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
  // Host is little-endian — reverse byte order to get BE on wire.
  for (std::size_t i = 0; i < sizeof(T); ++i) out[i] = raw[sizeof(T) - 1 - i];
#else
  for (std::size_t i = 0; i < sizeof(T); ++i) out[i] = raw[i];
#endif
  return out;
}

template <typename T>
T unpackBE(const VariableLengthData& bytes, const char* type_name) {
  static_assert(std::is_trivially_copyable_v<T>, "encoding requires trivial layout");
  if (bytes.size() < sizeof(T)) {
    throw EncodingError(std::string("decode") + type_name +
                        ": insufficient bytes (need " +
                        std::to_string(sizeof(T)) + ", have " +
                        std::to_string(bytes.size()) + ")");
  }
  std::uint8_t raw[sizeof(T)];
#if defined(__BYTE_ORDER__) && __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
  for (std::size_t i = 0; i < sizeof(T); ++i) raw[i] = bytes[sizeof(T) - 1 - i];
#else
  for (std::size_t i = 0; i < sizeof(T); ++i) raw[i] = bytes[i];
#endif
  T v;
  std::memcpy(&v, raw, sizeof(T));
  return v;
}

}  // namespace detail

// --- HLAfloat ---------------------------------------------------------------

inline VariableLengthData encodeHLAfloat64BE(double v) {
  return detail::packBE(v);
}
inline double decodeHLAfloat64BE(const VariableLengthData& bytes) {
  return detail::unpackBE<double>(bytes, "HLAfloat64BE");
}
inline VariableLengthData encodeHLAfloat32BE(float v) {
  return detail::packBE(v);
}
inline float decodeHLAfloat32BE(const VariableLengthData& bytes) {
  return detail::unpackBE<float>(bytes, "HLAfloat32BE");
}

// --- HLAinteger --------------------------------------------------------------

inline VariableLengthData encodeHLAinteger32BE(std::int32_t v) {
  return detail::packBE(v);
}
inline std::int32_t decodeHLAinteger32BE(const VariableLengthData& bytes) {
  return detail::unpackBE<std::int32_t>(bytes, "HLAinteger32BE");
}
inline VariableLengthData encodeHLAinteger64BE(std::int64_t v) {
  return detail::packBE(v);
}
inline std::int64_t decodeHLAinteger64BE(const VariableLengthData& bytes) {
  return detail::unpackBE<std::int64_t>(bytes, "HLAinteger64BE");
}

// --- HLAoctet ----------------------------------------------------------------

inline VariableLengthData encodeHLAoctet(std::uint8_t v) {
  return VariableLengthData{v};
}
inline std::uint8_t decodeHLAoctet(const VariableLengthData& bytes) {
  if (bytes.empty()) throw EncodingError("decodeHLAoctet: empty input");
  return bytes[0];
}

// --- HLAunicodeString --------------------------------------------------------
//
// Wire layout: 4-byte BE length (UTF-16 code units), then the code
// units packed as UTF-16BE (2 bytes each). Matches Pitch / Portico's
// HLAunicodeString.

inline VariableLengthData encodeHLAunicodeString(std::u16string_view s) {
  const std::uint32_t n = static_cast<std::uint32_t>(s.size());
  VariableLengthData out;
  out.reserve(4 + 2 * n);
  // length prefix (BE)
  out.push_back(static_cast<std::uint8_t>((n >> 24) & 0xff));
  out.push_back(static_cast<std::uint8_t>((n >> 16) & 0xff));
  out.push_back(static_cast<std::uint8_t>((n >> 8) & 0xff));
  out.push_back(static_cast<std::uint8_t>(n & 0xff));
  for (auto c : s) {
    const auto cu = static_cast<std::uint16_t>(c);
    out.push_back(static_cast<std::uint8_t>((cu >> 8) & 0xff));
    out.push_back(static_cast<std::uint8_t>(cu & 0xff));
  }
  return out;
}

inline std::u16string decodeHLAunicodeString(const VariableLengthData& bytes) {
  if (bytes.size() < 4) {
    throw EncodingError("decodeHLAunicodeString: missing length prefix");
  }
  const std::uint32_t n = (static_cast<std::uint32_t>(bytes[0]) << 24) |
                          (static_cast<std::uint32_t>(bytes[1]) << 16) |
                          (static_cast<std::uint32_t>(bytes[2]) << 8) |
                          static_cast<std::uint32_t>(bytes[3]);
  if (bytes.size() < 4u + 2u * n) {
    throw EncodingError("decodeHLAunicodeString: truncated payload");
  }
  std::u16string out;
  out.reserve(n);
  for (std::uint32_t i = 0; i < n; ++i) {
    const auto hi = bytes[4 + 2 * i];
    const auto lo = bytes[4 + 2 * i + 1];
    out.push_back(static_cast<char16_t>((static_cast<std::uint16_t>(hi) << 8) |
                                        static_cast<std::uint16_t>(lo)));
  }
  return out;
}

}  // namespace rti1516e::encoding
