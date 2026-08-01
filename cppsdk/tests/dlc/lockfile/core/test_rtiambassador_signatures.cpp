// Lockfile: RTIambassador class shape per IEEE 1516.1-2010 §10.
// Locks: pure-abstract class, virtual dtor, namespace, protected ctor visibility.
// RED in M31: fails to compile until RTI/RTIambassador.h declares the spec
// surface. Each static_assert tagged with the spec § it locks.
//
// Catalogue rows covered: 1.1, 1.3, 2.1, 2.3, 2.4, 2.5.

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/FederateAmbassador.h>
#include <RTI/Exception.h>
#include <type_traits>

namespace {

using rti1516e::RTIambassador;
using rti1516e::RTIambassadorFactory;
using rti1516e::FederateAmbassador;

// §10 / Annex A — RTIambassador lives in namespace rti1516e (catalogue 1.3)
static_assert(std::is_class_v<RTIambassador>);

// §10.6.1 — RTIambassador is polymorphic (every method virtual; dtor virtual).
//           Catalogue 2.3, 2.4.
static_assert(std::is_polymorphic_v<RTIambassador>);

// §10 / catalogue 2.4 — virtual destructor (so federate code can `delete amb;`
//                       through a base-class pointer obtained from the factory).
static_assert(std::has_virtual_destructor_v<RTIambassador>);

// §10.6.1 / catalogue 2.1, 2.3 — pure-abstract: cannot be instantiated
//                                directly; federates must go through the factory.
static_assert(std::is_abstract_v<RTIambassador>);

// §10 / catalogue 2.1 — federate may NOT default-construct an RTIambassador
//                       (ctor is protected per reference_rtiambassador.h:37-42).
static_assert(!std::is_default_constructible_v<RTIambassador>);

// §10 / catalogue 2.5 — once abstract, copy / move slicing is moot; the
//                       factory hands out a smart-pointer wrapper. Verify
//                       that RTIambassador is not copy- or move-constructible
//                       from the public surface.
static_assert(!std::is_copy_constructible_v<RTIambassador>);
static_assert(!std::is_move_constructible_v<RTIambassador>);
static_assert(!std::is_copy_assignable_v<RTIambassador>);
static_assert(!std::is_move_assignable_v<RTIambassador>);

// §10 — RTIambassador is not derived from std::exception or any STL type.
//        Sibling to the rti1516e::Exception hierarchy, not subclass of it.
static_assert(!std::is_base_of_v<rti1516e::Exception, RTIambassador>);

// §10 — RTIambassadorFactory is the sole construction path (catalogue 2.2).
//        It is a concrete class (NOT pure-abstract) — federates instantiate
//        it directly and call createRTIambassador().
static_assert(std::is_class_v<RTIambassadorFactory>);
static_assert(!std::is_abstract_v<RTIambassadorFactory>);
static_assert(std::is_default_constructible_v<RTIambassadorFactory>);
static_assert(std::has_virtual_destructor_v<RTIambassadorFactory>);

// §10 — FederateAmbassador is the callback interface. Also pure-abstract
//        per reference_rti FederateAmbassador.h:32-41 (catalogue 4.1).
static_assert(std::is_class_v<FederateAmbassador>);
static_assert(std::is_polymorphic_v<FederateAmbassador>);
static_assert(std::has_virtual_destructor_v<FederateAmbassador>);
static_assert(std::is_abstract_v<FederateAmbassador>);

// §10 — RTIambassador and FederateAmbassador are NOT related by inheritance.
//        The ambassador HOLDS a reference to the federate-ambassador via
//        connect(); they are not subclasses of one another.
static_assert(!std::is_base_of_v<RTIambassador, FederateAmbassador>);
static_assert(!std::is_base_of_v<FederateAmbassador, RTIambassador>);

}  // namespace
