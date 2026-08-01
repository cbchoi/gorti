// IEEE 1516.1-2010 §C.3 / Annex A — RTI/SpecificConfig.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Defines visibility macros (RTI_EXPORT), deprecated exception-spec macros
// (RTI_NOEXCEPT / RTI_THROW), and the C++17-bridge `rti1516e::auto_ptr<T>`
// alias per `docs/DLC_COMPLIANCE_PROGRAM.md §3.1.0`.
//
// The spec text returns `std::auto_ptr<T>` (C++03) from RTIambassadorFactory
// and other factories. C++17 removed std::auto_ptr; gorti aliases to
// std::unique_ptr by default. Federates porting from reference_rti that wrote
// `std::auto_ptr<RTIambassador>` literally need to switch to `auto` or to
// `rti1516e::auto_ptr<RTIambassador>`.

#ifndef RTI_SpecificConfig_h
#define RTI_SpecificConfig_h

#include <memory>

// --- RTI_EXPORT visibility macro (FR-DLC-17) ---
#if defined(_WIN32)
  #if defined(STATIC_RTI)
    #define RTI_EXPORT
  #else
    #if defined(BUILDING_RTI)
      #define RTI_EXPORT __declspec(dllexport)
    #else
      #define RTI_EXPORT __declspec(dllimport)
    #endif
  #endif
  #if defined(STATIC_FEDTIME)
    #define RTI_EXPORT_FEDTIME
  #else
    #if defined(BUILDING_FEDTIME)
      #define RTI_EXPORT_FEDTIME __declspec(dllexport)
    #else
      #define RTI_EXPORT_FEDTIME __declspec(dllimport)
    #endif
  #endif
#else
  #define RTI_EXPORT __attribute__((visibility("default")))
  #define RTI_EXPORT_FEDTIME __attribute__((visibility("default")))
#endif

// --- Deprecated dynamic-exception-spec macros ---
// C++17 removed dynamic-exception-specs (`throw(T)`); RTI_THROW expands to
// a no-op so spec-literal headers still parse. RTI_NOEXCEPT maps to noexcept.
#if __cplusplus >= 201703L
  #define RTI_NOEXCEPT noexcept
  #define RTI_THROW(...) /* no-op under C++17 */
#else
  #define RTI_NOEXCEPT throw()
  #define RTI_THROW    throw
#endif

namespace rti1516e {

// FR-DLC-2 C++17 resolution. Spec text says std::auto_ptr; we alias to
// std::unique_ptr. Opt-in `-DGORTI_DLC_USE_REAL_AUTO_PTR` switches to the
// real (deprecated, removed) std::auto_ptr — only meaningful under C++14
// toolchains for source ports that wrote `std::auto_ptr` literally.
#if defined(GORTI_DLC_USE_REAL_AUTO_PTR) && __cplusplus < 201703L
  template <typename T> using auto_ptr = std::auto_ptr<T>;
#else
  template <typename T> using auto_ptr = std::unique_ptr<T>;
#endif

}  // namespace rti1516e

#endif  // RTI_SpecificConfig_h
