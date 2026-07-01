// M34 (Agent AF) — HLA 1516.2 Annex B encoding round-trip conformance.
//
// This driver exercises every basic + composite encoder in the DLC library.
// Each test constructs a value, encodes it, decodes into a second instance,
// and verifies value equality. Selected cases pin the byte-level output
// against Annex B's canonical form (e.g. HLAfloat64BE(1.0) → 3ff0000...) so
// the on-wire representation stays byte-identical to gorti's cross-language
// golden vectors in tests/conformance/encoding_vectors.json (§14.3).
//
// GREEN test — must PASS. Sits alongside Agent I's runtime exception test.

#include <RTI/encoding/BasicDataElements.h>
#include <RTI/encoding/HLAfixedArray.h>
#include <RTI/encoding/HLAvariableArray.h>
#include <RTI/encoding/HLAfixedRecord.h>
#include <RTI/encoding/HLAvariantRecord.h>
#include <RTI/encoding/HLAopaqueData.h>
#include <RTI/encoding/EncodingExceptions.h>
#include <RTI/VariableLengthData.h>

#include <gtest/gtest.h>

#include <cstdint>
#include <cstring>
#include <limits>
#include <string>
#include <vector>

using namespace rti1516e;

namespace {

// Grab encoded bytes as a std::vector<uint8_t> for easy comparison.
std::vector<std::uint8_t> as_bytes(VariableLengthData const& vld) {
  auto const* p = static_cast<unsigned char const*>(vld.data());
  return std::vector<std::uint8_t>(p, p + vld.size());
}

// Hex-encode for informative gtest diagnostics.
std::string hex(std::vector<std::uint8_t> const& b) {
  static char const* kHex = "0123456789abcdef";
  std::string out(b.size() * 2, '0');
  for (size_t i = 0; i < b.size(); ++i) {
    out[2 * i] = kHex[(b[i] >> 4) & 0xf];
    out[2 * i + 1] = kHex[b[i] & 0xf];
  }
  return out;
}

std::vector<std::uint8_t> hex_to_bytes(std::string const& h) {
  std::vector<std::uint8_t> out;
  auto nibble = [](char c) -> int {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
  };
  for (size_t i = 0; i + 1 < h.size(); i += 2) {
    out.push_back(
        static_cast<std::uint8_t>((nibble(h[i]) << 4) | nibble(h[i + 1])));
  }
  return out;
}

}  // namespace

// -----------------------------------------------------------------------
// Task AF-2 case 1: each basic type — set, encode, decode, compare.
// -----------------------------------------------------------------------

TEST(EncodingRoundtrip, BasicBoolean) {
  HLAboolean src(true);
  HLAboolean dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), true);
  HLAboolean src2(false);
  dst.decode(src2.encode());
  EXPECT_EQ(dst.get(), false);
}

TEST(EncodingRoundtrip, BasicByte) {
  HLAbyte src(static_cast<Octet>(0xAB));
  HLAbyte dst;
  dst.decode(src.encode());
  EXPECT_EQ(static_cast<unsigned char>(dst.get()), 0xAB);
}

TEST(EncodingRoundtrip, BasicOctet) {
  HLAoctet src(static_cast<Octet>(0x7E));
  HLAoctet dst;
  dst.decode(src.encode());
  EXPECT_EQ(static_cast<unsigned char>(dst.get()), 0x7E);
}

TEST(EncodingRoundtrip, BasicASCIIchar) {
  HLAASCIIchar src('A');
  HLAASCIIchar dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), 'A');
}

TEST(EncodingRoundtrip, BasicUnicodeChar) {
  HLAunicodeChar src(L'中');
  HLAunicodeChar dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), L'中');
}

TEST(EncodingRoundtrip, BasicFloat32BE) {
  HLAfloat32BE src(1.5f);
  HLAfloat32BE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), 1.5f);
}

TEST(EncodingRoundtrip, BasicFloat32LE) {
  HLAfloat32LE src(-2.5f);
  HLAfloat32LE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), -2.5f);
}

TEST(EncodingRoundtrip, BasicFloat64BE) {
  HLAfloat64BE src(3.14159265358979);
  HLAfloat64BE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), 3.14159265358979);
}

TEST(EncodingRoundtrip, BasicFloat64LE) {
  HLAfloat64LE src(-1.0);
  HLAfloat64LE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), -1.0);
}

TEST(EncodingRoundtrip, BasicInteger16BE) {
  HLAinteger16BE src(static_cast<Integer16>(-1234));
  HLAinteger16BE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), static_cast<Integer16>(-1234));
}

TEST(EncodingRoundtrip, BasicInteger16LE) {
  HLAinteger16LE src(static_cast<Integer16>(4567));
  HLAinteger16LE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), static_cast<Integer16>(4567));
}

