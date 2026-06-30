// Lockfile: MOM delivery via standard publish/subscribe per IEEE 1516.1-2010
// §11. The spec mandates that the Management Object Model (MOM) is exposed
// EXCLUSIVELY via the standard object-management subscription path on
// HLAobjectRoot.HLAmanager.* — there are NO bespoke MOM methods on
// RTIambassador.
//
// gorti M17 grew helpers `queryFederationAttributes`, `queryFederateAttributes`,
// `enumerateMomInstances` (RtiAmbassador.h:552-586). The DLC surface drops
// them all (catalogue row 16.1). This TU lock is a *positive* spec-shape
// assertion: it instantiates the canonical pub/sub call chain at the MOM
// object class to prove MOM is reachable through the standard surface.
//
// We cannot directly assert "method X does NOT exist" via static_assert,
// but the surface intent is locked by: (a) the conformance fixture
// mom_federation_lifecycle (Agent C/D) drives MOM through standard
// callbacks; (b) test_rtiambassador_signatures.cpp's `is_abstract_v`
// assertion forbids any pimpl that smuggled MOM helpers; (c) we re-assert
// the standard subscribeObjectClassAttributes path here for redundancy.
//
// Catalogue rows covered: 16.1.

#include <RTI/RTIambassador.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/VariableLengthData.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <string>

namespace {

using rti1516e::RTIambassador;
using rti1516e::ObjectClassHandle;
using rti1516e::AttributeHandle;
using rti1516e::AttributeHandleSet;
using rti1516e::ObjectInstanceHandle;

// §11 — MOM federation lifecycle is observed via standard
//        subscribeObjectClassAttributes against HLAobjectRoot.HLAmanager.HLAfederation.
//        The class is resolved via getObjectClassHandle("HLAobjectRoot.HLAmanager.HLAfederation").
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getObjectClassHandle(
        std::declval<std::wstring const&>())),
    ObjectClassHandle>);

// §11 — the federate publishes/subscribes MOM exactly like any object class.
//        No special "publishMOM" or "subscribeMOM" helper.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().subscribeObjectClassAttributes(
        std::declval<ObjectClassHandle>(),
        std::declval<AttributeHandleSet const&>())),
    void>);

// §11 — discovered MOM instances arrive on the standard
//        FederateAmbassador::discoverObjectInstance(...). The fact that
//        getKnownObjectClassHandle works on MOM-discovered instances is
//        guaranteed by the §6 surface (already locked in test_rtiambassador_handle_services.cpp).

// §11 — the spec does NOT define `queryFederationAttributes` or
//        `queryFederateAttributes` on RTIambassador (catalogue 16.1).
//        We document the negation here as a soft-lock; the strong lock is
//        Agent E's `RTI/RTIambassador.h` stub omitting these methods.
//        Once Agent E's stub lands, federates that call the M17 helpers
//        will fail to compile — that is the per-assertion RED signal for
//        catalogue row 16.1.

// §11 — MOM-related exception types.
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::NameNotFound>);

// §11 — MOM well-known object class names are wstring-keyed (FR-DLC-4).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().getObjectInstanceName(
        std::declval<ObjectInstanceHandle>())),
    std::wstring>);

}  // namespace
