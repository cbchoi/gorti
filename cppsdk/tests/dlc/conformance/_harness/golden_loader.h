// _harness/golden_loader.h — loads expected.*.log golden files for diffing.
//
// Per docs/M31_DISPATCH_PLAN.md §2.2 golden format: one canonical event
// per line, with handle integers replaced by `<H>`. Comments (lines
// starting with `#`) are stripped — these are used for spec citations
// and `// TBD-pitch-capture` markers per ralph.md §2 rule 5.

#pragma once

#include <fstream>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

#include "log_diff.h"

namespace gorti_dlc_harness {

// Returns the file contents verbatim (trailing newline-trimmed).
inline std::string slurp(const std::string& path) {
  std::ifstream is(path);
  if (!is) throw std::runtime_error("cannot open golden: " + path);
  std::ostringstream os;
  os << is.rdbuf();
  return os.str();
}

// Load a golden file and return its non-comment lines, ready for diff.
inline std::vector<std::string> loadGolden(const std::string& path) {
  return splitNonComment(slurp(path));
}

// Compare actual log against golden. Returns empty string on match;
// otherwise returns a unified-diff-ish summary.
inline std::string diffAgainstGolden(const std::vector<std::string>& actual,
                                     const std::vector<std::string>& golden) {
  if (actual == golden) return "";
  std::ostringstream os;
  os << "log diff (expected " << golden.size() << " lines, got "
     << actual.size() << " lines):\n";
  const size_t n = std::max(actual.size(), golden.size());
  for (size_t i = 0; i < n; ++i) {
    const std::string& a = i < actual.size() ? actual[i] : "<missing>";
    const std::string& g = i < golden.size() ? golden[i] : "<extra>";
    if (a != g) {
      os << "  line " << (i + 1) << ":\n";
      os << "    expected: " << g << "\n";
      os << "    actual  : " << a << "\n";
    }
  }
  return os.str();
}

}  // namespace gorti_dlc_harness
