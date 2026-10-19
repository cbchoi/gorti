# Federation Time Selection and Known Object Class

Date: 2026-09-30. Branch: `codex/ieee2010-feature-completion`.
This is an implementation increment, not a full IEEE conformance assessment.
The later [atomic ownership follow-up](ieee1516-2010-ownership-atomic-20260930.md)
closes the public request mapping/mutation gap listed below and corrects MOM
reflected-object history after rediscovery. Other remaining boundaries still apply.

## Source Basis

The supplied IEEE 1516.1-2010 redline extracts were reviewed using retained/added
2010 text. Physical PDF pages 50 and 52-56 cover common federation time,
creation selection, execution reports and join; pages 325-326 and 338-340 cover
time interfaces and the standard representations. Pages 107-108, 122-124,
155-156 and 253 cover known class, discovery/reflection, publication for
acquisition, and known-class lookup. Licensed passages are not copied here.

Both standardized time implementations are required for full conformance.
Explicitly rejecting Integer64 is an honest current limitation, not a compliant
exclusion of that obligation.

## Changes

### Time Selection

- Federation creation resolves the exact empty/default or `HLAfloat64Time` name
  before FOM loading, setup, generation allocation or state publication.
  Integer64, custom names and case/whitespace variants fail explicitly.
- Create/list/join metadata, the internal roster, MOM's
  `HLAtimeImplementationName` and save manifests use the immutable selection.
  Join reads metadata before committing membership under the generation fence.
- Restore validates representation before state preparation and again at target
  commit. Cross-generation rebinding cannot change it. Absent historical fields
  mean Float64; unknown nonempty names never silently default.
- Go, Python and Java expose the reported selection. SDK creation rejects an
  unsupported explicit selector before contacting a server that might ignore
  the new request field. List results preserve unfamiliar nonempty server names
  as metadata, without pretending the SDK can join those executions.
- Unsupported join-response metadata triggers compensating resignation rather
  than starting callbacks with an unusable time factory. Cleanup must use the
  returned session capability and retain a retry path when remote cleanup fails.

### Known Object Class

- Discovery remembers a class for each recipient/object instead of a boolean.
  Candidate selection chooses the nearest subscribed ancestor; region overlap
  is evaluated at that class rather than falling back to another ancestor.
- Subsequent subscription changes do not rewrite discovery history. Local
  deletion followed by rediscovery can establish a new known class.
- Reflection and object services translate inherited attribute handles by name.
  Ownership state stays in registered-class coordinates; public ownership
  requests and recipient callbacks are translated at the service boundary.
  Divest results use an immutable mapping captured from the admitted request,
  rather than reading possibly changed object knowledge after transfer.
  Callback projection preserves atomic all-recipient outbox reservation.
- New publication of an ancestor attribute can resume an existing ownership
  offer. Candidate discovery and offer matching no longer assume that an
  inherited attribute has the same numeric handle in every class.
- Saved discovery/deletion-recipient history uses the object snapshot extension;
  legacy entries retain registered-class behavior. Restore validates saved-FOM
  ancestry and callback recipient coordinates rather than current subscriptions.
- Committed region/association changes and ownership changes refresh discovery
  after service locks are released. Failed rate admission rolls regional state
  back without invoking this refresh. This closes a regression masked by earlier
  discovery of an entirely unowned object.

## Compatibility

Four additive string fields in `federation.proto` carry the selected type:
create request tag 9, create response tag 3, join response tag 6 and federation
summary tag 5. No RPC was added or removed by this increment. Golden tests strip
only those exact additions and compare the preceding descriptor; all earlier
projections remain in the test chain.

The new descriptor SHA-256 is
`cb296e4132f995969ea299ad0d075576ce5e1f265f4ae358f4db729492703aa4`.
The preceding projection remains
`4732fe65bcbb935f49b3dc98e4b6b7c3f08ec0ae13f8917db999684204aef136`.
The RPC inventory hash remains
`e5a95177a500e880e2cd65b83f5c856f92eec28add7498fac9a093f443484f97`.

Object snapshot version 4 records nonzero known-class history; versions 1-3
remain readable without the new fields. Explicit field presence requires the
new version, including an explicit zero. Ordinary exports with no differing
known class retain the earlier versions. Rebinding changes federate identity,
not class identity.

## Plan, Do, Review, Reflect

- Plan: separate time metadata/restore, Go/Python SDKs, Java SDK, object semantics,
  and independent review into bounded work areas.
- Do: reproduce missing metadata, late rejection, unsupported restore acceptance,
  incorrect inherited handles and missing ownership offers before their fixes.
- Review: independent tests exposed lost session credentials during Go failed-join
  cleanup. Java review also exposed ignored contradictory create metadata and
  cleanup identity loss during callback evocation. Fixes preserve authenticated
  retry state and do not automatically destroy a possibly shared federation.
