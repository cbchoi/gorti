// Package m19spec contains the orchestrator-frozen specification tests
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
// Per docs/TDD.md §5, these tests are RED before the milestone is
// dispatched and turn green as the Phase 1a / 1b deliverables land.
package m19spec
