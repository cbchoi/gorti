# IEEE 1516-2010 Conformance Assessment

Assessment date: 2026-09-18

Inspected baseline: working tree at `067082f` on
`feature/pyjevsim-rl-thesis-governance`, including uncommitted HLA refactoring.
The findings describe this working tree, not an immutable release.

## Assessment

Implementation updates after this inspected baseline are tracked in the
[2010 implementation progress](ieee1516-2010-implementation.md). Findings
below describe the original assessment; a partial SDK correction does not
close the corresponding server-semantic obligation.

gorti cannot currently claim complete IEEE 1516-2010 conformance. The
implementation contains substantial HLA functionality, but standard API
operations are missing, some public parameters are ignored, and some
callbacks and data encodings do not preserve standard semantics.

This assessment combines source inspection with the official IEEE 2010 API,
MIM, and FDD schema distribution. It is not a completed clause-by-clause audit
of all three normative standards. The full normative texts, applicable
corrections, and their conformance provisions must be reviewed before closing
the requirements matrix. No conformance percentage is assigned.

No production code was changed and no new runtime conformance campaign was
executed for this assessment. Existing passing tests demonstrate only their
asserted behavior; they do not resolve the source-level gaps below.

## Target and evidence

The target comprises three related documents:

| Document | Responsibility to assess |
|---|---|
| IEEE 1516-2010 | Framework and rules, including the division of responsibilities between RTI, federates, and federation |
| IEEE 1516.1-2010 | Services, state transitions, callbacks, exceptions, language bindings, support services, and MOM |
| IEEE 1516.2-2010 | Object-model representation and the RTI's applicable FOM/FDD, modularity, data-type, and encoding obligations |

An RTI cannot establish that every user-supplied federate and federation
complies with the rules. The product claim must identify RTI obligations,
supported standard bindings, platform/compiler combinations, and any
conditions permitted by the normative conformance provisions.

The official 2010 distribution provides C++, Java, and WSDL APIs. Go and
Python are gorti-specific bindings to the services, not standardized 2010
language bindings. Completing the C++ standard binding is a useful first
milestone. Whether a broader claim requires additional bindings must be
resolved from the normative conformance provisions; neither universal
Java/WSDL support nor their irrelevance should be assumed.

The IEEE site now lists 2025 successors. A 2010 compatibility target remains
well defined, but must be named as the 2010 edition.

### Official artifacts checked

