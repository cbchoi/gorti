# IEEE 1516-2010 Java Binding Plan

Date: 2026-09-22
Status: Selected implementation target; no Java completion or conformance verdict.

The [foundation implementation report](../verification/ieee1516-2010-java-foundation-20260922.md)
records the initial Windows provider, real-server verification and outstanding
acceptance work. It does not mark the complete Java mapping or any whole-RTI gate
as satisfied.

The [exchange implementation report](../verification/ieee1516-2010-java-exchange-20260922.md)
adds collection factories, declaration/object/synchronization services and
callback metadata handling. It advances J2-J4 without marking those stages
complete; encoding helpers, stateful services and cross-binding evidence remain.

The [stateful-services report](../verification/ieee1516-2010-java-stateful-20260929.md)
records the next J2/J4 increment: standard encoding helpers, ownership, regional
routing and same-session save/restore. Additive DDM association, unrestricted
multi-pair operations, restart restoration and the remaining standard-provider
services were the remaining scope at that checkpoint.

The subsequent 2026-09-29 completion increment maps all 172 RTIambassador
signatures, adds atomic grouped DDM operations and registration, persists private
Java runtime checkpoints, and adds exact local Integer64 factories. It also tests
restoration in a replacement JVM. This closes specific implementation gaps, not
J4-J6 as whole semantic/release gates: exact server-side Integer64 time, complete
region lifecycle, saved-message recovery and full cross-binding evidence remain.

## Decision

Go remains the RTI implementation language. Go and Python remain the primary
federate development environments. Add Java as the selected IEEE 1516.1-2010
standard language mapping, using `hla.rti1516e`, its subpackages, and the existing
gorti server. C++ completion is not a prerequisite for the Go/Python milestones
or the selected Java assessment. Preserve existing C++ code and compatibility
evidence without extending its claim.

This decision replaces the C++-first choice in W7 of the
[conformance execution plan](ieee1516-2010-conformance-execution-plan.md).
It does not waive the shared backend requirements or change the
[quality gates](../verification/ieee1516-2010-quality-gates.md).
A generated Java gRPC client or a product-specific Java SDK is a useful
foundation, but is not the standard Java mapping. The first standard-provider
increment targets FM/TM foundation paths; it is not a complete FM/TM or Java API
implementation merely because those paths become runnable.

## Sources And Licensing Boundary

The source identity and redline interpretation policy are registered in the
[source-based review](../verification/ieee1516-2010-normative-review-20260921.md).
The following locators use one-based pages of the supplied
`15161_2010-2004_redline.pdf`; they are not printed page numbers.

| Source | Locator | Planning consequence |
|---|---|---|
| Language-neutral mapping | 12.1, PDF 323 | Federate implementation language must not alter the required federation behavior. |
| Standardized time and callback concurrency | 12.3-12.6, PDF 325-340 | Assess both standard time types, callback serialization, evoked-thread identity and service reentrancy. |
| Callback overload selection | 12.8-12.10, PDF 341-342 | Untimestamped RO, timestamped RO and received-TSO callbacks have distinct argument forms. |
| Java API, factories and packaging | 12.11-12.11.3, PDF 342-346 | Preserve the standard API surface; implementation classes belong outside `hla.rti1516e`; register RTI and time factories. |
| Java service/type mapping | 12.11.4-12.11.6, PDF 346-361 | Cover overloads, exceptions, handle types, collections, encodings, time factories and parameter ownership. |
| RTI conformance | 13.2, PDF 390 | At least one standard language-specific API is required; the assessment identifies the API actually verified. Java is the selected API, not an additional universal C++ obligation. |

