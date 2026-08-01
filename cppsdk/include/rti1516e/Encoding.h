// rti1516e::encoding — HLA Evolved Annex B encoding helpers.
//
// IEEE 1516.2-2010 Annex B defines a fixed encoding for the basic
// data types (HLAfloat64BE, HLAinteger32BE, etc.). Cross-language
// federations rely on byte-identical encoding — a C++ federate that
// emits HLAfloat64BE(42.0) must produce bytes a Python or Go
// subscriber decodes back to the same value.
//
// M17.8 ships the "basic" set (Annex B §B.1) most reference_rti federates
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
#include <type_traits>
#include <utility>
#include <vector>

#include "Types.h"

// M35 — M17 shim deprecation gate. See Types.h for the full
// macro definition; the encoding namespace piggybacks on the same silencer.
#ifdef GORTI_ACCEPT_M17_SHIM
#  define GORTI_M17_SHIM_DEPRECATED_ENC /* silenced */
#else
#  define GORTI_M17_SHIM_DEPRECATED_ENC \
     [[deprecated("gorti M17 shim — use <RTI/...> per IEEE 1516.1-2010 DLC (M35). Define GORTI_ACCEPT_M17_SHIM to silence.")]]
#endif
namespace rti1516e {
namespace GORTI_M17_SHIM_DEPRECATED_ENC encoding {

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
// units packed as UTF-16BE (2 bytes each). Matches reference_rti / Portico's
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

// --- HLAenumeratedType -------------------------------------------------------
//
// IEEE 1516.2 §B.2 enumerated types are encoded as their underlying
// numeric representation (typically HLAinteger32BE for cross-vendor
// compat). Federate code passes enum class values; the helpers
// reuse the integer encoders.

template <typename Enum>
inline VariableLengthData encodeHLAenum32BE(Enum e) {
  static_assert(std::is_enum_v<Enum> || std::is_integral_v<Enum>,
                "encodeHLAenum32BE requires an enum or integral type");
  return encodeHLAinteger32BE(static_cast<std::int32_t>(e));
}

template <typename Enum>
inline Enum decodeHLAenum32BE(const VariableLengthData& bytes) {
  static_assert(std::is_enum_v<Enum> || std::is_integral_v<Enum>,
                "decodeHLAenum32BE requires an enum or integral type");
  return static_cast<Enum>(decodeHLAinteger32BE(bytes));
}

// --- HLAfixedArray<T> --------------------------------------------------------
//
// IEEE 1516.2 §B.3.2 — N elements of T encoded back-to-back. No
// length prefix (consumers know N from the FOM declaration).
//
// The federate supplies an encoder for one element (encodeFn(T) ->
// VariableLengthData); the helper concatenates the per-element
// byte streams. Decode is symmetric: federate supplies decodeFn
// and the per-element stride.

template <typename T, typename EncodeFn>
inline VariableLengthData encodeHLAfixedArray(
    const std::vector<T>& items, EncodeFn encode_fn) {
  VariableLengthData out;
  for (const auto& item : items) {
    const auto enc = encode_fn(item);
    out.insert(out.end(), enc.begin(), enc.end());
  }
  return out;
}

template <typename T, typename DecodeFn>
inline std::vector<T> decodeHLAfixedArray(
    const VariableLengthData& bytes,
    std::size_t element_count,
    std::size_t element_stride,
    DecodeFn decode_fn) {
  if (bytes.size() < element_count * element_stride) {
    throw EncodingError(
        "decodeHLAfixedArray: truncated payload (need " +
        std::to_string(element_count * element_stride) + ", have " +
        std::to_string(bytes.size()) + ")");
  }
  std::vector<T> out;
  out.reserve(element_count);
  for (std::size_t i = 0; i < element_count; ++i) {
    VariableLengthData slice(bytes.begin() + i * element_stride,
                             bytes.begin() + (i + 1) * element_stride);
    out.push_back(decode_fn(slice));
  }
  return out;
}

// --- HLAvariableArray<T> -----------------------------------------------------
//
// IEEE 1516.2 §B.3.3 — 4-byte BE length prefix + N elements of T.

template <typename T, typename EncodeFn>
inline VariableLengthData encodeHLAvariableArray(
    const std::vector<T>& items, EncodeFn encode_fn) {
  const std::uint32_t n = static_cast<std::uint32_t>(items.size());
  VariableLengthData out;
  out.push_back(static_cast<std::uint8_t>((n >> 24) & 0xff));
  out.push_back(static_cast<std::uint8_t>((n >> 16) & 0xff));
  out.push_back(static_cast<std::uint8_t>((n >> 8) & 0xff));
  out.push_back(static_cast<std::uint8_t>(n & 0xff));
  for (const auto& item : items) {
    const auto enc = encode_fn(item);
    out.insert(out.end(), enc.begin(), enc.end());
  }
  return out;
}

template <typename T, typename DecodeFn>
inline std::vector<T> decodeHLAvariableArray(
    const VariableLengthData& bytes,
    std::size_t element_stride,
    DecodeFn decode_fn) {
  if (bytes.size() < 4) {
    throw EncodingError("decodeHLAvariableArray: missing length prefix");
  }
  const std::uint32_t n = (static_cast<std::uint32_t>(bytes[0]) << 24) |
                          (static_cast<std::uint32_t>(bytes[1]) << 16) |
                          (static_cast<std::uint32_t>(bytes[2]) << 8) |
                          static_cast<std::uint32_t>(bytes[3]);
  if (bytes.size() < 4u + n * element_stride) {
    throw EncodingError(
        "decodeHLAvariableArray: truncated payload (need " +
        std::to_string(4u + n * element_stride) + ", have " +
        std::to_string(bytes.size()) + ")");
  }
  std::vector<T> out;
  out.reserve(n);
  for (std::uint32_t i = 0; i < n; ++i) {
    VariableLengthData slice(bytes.begin() + 4 + i * element_stride,
                             bytes.begin() + 4 + (i + 1) * element_stride);
    out.push_back(decode_fn(slice));
  }
  return out;
}

// Variable-width variant — for arrays of types whose encoded width
// depends on the value (HLAunicodeString, nested HLAvariableArray,
// HLAvariantRecord). M17.23 (Cut-4).
//
// The DecodeFn callback takes the byte slice STARTING at the
// element and returns {decoded value, bytes consumed}. The helper
// uses the returned consumed-count to advance to the next element.
//
// Federate-side example (vector of HLAunicodeString):
//   auto names = decodeHLAvariableArrayVarWidth<std::u16string>(
//       bytes, [](auto slice) -> std::pair<std::u16string, std::size_t> {
//         auto s = decodeHLAunicodeString(slice);
//         // HLAunicodeString consumes 4 + 2*N where N is from the prefix.
//         std::size_t n = (slice[0] << 24) | (slice[1] << 16) |
//                         (slice[2] << 8)  | slice[3];
//         return {s, 4 + 2 * n};
//       });

template <typename T, typename DecodeFn>
inline std::vector<T> decodeHLAvariableArrayVarWidth(
    const VariableLengthData& bytes, DecodeFn decode_fn) {
  if (bytes.size() < 4) {
    throw EncodingError(
        "decodeHLAvariableArrayVarWidth: missing length prefix");
  }
  const std::uint32_t n = (static_cast<std::uint32_t>(bytes[0]) << 24) |
                          (static_cast<std::uint32_t>(bytes[1]) << 16) |
                          (static_cast<std::uint32_t>(bytes[2]) << 8) |
                          static_cast<std::uint32_t>(bytes[3]);
  std::vector<T> out;
  out.reserve(n);
  std::size_t pos = 4;
  for (std::uint32_t i = 0; i < n; ++i) {
    if (pos > bytes.size()) {
      throw EncodingError(
          "decodeHLAvariableArrayVarWidth: read past end at element " +
          std::to_string(i));
    }
    VariableLengthData slice(bytes.begin() + pos, bytes.end());
    auto [value, consumed] = decode_fn(slice);
    if (consumed == 0) {
      throw EncodingError(
          "decodeHLAvariableArrayVarWidth: callback consumed 0 bytes "
          "at element " +
          std::to_string(i));
    }
    out.push_back(std::move(value));
    pos += consumed;
  }
  return out;
}

// --- HLAfixedRecord ----------------------------------------------------------
//
// IEEE 1516.2 §B.4.1 — a fixed-shape record concatenates its
// field encodings in declaration order. Per-field padding to the
// next alignment boundary is the field's responsibility in this
// Cut-3 surface — federates encode each field, then call
// encodeHLAfixedRecord with the pre-encoded list.
//
// Most cross-language FOMs use natural alignment (HLAfloat64BE
// is 8-aligned, HLAinteger32BE is 4-aligned). The simplest pattern
// is to declare records with naturally-ordered fields so no
// padding is needed; for awkward orderings, federates encode a
// zero-byte HLAoctet between fields.

inline VariableLengthData encodeHLAfixedRecord(
    const std::vector<VariableLengthData>& fields) {
  VariableLengthData out;
  for (const auto& f : fields) {
    out.insert(out.end(), f.begin(), f.end());
  }
  return out;
}

// Slice a fixed record into per-field VariableLengthData windows.
// The federate supplies the byte-offset of each field's start;
// the helper returns the slices in the same order. Last field's
// extent runs to end-of-input.
inline std::vector<VariableLengthData> decodeHLAfixedRecord(
    const VariableLengthData& bytes,
    const std::vector<std::size_t>& field_offsets) {
  std::vector<VariableLengthData> out;
  out.reserve(field_offsets.size());
  for (std::size_t i = 0; i < field_offsets.size(); ++i) {
    const std::size_t start = field_offsets[i];
    const std::size_t end = (i + 1 < field_offsets.size())
                                ? field_offsets[i + 1]
                                : bytes.size();
    if (start > bytes.size() || end > bytes.size() || end < start) {
      throw EncodingError(
          "decodeHLAfixedRecord: invalid offsets for field " +
          std::to_string(i));
    }
    out.emplace_back(bytes.begin() + start, bytes.begin() + end);
  }
  return out;
}

// --- HLAfixedRecord auto-alignment (M17.24) ---------------------------------
//
// IEEE 1516.2 §B.4.1: each field starts at the next multiple of
// its declared alignment ("octetBoundary"). The padding bytes are
// zero. Common octet boundaries:
//   HLAoctet                 1
//   HLAinteger16BE / 32BE    2 / 4
//   HLAinteger64BE           8
//   HLAfloat32BE / 64BE      4 / 8
//   HLAunicodeString         1
//   HLA(variable|fixed)Array max alignment of element type
//
// The federate code passes a list of {field_bytes, alignment} pairs.
// The helper inserts zero-pad bytes before each field whose start
// offset isn't already a multiple of its alignment.
//
// Decode-side: takes the alignment list and the field widths
// (variable-width fields supply std::nullopt and a decode callback
// — Cut-4 ships the fixed-width variant; the variable case is
// left for federate composition with HLAvariableArrayVarWidth).

struct AlignedField {
  VariableLengthData bytes;
  std::size_t alignment;
};

inline VariableLengthData encodeHLAfixedRecordAligned(
    const std::vector<AlignedField>& fields) {
  VariableLengthData out;
  for (const auto& f : fields) {
    if (f.alignment > 1) {
      while (out.size() % f.alignment != 0) out.push_back(0);
    }
    out.insert(out.end(), f.bytes.begin(), f.bytes.end());
  }
  return out;
}

// Decode-side: walks the record left-to-right inserting alignment
// padding between fields, then slices each field at its known
// width. Used when the FOM declares fixed-width fields with
// non-trivial alignment. ``widths[i]`` is the byte width of
// field i.
inline std::vector<VariableLengthData> decodeHLAfixedRecordAligned(
    const VariableLengthData& bytes,
    const std::vector<std::size_t>& widths,
    const std::vector<std::size_t>& alignments) {
  if (widths.size() != alignments.size()) {
    throw EncodingError(
        "decodeHLAfixedRecordAligned: widths/alignments length mismatch");
  }
  std::vector<VariableLengthData> out;
  out.reserve(widths.size());
  std::size_t pos = 0;
  for (std::size_t i = 0; i < widths.size(); ++i) {
    if (alignments[i] > 1) {
      while (pos % alignments[i] != 0) ++pos;
    }
    if (pos + widths[i] > bytes.size()) {
      throw EncodingError(
          "decodeHLAfixedRecordAligned: field " + std::to_string(i) +
          " runs past end of record");
    }
    out.emplace_back(bytes.begin() + pos, bytes.begin() + pos + widths[i]);
    pos += widths[i];
  }
  return out;
}

}  // namespace encoding
}  // namespace rti1516e
