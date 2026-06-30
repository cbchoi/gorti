// Lockfile: RTI_THROW macro from <RTI/SpecificConfig.h>.
// Catalogue §1 row 1.5.
//
// M31 RED — fails until M32 lands `RTI/SpecificConfig.h` with RTI_EXPORT,
// RTI_NOEXCEPT, and RTI_THROW.
//
// The spec uses `RTI_THROW(ExceptionA, ExceptionB)` as the exception
// specification on every public ambassador and callback method. The macro
// must therefore be usable as a method exception spec without breaking
// the surrounding declaration. Under C++17, dynamic exception specifications
// are removed from the language, so the macro must expand to a noexcept-safe
// form (typically empty / `noexcept(false)` / a comment) per §3.1.0 of the
// compliance program doc.
//
// This TU exercises the macro in the only two positions the spec uses it:
//   1. As a method exception specification on a free function.
//   2. As a method exception specification on a member function.

#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>

namespace {

// Position #1 — free function exception spec.
void freeFunctionThrowsA() RTI_THROW(rti1516e::Exception);

// Position #2 — member function exception spec.
struct UsesMacro {
  void memberThrowsA() const RTI_THROW(rti1516e::Exception);
  void memberThrowsAB() const RTI_THROW(rti1516e::Exception, rti1516e::Exception);
};

// Compile-time existence check on RTI_EXPORT (the other companion macro):
// RTI_EXPORT must expand to something that can prefix a class declaration.
class RTI_EXPORT UsesExport {
 public:
  void method() RTI_NOEXCEPT;
};

}  // namespace
