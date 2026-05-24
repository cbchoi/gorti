// Typed handles + value maps for the rti1516e C++ SDK.
//
// Mirrors IEEE 1516.1-2010 §10.5 handle types. Each handle is a
// strong typedef over uint64_t so the compiler catches a
// ParameterHandle-where-AttributeHandle-was-expected bug at the
// call site. Invalid handles compare equal to the Invalid* sentinels.
//
// The handle/value-map types are deliberately minimal: a Pitch
// federate port typically does
//
//     ObjectClassHandle vc = amb.getObjectClassHandle(L"Vehicle");
//     AttributeHandle ph = amb.getAttributeHandle(vc, L"Position");
//     AttributeHandleValueMap m;
//     m[ph] = encode_double(42.0);
//     amb.updateAttributeValues(obj, m, /*tag=*/{}, /*time=*/{});
//
// — and that pattern is what the M17 Cut-1 surface targets.

#pragma once

#include <cstdint>
#include <map>
#include <set>
#include <string>
#include <vector>

namespace rti1516e {

// Underlying integer width for every handle. Matches the proto wire
// (uint64) and the Python SDK's int handles. Zero is reserved as the
// invalid sentinel.
using HandleValue = std::uint64_t;

namespace detail {

// Strong typedef over HandleValue. The Tag template parameter
// distinguishes ObjectClassHandle from AttributeHandle at the type
// level so a value confusion is a compile error, not a runtime bug.
template <typename Tag>
struct StrongHandle {
  HandleValue value{0};

  constexpr StrongHandle() = default;
  constexpr explicit StrongHandle(HandleValue v) : value(v) {}

  constexpr bool isValid() const noexcept { return value != 0; }
  constexpr explicit operator bool() const noexcept { return isValid(); }
  constexpr HandleValue raw() const noexcept { return value; }

  friend constexpr bool operator==(StrongHandle a, StrongHandle b) noexcept {
    return a.value == b.value;
  }
  friend constexpr bool operator!=(StrongHandle a, StrongHandle b) noexcept {
    return a.value != b.value;
  }
  friend constexpr bool operator<(StrongHandle a, StrongHandle b) noexcept {
    return a.value < b.value;
  }
};

struct ObjectClassTag {};
struct AttributeTag {};
struct InteractionClassTag {};
struct ParameterTag {};
struct ObjectInstanceTag {};
struct FederateTag {};
struct DimensionTag {};
struct RoutingSpaceTag {};
struct RegionTag {};

}  // namespace detail

using ObjectClassHandle = detail::StrongHandle<detail::ObjectClassTag>;
using AttributeHandle = detail::StrongHandle<detail::AttributeTag>;
using InteractionClassHandle = detail::StrongHandle<detail::InteractionClassTag>;
using ParameterHandle = detail::StrongHandle<detail::ParameterTag>;
using ObjectInstanceHandle = detail::StrongHandle<detail::ObjectInstanceTag>;
using FederateHandle = detail::StrongHandle<detail::FederateTag>;
using DimensionHandle = detail::StrongHandle<detail::DimensionTag>;
// IEEE 1516.1 §9 Data Distribution Management — M17.17 (Cut-3).
using RoutingSpaceHandle = detail::StrongHandle<detail::RoutingSpaceTag>;
using RegionHandle = detail::StrongHandle<detail::RegionTag>;

// IEEE 1516.1 §10.5 handle-set types. std::set keeps deterministic
// iteration order, matching the Java/C++ ambassador's
// ``Set<AttributeHandle>`` shape.
using AttributeHandleSet = std::set<AttributeHandle>;
using ParameterHandleSet = std::set<ParameterHandle>;
using FederateHandleSet = std::set<FederateHandle>;
using RegionHandleSet = std::set<RegionHandle>;

// IEEE 1516.1 §9.5 — one dimension's lower/upper extent in a region.
struct DimensionRange {
  std::uint64_t lower;
  std::uint64_t upper;
};

// Per-attribute region binding used by RegisterObjectWithRegions /
// Associate / Unassociate / Unsubscribe.
using AttributeRegionMap = std::map<AttributeHandle, RegionHandleSet>;

// Variable-length opaque payload. Federates encode attribute /
// parameter bytes via the FOM datatype rules (HLAfloat64BE etc.);
// the SDK passes them through unchanged.
using VariableLengthData = std::vector<std::uint8_t>;

// IEEE 1516.1 §10.5 value-map types. Order: AttributeHandle ->
// payload bytes (or ParameterHandle -> bytes for interactions).
using AttributeHandleValueMap = std::map<AttributeHandle, VariableLengthData>;
using ParameterHandleValueMap = std::map<ParameterHandle, VariableLengthData>;

}  // namespace rti1516e