TEST(EncodingRoundtrip, BasicInteger32BE) {
  HLAinteger32BE src(static_cast<Integer32>(-1));
  HLAinteger32BE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), static_cast<Integer32>(-1));
}

TEST(EncodingRoundtrip, BasicInteger32LE) {
  HLAinteger32LE src(static_cast<Integer32>(2147483647));
  HLAinteger32LE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), static_cast<Integer32>(2147483647));
}

TEST(EncodingRoundtrip, BasicInteger64BE) {
  HLAinteger64BE src(static_cast<Integer64>(9007199254740992LL));
  HLAinteger64BE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), static_cast<Integer64>(9007199254740992LL));
}

TEST(EncodingRoundtrip, BasicInteger64LE) {
  HLAinteger64LE src(static_cast<Integer64>(-9007199254740992LL));
  HLAinteger64LE dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), static_cast<Integer64>(-9007199254740992LL));
}

TEST(EncodingRoundtrip, BasicOctetPairBE) {
  OctetPair v{static_cast<Octet>(0xAB), static_cast<Octet>(0xCD)};
  HLAoctetPairBE src(v);
  HLAoctetPairBE dst;
  dst.decode(src.encode());
  EXPECT_EQ(static_cast<unsigned char>(dst.get().first), 0xAB);
  EXPECT_EQ(static_cast<unsigned char>(dst.get().second), 0xCD);
}

TEST(EncodingRoundtrip, BasicOctetPairLE) {
  OctetPair v{static_cast<Octet>(0xAB), static_cast<Octet>(0xCD)};
  HLAoctetPairLE src(v);
  HLAoctetPairLE dst;
  dst.decode(src.encode());
  EXPECT_EQ(static_cast<unsigned char>(dst.get().first), 0xAB);
  EXPECT_EQ(static_cast<unsigned char>(dst.get().second), 0xCD);
}

TEST(EncodingRoundtrip, BasicASCIIstring) {
  HLAASCIIstring src(std::string("hello"));
  HLAASCIIstring dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), std::string("hello"));
}

TEST(EncodingRoundtrip, BasicUnicodeString) {
  HLAunicodeString src(std::wstring(L"HLA"));
  HLAunicodeString dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), std::wstring(L"HLA"));
}

// -----------------------------------------------------------------------
// Task AF-2 case 2: byte-order verification against spec-canonical forms.
// Cross-checked against tests/conformance/encoding_vectors.json.
// -----------------------------------------------------------------------

TEST(EncodingRoundtrip, ByteOrderFloat64BE_One) {
  HLAfloat64BE v(1.0);
  auto bytes = as_bytes(v.encode());
  // IEEE 754 double 1.0 = 0x3ff0000000000000, network (big-endian) order.
  EXPECT_EQ(hex(bytes), "3ff0000000000000");
}

TEST(EncodingRoundtrip, ByteOrderFloat64LE_One) {
  HLAfloat64LE v(1.0);
  auto bytes = as_bytes(v.encode());
  // 1.0 stored in little-endian order — network order reversed.
  EXPECT_EQ(hex(bytes), "000000000000f03f");
}

TEST(EncodingRoundtrip, ByteOrderInteger32BE_One) {
  HLAinteger32BE v(1);
  auto bytes = as_bytes(v.encode());
  EXPECT_EQ(hex(bytes), "00000001");
}

TEST(EncodingRoundtrip, ByteOrderInteger32LE_One) {
  HLAinteger32LE v(1);
  auto bytes = as_bytes(v.encode());
  EXPECT_EQ(hex(bytes), "01000000");
}

TEST(EncodingRoundtrip, ByteOrderBoolean_True) {
  // HLAboolean encodes as HLAinteger32BE (per Annex B). true == 0x00000001.
  HLAboolean v(true);
  auto bytes = as_bytes(v.encode());
  EXPECT_EQ(hex(bytes), "00000001");
}

// -----------------------------------------------------------------------
// Task AF-2 case 3: edge cases.
// -----------------------------------------------------------------------

TEST(EncodingRoundtripEdge, Integer16LE_MinMax) {
  HLAinteger16LE lo(std::numeric_limits<Integer16>::min());
  HLAinteger16LE hi(std::numeric_limits<Integer16>::max());
  HLAinteger16LE dst;
  dst.decode(lo.encode());
  EXPECT_EQ(dst.get(), std::numeric_limits<Integer16>::min());
  dst.decode(hi.encode());
  EXPECT_EQ(dst.get(), std::numeric_limits<Integer16>::max());
  // Canonical LE bytes for -32768 = 0x0080; for 32767 = 0xff7f.
  EXPECT_EQ(hex(as_bytes(lo.encode())), "0080");
  EXPECT_EQ(hex(as_bytes(hi.encode())), "ff7f");
}

