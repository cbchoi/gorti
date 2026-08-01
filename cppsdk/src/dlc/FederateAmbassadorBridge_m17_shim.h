// M34 — namespace shim to co-host the M17 FederateAmbassador
// alongside the DLC FederateAmbassador in the SAME translation unit.
//
// PROBLEM. gorti's M17 SDK and the strict IEEE 1516.1-2010 DLC surface both
// define `class FederateAmbassador` inside `namespace rti1516e`. They have
// incompatible method signatures (M17: std::string + std::optional<double> +
// uint64 handles; DLC: std::wstring + LogicalTime + typed handle classes).
// Including both headers directly is a redefinition error.
//
// SOLUTION. This shim `#define rti1516e rti1516e_m17` and then includes the
// M17 headers, so the WHOLE M17 API (Types + FederateAmbassador) lands under
// `namespace rti1516e_m17`. The macro is `#undef`'d immediately after the
// include chain so downstream code sees `rti1516e` as the DLC namespace,
// unchanged.
//
// USAGE. Include this header BEFORE any include chain that pulls in either
// `<rti1516e/*.h>` or `<RTI/*.h>` in the SAME TU. The bridge header
// (FederateAmbassadorBridge.h) does this by making the shim its first
// include. TUs that only need the DLC surface never touch this shim.
//
// SAFETY. `#pragma once` in the M17 headers guards by inode, so once this
// shim has been included the M17 headers cannot be re-included in the same
// TU with the real `rti1516e` name — that is intentional. Bridge TUs must
// commit to seeing M17 as `rti1516e_m17` throughout the whole TU.

#pragma once

#ifdef rti1516e
#  error "FederateAmbassadorBridge_m17_shim.h: `rti1516e` macro already defined; \
include this shim BEFORE any other rti1516e/RTI header"
#endif

// Rewrite every occurrence of the token `rti1516e` inside the two M17 headers
// below into `rti1516e_m17`. The headers use `namespace rti1516e { ... }` at
// the top level and unqualified handle types in method signatures, so this
// wholesale rename lands the whole M17 vocabulary inside `rti1516e_m17`.
#define rti1516e rti1516e_m17

#include "rti1516e/Types.h"
#include "rti1516e/FederateAmbassador.h"

#undef rti1516e
