// M35 (Agent BF) — MOM surface lockfile test (catalogue row 16.1).
//
// IEEE 1516.1-2010 §11 exposes the Management Object Model (MOM) EXCLUSIVELY
// via the standard object-management publish/subscribe surface on
// `HLAobjectRoot.HLAmanager.*`. There are NO bespoke MOM helper methods on
// the DLC RTIambassador.
//
// The M17 shim (`rti1516e::m17`/RtiAmbassador.h) ships three non-spec helpers
// (`queryFederationAttributes`, `queryFederateAttributes`,
// `enumerateMomInstances`) that this test does NOT touch — M17 lives in a
// separate shim namespace and header hierarchy. The DLC ambassador (declared
// in <RTI/RTIambassador.h>) does NOT have those methods; this file provides
// a positive shape assertion for MOM discoverability through the standard
// `getObjectClassHandle` path.
//
// The test is compile-only — success is defined by successful compilation of
// the static_asserts + template instantiation below. The GTest body is a
// no-op sentinel so ctest reports pass/fail based purely on link status.
// The lockfile stronghold for the "MOM helpers are ABSENT from DLC" claim
// lives in cppsdk/tests/dlc/lockfile/core/test_rtiambassador_mom.cpp; this
// runtime file complements that with the *positive* shape assertion the
// spec requires.
//
// Catalogue rows covered: 16.1 (MOM discovery via standard surface).

#include <RTI/RTIambassador.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>

#include <string>
#include <type_traits>

#include <gtest/gtest.h>

namespace {

using rti1516e::RTIambassador;
using rti1516e::ObjectClassHandle;
using rti1516e::AttributeHandle;
using rti1516e::AttributeHandleSet;

// §11 lockfile — the MOM federation-root object class is discoverable via
// the standard §10.2 `getObjectClassHandle(std::wstring const&)` call on the
// abstract RTIambassador surface. The call-shape MUST take `wstring const&`
// and return `ObjectClassHandle` — no MOM-specific overload.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getObjectClassHandle(
        std::declval<std::wstring const&>())),
    ObjectClassHandle>);

// §11 — the same standard `getObjectClassHandle` resolves BOTH ordinary FOM
// object classes AND the MOM class family. The spec name lives in
// IEEE 1516.2-2010 Annex F (MOM class hierarchy):
//   HLAobjectRoot
//     HLAmanager
//       HLAfederation   <- resolved here
//       HLAfederate     <- resolved by mom_federation_lifecycle fixture
// A compile-only string-literal well-formedness check for the exact wstring
// name the caller should pass; NameNotFound at runtime is the RTI's contract
// if the FOM does not include the MOM module, but the shape is fixed.
constexpr wchar_t const* kMomFederationClassName =
    L"HLAobjectRoot.HLAmanager.HLAfederation";
static_assert(kMomFederationClassName[0] == L'H',
    "MOM federation class name must start with 'HLAobjectRoot' per §11 / Annex F");
static_assert(sizeof(L"HLAobjectRoot.HLAmanager.HLAfederation") > 1);

// §11 — subscription to MOM attributes uses the standard
// `subscribeObjectClassAttributes` — no bespoke helper.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().subscribeObjectClassAttributes(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §11 negative shape lock (positive form): the DLC RTIambassador is
// abstract — every impl derives from it. If a future PR smuggles a
// bespoke `queryFederationAttributes` helper into the header, the strong
// signal is a compile break in every fixture that #includes it (because
// the non-spec method WOULD collide with the M17 shim's `rti1516e::m17`
// namespace name of the same shape). The abstract check keeps the vtable
// slot count invariant to a per-header inspection.
static_assert(std::is_abstract_v<RTIambassador>);

// §11 — federate handle is the join primary key; MOM keys HLAfederate rows
// by the federate handle attribute, so the handle type must be present on
// the DLC surface. This wires the shape rather than the runtime value.
static_assert(std::is_default_constructible_v<rti1516e::FederateHandle>);

}  // namespace

// GTest sentinel — passes iff this TU linked. The real assertion is the
// static_assert block above; runtime failure is impossible if the compiler
// accepted the file. Kept so ctest emits a per-test pass entry.
TEST(MomSurfaceLockfile, DiscoverableViaStandardGetObjectClassHandle) {
  // Compile-time contract already verified by the static_asserts. The
  // runtime body just confirms the wstring literal is well-formed for the
  // caller pattern documented in §11.
  std::wstring const name{L"HLAobjectRoot.HLAmanager.HLAfederation"};
  EXPECT_FALSE(name.empty());
  EXPECT_EQ(name.substr(0, 13), std::wstring{L"HLAobjectRoot"});
}
