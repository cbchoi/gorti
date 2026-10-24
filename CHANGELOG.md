# Changelog

This file records user-visible changes for published gorti releases.

## 1.0.0 - 2026-10-24

### Added

- Standalone RTI server implemented in Go, targeting the IEEE 1516-2010 family.
- Go, Python, and Java federate interfaces with shared wire schemas and FOM handling.
- Federation, declaration, object, ownership, data distribution, time,
  synchronization, save/restore, and management-object services.
- Receive-order LocalLRC and confirmed-stream transports with explicit
  admission and completion boundaries.
- Float64 and Integer64 logical time, dynamic FOM joins, callback recovery,
  and server/SDK save and restore.
- Optional audit/replay journaling, separate from the default HLA core profile.
- Requirement-derived Go, Python, and Java tests, traceability records, and
  dated execution summaries.
- Python-Go model integration with reference trajectories and Windows/Linux launchers.
- User documentation, engineering specifications, citation metadata, and
  automated release checks.

### Distribution and validation

- Source release under tag `1.0.0`; build the server and SDKs from this checkout.
  No prebuilt binaries or package-registry upload is part of this release.
- The retained service campaign assessed `4b11da4` on 19 October 2026.
  Subsequent changes add examples, result guides, documentation, and release
  metadata without changing RTI/SDK behavior. See [verification](docs/verification.md)
  for selections, counts, skips, and unresolved requirement records.
- The Portico comparisons remain the historical 7-8 October measurements,
  not benchmarks of this release.

### Deployment and scope

- The supported deployment is one authoritative `rtid` process; cluster and
  failover code is experimental.
- Current SDKs are Go, Python, and Java. Retained C++ material is legacy.
- The tests support conformance assessment; this release is not an IVCT certificate.
- LocalLRC queue admission is not equivalent to server-confirmed completion;
  cross-implementation comparisons must use the same completion boundary.
- HLA 1.3, IEEE 1516-2000, and HLA 4 interfaces are outside this release.
