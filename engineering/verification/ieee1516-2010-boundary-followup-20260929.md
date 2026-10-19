# IEEE 1516-2010 Boundary And Lifecycle Follow-Up

Date: 2026-09-29. Branch: `codex/ieee2010-feature-completion`.

This follows the [service completion increment](ieee1516-2010-service-completion-20260929.md).
It records implementation and regression evidence, not complete RTI conformance.

## Time Representation And Arithmetic

`rti/pkg/logicaltime` provides immutable, explicitly typed Float64 and Integer64
time and interval values. Integer64 arithmetic, comparison and eight-byte
big-endian encoding do not convert through Float64. Range overflow, underflow,
invalid encodings, uninitialized values and mixed representations are rejected.
Factories expose the standardized initial, final, zero and epsilon values.

Float64 addition/subtraction by a nonzero interval produces a distinguishable
result or reports an out-of-range operation. Absolute time differences use
ordinary subtraction; they must not apply the time-plus-interval adjustment.
This distinction was caught by a failing boundary test before correction.

The running server now uses this arithmetic for LBTS, GALT, outgoing TSO
validation, retraction expiry and regulation enablement. Previously, at time
`2^53` with lookahead `1`, ordinary Float64 addition returned `2^53` again.
That admitted an illegal same-time send and exposed an incorrect peer bound.
The corrected first legal timestamp is `9007199254740994`. The corresponding
epsilon case at time `1` is also exercised through real Java RTI sessions.

Regulation enablement searches the representable time space for the first legal
send bound. Subtraction is only an upper-bound estimate, not an assumed inverse
of HLA addition. For an inclusive peer frontier of `10` and lookahead `2`, the
smallest legal Float64 activation time is `nextafter(8, 0)`: adding `2` rounds to
`10`. Older Go and Python tests assumed exactly `8`; their oracle now checks the
minimum and its illegal predecessor without relaxing callback-order assertions.

Source locators: supplied retained IEEE 1516.1-2010 text, clause 8.2, physical
PDF pages 192-193; clause 12.3, pages 325-326; clause 12.4/table 18, pages 339-340.
Removed Ada text in the redline is not treated as a current requirement.
No licensed passages are reproduced here.

**Integer64 federation support remains open.** The server's legacy
`core.LogicalTime`, wire time fields, queues and persistence still use Float64.
This package is the exact-value foundation for their migration; it does not
enable Integer64 federation creation or imply that integers above `2^53` can
already travel through existing RPCs without loss. Legacy infinity sentinels
remain internal bound behavior, separate from finite standardized time values.

## DDM Association Validation

Valid empty attribute/region products do not require attribute ownership.
Unassociation removes only the caller's specified regions and likewise does not
require current attribute ownership. All supplied object, attribute and region
references are validated before any change, including later invalid pairs and
foreign region creators. Nonempty additions retain the ownership lease until
inactive pre-acquisition associations are implemented coherently.

The corrected object context also validates attribute names when dimension
metadata is absent. Tests cover grouped atomicity, retained unrelated pairs,
region release and the unchanged ownership requirements of order-type changes.

Source locators: IEEE 1516.1-2010 clauses 9.1.3.2, 9.6 and 9.7, supplied physical
pages 223-224 and 234-238. Full inactive association storage, activation on
acquisition, permanent removal on divestiture, routing and restore remain open.

## Restore Queries

The Java binding separates transport/state hydration from delivery of the final
restore callback. Permitted support queries can use the restored object knowledge
and rebound session identity before `federationRestored` is evoked, including
while callbacks are disabled. Queries do not invoke application callbacks or
reapply the saved state after later discovery.

Mutations stay fenced until callback delivery. An inconsistent restored identity
fails closed before a query reaches the server. A real-server replacement
ambassador test verifies saved object knowledge, membership names, stale handle
rejection and a successful update after restoration completes. This does not
close cross-generation private checkpoint translation or complete callback replay.

## Continuing Ownership Solicitation

Publication of newly added attributes reconsiders existing negotiated and
unconditional divestiture offers after publication commits. Repeated publication
does not duplicate solicitation. Candidates still need object knowledge and valid
publication; cancellation and acquisition remove the corresponding outstanding
offers. Attributes and requests retain their distinct tags and callback grouping.

Unowned offers now have explicit snapshot state. The save adapter reads existing
formats and records continuation data in a versioned extension. Restore graph
validation rejects invalid object/attribute/member references; checked rebinding
translates the former divester without changing object or attribute handles.
After that federate resigns, the zero former-divester marker remains valid.

