# gorti

gorti is an open-source IEEE 1516-2010 (HLA Evolved) Run-Time
Infrastructure implemented in Go. It provides a standalone `rtid` server,
federate SDKs for Go, Python, and C++, deterministic event logging, and a
cross-RTI verification harness.

## Start here

- [Install gorti](installation.md) on Linux, macOS, or Windows.
- Follow the [quickstart](quickstart.md) to run a server and two federates.
- Read the [user guide](user-guide.md) for federation and SDK concepts.
- Use the [time-management example](time-management.md) to observe a blocked
  time advance and its grant.
- Review [verification and interoperability](verification.md) before relying on
  a service in a production federation.

## Supported scope

gorti targets IEEE 1516-2010. The single-node RTI is the supported deployment
mode. Federation, declaration, object, time, ownership, data distribution,
synchronization, save/restore, and management-object services are covered by
the repository's conformance program.

The project does not claim formal IVCT certification, support HLA 1.3 or
IEEE 1516-2000, or provide a Java federate SDK. The
[conformance documentation](PITCH_PARITY.md) records tested behavior and known
limits.

## Research and reproducibility

The repository includes deterministic semantic projections and a fail-closed
Pitch pRTI comparison protocol. Performance results are accepted only when
both systems use the same FOM bytes, seed, two-process choreography, callbacks,
logging, warmups, measurements, and AB/BA order. See
[reproducibility](reproducibility.md) and [performance](performance.md).

## Project information

- Source: <https://github.com/cbchoi/gorti>
- Releases: <https://github.com/cbchoi/gorti/releases>
- License: MIT
- Citation: [citation metadata](citation.md)
- SoftwareX: [submission package](softwarex.md)
