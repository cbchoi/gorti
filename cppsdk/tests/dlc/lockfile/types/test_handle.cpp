// Lockfile: IEEE 1516.1-2010 RTI/Handle.h — DEFINE_HANDLE_CLASS pattern.
// Catalogue §7 rows 7.1-7.7. Locks all 9 spec-mandated typed handle classes.
//
// M31 RED — fails until M32 lands `RTI/Handle.h` with the per-class shape.
//
// Each handle class produced by DEFINE_HANDLE_CLASS must expose:
//   * default ctor producing an invalid handle
//   * copy ctor + copy-assignment
//   * dtor
//   * bool isValid() const
//   * operator==, operator!=, operator<  (heterogeneous-unsafe, same-type only)
//   * long hash() const
//   * VariableLengthData encode() const
//   * void encode(VariableLengthData&) const
//   * size_t encode(void*, size_t) const
//   * size_t encodedLength() const
//   * std::wstring toString() const
// And a free function:
//   * std::wostream& operator<<(std::wostream&, HandleKind const&)
//
// IEEE 1516.1-2010 API reference: RTI/Handle.h

#include <RTI/Handle.h>
#include <RTI/VariableLengthData.h>
#include <type_traits>
#include <ostream>
#include <string>

namespace {

// ---------- Row 7.2: 9 spec handle classes exist (no RoutingSpaceHandle) ----------
static_assert(std::is_class_v<rti1516e::FederateHandle>);
static_assert(std::is_class_v<rti1516e::ObjectClassHandle>);
static_assert(std::is_class_v<rti1516e::InteractionClassHandle>);
static_assert(std::is_class_v<rti1516e::ObjectInstanceHandle>);
static_assert(std::is_class_v<rti1516e::AttributeHandle>);
static_assert(std::is_class_v<rti1516e::ParameterHandle>);
static_assert(std::is_class_v<rti1516e::DimensionHandle>);
static_assert(std::is_class_v<rti1516e::MessageRetractionHandle>);
static_assert(std::is_class_v<rti1516e::RegionHandle>);

// ---------- Row 7.1 + 7.3 + 7.4 + 7.5 + 7.6: per-handle surface lock ----------
// One macro factors the per-class assertions. If the surface shifts for ANY
// handle class, the static_assert tied to that class fires.

#define LOCK_HANDLE_SURFACE(HandleKind)                                        \
  /* Row 7.1: default-constructible (produces invalid handle) */                \
  static_assert(std::is_default_constructible_v<rti1516e::HandleKind>);         \
  /* Row 7.1: copy-constructible + copy-assignable */                           \
  static_assert(std::is_copy_constructible_v<rti1516e::HandleKind>);            \
  static_assert(std::is_copy_assignable_v<rti1516e::HandleKind>);               \
  /* Row 7.1: destructible */                                                   \
  static_assert(std::is_destructible_v<rti1516e::HandleKind>);                  \
  /* Row 7.1: bool isValid() const */                                           \
  static_assert(std::is_same_v<                                                 \
      decltype(std::declval<rti1516e::HandleKind const&>().isValid()), bool>);  \
  /* Row 7.1: operator==, !=, < — same-type only, bool return */                \
  static_assert(std::is_same_v<                                                 \
      decltype(std::declval<rti1516e::HandleKind const&>() ==                   \
               std::declval<rti1516e::HandleKind const&>()), bool>);            \
  static_assert(std::is_same_v<                                                 \
      decltype(std::declval<rti1516e::HandleKind const&>() !=                   \
               std::declval<rti1516e::HandleKind const&>()), bool>);            \
  static_assert(std::is_same_v<                                                 \
      decltype(std::declval<rti1516e::HandleKind const&>() <                    \
               std::declval<rti1516e::HandleKind const&>()), bool>);            \
  /* Row 7.3: long hash() const */                                              \
  static_assert(std::is_same_v<                                                 \
      decltype(std::declval<rti1516e::HandleKind const&>().hash()), long>);     \
  /* Row 7.4: encode/encodedLength */                                           \
  static_assert(std::is_same_v<                                                 \
      decltype(std::declval<rti1516e::HandleKind const&>().encode()),           \
      rti1516e::VariableLengthData>);                                           \
  static_assert(std::is_same_v<                                                 \
      decltype(std::declval<rti1516e::HandleKind const&>().encodedLength()),    \
      size_t>);                                                                 \
  /* Row 7.5: wstring toString() const */                                       \
  static_assert(std::is_same_v<                                                 \
      decltype(std::declval<rti1516e::HandleKind const&>().toString()),         \
      std::wstring>);                                                           \
  /* Row 7.6: wostream operator<< */                                            \
  static_assert(std::is_invocable_r_v<                                          \
      std::wostream&,                                                           \
      decltype(static_cast<std::wostream& (*)(std::wostream&,                   \
                                              rti1516e::HandleKind const&)>(    \
          &rti1516e::operator<<)),                                              \
      std::wostream&, rti1516e::HandleKind const&>)

LOCK_HANDLE_SURFACE(FederateHandle);
LOCK_HANDLE_SURFACE(ObjectClassHandle);
LOCK_HANDLE_SURFACE(InteractionClassHandle);
LOCK_HANDLE_SURFACE(ObjectInstanceHandle);
LOCK_HANDLE_SURFACE(AttributeHandle);
LOCK_HANDLE_SURFACE(ParameterHandle);
LOCK_HANDLE_SURFACE(DimensionHandle);
LOCK_HANDLE_SURFACE(MessageRetractionHandle);
LOCK_HANDLE_SURFACE(RegionHandle);

#undef LOCK_HANDLE_SURFACE

// ---------- Row 7.2 (negative): RoutingSpaceHandle must NOT be in rti1516e ----------
// The reference_rti spec does not include a RoutingSpaceHandle — it's a gorti M17
// holdover from HLA 1.3. We lock its absence via a SFINAE probe: any reference
// to `rti1516e::RoutingSpaceHandle` should fail to find a declaration.
// (We can't directly test "does not exist" without compiler-extension trickery;
//  the row stays as a documentation-only assertion. See divergence catalogue §7.)

// ---------- Row 7.2: MessageRetractionHandle must be class, not uint64 alias --
static_assert(!std::is_integral_v<rti1516e::MessageRetractionHandle>);
static_assert(!std::is_same_v<rti1516e::MessageRetractionHandle, unsigned long long>);
static_assert(!std::is_same_v<rti1516e::MessageRetractionHandle, uint64_t>);

// ---------- Handle classes are distinct types (no aliasing) ----------
static_assert(!std::is_same_v<rti1516e::FederateHandle, rti1516e::ObjectClassHandle>);
static_assert(!std::is_same_v<rti1516e::AttributeHandle, rti1516e::ParameterHandle>);
static_assert(!std::is_same_v<rti1516e::DimensionHandle, rti1516e::RegionHandle>);
static_assert(!std::is_same_v<rti1516e::ObjectClassHandle, rti1516e::ObjectInstanceHandle>);

}  // namespace
