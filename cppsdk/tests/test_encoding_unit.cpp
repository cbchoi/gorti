// M17.8 — unit tests for rti1516e::encoding helpers.
//
// IEEE 1516.2-2010 Annex B encoding rules: HLA basic types are
// big-endian. Tests pin each helper round-trips through encode/decode
// AND that the on-wire bytes match the spec layout (since
// cross-language federations rely on byte-identical encoding).

#include <gtest/gtest.h>

#include <cstdint>

#include "rti1516e/Encoding.h"

namespace {

using namespace rti1516e::encoding;
using rti1516e::VariableLengthData;

// --- HLAfloat64BE ----------------------------------------------------------

TEST(HLAfloat64BE, EncodeKnownValueIsBigEndian) {
  // 1.0 as IEEE-754 double = 0x3FF0000000000000.
  const auto bytes = encodeHLAfloat64BE(1.0);
  ASSERT_EQ(bytes.size(), 8u);
  EXPECT_EQ(bytes[0], 0x3Fu);
  EXPECT_EQ(bytes[1], 0xF0u);
  for (std::size_t i = 2; i < 8; ++i) EXPECT_EQ(bytes[i], 0x00u);
}

TEST(HLAfloat64BE, RoundTripZero) {
  EXPECT_EQ(decodeHLAfloat64BE(encodeHLAfloat64BE(0.0)), 0.0);
}

TEST(HLAfloat64BE, RoundTripNegative) {
  EXPECT_EQ(decodeHLAfloat64BE(encodeHLAfloat64BE(-3.14)), -3.14);
}

TEST(HLAfloat64BE, RoundTripLargeMagnitude) {
  EXPECT_EQ(decodeHLAfloat64BE(encodeHLAfloat64BE(1.7e308)), 1.7e308);
}

TEST(HLAfloat64BE, DecodeShortBytesThrows) {
  VariableLengthData too_short(4);
  EXPECT_THROW(decodeHLAfloat64BE(too_short), EncodingError);
}

// --- HLAfloat32BE ----------------------------------------------------------

TEST(HLAfloat32BE, RoundTrip) {
  EXPECT_FLOAT_EQ(decodeHLAfloat32BE(encodeHLAfloat32BE(2.5f)), 2.5f);
}

TEST(HLAfloat32BE, EncodeIs4Bytes) {
  EXPECT_EQ(encodeHLAfloat32BE(0.0f).size(), 4u);
}

// --- HLAinteger32BE / HLAinteger64BE --------------------------------------

TEST(HLAinteger32BE, EncodeKnownValueIsBigEndian) {
  const auto bytes = encodeHLAinteger32BE(0x12345678);
  ASSERT_EQ(bytes.size(), 4u);
  EXPECT_EQ(bytes[0], 0x12u);
  EXPECT_EQ(bytes[1], 0x34u);
  EXPECT_EQ(bytes[2], 0x56u);
  EXPECT_EQ(bytes[3], 0x78u);
}

TEST(HLAinteger32BE, RoundTripNegative) {
  EXPECT_EQ(decodeHLAinteger32BE(encodeHLAinteger32BE(-42)), -42);
}

TEST(HLAinteger32BE, RoundTripBounds) {
  EXPECT_EQ(decodeHLAinteger32BE(
                encodeHLAinteger32BE(std::numeric_limits<std::int32_t>::min())),
            std::numeric_limits<std::int32_t>::min());
  EXPECT_EQ(decodeHLAinteger32BE(
                encodeHLAinteger32BE(std::numeric_limits<std::int32_t>::max())),
            std::numeric_limits<std::int32_t>::max());
}

TEST(HLAinteger64BE, RoundTripBounds) {
  EXPECT_EQ(decodeHLAinteger64BE(
                encodeHLAinteger64BE(std::numeric_limits<std::int64_t>::min())),
            std::numeric_limits<std::int64_t>::min());
  EXPECT_EQ(decodeHLAinteger64BE(
                encodeHLAinteger64BE(std::numeric_limits<std::int64_t>::max())),
            std::numeric_limits<std::int64_t>::max());
}

TEST(HLAinteger64BE, EncodeIs8Bytes) {
  EXPECT_EQ(encodeHLAinteger64BE(0).size(), 8u);
}

// --- HLAoctet --------------------------------------------------------------

TEST(HLAoctet, SingleByte) {
  const auto bytes = encodeHLAoctet(0xAB);
  ASSERT_EQ(bytes.size(), 1u);
  EXPECT_EQ(bytes[0], 0xABu);
  EXPECT_EQ(decodeHLAoctet(bytes), 0xABu);
}

// --- HLAunicodeString ------------------------------------------------------
//
// Encoding: 4-byte BE length prefix (count of UTF-16 code units), then
// the UTF-16BE code units (2 bytes each). Cut-2 ships UTF-16 directly;
// federate code that has UTF-8 should convert separately before calling.

TEST(HLAunicodeString, EmptyString) {
  const auto bytes = encodeHLAunicodeString(u"");
  ASSERT_EQ(bytes.size(), 4u);
  for (auto b : bytes) EXPECT_EQ(b, 0x00u);
  EXPECT_EQ(decodeHLAunicodeString(bytes), u"");
}

TEST(HLAunicodeString, AsciiRoundTrip) {
  const auto bytes = encodeHLAunicodeString(u"Vehicle");
  // length=7 → 0x00000007
  EXPECT_EQ(bytes[0], 0x00u);
  EXPECT_EQ(bytes[1], 0x00u);
  EXPECT_EQ(bytes[2], 0x00u);
  EXPECT_EQ(bytes[3], 0x07u);
  // 'V' = 0x0056 in UTF-16BE.
  EXPECT_EQ(bytes[4], 0x00u);
  EXPECT_EQ(bytes[5], 0x56u);
  EXPECT_EQ(decodeHLAunicodeString(bytes), u"Vehicle");
}

TEST(HLAunicodeString, DecodeMalformedThrows) {
  VariableLengthData malformed{0x00, 0x00, 0x00, 0x05, 0x00, 0x41};  // claims 5, has 1
  EXPECT_THROW(decodeHLAunicodeString(malformed), EncodingError);
}

// --- HLAenumeratedType ------------------------------------------------------

enum class Color : std::int32_t {
  Red = 1,
  Green = 2,
  Blue = 0x7FFFFFFF,
};

TEST(HLAenum32BE, EncodeMatchesInteger32BE) {
  const auto bytes = encodeHLAenum32BE(Color::Green);
  EXPECT_EQ(bytes, encodeHLAinteger32BE(2));
}

TEST(HLAenum32BE, RoundTripUserEnum) {
  for (auto v : {Color::Red, Color::Green, Color::Blue}) {
    const auto bytes = encodeHLAenum32BE(v);
    EXPECT_EQ(decodeHLAenum32BE<Color>(bytes), v);
  }
}

// --- HLAfixedArray ----------------------------------------------------------

TEST(HLAfixedArray, ThreeDoublesEncodesAs24Bytes) {
  std::vector<double> xs{1.0, 2.0, 3.0};
  const auto bytes = encodeHLAfixedArray<double>(
      xs, [](double v) { return encodeHLAfloat64BE(v); });
  ASSERT_EQ(bytes.size(), 24u);
  // First element matches HLAfloat64BE(1.0).
  EXPECT_EQ(bytes[0], 0x3Fu);
  EXPECT_EQ(bytes[1], 0xF0u);
}

TEST(HLAfixedArray, RoundTripThreeInts) {
  std::vector<std::int32_t> xs{10, -20, 30};
  const auto bytes = encodeHLAfixedArray<std::int32_t>(
      xs, [](std::int32_t v) { return encodeHLAinteger32BE(v); });
  const auto round = decodeHLAfixedArray<std::int32_t>(
      bytes, 3, 4, [](const VariableLengthData& b) {
        return decodeHLAinteger32BE(b);
      });
  EXPECT_EQ(round, xs);
}

TEST(HLAfixedArray, DecodeTruncatedThrows) {
  VariableLengthData bytes{0x00, 0x00, 0x00, 0x01};  // 4 bytes, want 8
  EXPECT_THROW(
      (decodeHLAfixedArray<std::int32_t>(bytes, /*N=*/2, /*stride=*/4,
                                          [](const VariableLengthData& b) {
                                            return decodeHLAinteger32BE(b);
                                          })),
      EncodingError);
}

// --- HLAvariableArray -------------------------------------------------------

TEST(HLAvariableArray, EncodeHasLengthPrefix) {
  std::vector<std::int32_t> xs{42};
  const auto bytes = encodeHLAvariableArray<std::int32_t>(
      xs, [](std::int32_t v) { return encodeHLAinteger32BE(v); });
  ASSERT_EQ(bytes.size(), 8u);  // 4 prefix + 4 body
  EXPECT_EQ(bytes[0], 0x00u);
  EXPECT_EQ(bytes[1], 0x00u);
  EXPECT_EQ(bytes[2], 0x00u);
  EXPECT_EQ(bytes[3], 0x01u);  // count = 1
}

TEST(HLAvariableArray, RoundTripEmpty) {
  std::vector<std::int32_t> empty;
  const auto bytes = encodeHLAvariableArray<std::int32_t>(
      empty, [](std::int32_t v) { return encodeHLAinteger32BE(v); });
  ASSERT_EQ(bytes.size(), 4u);
  const auto round = decodeHLAvariableArray<std::int32_t>(
      bytes, 4, [](const VariableLengthData& b) {
        return decodeHLAinteger32BE(b);
      });
  EXPECT_TRUE(round.empty());
}

TEST(HLAvariableArray, RoundTripDoubles) {
  std::vector<double> xs{1.5, 2.5, 3.5, 4.5};
  const auto bytes = encodeHLAvariableArray<double>(
      xs, [](double v) { return encodeHLAfloat64BE(v); });
  const auto round = decodeHLAvariableArray<double>(
      bytes, 8, [](const VariableLengthData& b) {
        return decodeHLAfloat64BE(b);
      });
  EXPECT_EQ(round, xs);
}

TEST(HLAvariableArray, DecodeMissingPrefixThrows) {
  VariableLengthData empty;
  EXPECT_THROW(
      (decodeHLAvariableArray<std::int32_t>(
          empty, 4, [](const VariableLengthData& b) {
            return decodeHLAinteger32BE(b);
          })),
      EncodingError);
}

// --- HLAvariableArrayVarWidth (M17.23) -------------------------------------

namespace {
// Helper: decode an HLAunicodeString from a slice and report bytes
// consumed (4-byte prefix + 2*N body).
inline std::pair<std::u16string, std::size_t> decodeUnicodeWithLen(
    const VariableLengthData& slice) {
  if (slice.size() < 4) {
    throw EncodingError("decodeUnicodeWithLen: short prefix");
  }
  const std::uint32_t n = (static_cast<std::uint32_t>(slice[0]) << 24) |
                          (static_cast<std::uint32_t>(slice[1]) << 16) |
                          (static_cast<std::uint32_t>(slice[2]) << 8) |
                          static_cast<std::uint32_t>(slice[3]);
  // Build a copy of just the unicode encoding's bytes for the
  // existing decoder.
  VariableLengthData this_one(slice.begin(), slice.begin() + 4 + 2 * n);
  return {decodeHLAunicodeString(this_one), 4 + 2 * n};
}
}  // namespace

TEST(HLAvariableArrayVarWidth, RoundTripUnicodeStrings) {
  // Encode {"hi", "world"} into a variable array of HLAunicodeString.
  const auto s1 = encodeHLAunicodeString(u"hi");
  const auto s2 = encodeHLAunicodeString(u"world");
  VariableLengthData bytes;
  // Length prefix (BE): 2 elements.
  bytes.push_back(0); bytes.push_back(0); bytes.push_back(0); bytes.push_back(2);
  bytes.insert(bytes.end(), s1.begin(), s1.end());
  bytes.insert(bytes.end(), s2.begin(), s2.end());

  const auto out =
      decodeHLAvariableArrayVarWidth<std::u16string>(bytes,
                                                     decodeUnicodeWithLen);
  ASSERT_EQ(out.size(), 2u);
  EXPECT_EQ(out[0], u"hi");
  EXPECT_EQ(out[1], u"world");
}

TEST(HLAvariableArrayVarWidth, EmptyArrayDecodesToEmptyVector) {
  VariableLengthData bytes{0, 0, 0, 0};  // length = 0
  const auto out =
      decodeHLAvariableArrayVarWidth<std::u16string>(bytes,
                                                     decodeUnicodeWithLen);
  EXPECT_TRUE(out.empty());
}

TEST(HLAvariableArrayVarWidth, MissingPrefixThrows) {
  VariableLengthData bytes{0, 1};  // not even 4 bytes
  EXPECT_THROW(
      (decodeHLAvariableArrayVarWidth<std::u16string>(bytes,
                                                      decodeUnicodeWithLen)),
      EncodingError);
}

TEST(HLAvariableArrayVarWidth, CallbackConsumingZeroThrows) {
  VariableLengthData bytes{0, 0, 0, 1, 0xAA};  // claim 1 element
  auto bad_decoder = [](const VariableLengthData&) {
    return std::make_pair(0, std::size_t{0});  // pretend consumed nothing
  };
  EXPECT_THROW(
      (decodeHLAvariableArrayVarWidth<int>(bytes, bad_decoder)),
      EncodingError);
}

// --- HLAfixedRecord ---------------------------------------------------------

TEST(HLAfixedRecord, EncodeConcatenatesFields) {
  const auto f1 = encodeHLAinteger32BE(42);
  const auto f2 = encodeHLAfloat64BE(3.14);
  const auto rec = encodeHLAfixedRecord({f1, f2});
  ASSERT_EQ(rec.size(), 12u);  // 4 + 8
  // First 4 bytes are the integer; bytes[4..12] are the double.
  EXPECT_EQ(std::vector<std::uint8_t>(rec.begin(), rec.begin() + 4),
            std::vector<std::uint8_t>(f1.begin(), f1.end()));
  EXPECT_EQ(std::vector<std::uint8_t>(rec.begin() + 4, rec.end()),
            std::vector<std::uint8_t>(f2.begin(), f2.end()));
}

TEST(HLAfixedRecord, DecodeWithOffsetsRecoversFields) {
  const auto f1 = encodeHLAinteger32BE(7);
  const auto f2 = encodeHLAfloat64BE(2.5);
  const auto rec = encodeHLAfixedRecord({f1, f2});
  // f1 starts at offset 0, f2 at offset 4.
  const auto slices = decodeHLAfixedRecord(rec, {0, 4});
  ASSERT_EQ(slices.size(), 2u);
  EXPECT_EQ(decodeHLAinteger32BE(slices[0]), 7);
  EXPECT_DOUBLE_EQ(decodeHLAfloat64BE(slices[1]), 2.5);
}

TEST(HLAfixedRecord, DecodeBadOffsetsThrows) {
  VariableLengthData bytes(8, 0);
  EXPECT_THROW(decodeHLAfixedRecord(bytes, {0, 99}),
               EncodingError);
}

}  // namespace
