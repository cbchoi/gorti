# IEEE 1516-2010 Quality Gates

Java foundation evidence is recorded in the
[2026-09-22 provider report](ieee1516-2010-java-foundation-20260922.md).
Its bounded service results do not close the complete binding or RTI gates below.
The [Java exchange increment](ieee1516-2010-java-exchange-20260922.md) records
subsequent declaration/object/synchronization and callback verification, with
the same distinction between mapped signatures and completed conformance.

Public-source research checked: **2026-09-18**; supplied normative redlines
review started **2026-09-21**. Target: the **2010 editions** of IEEE 1516,
1516.1, and 1516.2. These are proposed gorti release/claim gates, not IEEE clause
numbers or a certification scheme. Read with the
[assessment](ieee1516-2010-conformance-assessment.md) and
[requirements matrix](ieee1516-2010-requirements-matrix.md), including its later
correction checkpoints. Source research alone closes no implementation row;
executed corrections are recorded separately in the implementation progress.

Binding decision updated **2026-09-22**: Go/Python remain primary product
interfaces; Java is selected for the standard language mapping. The
[Java binding plan](../design/ieee1516-2010-java-binding-plan.md) replaces the
previous C++-first sequence without changing these claim levels or shared RTI
requirements. This selection is not evidence that Java is implemented or verified.

## Claim Levels

Every claim must identify release/build, assessed component, edition, binding,
platform, configuration, options, evidence date, and exclusions. The labels below
are local policy, not official grades. Avoid unqualified "IEEE 1516 compliant",
"HLA certified", and conformance percentages derived from test counts.

| Level / allowed wording | Required evidence | Does not establish |
|---|---|---|
| L0: "Implements listed HLA Evolved/2010 features; gaps documented" | Feature inventory and explicit limitations. | Behavioral completeness, standard binding compatibility, or conformance. |
| L1: "Passed named tests for the stated 2010 subset/profile" | Executed cases, independently reviewed assertions, exact build/configuration, raw results and all exclusions (G1-G3 for that subset). | Anything outside those assertions; a complete service group from one corrected defect; full-family conformance. |
| L2: "Internal assessment found no deviations from the listed applicable 2010 RTI requirements" | G0-G4 complete for **every applicable obligation**, including non-executable inspection/review evidence. Scope must be permitted by the standards, not selected to omit mandatory behavior. | Permission to use the standard's "HLA Conforming" label, independent organizational assessment, certification, or correctness of arbitrary user federates/federations. |
| L3: "Independently verified against the stated requirements/profile" | Named external assessor, independence/conflicts declaration, methods, exact scope and signed report. Full-conformance wording additionally requires L2 completeness. | Certification merely because the tester was independent; requirements excluded by the report. |
| L4: "Certified by [body/program] for [scope/version]" | Actual certificate, issuing authority, scheme/version, applicable RTI scope, test report, conditions and validity/status. | A broader edition, binding, release or scope than the certificate covers. |

**Interoperability is a separate claim, not a rung above L1.** Demonstrate named
independently developed federates exchanging/using information under a declared
FOM and federation agreement on a named RTI/configuration. That establishes the
tested collaboration, not complete conformance. Conversely, RTI conformance alone
does not establish agreement on application meaning, encodings or execution
policy. Running one federate separately on two RTIs is portability/differential
evidence, not simultaneous cross-vendor RTI wire interoperability. Go/Python/Java/C++
clients sharing gorti's backend are cross-binding evidence, not independent RTIs.

## Normative Baseline And Scope