This increment does not establish complete known-ancestor-class eligibility,
late-discovery solicitation, failed callback admission retry or all ownership
state-chart paths. Those require further work with per-recipient object knowledge
and delivery lifecycle handling.

## Plan, Do, Review, Reflect

- Plan: split time arithmetic, DDM validation, ownership continuation and Java
  restore queries into disjoint changes, using the retained standard text.
- Do: add failing tests before the corresponding production changes; keep wire
  schemas unchanged and preserve legacy snapshot readers.
- Review: independently compare Integer64 results against `big.Int` and Float64
  results against a 2200-bit binary oracle. The review found additional activation
  arithmetic using the old bounds; that path and its exact minimum were corrected.
- Reflect: keep exact-value support distinct from end-to-end Integer64 support;
  narrow the DDM claim to implemented semantics and retain the remaining lifecycle
  requirements. No full-conformance gate is waived.

## Verification

Focused red/green checks passed for time values, server bounds, DDM validation,
restore queries and ownership snapshot integration. Independent time review
includes 4,196 Integer64 and 2,217 Float64 input pairs, not that many independent
IEEE requirements. Real Java tests reproduce the old server's incorrect GALT at
both precision boundaries and pass against the rebuilt server.

Final integration used Go 1.26.5, Java 11.0.31, Maven 3.9.16 and the existing
Python 3.14/gRPC 1.84 environment on Windows. Race tests ran in WSL Ubuntu.
The Java reactor regenerated its wire classes and ran without test filtering
against a newly built production server. No test was disabled to pass this run.

| Campaign | Result |
|---|---|
| Complete `go test ./rti/... -count=1 -timeout=300s` | 68 tested packages passed; 11 packages have no tests |
| Complete Java Maven reactor with real RTI endpoint and FOM | 403 passed, including 35 real-server tests; no failures, errors or skips |
| Python savepoint, ownership, DDM, metadata, time, enablement and FQR/retraction selection | 79 passed |
| Linux race: logicaltime, time, DDM, object, ownership, declaration, save/restore, savebundle, application/server | All 9 packages passed |
| Java generated ambassador/coverage consistency | Passed |
| Historical/current Protobuf descriptor and RPC tests | Passed in the complete Go campaign; schemas unchanged |

Logs and copied Java XML reports are outside Git under `D:/workspace/gorti`:

- `java-time-arithmetic-red-20260929/`: both new precision cases fail against
  the preceding production server, returning the old incorrect GALT.
- `java-ieee-next-final-20260929/`: final Maven transcript, server logs and reports.
- `go-ieee-next-all-20260929.log`, `go-ieee-next-race-20260929.log` and
  `python-ieee-next-final-20260929.log`: final combined regressions.
- `exact-time-review-20260929/`, `ownership-publication-20260929/` and
  `restore-lookup-20260929/`: independent oracle checks and focused red/green evidence.
- `ownership-continuation-graph-red-20260929.log`: valid saved continuation state
  rejected before graph/rebind integration; the new graph test now passes.

The tested server `rtid-ieee-next.exe` has SHA-256
`a2bdce63a827a65d6f97173da0c2b6abd0ae93eb7285ca0266d88f3d845ac374`.
The Java client JAR has SHA-256
`143190014314fd69c1cb434e9da1ed5b9bdb7751e1c855f8ab3756ad6b3b0a61`.

The initial expanded Python run passed 78 cases and exposed the obsolete exact-8
activation expectation. The final run includes its corrected minimum/predecessor
assertions. The full normative inventory and requirement-linked assessment are
still incomplete; these regressions do not establish full IEEE conformance.

## Next Work

Subsequent implementation and fresh test results are recorded in the
[time-selection and known-class increment](ieee1516-2010-time-selection-known-class-20260930.md).
The list below retains this report's original planning snapshot.

1. Carry the selected time representation through federation metadata, wire
   values, queues, callbacks, MOM and saved state; verify real Integer64 grants
   and messages above `2^53` in Go, Python and Java.
2. Store DDM associations per federate with explicit inactive/active lifecycle,
   including ownership transfer, region-in-use checks and snapshot rebinding.
3. Complete historical known-class handling and the remaining ownership delivery
   and late-discovery paths.
4. Finish delayed-subscription evaluation, MOM pending-message accounting and
   cross-generation restore/callback state.
5. Complete the normative obligation inventory and independently reviewed,
   requirement-linked evidence for the full-2010 claim.
