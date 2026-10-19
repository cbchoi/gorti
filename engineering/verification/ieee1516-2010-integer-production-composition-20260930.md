# Integer64 Production Composition

## Plan

Bind independently typed Float64 and Integer64 object, time and save managers to
immutable federation metadata. Share federation membership, declarations,
ownership, DDM, synchronization, MOM, authenticated sessions and callback queues.
Keep unspecified factory selection Float64. Never narrow Integer64 time into a
Float64 field. Reject unsupported optimized transports explicitly. Production
acceptance must use the real assembled server and actual SDK clients.

## Do

- Added a capability resolver injected into federation creation and gRPC
  validation; the unconfigured core resolver remains Float64-only.
- Composed exact Integer64 time, object, retraction, checkpoint storage and
  restore graph services alongside the existing Float64 family.
- Selected object-dependent ownership, DDM, unpublication, discovery, MOM and
  resign operations through the same federation metadata. Resign uses the
  generic lifecycle transaction supplied by the lifecycle worker.
- Recorded exact idle Integer64 MOM time after the federate entry exists;
  management metrics retain exact time/interval encodings and undefined horizons.
- Kept save observations, service admission, lifecycle cleanup, retry and admin
  reads attached to the selected manager family.
- Exposed `--enable-integer64` through the executable's CLI and application
  arguments, rather than an inaccessible test-only configuration flag.
- Corrected Go SDK creation-response validation to use its existing supported
  factory resolver. Unsupported successful responses still retain creation
  evidence and do not automatically destroy an execution.

## Review

The tests below start the real production gRPC server on loopback TCP, not a fake
time backend. The two-SDK test uses separate Go SDK gRPC connections in the test
process; it is not evidence of two independent operating-system processes.

| Test | Evidence at this checkpoint |
| --- | --- |
| Idle MOM, factory isolation, adjacent times above 2^53, finite MaxInt64, mixed representation rejection | Passed |
| Regulating requester, MaxInt64 timed save, filesystem storage, restart and reverse-name identity rebind | Passed |
| Two Go SDK federates: exact grants and queries at adjacent times above 2^53 and MaxInt64 | Actual RED then GREEN |
| Integer resignation deletes the selected registry object and recipient knowledge | Actual RED then GREEN |
| Timestamped Integer object reflection precedes its exact grant | Passed |
| Unsupported successful Go SDK response retains committed execution metadata | Passed in earlier focused SDK campaign |
| Parent unpublication production tests for both factories | Parent reports passed |

The creation-response RED returned `FederationCreationResponseError` after an
Integer execution had actually been created. The resign RED returned success but
left the Integer object alive because the Float lifecycle composer was selected.
Both failures were reproduced before their integration changes.

Current review boundaries: the complete root regression exposed six existing
R4 MOM resign deletion subcases whose reservation-count expectations omit the
new mandatory ownership deletion outcome; these are owned by the lifecycle/
parent workers. A subsequent focused run was temporarily blocked by a concurrent
gRPC DDM edit using undefined `toStatus`. This report does not label those runs
green. Independent Python/Java executable acceptance and final race verification
remain separate evidence.

## Reflect

Generic managers alone were insufficient: metadata routing, exact idle MOM,
checkpoint admission, object-dependent timeless services and resignation all
needed real production composition. Factory acceptance remains a capability
decision, not evidence of full IEEE conformance. Integer DDS/cluster and
Float64-only research algorithms are explicitly unsupported; confirmed/LocalLRC
optimized streams reject Integer rather than convert timestamps.

The existing scheduled-save coordinator is still the bounded all-participant
barrier implementation. G14-G16 request replacement, authenticated requester
conditions and per-federate initiation are the next separately tested change.
The MaxInt64 fixture now has a regulating requester and exact lookahead one;
its pass is not a claim that all timed-save obligations have been completed.

Initial executable: `D:/workspace/gorti/rtid-integer-production-20260930.exe`.
SHA256 before the generic-resign/SDK response patch:
`EA1C12BA7EEC59A87D348B6C64CFC90B47CFA4CEFB2E027FA9356E4CCD115FAF`.
Use a freshly rebuilt executable for final acceptance, not this pre-fix digest.

## Frozen Integer Milestone

The executable was rebuilt after selected generic resignation and the Go SDK
creation-response correction. Its SHA256 is
`D472FC99FC490C9AEA07182B01783B8E31C128EC6E6FE587D3195E72E1BA3846`.
The normal CLI enables explicit Integer64 selection by default; omitted factory
selection still creates Float64. Operators can set `--enable-integer64=false`.
All five root production tests and the parent's dual-factory unpublication
production test passed in one fresh focused run. That command also requested
SDK package tests, whose separate compilation was blocked by a concurrent
duplicate unsubscribe test name; the real two-SDK root test itself passed.

Linux `/usr/bin/go` 1.22.2, CGO-enabled race verification of application/server,
time and save_restore passed, three repetitions, for those six production tests
and the new requester-validation tests: 3.902s / 1.100s / 1.052s. This is not
described as Linux Go 1.26.5. The Windows non-race runs used Go 1.26.5.
Windows race execution was unavailable with the existing CGO-disabled runtime;
Linux supplied the executed race evidence instead.

## Next Timed-Save Stage

G15 pure typed requester validation and the additive
`RequestFederationSaveFrom` capability now have actual RED-to-GREEN tests for
both scalars, undefined GALT, strict GALT equality, regulating send floors,
Integer64 overflow and invalid-call state/callback preservation. Full time and
save_restore package runs pass. These capabilities are deliberately NOT wired
into the production handler/composition yet. Production still uses the prior
all-participant save barrier and global admission. G14 replacement and G16
per-participant activation/queue preservation must be implemented and tested
before that wiring. The new port is not presented as completed G15 runtime
behavior or completed G14-G16 conformance.

Latest full source-package run (not `./...`, which traverses inaccessible ignored
old directories): Go SDK, federation, time and save_restore PASS. Application/
server remains non-green on three top-level tests: six deletion reservation-count
subcases in R4 MOM resign; target-token PermissionDenied versus expected
Unauthenticated in MOM service admission; restore-restart fixture using a DDM
region before new template realization. These are reported as acceptance blockers,
not hidden by the passing focused Integer milestone. The prior SDK duplicate test
compile blocker has been corrected by its owner and its full package now passes.

The independent Python executable campaign then exposed a real CLI-only defect:
ordinary research configuration resolution fills concrete standard LBTS/grant
objects, so testing only non-nil strategy fields rejected Integer even with no
research config. A separate-process `Main` test reproduced this RED. The guard
now recognizes concrete standard implementations, not strategy names, and the
actual CLI test passes omitted Float and explicit Integer. Custom Float strategy
wrappers with the same `default` name remain rejected. Integer receives its own
generic default strategies, never the Float instances.

Root `run.go` and typed composition are frozen/released to parent for coordinated
MOM, synchronization and receipt wiring. Fresh CLI-fixed executable is
`D:/workspace/gorti/rtid-integer-cli-fixed-20260930.exe`; previous executable
digests above do not include this CLI fix. G14-G16 continuation will not edit
these shared root files without further coordination.

CLI-fixed binary SHA256:
`B02F5D4513F9D8009A142B716EDE1859B11C036D48D6417CD1E9419AA6F772BD`.
Linux Go1.22.2 race: actual separate-process CLI, focused Integer root tests,
parent unpublication and custom-profile rejection PASS, three repetitions,
6.508s. All worker-owned executable/test sessions have completed.

No commit or reset was performed. Existing dirty-tree work and worker boundaries
were preserved.
