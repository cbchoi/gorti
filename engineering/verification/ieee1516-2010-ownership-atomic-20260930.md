# Atomic Known-Class Ownership Commands

Date: 2026-09-30. Branch: `codex/ieee2010-feature-completion`.
This records a behavioral correction, not a complete IEEE conformance assessment.

## Problem and Design

The preceding known-class increment translated public ownership attributes before
calling the ownership manager. A concurrent local deletion and rediscovery could
change the caller's known class between translation and mutation. Inherited
attributes have class-relative numeric handles, so checking only numeric identity
was insufficient.

The production gRPC adapter now submits a typed ownership command in caller-known
coordinates. The ownership manager prepares that command through an object-registry
port that maps and validates the complete attribute list while holding the same
object lease through ownership mutation. Existing canonical internal APIs remain
unchanged. Acquisition uses publication validation; cancellation and divestiture
use the non-acquisition preparation port. Invalid complete mappings are rejected
before state changes, and leases are released once, including error paths.

The object-before-ownership lock order is unchanged. Callbacks and ownership-change
observers run after both locks are released. Divest-if-wanted results use the
immutable request mapping rather than querying possibly changed knowledge later.
Release denial is a two-phase operation: release service locks, reserve callback
capacity, then reacquire and compare both mapped attributes and pending requests.
If either changes, discard the reservation and retry against current state.

No Protobuf fields, RPCs, SDK signatures, event-log formats or save-bundle versions
were changed by this increment. Minimal algorithm compositions without the new
preparation ports retain their earlier adapter path; that fallback does not acquire
the production path's concurrency guarantee.

## MOM Reflected-Object History

Runtime reflected-object metrics previously rejected a different class for an
already observed object. The reflected callback carries the recipient's known
class, which may legitimately change after local deletion and rediscovery. MOM
now retains the latest observed callback class while counting the object only
once. Registered/sent traffic accounting still uses the actual class and rejects
changes to that class. Snapshot identity validation and the unavailable-metric
state after an invalid observation remain in effect. This is not a general MOM
accounting completion claim.

## Plan, Do, Review, Reflect

- Plan: preserve canonical internal callers and move public mapping into the
  existing object-before-ownership transaction boundary.
- Do: add missing-capability regression tests, implement the command port and
  cover all ten operations, invalid mappings and admission retries.
- Review: four deterministic composition cases passed over 20 repetitions.
  A legacy-dispatch negative control reproduced wrong-attribute mutation in both
  acquisition modes. Independent review found no blocking defect in this bounded
  input-mapping change; recipient callback epochs remain open.
- Reflect: input mapping atomicity does not by itself fix callback-history epochs,
  generic delivery-failure recovery or other remaining IEEE service obligations.

## Verification

Final regression used Go 1.26.5 on Windows, Python 3.14, Java 11.0.31 and
Maven 3.9.16. Race checks ran under WSL Ubuntu. The full Go and race campaigns
were repeated after the independent concurrency tests were added.

| Campaign | Result |
|---|---|
| `go test ./rti/... -count=1 -timeout=300s` | 68 packages passed; 11 have no tests |
| Full Java Maven `verify`, real RTI endpoint | 421 passed, including 40 actual-server cases; no failures, errors or skips |
| Python selected service/SDK regressions | 212 passed; 1 historical MIM-server rebuild case deselected |
| Linux race: ownership, object, gRPC, server, MOM, savebundle | All 6 packages passed; no race report |
| Generated Java ambassador/coverage consistency | Passed |
| Descriptor/RPC compatibility checks | Passed within the full Go campaign |
| `git diff --check` | Passed |

The Python selection matches the preceding increment; it is not the entire
Python test suite. The deselected case is
`test_legacy_server_rejects_explicit_mim_without_creation_or_downgrade`.
No performance benchmark or external RTI comparison was run for this correction.

Evidence is outside Git beneath `D:/workspace/gorti/`:

- `go-ieee-ownership-atomic-verified-20260930.log`
- `go-ieee-ownership-atomic-race-verified-20260930.log`
- `python-ieee-ownership-atomic-20260930.log`
- `java-ownership-atomic-20260930/`, containing the Maven log and copied XML reports
- `ownership-atomic-review-20260930/`, containing independent tests and the
  legacy-dispatch negative control

The real-client server was `java-tools/rtid-ownership-atomic.exe`, SHA-256
`f9165fd2c01dfab3698d7bd56945030d2a441f158e04e3da9dd481b5ed11e228`.

## Remaining Work

- Recipient-specific callback history across concurrent rediscovery still needs
  a separate delivery-boundary analysis.
- Complete DDM per-federate association lifecycle and ancestor-known association
  handling remain open.
- Exact Integer64 time through federation services and persistence remains open.
- The complete applicable normative obligation inventory and independent
  assessment remain necessary for an RTI-wide conformance claim.

The normative source basis for known-class behavior remains the supplied 2010
redline review documented in the
[preceding increment](ieee1516-2010-time-selection-known-class-20260930.md).
Licensed passages and execution logs are not included in this report.