The official [IEEE interface download](https://standards.ieee.org/wp-content/uploads/import/download/1516.1-2010_downloads.zip)
contains `IEEE1516-2010_Java_API.zip`. The local copy was inspected on 2026-09-22:

- Outer download SHA-256, from the registered source check:
  `e6343f40b066d51b7f435c9aae6788be24f5d585e370d972c032ab0b503e27c1`.
- Java ZIP SHA-256:
  `66485e053b6183df2df94e35de7c609677f2ccaef0830edde014aad066e41890`.
- It contains 204 Java source files under `java/src/hla/rti1516e/`; no separate
  license or README file was present. This is an artifact count, not a requirement
  or test-coverage denominator.
- The inspected API headers grant royalty-free copying, distribution, display
  and derivative works with the attribution
  `Reprinted with permission from IEEE 1516.1(TM)-2010`.
  Preserve the original notices and that attribution for material used under
  this grant; do not describe it as gorti-authored or automatically relicense it
  under gorti's project license.

The API-header grant is separate from the licensed standard PDFs. This planning
change copies neither the PDFs nor the API source into Git. The SDK integration
owner manages API packaging, notices and provenance separately. Retain the
original external ZIP for an independent compile baseline and record any later
modifications explicitly. This is a source-header inventory, not a general legal
opinion about third-party RTI code or their distribution terms.

### Baseline Discrepancies To Resolve Explicitly

The ZIP declares `RtiFactory.getRtiAmbassador()` and
`RtiFactoryFactory.getAvailableRtiFactories()`. Some prose examples on PDF 343
use different names. Compile against the actual pinned API declarations; record
the discrepancy and check applicable corrections rather than inventing aliases
inside the standard API package or treating an example as its signature.

The supplied `RtiFactoryFactory` and `LogicalTimeFactoryFactory` use
`javax.imageio.spi.ServiceRegistry.lookupProviders`. Java 11 limits that loader
to Image I/O service types and documents `IllegalArgumentException` for other
types; it directs general providers to `ServiceLoader`.
[Java 11 ServiceRegistry documentation](https://docs.oracle.com/en/java/javase/11/docs/api/java.desktop/javax/imageio/spi/ServiceRegistry.html).

J0 must reproduce the factory-loading outcome on the declared JDKs. Keep an
unaltered API baseline distinct from any modern-JDK compatibility adaptation.
For an adaptation, preserve public signatures and service-registration entries,
retain attribution, publish the exact change and test named/default/enumerated
factory selection. Direct construction of a gorti factory cannot replace this
test. Do not call an adapted helper byte-identical to the official source or
count it as unmodified-runtime evidence.

## Architecture

```text
Java federate (standard hla.rti1516e interfaces)
    -> gorti Java standard provider
         handles / encodings / exception mapping / callback dispatch
    -> shared Java transport foundation
         generated Protobuf / gRPC / session and stream lifecycle
    -> existing Go gRPC adapter and application services
    -> existing HLA domain services

Go and Python federates -> their existing SDKs -> the same Go backend
```

The planned build uses `javasdk/ieee1516-api` for the API artifact,
`javasdk/wire` for generated transport types and `javasdk/client` for gorti's
client/provider implementations. The initial build target is Java 11. Pin actual
Maven, gRPC and Protobuf versions in the build and evidence report; a target
setting alone does not establish a tested runtime matrix.

Keep Java under the SDK boundary; do not create a second RTI algorithm in Java.
Generated wire types stay behind the standard provider and must not leak into
standard method signatures or callbacks. Product extensions use a separate
implementation package. Reuse the transport foundation rather than maintaining
separate protocol clients for product and standard APIs.

RPC acknowledgement means request acceptance/completion as specified for that
service, not completion of a pending HLA state transition. Deliver
`timeRegulationEnabled`, `timeConstrainedEnabled`, `timeAdvanceGrant` and
`requestRetraction` from actual server events with their actual metadata.
Never synthesize enable callbacks after a successful RPC. Do not put all standard
calls through an asynchronous local-acceptance path that loses required immediate
exceptions; transport pipelining must preserve each service's observable contract.

## Staged Delivery

J0-J6 are project stages within W7, not IEEE clause numbers. Source inspection
above is preparatory evidence only; no stage is marked complete by this plan.

| Stage | Implementation and dependency | Acceptance evidence |
|---|---|---|
| J0: API and runtime baseline | Pin official ZIP, derive the signature/type inventory and select supported Windows/Linux JDKs. Resolve factory-loading compatibility and distribution notices. Can start independently of W1-W6. | Independent clients compile against the external API; runtime class origins and JAR hashes prove which API/provider loaded. Explicit results for factory discovery, naming, provider replacement and any adapted helper. |
| J1: FM/TM provider foundation | Use existing protocol, configuration, session fencing and stream lifecycle. Attach the standard facade to the product foundation for connect/create/join/resign/disconnect, safe time activation, advance and query paths. | Real-server lifecycle and FM/TM cases. Events before/after RPC completion are neither lost nor duplicated. Actual enable callbacks; serialized dispatch, evoked-thread identity, callback enable/disable, reentrancy and teardown cases. Publish the exact implemented signatures; other methods remain open. |
| J2: Types, factories and encoding | Complete official handles, collections, all required encoding helpers and the two standard time factories. Backend exact-time delivery depends on W2. | Independent byte vectors, nested/aligned encodings, malformed input, type isolation, equality/hash, copy/alias rules, full required collection operations, factory registration and exact integer values above `2^53`. Local time objects alone do not close end-to-end time support. |
| J3: Federation and basic exchange | Complete every FM, synchronization, declaration, OM and associated support overload, including FOM/MIM URLs and additional join modules. Reuse W3/W6 corrections. | Two independent Java processes plus Java/Python and Java/Go cases for create/join, synchronization, discovery, update/reflect, interaction, removal and typed errors. Check all metadata-bearing callback forms, not just payload equality. |
| J4: Time and stateful services | Complete TAR/TARA/NMR/NMRA/FQR, safe activation, retraction, DDM, ownership and immediate/timed save/restore. Depends on applicable W1-W4/W6 closure and W2 for both time kinds. | Pending-state and failed-admission cases; actual delivered-message/grant order; legal/illegal retract and object-knowledge restoration; region routing; ownership negotiation; restored identities, times and callback epochs. |
| J5: Complete mapping and MOM | Reconcile all official methods/overloads, exceptions, enums, callbacks, encoders and factories with the normative inventory. Complete support switches and MOM paths with W5/W6. | No unexplained unmapped member, ignored argument, silent no-op, placeholder exception or missing callback. Positive and negative cases exercise the real provider/server path; non-executable duties have reviewed inspection evidence. |
| J6: Release assessment | Integrate with W0-W8. Run the complete selected Java campaign and Go/Python regressions on each claimed platform/runtime. | Exact expected/executed test IDs, no hidden skips, independent API provenance, settled-source/build hashes and reviewed coverage. Java subset release and whole-RTI assessment remain separate verdicts under G0-G4. |

Go/Python correction work continues throughout. Java can consume already stable
services while unfinished backend obligations remain explicit dependencies;
staging does not justify declaring required services unsupported at final closure.

## Coverage Acceptance

Maintain one Java mapping record per official method signature or callback
overload, linked to its separate normative obligations. Record API source member,
clause, backend path, conversion rules, declared exceptions, test IDs, platform,
callback model, time kind, implementation status and verification status.
Do not use interface compilation or a generic catch-all adapter as semantic proof.

| Area | Required checks beyond a successful method return |
|---|---|
| API and discovery | No renamed/extra/missing standard API members; independent compile and runtime linkage; expected provider and time factories discoverable; no accidental vendor JAR or duplicate class shadowing. |
| Types and payloads | Different handle kinds cannot compare as the same designator; encoded offsets and lengths validated; tags/values preserved; caller mutation cannot change retained request state contrary to the parameter contract. |
| Callback execution | Both models assessed against their applicable rules; unsupported selections fail with the prescribed error, never silently switch. No concurrent invocation on one ambassador; evoked callbacks run on the invoking thread. Allowed callback reentry works; prohibited calls produce the specified exception. |
| Callback meaning | Correct overload and sent/received order, transport, timestamp, retraction handle and supplemental information. No fabricated activation callback, early TAG, or stale callback after resign/rejoin/restore. |
| Service errors | Standard checked exceptions per operation and state; no raw gRPC status leak or blanket `RTIinternalError` for identified service errors. Ambiguous precedence is not invented. |
| Time | Float64 and integer64 factory selection propagates through server comparisons, sends, grants, callbacks, MOM and snapshots. Preserve adjacent integers beyond `2^53`; fail unsupported kinds explicitly rather than coercing to double. |
| Stateful services | Save/restore, ownership, DDM, FOM extension, switches and MOM observed through public services, including rollback and delayed callbacks. |
| Interoperation | Same FOM bytes and encoded values across Java/Go/Python; independent federate processes and source-derived oracles. Shared backend success is cross-binding evidence, not cross-vendor RTI wire interoperability. |

Use a dispatch table or explicit adapters only where they keep these per-service
contracts visible. Temporary stubs must remain failing/unimplemented coverage
rows; making every method return a generic error is not a completed mapping.

## Parallel Work And Review

Assign exclusive files before dispatch. Java workers may own transport/lifecycle,
types/encoding, standard service mapping and independent test clients separately.
One integration owner controls shared protocol generation and session/callback
contracts. Documentation and normative review can proceed alongside code without
editing the same adapter or generated sources. This does not require adding a
second Java-specific protocol or changing the core algorithm.

For every bounded set, run plan, do, review and reflect: locate the requirement,
write a failing behavioral case for a known gap, implement in its owning layer,
review independently, then rerun and record the remaining dependencies. Preserve
RED evidence only for a real behavior failure, not missing tools or compilation.

Retain raw results outside Git, with concise reports and reproducible tests in
the repository. Link Java evidence from the existing gate rather than creating a
parallel claim system. Do not reuse earlier Go/Python pass counts as Java proof.

## Release Wording

A foundation release may state exactly which Java services and callback paths
were executed. It must not advertise a complete IEEE Java mapping, standard
language compatibility or full RTI conformance before J0-J6 and the applicable
shared requirements are satisfied. C++ remains a separately scoped product
surface; its unfinished callbacks neither prove Java failure nor become silently
certified when Java passes. External certification requires its own scoped
evidence under the existing quality-gate policy.