The public IEEE pages distinguish framework/rules, RTI services/interfaces, and
object-model format/syntax. They identify 2025 successors; a 2010 claim remains
edition-specific. [IEEE 1516](https://standards.ieee.org/ieee/1516/3744/),
[IEEE 1516.1](https://standards.ieee.org/ieee/1516.1/3745/),
[IEEE 1516.2](https://standards.ieee.org/ieee/1516.2/3746/).

The full texts were unavailable during the September 18 assessment. On
September 21, the user supplied all three IEEE 2010 redline publications.
[The source-based review](ieee1516-2010-normative-review-20260921.md) registers
their hashes, page locators and interpretation method. Review uses retained and
added 2010 text, not deleted earlier-edition requirements. An ambiguous passage
requires a specific base-publication check; it does not block all other review.
Applicable corrections still need confirmation. Do not import 2000 errata or
2025 requirements without establishing applicability. Q01-Q10 remain open where
their obligation inventory or implementation evidence is incomplete, not because
all normative text is missing.

IEEE 1516.1-2010 Clause 13 (supplied PDF page 390) reserves the label
"HLA Conforming" for publicly available results certified by a recognized
authority. Clause 13.2 requires all specified services and at least one
language-specific API mapped according to Clause 12. The local L2 record is an
internal assessment, not permission to use that label.

The official [IEEE download](https://standards.ieee.org/wp-content/uploads/import/download/1516.1-2010_downloads.zip)
was fetched into memory and its inventory/hash checked on 2026-09-18:

- SHA-256: `e6343f40b066d51b7f435c9aae6788be24f5d585e370d972c032ab0b503e27c1`.
- Contains C++ and Java API ZIPs, `hla1516e.wsdl`, `HLAstandardMIM.xml`, and
  `IEEE1516-FDD-2010.xsd`; it does not contain the three full standards.
- These are independent interface/model baselines, not a complete test oracle.
  Clause 13.2 now resolves the minimum to at least one language-specific API;
  Clause 12 determines its mapping. Go/Python remain product-specific bindings,
  not substitutes for proving the selected standard Java binding. The Java plan
  records its separate API ZIP hash, attribution and runtime-loader review.

Before L2, resolve Q01 and record the applicable RTI, federate and federation
responsibilities separately. Declare supported binding/API version, OS/architecture,
compiler/runtime/ABI, callback models, time factories and exact representations,
transport/order modes, FOM/MIM/module handling, and enabled service options. For
each choice record **mandatory**, **conditional with trigger**, or **not applicable
with normative rationale**. "Not implemented" is not a not-applicable rationale.
Selecting Java does not require C++ and WSDL merely because their mappings exist.
The Java milestone requires the complete applicable Java contract and service
behavior, not only a generated gRPC client or passing FM/TM foundation checks.
Existing C++ evidence retains its historical scope; Java closure does not certify
that separately supplied binding.

## Quality Gates

G1-G3 can produce useful bounded evidence while G0 remains open. L2 cannot pass
without all G0-G4 obligations for a normatively valid scope. G5 is required only
for the corresponding external claim.

| Gate | Required exit evidence / blocking condition |
|---|---|
| G0: normative applicability | Authorized editions/corrections registered; complete obligation inventory with exact source locator, responsible component, applicability rationale and reviewer. Resolve Q01-Q10 or document a reviewed, clause-supported exclusion. C01-C10/N01-N12 are a defect backlog, **not** the complete standards inventory. Missing text or unresolved applicability blocks L2. |
| G1: reproducible evidence | Immutable source snapshot and binaries, official-artifact hashes, dependency/toolchain versions, options, FOM/MIM inputs, exact commands, test selection, expected case manifest, exit codes, actual verdicts and raw logs. For dirty-tree development runs include the patch/untracked inputs or content-addressed snapshot; HEAD alone is insufficient. Missing/stale artifacts, zero executed cases or undisclosed exclusions fail the claimed gate. |
| G2: binding/model contract | Compile, link and execute independent clients against official claimed interfaces without silently resolving replacement API classes or headers. For Java, retain external API hashes, compile/runtime JAR and class origins, RTI/time factory service discovery and any documented modern-JDK loader adaptation. Check overloads, types, factories, encoders, exceptions and callbacks; retain include/link provenance for any separately claimed C++ binding. Verify replacement/DLC and runtime or ABI claims for each advertised toolchain. Independently authored encoding vectors and correct schema validation supplement, not replace, semantic checks. |
| G3: observable semantics | Per applicable obligation, review the oracle and execute positive, invalid-input/state, boundary and relevant concurrent/failure cases through the real server and advertised binding. Assert resulting state, callback identity/metadata, errors, atomic outcomes and allowed partial orders, not just method return. Use matrix T01-T21 as starting fixtures, then fill the normative inventory; no-op/stub-friendly oracles block closure. |
| G4: release review | Rerun the complete applicable campaign on the settled release across each declared profile. Every obligation has passing evidence or a reviewed non-test verification argument; no applicable fail/skip/xfail/untested item is counted as satisfied. Reviewer checks oracle independence, coverage and claim wording. Archive raw results and a signed self-assessment; explicitly record remaining product limitations. |
| G5: external assessment | Agree in advance whether the assessor tests the RTI or a federate, exact 2010 requirements, bindings/options, suite coverage, exclusions, independence and deliverable. Obtain the actual report/certificate before L3/L4 wording. Tool execution, a capability badge or a generic certification webpage is insufficient. |

For each requirement retain: source/applicability; implementation and test
revisions; test IDs and oracle provenance; actual observations/verdict; evidence
location/hash; reviewer/date. Keep **engineering evidence passing** distinct from
**normative obligation closed** when a Q gate remains unresolved. A passing local
evidence-gate script can validate this record's integrity, not establish the
semantic correctness or completeness of the underlying standards mapping.

Test deadlines are harness budgets, not invented IEEE response-time rules.
Preserve tags, exception identity, payloads, order/transport, supplemental data,
retraction handles and states relevant to the assertion. Normalized/projected
traces must retain these distinctions and link to originals. Comparison with a
reference RTI is diagnostic: agreement can share a defect, and disagreement can
reflect a permitted scheduling choice. Performance/race/soak results are useful
engineering evidence, not independent substitutes for requirement coverage.

## IVCT And Other Evidence Limits

The [original IVCT framework](https://github.com/IVCTool/IVCT_Framework) describes
testing **HLA federates** against interoperability requirements. Its
[operation repository](https://github.com/IVCTool/IVCT_Operation) composes selected
test suites with an RTI as the communication environment. The concrete
[HLA_BASE suite](https://github.com/IVCTool/TS_HLA_BASE) checks a federate's
declarations, service use and object/interaction behavior against its SOM, and
checks encodings of published data. Thus "HLA services tested" here does **not**
mean a complete RTI service implementation is the certified subject.

For gorti, an IVCT run could provide evidence about a particular federate on
gorti and incidentally exercise some RTI paths. State the federate/CS/SOM,
badges, suite revisions, selected cases, RTI/provider and configuration. A custom
RTI-directed suite is possible within a general framework, but needs its own
RTI obligation map and reviewed oracles; it does not inherit full RTI coverage
or certification from the IVCT name. Confirm a usable gorti provider/binding and
deployment first; running IVCT on another vendor's RTI does not test gorti.

The matrix records the local `ivct-subset` as 35 selected Python-native tests
inspired by IVCT, not the upstream harness. Its exclusions and any xfails limit
its claim to L1. C++ `FULL` expectations likewise describe fixture-local
assertions, not entire services. Recheck actual manifests and verdicts at release;
do not treat historical expected counts as newly executed results.

[SISO's data-file descriptions](https://www.sisostandards.org/page/DataFiles)
distinguish DIF for possibly incomplete model exchange, FDD as the relaxed
minimum-information schema for RTIs, and the stricter OMT schema. SISO explicitly
warns that even OMT-schema validation does not guarantee full OMT conformance.
An official MIM hash proves file identity, not MOM behavior or encoding accuracy.

[SISO's standards catalog](https://www.sisostandards.org/page/StandardsProducts)
describes DLC as a link-compatibility objective, not equal performance or whole
HLA conformance. Its listed 2004 DLC products must not be presented as a 2010 RTI
certificate. [IEEE ICAP](https://standards.ieee.org/products-programs/icap/)
describes program-specific test plans, laboratory authorization and certificate
issuance. **A currently available, complete 2010 RTI certification service was
not established by this research**; this is not a claim that none exists. Seek
written scope/availability confirmation from the responsible program before
planning a certification gate. Independent test authorship, independent review,
external assessment and certification are different kinds of evidence.

## Actionable Proof Path

1. Complete review of the three supplied 2010 redlines and applicable corrections;
   resolve the remaining Q01 scope details. Publish only a qualified feature or
   subset claim until a permitted scope and complete inventory exist.
2. Select the official Java interface for the standard-binding candidate while
   keeping Go/Python central. Capture G1 evidence and run G2 against an external
   pinned API baseline, independently of gorti's provider/codecs. Declare exact
   JDKs, callback models, time factories and any factory-loader adaptation;
   preserve existing C++ scope without making its completion a Java prerequisite.
3. Integrate the service corrections, then execute T01-T21 and newly mapped
   obligations through the complete binding/server path. Ownership and
   MOM corrections feed this evidence record; they do not close whole services.
4. Run T22 with independently authored clients/oracles and named reference RTIs;
   record portability, federate interoperability and differential results
   separately. Review all uncovered obligations and issue L2 only after G0-G4.
5. Pursue L3/L4 only with an identified assessor/scheme accepting the actual RTI
   and 2010 scope; otherwise retain the transparent self-assessed claim.

## Source Check Record

The original authoritative URLs are linked beside their findings above and were
checked **2026-09-18**. The **2026-09-22** Java selection uses the local official
Java ZIP and supplied Clause 12/13.2 evidence; its plan separately records the
Java runtime documentation check. This update executes no Java conformance tests.
IEEE pages and the original IVCT GitHub READMEs were read via web;
the IEEE ZIP was downloaded and inspected in memory without writing artifacts.
SISO Data Files and Standards Products were read through the web search index's
contents for those official URLs; direct page opens returned HTTP 403. Accordingly,
their indexed descriptions support the narrow findings above, not confirmation
of a live certification offering. Repository documentation is mutable: pin exact
commits, suite/provider versions and retained source snapshots for an actual
campaign. No unavailable normative text or unexecuted test is represented as
verified evidence.
