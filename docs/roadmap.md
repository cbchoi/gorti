# Roadmap

## Implemented foundation

The current single-server implementation provides Go, Python, and Java
interfaces, dynamic FOM joins, Float64 and Integer64 execution, timed
save/restore, callback and SDK-state recovery, ownership, DDM, and MOM services.
Go receive-order traffic uses LocalLRC admission by default; confirmed calls
remain available. The optional `callback_async` build pipelines eligible
callback reports. Audit/replay is a separate runtime plugin.

[Verification](verification.md) records the completed service campaigns and
requirements review. [Performance](performance.md) reports the October 2026
Portico comparisons.

## Standards and release work

- Resolve the 25 open source-interpretation records in the IEEE requirements
  inventory and review the completeness of clause coverage.
- Obtain independent service and language-mapping assessment.
- Repeat the full Go/Python/Java and race selections on an archived release
  candidate, with exact source and fixture identities.
- Expand Java and cross-language execution coverage on Linux and macOS.
- Publish release archives, documentation, and reproducible experiment manifests.

## Performance work

- Evaluate a sliding callback-report window that can replace acknowledged
  frames without waiting for the entire current window.
- Measure long-lived participants across independent sessions, alongside the
  existing fresh-process comparisons.
- Extend matched workloads to multiple hosts, more federates, and time management.
- Reduce callback serialization and scheduling costs while retaining admission,
  callback ordering, error reporting, and checkpoint behavior.
- Assess a consistent asynchronous completion interface across SDKs.
- Measure audit/replay overhead separately from the default core profile.

Each optimization is compared with the previous build under the same workload
and checked with the affected IEEE service tests.

## Separate experimental work

Cluster/failover deployment and non-default time/ownership algorithms live
under `rti/experiments`. They are separate from the supported single-server
configuration. HLA 1.3 and IEEE 1516-2000 API compatibility are not current
development targets.
