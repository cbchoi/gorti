# Production DDM Registration Regression Tests

Engineering regression coverage only. These tests make no standards-conformance
claim. All repository changes for this test suite are local to this directory.
Raw execution logs and generated replay artifacts live outside the source tree
in `D:/workspace/gorti/ddm-evidence-20260919`.

## Execution

From the repository root:

```powershell
go test ./rti/tests/integration/ddm_registration -count=1 -v
go test -race ./rti/tests/integration/ddm_registration -count=1 -v
```

`process_test.go` starts the test executable as an owned subprocess that calls
the existing production `server.Main`. The federate, admin, and metrics listeners
use loopback ephemeral ports. Cleanup terminates and reaps only that child.
Callback streams acknowledge readiness; synchronization callbacks fence negative
assertions. Deadline waits, not arbitrary sleeps, bound callback assertions.
The object count is compared against its initial value because the production
server also registers MOM objects.

`testdata/registration.xml` declares X and Y with upper bound 1000. Position and
Velocity explicitly reference X. Tests use generated clients for the existing
RPC schema, including structured status/trailer checks; no production exports,
fake managers, or reconstructed service graph are introduced.

## Cases

- `TestProductionRegisterObjectInstanceWithRegionsRouting`: a nonzero handle,
  bidirectional name lookup, exactly one additional real object, distinct
  producer/near/far regions, Discover then Reflect only for the overlapping
  regional subscriber, and delivery to an ordinary subscriber. Deleting the
  object must make the producer region deletable.
- `TestProductionRegisterObjectInstanceWithRegionsFailureAtomicity`: a valid
  first pair followed by an invalid region, foreign region, invalid attribute,
  or actual Y-vs-X dimension mismatch must leave no object/name/discovery. The
  same name then succeeds without cleanup, and producer-region deletion succeeds
  after deleting that object.
- `TestProductionSetRangeBoundsRejectsInvalidPreservesPending`: inverted and
  over-limit ranges must be rejected without changing committed or pending
  bounds. A subsequent commit exposes the preceding valid pending range. The
  exact declared upper bound remains accepted.
- `TestProductionRegisterObjectInstanceWithRegionsSaveRestoreAssociation`:
  save the registered association, unassociate it, delete the producer region,
  restore, and prove the region is in use again and routing is restored. This
  covers same-process, same-roster snapshot restoration, not restart recovery.

## Profile Limitation

The child explicitly sets `--audit-replay-plugin=none --log-dir=` and clears the
research-config environment override. This is the production core profile with
no audit eventlog. The frozen audit/replay codec cannot represent nonempty
region-scoped registration and rejects that record before writing it. A directly
injected strict writer therefore rejects the operation, but the production
audit plugin reports the recording failure without blocking the core operation.
This suite does not assert audit-journal support or replay of regional registrations.
Snapshot save/restore in the optional case is independent of eventlog replay.

## RED Evidence

The first live-source run was already after the parallel RPC/range changes had
landed. Registration failed with `region dimensions do not match the attribute
context` while the attribute-dimension parser change was pending; both range
tests already passed. A later transient build failed on the pending
`eventRecord.RegionScoped` field. Neither is presented as the original bug's RED.

[red-pre-fix.txt](D:/workspace/gorti/ddm-evidence-20260919/red-pre-fix.txt)
records an actual behavioral RED run using a Go build
overlay of the exact pre-fix Git index blobs:

- `64d0a12d8fa73083eef3bcdc4cd399467d4b97e8`: gRPC DDM handler.
- `dfdf485bd510dc99ff444d504d5e4b5162d06a37`: DDM manager.

Only their import paths are relocated for the concurrent repository refactor.
All remaining server code comes from the checkout at replay time. This is a
two-file pre-fix overlay, **not an untouched old release or pre-fix checkout run**. No production
file is overwritten. Reproduce with `./rti/tests/integration/ddm_registration/record_pre_fix.ps1`.
The script writes the log and temporary overlay files under the external evidence
directory, checks the behavioral failure signatures, and removes its generated
overlay files. It does not write raw execution logs into the source tree.

Observed original-behavior failures: registration returned zero with object
count unchanged (5 rather than 6), invalid registrations returned success, and
invalid bounds `[40,30]` and `[30,1001]` overwrote pending `[30,40]`.

## GREEN Evidence

[green-current.txt](D:/workspace/gorti/ddm-evidence-20260919/green-current.txt)
contains the complete live-source output from:

```powershell
go test ./rti/tests/integration/ddm_registration -count=3 -v
```

All four top-level tests and every subtest passed on all three runs (25.835s
total), including the dimension mismatch, post-delete producer-region cleanup,
and same-process association save/restore checks. No overlay was used for GREEN.

The race attempt could not start: this Windows environment has `CGO_ENABLED=0`
and no `gcc`, `clang`, or `cl` on PATH.
[race-unavailable.txt](D:/workspace/gorti/ddm-evidence-20260919/race-unavailable.txt)
records that Windows attempt's command and diagnostic. The final integrated
WSL run of `go test -race -count=1 ./rti/...` passed, including this package.
The local/manual quality gate also passed with all four tests required.
See the [integrated verification report](../../../../engineering/verification/ieee1516-2010-ddm-registration.md)
for the environment, final results and remaining limitations.