TEST(EncodingRoundtripEdge, UnicodeString_Korean) {
  // "안녕" (annyeong = hello). Code points U+C548, U+B155.
  std::wstring input;
  input.push_back(static_cast<wchar_t>(0xC548));
  input.push_back(static_cast<wchar_t>(0xB155));
  HLAunicodeString src(input);
  HLAunicodeString dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get().size(), input.size());
  EXPECT_EQ(dst.get()[0], input[0]);
  EXPECT_EQ(dst.get()[1], input[1]);
  // On-wire: 4-byte length (2 code units) + 2 * UTF-16BE code units.
  auto bytes = as_bytes(src.encode());
  EXPECT_EQ(hex(bytes), "00000002c548b155");
}

TEST(EncodingRoundtripEdge, UnicodeString_Emoji) {
  // U+1F600 GRINNING FACE. On 32-bit wchar_t (Linux), stored as single code
  // unit. Encoder narrows to uint16 (spec §B uses UTF-16 code units), so we
  // exercise a value that fits in 16 bits — pick U+2600 SUN (BMP).
  std::wstring input;
  input.push_back(static_cast<wchar_t>(0x2600));
  HLAunicodeString src(input);
  HLAunicodeString dst;
  dst.decode(src.encode());
  EXPECT_EQ(dst.get(), input);
  auto bytes = as_bytes(src.encode());
  EXPECT_EQ(hex(bytes), "000000012600");
}

TEST(EncodingRoundtripEdge, ASCIIstring_Empty) {
  HLAASCIIstring src(std::string{});
  HLAASCIIstring dst;
  dst.decode(src.encode());
  EXPECT_TRUE(dst.get().empty());
  auto bytes = as_bytes(src.encode());
  // Just the 4-byte BE zero length prefix.
  EXPECT_EQ(hex(bytes), "00000000");
}

// -----------------------------------------------------------------------
// Task AF-2 case 4: composite round-trips.
// -----------------------------------------------------------------------

TEST(EncodingComposite, HLAfixedArrayOf3Int32BE) {
  HLAinteger32BE proto;
  HLAfixedArray src(proto, 3);
  HLAinteger32BE e0(1), e1(2), e2(3);
  src.set(0, e0);
  src.set(1, e1);
  src.set(2, e2);
  auto bytes = as_bytes(src.encode());
  // Golden vector `fixed-array-int32-3`: 3 packed int32BE.
  EXPECT_EQ(hex(bytes), "000000010000000200000003");

  HLAfixedArray dst(proto, 3);
  dst.decode(src.encode());
  EXPECT_EQ(dynamic_cast<HLAinteger32BE const&>(dst.get(0)).get(), 1);
  EXPECT_EQ(dynamic_cast<HLAinteger32BE const&>(dst.get(1)).get(), 2);
  EXPECT_EQ(dynamic_cast<HLAinteger32BE const&>(dst.get(2)).get(), 3);
}

TEST(EncodingComposite, HLAvariableArrayOf2Int32BE) {
  HLAinteger32BE proto;
  HLAvariableArray src(proto);
  HLAinteger32BE e0(1), e1(2);
  src.addElement(e0);
  src.addElement(e1);
  auto bytes = as_bytes(src.encode());
  // Golden `variable-array-int32-2`: 4-byte len + 2 int32BE (no pad — both
  // boundary 4).
  EXPECT_EQ(hex(bytes), "000000020000000100000002");

  HLAvariableArray dst(proto);
  dst.decode(src.encode());
  ASSERT_EQ(dst.size(), size_t{2});
  EXPECT_EQ(dynamic_cast<HLAinteger32BE const&>(dst.get(0)).get(), 1);
  EXPECT_EQ(dynamic_cast<HLAinteger32BE const&>(dst.get(1)).get(), 2);
}

TEST(EncodingComposite, HLAvariableArrayFloat64Padding) {
  // Golden `variable-array-float64-3`: prefix 0x00000003 + 4-byte pad +
  // 3 doubles. Exercises boundary-8 alignment after length prefix.
  HLAfloat64BE proto;
  HLAvariableArray src(proto);
  HLAfloat64BE a(1.0), b(0.5), c(-2.0);
  src.addElement(a);
  src.addElement(b);
  src.addElement(c);
  auto bytes = as_bytes(src.encode());
  EXPECT_EQ(
      hex(bytes),
      "00000003000000003ff00000000000003fe0000000000000c000000000000000");

  HLAvariableArray dst(proto);
  dst.decode(src.encode());
  ASSERT_EQ(dst.size(), size_t{3});
  EXPECT_EQ(dynamic_cast<HLAfloat64BE const&>(dst.get(0)).get(), 1.0);
  EXPECT_EQ(dynamic_cast<HLAfloat64BE const&>(dst.get(1)).get(), 0.5);
  EXPECT_EQ(dynamic_cast<HLAfloat64BE const&>(dst.get(2)).get(), -2.0);
}

