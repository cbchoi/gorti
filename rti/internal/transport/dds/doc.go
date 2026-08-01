// Package dds is the M19 DDS / RTPS data-plane adapter.
//
// See docs/m19-dds-adapter.md §4.2 for the full design and §9 for the
// Phase 1 success criteria. The package fronts a thin Go-side wrapper
// around the four Cyclone DDS primitives the gorti data plane needs:
// DomainParticipant, Topic, DataWriter, DataReader. The actual C
// interop lives in cgo_dds.go (Phase 1b — see below).
//
// # Build constraint
//
// This package is gated behind the `dds` build tag. The default
// `rtid` binary (`go build -o bin/rtid ./rti/cmd/rtid`) does NOT
// link this package — it stays CGo-free + DDS-free. The opt-in
// `rtid-dds` binary (`go build -tags=dds -o bin/rtid-dds
// ./rti/cmd/rtid`) compiles this package; in Phase 1a (foundation
// only) every primitive returns errors.ErrUnsupported because the
// Cyclone DDS C library is not yet wired through CGo.
//
// # Phase split
//
//   - Phase 1a (this dispatch): foundation. Pure-Go interfaces +
//     stub implementations + HLA→DDS QoS mapping. The package
//     compiles under the `dds` tag but every method returns
//     errors.ErrUnsupported. The QoS mapping (qos.go) is real code
//     that Phase 1b will call from CGo without modification.
//   - Phase 1b (next dispatch, when libcyclonedds-dev is available
//     in the build environment): cgo_dds.go lands with the real C
//     interop. The defaultParticipant struct stops returning
//     ErrUnsupported and starts driving Cyclone DDS. The
//     end-to-end smoke test under rti/spec/M19/dds_smoke_test.go
//     (build tag `dds_e2e`) starts passing.
//
// # Determinism
//
// docs/m19-dds-adapter.md §6.7 PINNED: replay-determinism is gRPC-
// mode only. DDS-mode federations follow the research-platform
// per-impl-opt-in pattern and the M3/M4 byte-identical replay tests
// SKIP (not FAIL) with a clear reason in their output. Phase 1b's
// smoke test will assert that replay tests skip cleanly when DDS
// mode is active.
//
// # Why not import "C" yet
//
// libcyclonedds-dev is not installed in the build environment as of
// Phase 1a. Adding `import "C"` would break `go build -tags=dds
// ./...` on every developer machine and CI runner that doesn't have
// the apt package. The Phase 1a stub keeps the build green so the
// foundation can land independently of the C-toolchain bootstrap.
package dds

// Build constraint applied via build_constraint.go and per-file
// constraints. This file is ALWAYS compiled (no constraint) so
// `go doc github.com/cbchoi/gorti/rti/internal/transport/dds`
// resolves the package documentation in either build.
