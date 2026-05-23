// M17.1 — unit tests for rti1516e::RTIambassador construction +
// URL parsing. No rtid subprocess; these tests catch the
// compile-and-link path and the trivial sanity checks.

#include <gtest/gtest.h>

#include <utility>

#include "rti1516e/Exceptions.h"
#include "rti1516e/RtiAmbassador.h"

namespace {

TEST(RtiAmbassadorUnit, IsNotConnectedAtConstruction) {
  rti1516e::RTIambassador amb;
  EXPECT_FALSE(amb.isConnected());
}

TEST(RtiAmbassadorUnit, ConnectThenIsConnected) {
  rti1516e::RTIambassador amb;
  amb.connect("grpc://127.0.0.1:12345");
  EXPECT_TRUE(amb.isConnected());
}

TEST(RtiAmbassadorUnit, ConnectTwiceThrowsAlreadyConnected) {
  rti1516e::RTIambassador amb;
  amb.connect("grpc://127.0.0.1:12345");
  EXPECT_THROW(amb.connect("grpc://127.0.0.1:12345"),
               rti1516e::AlreadyConnected);
}

TEST(RtiAmbassadorUnit, DisconnectIsIdempotent) {
  rti1516e::RTIambassador amb;
  amb.connect("grpc://127.0.0.1:12345");
  amb.disconnect();
  EXPECT_FALSE(amb.isConnected());
  amb.disconnect();  // safe to call again
  EXPECT_FALSE(amb.isConnected());
}

TEST(RtiAmbassadorUnit, ConnectAfterDisconnectWorks) {
  rti1516e::RTIambassador amb;
  amb.connect("grpc://127.0.0.1:12345");
  amb.disconnect();
  amb.connect("grpc://127.0.0.1:12345");
  EXPECT_TRUE(amb.isConnected());
}

TEST(RtiAmbassadorUnit, ConnectMalformedUrlThrows) {
  rti1516e::RTIambassador amb;
  EXPECT_THROW(amb.connect("http://nope"), rti1516e::ConnectionFailed);
  EXPECT_THROW(amb.connect(""), rti1516e::ConnectionFailed);
  EXPECT_THROW(amb.connect("localhost:12345"), rti1516e::ConnectionFailed);
  EXPECT_FALSE(amb.isConnected());
}

TEST(RtiAmbassadorUnit, MoveConstructorTransfersState) {
  rti1516e::RTIambassador a;
  a.connect("grpc://127.0.0.1:12345");
  rti1516e::RTIambassador b(std::move(a));
  EXPECT_TRUE(b.isConnected());
}

// Strong-typedef property tests.

TEST(Handles, InvalidByDefault) {
  rti1516e::ObjectClassHandle h;
  EXPECT_FALSE(h.isValid());
  EXPECT_EQ(h.raw(), 0u);
}

TEST(Handles, ValidWhenNonZero) {
  rti1516e::ObjectClassHandle h(7);
  EXPECT_TRUE(h.isValid());
  EXPECT_TRUE(static_cast<bool>(h));
  EXPECT_EQ(h.raw(), 7u);
}

TEST(Handles, EqualityAndOrdering) {
  rti1516e::AttributeHandle a(3);
  rti1516e::AttributeHandle b(3);
  rti1516e::AttributeHandle c(5);
  EXPECT_EQ(a, b);
  EXPECT_NE(a, c);
  EXPECT_LT(a, c);
}

TEST(Handles, DifferentTagsDoNotMixAtCompileTime) {
  // The following lines would be a compile error if uncommented —
  // verifying via static_assert on the typedefs.
  static_assert(
      !std::is_same_v<rti1516e::ObjectClassHandle, rti1516e::AttributeHandle>,
      "Strong typedef should keep these distinct");
}

}  // namespace
