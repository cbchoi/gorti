// M39 — bridge-error → DLC spec-exception translation.
//
// Extracted from RTIambassadorImpl.cpp (where it lived as a TU-local
// helper since M34) so the prefix table is unit-testable from the
// conformance _runtime gtests without a live RTI.
//
// Contract: the M17 bridge (M17Bridge.cpp guard()) re-throws every M17
// exception as std::runtime_error whose message starts with a prefix
// `<ExceptionClassName>: `. Since M39 the M17 client derives that class
// name metadata-first from the server's `rti-spec-exception` trailer
// (see cppsdk/src/dlc/README.md), so the prefix vocabulary is the FULL
// IEEE 1516.1-2010 Annex C class list — translateBridgeError matches the
// prefix and throws the corresponding <RTI/Exception.h> type. Detail
// -string sniffs remain only as a DEPRECATED fallback for pre-M39 /
// third-party servers that do not send the trailer.

#pragma once

#include <stdexcept>

namespace gorti {
namespace dlc {

// Re-throw `e` as the matching <RTI/Exception.h> spec type. Never
// returns. Every unmatched message folds to rti1516e::RTIinternalError.
[[noreturn]] void translateBridgeError(std::runtime_error const& e);

}  // namespace dlc
}  // namespace gorti
