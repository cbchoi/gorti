# gorti: An open-source IEEE 1516-2010 Run-Time Infrastructure in Go

**Submission status:** draft for SoftwareX. This source intentionally leaves
affiliations, ORCID identifiers, funding declarations, and archive DOI unset
until the authors confirm them.

**Author:** Changbeom Choi

## Metadata

| Field | Value |
|---|---|
| Software name | gorti |
| Repository | <https://github.com/cbchoi/gorti> |
| Version | 0.9.0 |
| License | MIT |
| Core language | Go |
| SDK languages | Go, Python, C++ |
| Supported operating systems | Linux, macOS, Windows |
| Standard | IEEE 1516-2010 (HLA Evolved) |
| Archive DOI | To be assigned for the submission release |

## Abstract

The High Level Architecture (HLA) is widely used to coordinate independently
developed simulation components, but complete open implementations of the
IEEE 1516-2010 Run-Time Infrastructure (RTI) remain limited. gorti is an
open-source RTI implemented in Go with federate access paths for Go, Python,
and the IEEE C++ Dynamic Link Compatible interface. It implements federation,
declaration, object, time, ownership, data distribution, synchronization,
save/restore, and management-object services. A deterministic event log and a
canonical semantic projection make distributed runs inspectable and
repeatable. The repository also provides a fail-closed interoperability and
performance harness. Equivalent two-process workloads execute against gorti
and Pitch pRTI with identical FOM bytes, random seed, choreography, callback
handling, logging, and measurement boundaries. Five warmup and twenty measured
pairs use balanced alternating AB/BA order, and no performance ratio is
produced unless complete semantic records and delivery accounting agree. gorti
is intended for simulation research, education, continuous integration, and
single-node distributed simulation deployments that benefit from transparent
service semantics and reproducible evidence.

**Keywords:** High Level Architecture; HLA Evolved; Run-Time Infrastructure;
distributed simulation; logical time; reproducibility; interoperability

## 1. Motivation and significance

Distributed simulation studies often combine independently developed models,
different implementation languages, and multiple execution environments. HLA
defines the services by which federates discover shared objects, exchange
interactions, coordinate logical time, transfer ownership, and manage a
federation. The RTI is therefore both middleware and part of the experiment's
semantic boundary. An implementation defect or undocumented callback boundary
can change a simulation result even when application models are unchanged.

Commercial RTIs provide mature implementations, but they can limit inspection,
automation, redistribution, or modification. Research prototypes often cover
only a subset of HLA services or embed the scheduler in one process. These
constraints make it difficult to study RTI algorithms, run unrestricted CI,
or preserve enough evidence to reproduce a cross-RTI comparison.

gorti addresses this need with a standalone, source-available server and
independent federate processes. The project treats observable behavior as the
primary compatibility target. Each verification scenario records standard API
returns, callbacks, payloads, synchronization points, and logical times. A
canonical projector rejects incomplete runs before comparing their semantic
records. Performance measurements are accepted only after that semantic gate.

The main contributions are:

1. an IEEE 1516-2010 RTI service implementation in Go;
2. Go and Python APIs plus a C++ DLC-compatible SDK surface;
3. deterministic event logging and generation-aware lifecycle controls;
4. executable cross-language and cross-RTI semantic scenarios; and
5. a provenance-rich, paired AB/BA performance protocol that fails closed on
   semantic or accounting differences.

## 2. Software description

### 2.1 Architecture

The `rtid` process exposes versioned gRPC contracts. Service handlers delegate
to internal managers for federation membership, declarations, object state,
ownership, regions, logical time, synchronization, save/restore, and management
objects. Managers share explicit federation-generation identity so callbacks
and state from a destroyed federation cannot cross into a later federation
with the same name.

Federates maintain a bidirectional event stream for callbacks. Interaction
sends use a persistent client stream with a caller-visible server ACK. This
boundary makes synchronous success and failure unambiguous. Timestamp-order
recipients are reserved atomically in the time evaluator, and a
time-advance grant is withheld when a required timestamp-order delivery fails.
Teardown stops new work, drains or cancels streams, removes membership, and
closes persistent resources.

The Go SDK resolves FOM names to handles and exposes typed events without
leaking generated protocol types. The Python SDK provides an asyncio API and a
1516-shaped adapter. The C++ SDK supplies the IEEE 1516.1-2010 DLC header
surface for supported Unix-like platforms. All SDK paths use the same server
service semantics.

### 2.2 Service coverage

Federation management covers creation, joining, synchronization, resignation,
and destruction. Declaration and object management cover publication,
subscription, reservation, registration, discovery, attribute reflection,
interaction receipt, and deletion. Time management supports regulation,
constraints, lookahead, timestamp-order delivery, and the TAR/TARA/NER/NMRA/FQR
request families. Ownership, data distribution, save/restore, message
retraction, and management-object services are also represented in the
conformance suite.

The implementation targets IEEE 1516-2010 only. It does not expose HLA 1.3 or
IEEE 1516-2000 APIs.

### 2.3 Deterministic evidence

The event log stores ordered service events with stable identifiers and
canonical encoding. Tests compare byte-identical logs for deterministic
workloads. The comparison harness separately retains application logs, server
logs, workload contracts, process manifests, hashes, canonical semantic
records, and statistics. This separation prevents timing values from deciding
whether a run is semantically valid.

### 2.4 Example workflow

