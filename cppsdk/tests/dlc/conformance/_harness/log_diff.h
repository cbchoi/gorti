// _harness/log_diff.h — canonical-log diffing for conformance fixtures.
//
// Per docs/DLC_COMPLIANCE_PROGRAM.md §5.3.1 canonicalization rules:
//   1. Handle integers are replaced by `<H>` placeholders.
//   2. Receive-ordered (RO) events sort within their LBTS bucket
//      (LBTS = lower-bound time stamp; events within the same bucket
//      have no spec-required ordering across federates).
//   3. Time-stamp-ordered (TSO) events are strict: per §8.13-8.15
//      the receiver must see them in increasing-time order, with
//      same-time events ordered by a federation-canonical tiebreak.
//
// Used by every conformance fixture's test_<name>.cpp; M31 ships
// header-only stub implementations sufficient to compile fixture
// drivers. Actual normalization happens in `tests/parity/normalize.py`
// (Agent E's TASK-353) for cross-language parity runs.

#pragma once

#include <algorithm>
#include <regex>
#include <sstream>
#include <string>
#include <vector>

namespace gorti_dlc_harness {

// Replace every `handle=<integer>` token with `handle=<H>`.
// Per §5.3.1 rule 1.
inline std::string normalizeHandles(const std::string& line) {
  static const std::regex handle_re(R"(handle=\d+)");
  std::string out = std::regex_replace(line, handle_re, "handle=<H>");
  // M36 — RTI-assigned MOM instance names embed the federate handle
  // (`name=HLAfederate.3`; IEEE §6.2 uniqueness forces a suffix).
  // Same policy as bare handle ints; mirrors _harness/normalize.py.
  static const std::regex mom_name_re(R"(name=(HLAfederate|HLAfederation)\.\d+)");
  return std::regex_replace(out, mom_name_re, "name=$1.<H>");
}

// LBTS-bucket sort: lines marked `RO` (receive-ordered) within a bucket
// (bracketed by `LBTS=<n>` markers) get sorted lexically. Lines marked
// `TSO` keep their order. Per §5.3.1 rules 2-3.
//
// Bucket format expected in canonical log:
//   LBTS=1.0
//   RO: SUB: ... (any order within bucket)
//   RO: SUB: ...
//   LBTS=2.0
//   TSO: SUB: ... (strict order within bucket)
inline std::vector<std::string> bucketSortRO(
    const std::vector<std::string>& lines) {
  std::vector<std::string> out;
  out.reserve(lines.size());
  std::vector<std::string> ro_bucket;
  auto flush = [&]() {
    if (!ro_bucket.empty()) {
      std::sort(ro_bucket.begin(), ro_bucket.end());
      for (auto& l : ro_bucket) out.push_back(std::move(l));
      ro_bucket.clear();
    }
  };
  for (const auto& l : lines) {
    if (l.rfind("LBTS=", 0) == 0) {
      flush();
      out.push_back(l);
    } else if (l.rfind("RO:", 0) == 0) {
      ro_bucket.push_back(l);
    } else {
      flush();
      out.push_back(l);
    }
  }
  flush();
  return out;
}

// Read all non-comment lines (lines beginning with `#` are stripped).
// Per the convention used in expected.*.log golden files.
inline std::vector<std::string> splitNonComment(const std::string& text) {
  std::vector<std::string> out;
  std::istringstream is(text);
  std::string line;
  while (std::getline(is, line)) {
    // Trim trailing \r for Windows-CRLF tolerance.
    if (!line.empty() && line.back() == '\r') line.pop_back();
    if (line.empty()) continue;
    if (line[0] == '#') continue;
    out.push_back(line);
  }
  return out;
}

}  // namespace gorti_dlc_harness
