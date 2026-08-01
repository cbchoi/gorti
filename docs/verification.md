# Verification and interoperability

Verification is split across three levels:

1. Unit and integration tests exercise service invariants and generated
   protocol handlers.
2. Cross-language tests compare Go, Python, and C++ encodings and callbacks.
3. Canonical two-federate scenarios compare observable service semantics with
   reference IEEE 1516-2010 RTI.

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

Reference RTI evidence is black-box: standard API returns, callbacks, and
runtime logs. gorti also records its server event log. Comparisons cover only
the observable contract represented by each test.

## Run the release checks

Run the local implementation checks first:

```text
go test ./...
python -m pytest pysdk/tests verification/common verification/gorti
```

The reproducible Portico/gorti comparison is launched from PowerShell:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass `
  -File verification/portico/RunComparison.ps1
```

The comparison defaults to five warm-up pairs and thirty measured pairs with
alternating AB/BA order. See [Reproducibility](reproducibility.md) before using
its output in a performance claim.

## Limits

The repository's IVCT-inspired subset is not formal IVCT certification. Some
reference RTI fixtures are constrained to two federates. The normative
acceptance scope is maintained in the
[Software Test Description](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/STD.md).