The `go-tar-wait` example starts two independent federates. Both become time
regulating and time constrained. One requests logical time 5 while the other
remains at logical time 0, causing the first request to remain pending. When
the peer also requests time 5, both receive a grant. The example demonstrates
that logical-time progress depends on federation-wide state and that the RTI
delivers grants asynchronously rather than treating a request as a local clock
assignment.

## 3. Quality control

Go unit and integration tests cover service managers, protocol handlers,
event-log persistence, SDK behavior, and failure paths. Python tests cover the
async transport, public ambassador surface, generation fencing, and
cross-process behavior. C++ compile-time lockfiles protect the DLC header
shape, while runtime tests cover standard exception mappings. Cross-language
fixtures verify encoding and live publish/subscribe paths.

Concurrency-sensitive packages run under Go's race detector. Focused repeated
tests exercise atomic mixed-recipient fanout, failed-delivery grant
withholding, exactly-once grant evaluation, stream ACK drain, forced
cancellation, and complete federation teardown. CI also builds release targets
and the documentation site.

The Pitch comparison is intentionally black-box. Evidence consists of
successful standard API calls, callbacks, canonical application records, and
available runtime logs. The project does not claim source-level identity,
formal IVCT certification, or equivalence beyond the common observable
contract of each fixture.

## 4. Reproducible performance evaluation

The fair-comparison workload fixes the FOM byte sequence and SHA-256, seed
1516, two independent federate processes, sequential update/send/time-advance
choreography, immediate callbacks, file server logging, and the completed
delivery boundary. Payload construction and handle resolution occur outside
the caller sample in both systems. Request serialization, transport, server
processing, and response remain inside the synchronous caller sample.

Each claim-grade session uses five warmup pairs followed by twenty measured
pairs. Every pair runs both systems serially in alternating AB/BA order, with
ten measured pairs in each orientation. The analyzer validates the full
four-record FM/DM/OM/TM semantic projection, hashes, fanout, zero-error
accounting, metric identity, and pair balance before computing ratios. It then
reports medians, nearest-rank p95 and p99, paired ratios, deterministic
paired-bootstrap confidence intervals, and order effects.

In the retained v0.9 investigation, the paired median gorti/Pitch ratio for the
synchronous `sendInteraction` caller boundary was 1.218. gorti remained about
22% slower at that boundary, while its completed-delivery latency was lower and
object-update results were competitive. Microbenchmarks attributed only a few
microseconds to registry processing and durable event-log append; the dominant
remaining floor was the synchronous HTTP/2/gRPC request-ACK round trip and
scheduler interaction. These values describe one controlled machine and
workload, not a general vendor ranking. Submission numbers must be copied from
the archived `analysis.json` for the final tagged release.

## 5. Impact and use cases

gorti enables HLA federations to run in CI without a federate-count license and
allows researchers to inspect or replace internal strategies. Deterministic
logs support regression testing, replay analysis, and archival evidence.
Students can observe declaration, object, and time-management behavior in a
small codebase, while application developers can exercise Python, Go, or C++
federates against the same server.

The cross-RTI harness is also useful independently of absolute performance. It
turns a service expectation into an executable, canonical event sequence and
keeps the raw evidence needed to investigate a divergence. The paired protocol
discourages optimizations that appear faster only because they move the success
boundary or omit callbacks and logging.

## 6. Limitations and future work

Single-node operation is the supported deployment. Cluster and failover paths
are not production-ready. The project has no Java SDK, no formal IVCT
certification, and no HLA 1.3 or IEEE 1516-2000 compatibility. Windows C++ SDK
support remains incomplete. Pitch pRTI Free limits some scenarios to two
federates, and proprietary runtime behavior can be observed only through
public APIs and retained logs.

Further performance work is separated from the v0.9 release. The strict
synchronous interaction API will preserve its existing ACK and error boundary.
A future bounded asynchronous API may pipeline interactions with backpressure
and ordered completion handles. It will be compared only with an equivalent
Pitch asynchronous choreography. Candidates must preserve timestamp-order
delivery before grants, atomic recipient reservation, generation fencing,
complete teardown, and delivery accounting.

## 7. Availability and reuse

Source code, examples, tests, FOM modules, schemas, and verification scripts are
available under the MIT License at <https://github.com/cbchoi/gorti>. Release
archives contain the `rtid` and `rti-top` binaries and SHA-256 checksums. The
documentation describes installation, a two-federate quickstart, operations,
verification, and reproduction.

The final SoftwareX submission should cite an immutable release archive DOI.
The claim-grade output directory should be deposited separately because it
contains large raw logs and machine-specific manifests. Proprietary Pitch
binaries must not be redistributed.

## 8. Declarations

**Funding:** To be completed before submission.

**CRediT authorship contribution statement:** To be completed before
submission.

**Declaration of competing interest:** To be completed before submission.

**Data availability:** Source and executable verification inputs are public in
the repository. The immutable source and benchmark-evidence archive links will
be added before submission.

## References

1. IEEE Standards Association, *IEEE Standard for Modeling and Simulation
   (M&S) High Level Architecture (HLA): Framework and Rules*, IEEE 1516-2010.
2. IEEE Standards Association, *IEEE Standard for Modeling and Simulation
   (M&S) High Level Architecture (HLA): Federate Interface Specification*,
   IEEE 1516.1-2010.
3. IEEE Standards Association, *IEEE Standard for Modeling and Simulation
   (M&S) High Level Architecture (HLA): Object Model Template*,
   IEEE 1516.2-2010.
