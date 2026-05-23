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

}  // namespace
