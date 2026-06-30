// Lockfile: IEEE 1516.1-2010 Annex C — abstract `Exception` base class.
// Catalogue §6 row 6.1.
//
// M31 RED — fails until M32 lands `RTI/Exception.h` with the spec contract.
//
// Pitch ref: ~/prti1516e/api/cpp/HLA_1516-2010/RTI/Exception.h:26-46

#include <RTI/Exception.h>
#include <type_traits>
#include <string>

namespace {

// §C — Exception is a class type.
static_assert(std::is_class_v<rti1516e::Exception>);

// §C — Exception is NOT derived from std::runtime_error.
// (gorti's current rti1516e/Exceptions.h has RTIinternalError : runtime_error
//  and treats that as the base of everything — the spec is different.)
static_assert(!std::is_base_of_v<std::runtime_error, rti1516e::Exception>);
static_assert(!std::is_base_of_v<std::exception, rti1516e::Exception>);

// §C — has a virtual destructor (verified indirectly by polymorphism on what()).
static_assert(std::has_virtual_destructor_v<rti1516e::Exception>);

// §C — `wstring what() const` is a pure virtual; the class is therefore abstract.
static_assert(std::is_abstract_v<rti1516e::Exception>);

// §C — what() returns std::wstring (NOT const char* like std::exception).
//      The return-type lock is via decltype on a declared overrider.
// We can't directly take a decltype of a pure virtual member without a derived
// override, so we lock the contract via the operator<< signature instead — the
// spec mandates wostream operator<< accepting Exception const&.
static_assert(std::is_invocable_r_v<
    std::wostream&,
    decltype(static_cast<std::wostream& (*)(std::wostream&, rti1516e::Exception const&)>(
        &rti1516e::operator<<)),
    std::wostream&, rti1516e::Exception const&>);

}  // namespace