- Source: [IEEE 1516.1-2010 downloads](https://standards.ieee.org/wp-content/uploads/import/download/1516.1-2010_downloads.zip).
- Archive SHA-256: `e6343f40b066d51b7f435c9aae6788be24f5d585e370d972c032ab0b503e27c1`.
- The official `HLAstandardMIM.xml` and the repository's
  `rti/pkg/fom/mim/standard-mim.xml` have identical SHA-256:
  `839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023`.
- A lexical inventory of virtual service declarations found 151 in the
  official `RTIambassador.h` and 147 in the gorti header. Four distinct
  service names are missing. This inventory does not validate signatures,
  ABI, exceptions, or behavior.
- Both `FederateAmbassador.h` headers contain 60 callback declarations by
  the same inventory, excluding destructors. Matching declarations do not
  demonstrate that callbacks are delivered or populated correctly.

## Confirmed implementation gaps

Paths below are relative to the repository root. Line numbers identify the
inspected snapshot and may move during implementation.

| ID | Finding and source evidence | Required work and acceptance example |
|---|---|---|
| C01 | `cppsdk/include/RTI/RTIambassador.h` omits `requestAttributeTransportationTypeChange`, `queryAttributeTransportationType`, `requestInteractionTransportationTypeChange`, and `queryInteractionTransportationType`, present in the official API. | Add exact standard declarations and end-to-end behavior, including confirmation/report callbacks and operation-specific exceptions. Compile and execute clients using the official interfaces. |
| C02 | `cppsdk/src/dlc/RTIambassadorImpl.cpp:1363` accepts both order-change methods without changing state. `localDeleteObjectInstance` at line 929 is also a no-op. | Implement order changes and per-federate object knowledge. Test a subsequent observable operation and invalid states, not just successful method return. |
| C03 | `RTIambassadorImpl.cpp:419` ignores the supplied `ResignAction`. The bridge calls its parameterless resign operation. | Preserve all six actions through the binding and server. Verify object deletion, ownership disposition, pending acquisition cancellation, and action-specific errors separately. |
| C04 | `RTIambassadorImpl.cpp:386` and line 401 ignore join-time `additionalFomModules`; the explicit-name overload also ignores `federateType`. Federation creation at lines 307-345 ignores the requested logical-time implementation name. | Preserve names/types, apply permitted modular FOM extension atomically, and honor or correctly reject the requested time factory. Demonstrate a class added on join and an unsupported time implementation request. |
| C05 | `proto/rti/v1/stream.proto:104` and line 129 cannot carry the complete reflect/interaction callback information. `cppsdk/src/dlc/FederateAmbassadorBridge.cpp:215` supplies an empty tag, fixed transport/order values, and an invalid retraction handle. | Carry applicable tags, sent/received order, transport, retraction information, and supplemental data from the operation to the selected callback overload. Do not populate optional information unless its standard conditions are met. |
| C06 | `rti/internal/hla/mom/handlers_switches.go:41` rejects values whose length is not one. The official MIM, also present at `rti/pkg/fom/mim/standard-mim.xml:2646`, defines `HLAswitch` using `HLAinteger32BE`. | Decode the standard four-byte representation and validate enumerators. A switch encoded as `00 00 00 01` must be accepted in a valid MOM request; malformed encodings must follow the applicable error behavior. Audit other MOM codecs using independent standard vectors. |
| C07 | `RTIambassadorImpl.cpp:492` makes `federateSaveBegun` a no-op; lines 517 and 546 poll save/restore state and discard it without delivering the requested status callback. At line 532, restore-not-complete is mapped to a federation abort. | Implement the standard save/restore state machine, per-federate progress, status vectors, failure reasons, and scheduled saves. Test rejection and failure sequences as well as successful round trips. |
| C08 | `RTIambassadorImpl.cpp:630` and line 664 ignore active/passive subscription parameters; DDM subscriptions also ignore these and update-rate arguments. Most advisory switches at lines 1945-1956 are empty or merely record state without gating delivery. | Apply declaration and advisory semantics and handle update-rate designators according to the normative requirements. Verify callbacks after each switch and active/passive transition. |
| C09 | `RTIambassadorImpl.cpp:1639` always returns `NO_ACTION` and ignores the automatic-resign setter. Lines 1690 onward unconditionally reject several lookup services even after a connection. | Implement the actual state and lookups. Test getters after setters and connected/joined lookup calls with valid and invalid values. |
| C10 | `rti/internal/hla/core/time.go:7` and the wire use floating-point time. The C++ bridge accepts only `HLAfloat64Time` for service calls despite exposing integer-time headers. | Define the standard time implementations actually supported, validate requested factory names, and provide exact representation across the entire path for every supported implementation. Do not convert large integer time through `double`. Determine any additional mandatory time-type obligations from the standard. |

Comments describing a service as a "spec-legal no-op" are not evidence of
conformance. A no-op is acceptable only when the normative service semantics
permit it for that state and input. `NullFederateAmbassador` default callback
bodies are a different matter and are not classified as defects here.

## Areas requiring a full semantic audit

### Time management and callback scheduling

The wire contains no time-regulation-enabled or time-constrained-enabled
event. However, the C++ adapter does synthesize and queue both callbacks
after RPC success (`RTIambassadorImpl.cpp:1220` and line 1246). Therefore it
would be inaccurate to say that these callbacks do not exist anywhere.
Callback synthesis and the absence of a particular Protobuf message are not
themselves standard violations.

The domain currently enables the flags directly
(`rti/internal/hla/time/manager.go:145` and line 274). Audit the complete
enable-pending/enabled behavior, permitted calls before callback delivery,
the callback's granted initial time, late join, and peer time constraints.
Apply equivalent service semantics to each advertised language profile.

Audit TAR, TARA, NMR, NMRA, FQR, GALT, LITS, zero lookahead, lookahead
changes, asynchronous delivery, retraction, and RO/TSO behavior against their
individual rules. Do not impose one generic grant rule on all advance
services. Include three or more regulating federates and simultaneous
requests to expose dependencies absent from two-federate examples.

### FOM, MIM, MOM, and DDM

The standard MIM is already byte-identical to the official file. The
remaining work is to implement its applicable behavior and data encodings.
The default MOM dispatcher currently registers switch and counter-request
handlers (`rti/internal/hla/mom/dispatch.go:107`). Inventory every applicable
MOM object, attribute, request, report, and service against the standard;
the existence of the full XML is not behavioral coverage.

The FOM parser's strict validator checks an element-name whitelist
(`rti/pkg/fom/parser/strict.go:30`). This alone is not XML schema validation
or complete OMT validation. Audit namespaces, structure, cardinalities,
cross-module consistency, module merge rules, inheritance, data types,
dimensions, update rates, transport/order declarations, and custom MIM rules.
SISO explicitly states that even validation with its OMT schema does not
guarantee full OMT conformance.

Audit DDM region creation, dimension sets and limits, association, committed
modifications, deletion restrictions, default regions, ownership, and
overlap behavior at boundaries. The adapter uses an older routing-space
abstraction internally; that is acceptable only if its externally observable
2010 dimension/region semantics are preserved.

### Exceptions, ownership, and interface compatibility

Create a per-operation matrix of legal states, invalid arguments, defined
exceptions, allowed transitions, and resulting callbacks. Error mapping
currently varies by language (`engineering/specifications/current/IDD.md:144`).
Include not-connected/not-joined states, save/restore in progress, unknown
objects and handles, ownership changes, pending requests, and callback
re-entry restrictions. Apply exception ordering only where the standard
actually specifies it.

Audit ownership negotiation/cancellation, attribute-level atomicity,
divestiture-if-wanted results, delete privilege, and resign while negotiation
is pending. A post-query reconstruction of an atomic result needs particular
attention (`RTIambassadorImpl.cpp:1076`).

Check official headers, overloads, handles, enums, factories, encoders,
callback models, and supported C++ compiler/ABI combinations independently
of the repository's own headers. A client compiled solely against gorti's
modified header cannot expose methods missing from that header. The earlier
API name inventory is only an initial check, not an ABI test.

## Implementation milestones

| Milestone | Deliverable | Exit criterion |
|---|---|---|
| M0: Normative baseline | Obtain the three standards and applicable corrections; record edition, binding scope, and every applicable obligation in a traceability matrix. | Each obligation has an owner, source clause, applicability rationale, implementation location, and verification method. No unreviewed item is counted as compliant. |
| M1: Interfaces and encodings | Close C01 and C06; define complete domain/wire callback types and logical-time negotiation; audit official API signatures. | Independent standard-header checks and standard encoding vectors pass. New fields preserve old wire interpretation or have an explicit migration path. |
| M2: Service behavior | Close C02-C05 and C08-C09; complete FM, declaration, object, support, ownership, and DDM binding paths. | Positive, negative, and concurrent black-box tests exercise each applicable service, overload, parameter, and exception without compatibility stubs. |
| M3: Time and delivery | Complete the time/callback state audit, C10, retraction, ordering, and LocalLRC completion semantics. | Multi-federate adversarial schedules satisfy the standard's allowed ordering and state transitions for each advance primitive. Local admission cannot hide an exception required at service return or reorder a dependent service. |
| M4: Save/restore and MOM | Close C07, complete modular FOM/MIM audit, and implement remaining applicable MOM behavior. | Failure-injected save/restore, dynamic FOM extension, and independently encoded MOM requests produce correct standard callbacks and states. |
| M5: Independent evidence | Run the complete matrix on a clean release across supported platforms/bindings; use reference RTIs as supplementary comparisons. | Every applicable requirement has passing evidence or an accepted verification argument; no required case is skipped, expected to fail, or satisfied only by a projected trace that discards relevant information. |

For each milestone, use plan, implementation, independent review, and
reflection. Keep evidence linked to the exact commit and configuration. Fix
the oracle when a test merely preserves existing behavior. Performance
regression measurements should accompany delivery/time changes, but are a
product engineering gate rather than proof of IEEE conformance.

## Test strategy and claim rules

The local `tests/conformance/rti/ivct-subset/README.md` explicitly describes
35 selected tests and excludes several service groups. The C++ sweep's
`EXPECTED_VERDICTS.txt` contains scenario-local FULL verdicts and two explicit
skips. Neither is an all-requirements conformance statement.

The upstream IVCT framework focuses on interoperability requirements for
federated simulations and HLA federates. Running it, or tests inspired by it,
does not automatically certify an RTI implementation. Independent
certification, if sought, requires an identified program, applicable RTI
test scope, assessor, and report. A currently available full 2010 RTI
certification service was not established by this assessment.

Build test oracles from normative allowed behavior. Other RTIs are useful
comparators but are not the specification. Compare required partial orders,
payloads, state changes, and errors; permit scheduling differences that the
standard permits. Handle normalization must not discard tags, supplemental
information, exception identity, or distinctions under test.

Release evidence should include the requirement matrix, supported binding
and platform declaration, source/binary identifiers, exact tests and
commands, verdicts, and independently reviewable results. Only then is a
self-assessed conformance claim defensible. An independently verified or
certified claim requires the corresponding external evidence.

The following architectural choices do not by themselves need replacement:
the Go server, centralized authority, gRPC/Protobuf, LocalLRC queues,
pipelining, and cumulative ACKs. They are implementation mechanisms. Audit
journaling, replay, multi-server failover, Java performance, and a particular
network topology are not substitutes for service conformance. An API's
synchronous return is not a universal requirement to wait for remote receipt;
the obligation is its defined observable service behavior and errors.

## Sources

- [IEEE 1516-2010: Framework and Rules](https://standards.ieee.org/ieee/1516/3744/)
- [IEEE 1516.1-2010: Federate Interface Specification and official downloads](https://standards.ieee.org/ieee/1516.1/3745/)
- [IEEE 1516.2-2010: Object Model Template Specification](https://standards.ieee.org/ieee/1516.2/3746/)
- [SISO standards-related data files and schema distinctions](https://www.sisostandards.org/page/DataFiles)
- [IVCT project and test scope](https://github.com/IVCTool)
- [IVCT operation documentation](https://github.com/IVCTool/IVCT_Operation)