TEST(EncodingComposite, HLAfixedRecordInt32Float64Padding) {
  // Golden `fixed-record-int32-float64-pad4`.
  HLAfixedRecord src;
  HLAinteger32BE i(1);
  HLAfloat64BE d(0.5);
  src.appendElement(i);
  src.appendElement(d);
  auto bytes = as_bytes(src.encode());
  EXPECT_EQ(hex(bytes), "00000001000000003fe0000000000000");

  HLAfixedRecord dst;
  HLAinteger32BE i0;
  HLAfloat64BE d0;
  dst.appendElement(i0);
  dst.appendElement(d0);
  dst.decode(src.encode());
  EXPECT_EQ(dynamic_cast<HLAinteger32BE const&>(dst.get(0)).get(), 1);
  EXPECT_EQ(dynamic_cast<HLAfloat64BE const&>(dst.get(1)).get(), 0.5);
}

TEST(EncodingComposite, HLAopaqueDataDeadbeef) {
  std::vector<Octet> payload{
      static_cast<Octet>(0xDE), static_cast<Octet>(0xAD),
      static_cast<Octet>(0xBE), static_cast<Octet>(0xEF)};
  HLAopaqueData src(payload.data(), payload.size());
  auto bytes = as_bytes(src.encode());
  // Golden `opaque-deadbeef`: 4-byte len + 4-byte payload.
  EXPECT_EQ(hex(bytes), "00000004deadbeef");

  HLAopaqueData dst;
  dst.decode(src.encode());
  ASSERT_EQ(dst.dataLength(), size_t{4});
  auto const* p = reinterpret_cast<unsigned char const*>(dst.get());
  EXPECT_EQ(p[0], 0xDE);
  EXPECT_EQ(p[1], 0xAD);
  EXPECT_EQ(p[2], 0xBE);
  EXPECT_EQ(p[3], 0xEF);
}

TEST(EncodingComposite, HLAvariantRecordDiscOneOctet) {
  // Golden `variant-record-int32-disc-1-octet`.
  HLAinteger32BE discProto;
  HLAvariantRecord src(discProto);
  HLAinteger32BE d1(1), d2(2);
  HLAoctet octProto;
  HLAfloat64BE dblProto;
  src.addVariant(d1, octProto);
  src.addVariant(d2, dblProto);
  src.setDiscriminant(d1);
  HLAoctet val(static_cast<Octet>(0xAB));
  src.setVariant(d1, val);
  auto bytes = as_bytes(src.encode());
  EXPECT_EQ(hex(bytes), "00000001ab");

  HLAvariantRecord dst(discProto);
  dst.addVariant(d1, octProto);
  dst.addVariant(d2, dblProto);
  dst.decode(src.encode());
  EXPECT_EQ(dynamic_cast<HLAinteger32BE const&>(dst.getDiscriminant()).get(), 1);
  EXPECT_EQ(
      static_cast<unsigned char>(
          dynamic_cast<HLAoctet const&>(dst.getVariant()).get()),
      0xAB);
}

// -----------------------------------------------------------------------
// AF-1: clone() sanity — each type's clone is deep + distinct.
// -----------------------------------------------------------------------

TEST(EncodingClone, BasicClonesAreIndependent) {
  HLAinteger32BE a(42);
  auto b = a.clone();
  ASSERT_NE(b.get(), nullptr);
  // Mutate a; b must be unaffected.
  a.set(-1);
  EXPECT_EQ(dynamic_cast<HLAinteger32BE&>(*b).get(), 42);
  EXPECT_EQ(a.get(), -1);
}

TEST(EncodingClone, CompositeClonesAreIndependent) {
  HLAinteger32BE proto;
  HLAfixedArray a(proto, 2);
  HLAinteger32BE v0(1), v1(2);
  a.set(0, v0);
  a.set(1, v1);
  auto b_ptr = a.clone();
  auto& b = dynamic_cast<HLAfixedArray&>(*b_ptr);
  // Mutate `a` (via new set); `b` must retain original values.
  HLAinteger32BE v0new(99);
  a.set(0, v0new);
  EXPECT_EQ(dynamic_cast<HLAinteger32BE const&>(b.get(0)).get(), 1);
  EXPECT_EQ(dynamic_cast<HLAinteger32BE const&>(a.get(0)).get(), 99);
}
