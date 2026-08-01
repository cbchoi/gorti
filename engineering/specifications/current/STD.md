# Software Test Description

Document version: 4.0
Status: Current
Updated: 2026-07-17

## 1. Purpose

This baseline defines the minimum evidence required to accept gorti behavior.
Passing a demonstration is insufficient; observable semantics, failure paths,
ordering, and accounting must be checked at the appropriate boundary.

## 2. Test levels

| Level | Evidence |
|---|---|
| Unit | Service state transitions, validation, encodings, and error mapping |
| Integration | gRPC handlers, streams, persistence, teardown, and callbacks |
| Conformance | IEEE-shaped API scenarios and cross-language encoding vectors |
| Determinism | Repeated semantic projections and replay equality |
| Interoperability | Equivalent black-box scenarios on another IEEE 1516-2010 RTI |
| Performance | Semantics-gated caller, server, callback, and throughput metrics |

## 3. Required service scenarios

### 3.1 Federation and Declaration Management

- create, duplicate create, two joins, resign, destroy-with-members rejection,
  final destroy, and same-name recreation with a new generation;
- publish/subscribe and unpublish/unsubscribe with valid and invalid handles;
- synchronization success, missing participant, timeout, and teardown.

### 3.2 Object Management

- register/discover/update/remove an object instance;
- send and receive an interaction with fixed seeded payloads;
- receive-order and timestamp-order variants;
- zero, one, and multiple recipients;
- atomic fanout reservation failure and delivery failure;
- stale federation generation and complete teardown; and
- confirmed unary, confirmed stream, and LocalLRC completion accounting.

### 3.3 Time Management

Use two independent federate processes with lookahead 1. Both enable time
regulation and time constrained modes. Stage one timestamped attribute update
and one timestamped interaction for each measured logical step, then request
exact grants in lockstep.

The validator shall require:

- requests and grants exactly `1..count+1` for each role;
- both timestamped callbacks completed before the subscriber's related grant;
- no duplicate, rejected, dropped, or invalid callback;
- committed logical time and cleared pending state before grant visibility;
- grant withholding after TSO delivery failure; and
- immediate submission of the next request as a regression test for the grant
  visibility boundary.

### 3.4 Ownership, DDM, save/restore, and MOM

Tests cover single-owner invariants, acquisition/divestiture races, resign
cleanup, region overlap and non-overlap, non-DDM equivalence, versioned save
bundles, restore rejection, and standard management-object reflection.

## 4. Cross-language tests

Go, Python, and C++ shall consume the same FOM and encoding vectors. For each
supported data type, encoded bytes and decoded values must match. Equivalent
federates shall produce the same canonical FM, DM, OM, and TM projection after
language-specific setup records are removed.

## 5. Requirement traceability

| Requirement groups | Primary executable evidence |
|---|---|
| FR-FM, FR-DM, FR-OM | `rti/spec/`, `tests/conformance/rti/`, SDK integration tests |
| FR-TM | `rti/internal/time/`, `rti/spec/`, `verification/gorti-go-fair/` |
| FR-OWN, FR-DDM, FR-MOM | Corresponding `rti/internal/*` tests and conformance fixtures |
| FR-FOM, FR-ENC | `rti/pkg/fom/`, `rti/pkg/encoding/`, `tests/conformance/` |
| FR-EVT, save/restore | `rti/internal/eventlog/`, `rti/internal/savepoint/` |
| IR-WIRE, IR-SDK, IR-CB | `proto/rti/v1/`, transport tests, and SDK tests |
| NFR-DET, NFR-PERF | deterministic examples and `verification/fair-comparison/` |

The listed directories are ownership-neutral evidence locations. Each public
behavior change must add or update a focused test close to the affected
implementation and, when externally observable, an integration or conformance
scenario.

## 6. Fair performance protocol

Comparative evidence requires:

- identical FOM bytes and recorded SHA-256;
- seed 1516 and identical generated payloads;
- two independent federate processes per implementation;
- identical sequential or parallel choreography;
- identical callback handling and declared runtime profiles;
- the same caller and completed-delivery boundaries;
- five discarded warm-up pairs and 30 measured pairs;
- alternating, balanced AB/BA order;
- exact service and delivery accounting; and
- semantic acceptance before any latency or throughput result is reported.

Cross-product publication comparisons use the default `gorti-hla-core`
profile. The `gorti-audit-replay` profile is accepted only as a separately
labeled intra-gorti plugin-overhead experiment with exact successful-run
callback evidence equality.

Report median, p95, p99, paired ratios, paired-bootstrap 95% confidence
intervals, and order effect. Queue admission shall not be compared with a
confirmed server-completion boundary.

## 7. Acceptance and rejection

A test run is accepted only when configuration provenance, FOM identity,
process count, semantic projection, callback order, logical grants, and
delivery accounting all pass. Missing provenance, a changed measurement
boundary, callback-before-grant failure, partial teardown, or retries that hide
a deterministic failure reject the run.

## 8. Standard commands

The source and documentation baseline is:

```text
buf generate
go test ./...
python -m pytest pysdk/tests
make determinism
python -m mkdocs build --strict
```

The race profile additionally runs `go test -race` over the affected Go
packages. The C++ profile configures dependencies, builds the SDK, runs CTest,
and runs the DLC conformance fixtures. Verification-tool tests under
`verification/**/tests` are required when their corresponding runner changes.

`scripts/ci-gates.sh` requires a pre-provisioned C++ toolchain and is not a
clean-checkout bootstrap command. Comparison wrappers must establish every
Section 7 provenance and acceptance condition before their output is treated
as claim-grade evidence. Any omitted gate must include the reason.
