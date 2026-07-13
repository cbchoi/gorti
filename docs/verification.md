# Verification and interoperability

gorti verifies HLA behavior at three levels:

1. Unit and integration tests exercise service invariants and generated
   protocol handlers.
2. Cross-language tests compare Go, Python, and C++ encodings and callbacks.
3. Canonical two-federate scenarios compare observable service semantics with
   Pitch pRTI.

## Service coverage

The verification scenarios cover federation management (FM), declaration
management (DM), object management (OM), and time management (TM), including
create/join/resign/destroy, publication and subscription, discovery, attribute
updates, interactions, time regulation, time constraints, requests, grants,
and timestamp-order callbacks.

## Canonical logs

Each implementation writes a detailed raw log. A validator projects it to the
same FM, DM, OM, and TM records only after checking required calls, callbacks,
payloads, synchronization points, and logical times. Semantic equality is a
full canonical-record comparison, not an event-count comparison.

Pitch evidence is black-box: successful standard API returns, callbacks, and
runtime logs. gorti also records its server event log. The project claims
equivalence only for the common observable contract represented by each test.

## Run the semantic suite

On Windows PowerShell with Pitch pRTI installed and its RTIexec online:

```powershell
.\verification\run_all.ps1 -Seed 1516 -Count 100 -MaxIterations 3
```

The bounded runner records PLAN, DO, REVIEW, and REFLECT artifacts for every
iteration. Configuration errors fail immediately; retries are reserved for
runtime failures that can produce new evidence.

## Limits

The repository's IVCT-inspired subset is not formal IVCT certification. Some
Pitch fixtures are constrained by the free edition's two-federate limit. The
scoreboard and scope notes are maintained in [Pitch parity](PITCH_PARITY.md).
