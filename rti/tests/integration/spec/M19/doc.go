// Package m19spec contains the specification tests
// for milestone M19 — DDS / RTPS data-plane adapter (docs/m19-dds-adapter.md).
//
// Phase 1a scope (this dispatch): proto extensions + federation
// manager wiring + cmd/rtid flag plumbing + build-tag-gated package
// skeleton. NO CGo — the Cyclone DDS C library is not yet available
// in the build environment, so the Phase 1a tests cover the
// foundation only.
//
// Phase 1b lands the actual CGo implementation under cgo_dds.go and
// the corresponding end-to-end smoke test under
// rti/spec/M19/dds_smoke_test.go (build tag: dds_e2e).
//
// The suite tracks the Phase 1a and 1b behavior described above.
package m19spec
