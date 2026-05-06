//go:build dds_e2e

// End-to-end DDS smoke test. Build tag `dds_e2e` (NOT `dds`) — this
// test runs only when the test runner has both the `dds` build path
// AND a local Cyclone DDS install (libcyclonedds-dev on Linux,
// `brew install cyclonedds` on macOS).
//
// Phase 1a (this dispatch): the test SKIPS unconditionally with a
// clear reason. Phase 1b lands the actual CGo implementation under
// rti/internal/transport/dds/cgo_dds.go and replaces the body of
// this test with the §9 success-criteria checks:
//
//   1. Federate A publishes an interaction class via DDS
//   2. Federate B subscribes via DDS
//   3. Both federates see each other's samples end-to-end
//   4. The data plane traffic does NOT pass through rtid

package m19spec

import (
	"testing"
)

// TestDDSSmokeEndToEnd is the Phase 1a placeholder. Marked t.Skip so
// `go test -tags=dds_e2e ./...` reports a clean "skipped" rather than
// a fail when the CGo implementation has not yet landed.
func TestDDSSmokeEndToEnd(t *testing.T) {
	t.Skip("Phase 1b lands the CGo implementation; this test runs " +
		"end-to-end against a local Cyclone DDS install when " +
		"libcyclonedds-dev is available.")
}