- Integration: real Java ownership tests exposed a missing reservation capability
  on the callback wrapper; the wrapper now projects the complete fanout before
  reserving it. Additional save/restore tests exposed registered-class comparison
  of recipient-projected removal callbacks; removals now use remembered class,
  while internal deletion markers remain canonical.
- Reflect: follow inheritance through ownership, queues and persistence, not just
  the discovery callback. Keep precise value libraries distinct from complete
  Integer64 federation support. No full-conformance quality gate is waived.

The existing RO-filter test now distinguishes interaction ancestry from an
object's remembered class: a new parent subscription does not revive a queued
reflection for an object already discovered as a child. Snapshot tests move the
unsupported future version from 4 to 5. Neither change disables a failing case.
Minimal-FOM embedded fixtures retain their earlier no-ancestry behavior.
The regional restore test now checks discovery after removal of the last producer
region, verifies that restore removes this post-save server knowledge, and keeps
the already-delivered callback in its external log. Python's absent-rate helper
checks the public `RTIinternalError` and its exact underlying gRPC cause instead
of incorrectly expecting an untranslated transport exception.

## Verification

Final integration used Go 1.26.5, Java 11.0.31, Maven 3.9.16 and Python 3.14 with
gRPC 1.84 on Windows. Race checks ran in WSL Ubuntu. All results below were
executed after the final production changes; intermediate red runs remain
outside Git, including whole-suite runs while parallel changes were landing.

| Campaign | Result |
|---|---|
| `go test ./rti/... -count=1 -timeout=300s` | 68 packages passed; 11 packages have no tests |
| Full Java Maven `verify`, regenerated wire, real RTI endpoint | 421 passed, including 40 actual-server cases; no failures, errors or skips |
| Python service/SDK regression selection | 212 passed; one unrelated historical MIM-server rebuild case deselected |
| Linux race, ten affected packages | All ten passed; no race report |
| Java generated ambassador/coverage consistency | Passed |
| Current and historical descriptor/RPC compatibility | Passed in full Go campaign |
| `git diff --check` | Passed |

The race selection covers federation, save/restore, ownership, savebundle, gRPC,
object, declaration, DDM, application/server and the Go federate SDK. The Python
selection combines savepoint, ownership, DDM, callback metadata, time activation,
FQR/retraction, logical-time selection, failed-join recovery, create integrity,
MIM, transport, confirmation errors, connection lifecycle and facade inputs.
`test_legacy_server_rejects_explicit_mim_without_creation_or_downgrade` was not run
in this campaign. Absent legacy time metadata is covered by separate injected
wire-response tests, not by claiming an actual historical time server was run.

Final evidence, outside Git under `D:/workspace/gorti`:

- `go-ieee-time-known-verified-20260930.log`.
- `go-ieee-time-known-race-verified-20260930.log`.
- `python-ieee-time-known-verified-20260930.log` and
  `python-time-known-verified-20260930/` live artifacts.
- `java-time-selection-verified-20260930/`: full Maven transcript, server logs,
  and copied Surefire XML reports.
- `time-selection-review-20260930/`, `time-selection-sdk-20260930/`,
  `time-selection-java-20260930/`, `known-class-next-20260930/` and
  `ownership-known-class-grpc-20260930/`: focused red/green evidence.
- `region-commit-*-20260930.log`, `known-removal-*-20260930.log` and
  `known-reservation-*-20260930.log`: final integration regressions and fixes.

The final `rtid-time-selection.exe` SHA-256 is
`e14fdc3e2154efb6609604a5f1852d09521e22dd0684c2a8df4e64f3d35da4a3`.
The Java client JAR SHA-256 is
`52d54973e6ef9616ef58184ded357e5e5d3425a7dbd210a3fc33fc15eaa04f8b`.

## Remaining Work

1. Migrate all time-bearing server state, protocol messages, queues, callbacks,
   retraction, MOM and persistence to exact selected-type execution. Verify
   Integer64 messages/grants above `2^53` through real Go, Python and Java clients.
2. Complete nonempty ancestor-class DDM association mutation and inactive/active
   per-federate association lifecycle. The current ancestor mutation path rejects
   unsupported mapping before changing DDM state.
3. Review MOM-produced ancestor discovery, historical metric transitions after
   local deletion/rediscovery, and remaining ownership callback failure/retry paths.
4. Close atomic map/mutate and callback-history handling under concurrent local
   deletion/rediscovery; the new read-only ownership mapping ports do not claim a
   continuous object lease across the entire adapter request.
5. Finish delayed subscription evaluation, MOM pending-message accounting,
   cross-generation private-state restore and the complete normative obligation
   inventory with independently reviewed requirement-linked evidence.

No performance experiment or external-RTI comparison is part of this increment.
