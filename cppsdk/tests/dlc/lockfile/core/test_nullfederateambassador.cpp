// Lockfile: NullFederateAmbassador per IEEE 1516.1-2010 §4 (and reference_rti
// NullFederateAmbassador.h). The spec ships TWO callback classes:
//   - FederateAmbassador: pure-abstract, every method `= 0`
//   - NullFederateAmbassador: concrete subclass whose method bodies are empty
//                              (gives federates a default no-op base they
//                              can selectively override).
//
// Catalogue rows covered: 4.2 (NullFederateAmbassador absent in gorti M17).

#include <RTI/NullFederateAmbassador.h>
#include <RTI/FederateAmbassador.h>
#include <type_traits>

namespace {

using rti1516e::FederateAmbassador;
using rti1516e::NullFederateAmbassador;

// §4 / Annex A — NullFederateAmbassador IS-A FederateAmbassador.
static_assert(std::is_class_v<NullFederateAmbassador>);
static_assert(std::is_base_of_v<FederateAmbassador, NullFederateAmbassador>);

// §4 — NullFederateAmbassador is CONCRETE (not abstract): federates can
//       instantiate it directly and override only the callbacks they care
//       about. This is the headline distinction from FederateAmbassador.
static_assert(!std::is_abstract_v<NullFederateAmbassador>);

// §10 — has a virtual destructor inherited from FederateAmbassador.
static_assert(std::has_virtual_destructor_v<NullFederateAmbassador>);

// §10 — polymorphic (it has overrides on every virtual).
static_assert(std::is_polymorphic_v<NullFederateAmbassador>);

// §4 — federate code uses NullFederateAmbassador as a base for its own
//       callback class. Verify it is default-constructible (gives an
//       empty no-op handler).
static_assert(std::is_default_constructible_v<NullFederateAmbassador>);

// §4 — reference_rti's NullFederateAmbassador.h:32-36 declares both the ctor and
//       dtor as RTI_THROW(FederateInternalError). The actual exception
//       declaration is locked in test_exception_*; here we lock that the
//       class exists as a concrete spec-shaped subclass.

}  // namespace
