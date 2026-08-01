# Changelog

This file records user-visible changes for published gorti releases.

## 0.9.0 - Release Candidate

### Added

- Single-node IEEE 1516-2010 RTI server implemented in Go.
- Go, Python, and C++ federate APIs with shared HLA encodings and FOM handling.
- Federation, declaration, object, ownership, data distribution, time,
  synchronization, save/restore, and management-object services.
- Receive-order LocalLRC and confirmed-stream transports with explicit
  admission and completion boundaries.
- Deterministic event logging, generation fencing, teardown handling, and
  timestamp-order callback-before-grant enforcement.
- Executable Go, Python, and C++ conformance tests and reproducible two-federate
  comparison workloads.
- User documentation, engineering specifications, citation metadata, and
  automated release checks.

### Known Limitations

- The supported deployment is one authoritative `rtid` process; cluster and
  failover code is experimental.
- There is no Java federate SDK and no claim of formal IVCT certification.
- LocalLRC queue admission is not equivalent to server-confirmed completion;
  cross-implementation comparisons must use the same completion boundary.
- HLA 1.3, IEEE 1516-2000, and HLA 4 interfaces are outside this release.
