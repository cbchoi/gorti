# Java Provider Foundation Verification

Date: 2026-09-22

## Decision And Delivered Scope

Go and Python remain the primary federate development interfaces. Java is added
as the selected standard language mapping, using the official `hla.rti1516e`
API declarations and the existing Go server. Existing C++ code is preserved;
finishing C++ is not the prerequisite for this Java work.

The new Maven project contains API, generated wire, and client/provider modules.
No Go server algorithm, existing SDK source, or Protobuf schema was changed for
this increment. The provider uses authenticated session tokens, generation-aware
join/stream setup, explicit stream readiness, evoked callbacks and bounded RPCs.
It does not synthesize time activation or grant callbacks from RPC responses.

The generated [service inventory](../../javasdk/service-coverage.tsv) contains
172 `RTIambassador` signatures: 45 are mapped and 127 remain pending. This is a
method inventory, not a normative requirement coverage percentage. The implemented
paths and remaining areas are described in the [SDK guide](../../javasdk/README.md).

## Source And Runtime Provenance

- Official Java API archive SHA-256:
  `66485e053b6183df2df94e35de7c609677f2ccaef0830edde014aad066e41890`.
- All 204 imported source files were compared with the external ZIP. Exactly two
  differ: `RtiFactoryFactory` and `LogicalTimeFactoryFactory`. Both retain their
  public signatures and attribution; discovery uses `ServiceLoader` instead of
  Java 11's ImageIO-only `ServiceRegistry`. The other 202 files are byte-identical.
- No Portico or commercial RTI implementation or JAR is used. IEEE API material
  retains the permission described in [NOTICE](../../javasdk/ieee1516-api/NOTICE.md).
- Tested runtime: Windows, Temurin JDK 11.0.31, Maven 3.9.16, gRPC Java 1.84.0,
  Protobuf/protoc 3.25.9. The server was built from the current local source.
- Linux: Bash script syntax passed under WSL Ubuntu 24.04. The WSL environment
  has no Java runtime, so no Linux Java build or live run is claimed.

The complete independent compile/link/replacement campaign in J0 remains open;
source identity and a working adapted loader do not by themselves complete it.

## Plan, Do, Review, Reflect

**Plan.** Keep Go/Python central, reuse the existing wire contract, introduce the
standard Java provider without a second RTI algorithm, and test real callback
boundaries before claiming service completion.

**Do.** Added types, transport, typed service forwarding, service discovery,
exception mapping, examples and tests. The initial factory test failed on Java
11 with `RtiFactory is not an ImageIO SPI class`. Replacing the loader in the two
documented helpers made the same test pass. This is the retained RED/GREEN case;
tool installation and build preparation are not counted as RED evidence.

**Review.** Independent review identified pending-time state ending before callback
evocation, terminal errors competing with a full callback queue, stale membership
after connection loss, and incomplete cleanup if stream teardown fails. It also
identified duplicate-name exception translation and callback `AssertionError`
being reported as success.

**Reflect.** Pending enable/advance state now lasts until the actual callback is
invoked; it changes before invoking user code so legitimate callback reentry can
proceed. Terminal failures are retained outside the bounded queue. Evoking a
connection-loss callback clears local connection/membership state, enabling an
explicit reconnect. Successful resign clears local membership even if transport
teardown reports failure. Callback failures are reported as failed invocations.
The final regression campaign below passed after these changes.

## Results

Final Maven `verify` with the real-server endpoint supplied: **51 tests, 0 failures,
0 errors, 0 skips**.

| Suite | Tests | Evidence |
|---|---:|---|
| `HandlesTest` | 8 | Type isolation, wire values, factories and encoding bounds |
| `TimeValuesTest` | 13 | Float64 factory, values, arithmetic and encoding |
| `WireSessionTest` | 17 | Metadata, identity snapshots, deadlines, readiness, ordered stream delivery, errors and cancellation using in-process gRPC |
| `ProviderTest` | 6 | Standard factory discovery, disconnected calls, explicit unsupported services, configuration validation and canonical exceptions |
| `ProviderLifecycleTest` | 6 | Pending-state rejection, overflow while callbacks are disabled, connection-loss/reconnect and unsuccessful callback receipts using loopback gRPC fixtures |
| `LiveFederationTest` | 1 | Actual Go RTI lifecycle and two-federate time coordination, including negative cases |

The live test uses two participating Java sessions in one JVM, plus a temporary
third connection for the duplicate-name case. Both participants become regulating
and constrained through actual evoked server callbacks. A TAR remains blocked
while the peer stays at zero and is granted after the peer advances. The test also
covers TARA, NMR, NMRA, FQR, queries, lookahead modification, asynchronous-delivery
switch errors, handle roundtrip and withholding/releasing TAG through callback
disable/enable. Callback thread identity and cleanup are asserted.

This is not an independent-process, cross-language, performance, or cross-vendor
benchmark. The loopback fixture suites validate client behavior; they do not
substitute for real-server service tests.

The Windows example script completed a separate real-server run:
`timeRegulationEnabled(0)`, `timeConstrainedEnabled(0)`, then TAR/TAG at 1, 2 and 3,
followed by resign, destroy and disconnect. This diagnostic has one federate.
The example build skips tests for execution speed; its success is separate from
the full 51-test verification run.

Raw logs stay outside the repository under `D:/workspace/gorti/`:

- `java-foundation-test-initial.log`: reproduced factory-discovery failure.
- `java-foundation-test-fixed.log`: corrected factory and initial unit tests.
- `java-foundation-live.log`: first real-server campaign.
- `java-foundation-example.log`: Windows example execution.
- `java-foundation-final.log`: final 51-test verification campaign.

The generated-facade/inventory consistency check and imported-source comparison
are separate checks. Existing Go/Python/C++ full regression suites were not rerun
for this Java-only increment, and their earlier counts are not Java evidence.

## Remaining Acceptance Work

J0-J6 in the [Java plan](../design/ieee1516-2010-java-binding-plan.md) remain the
completion criteria. Declaration/object exchange, synchronization, ownership,
DDM, save/restore, MOM, retraction, remaining callbacks, collection/encoding
factories, Integer64, cross-binding/process runs and the complete normative
exception matrix are still required. TLS connection configuration is implemented
but was not exercised by the plaintext live campaign.

Teardown-timeout cleanup was reviewed and implemented; the final provider suite
does not inject that exact timeout. The full standardized Float64 domain and
encoding obligations still require normative review beyond the retained vectors.
No J0-J6 stage, G0-G4 gate, or whole-RTI conformance verdict is closed solely by
this foundation result.
