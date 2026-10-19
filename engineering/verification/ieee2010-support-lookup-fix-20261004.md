# Support Lookup Exception Repair, 2026-10-04

## Correction

This change fixes the implementation defects reproduced in the
[service-test assessment](ieee2010-service-test-assessment-20261004.md).
It does not change the disputed MIM attribute spelling or include PPO/RL
tests in the result.

The server now supplies the existing `rti-spec-exception` trailer consistently
for Support name lookups. `NOT_FOUND` remains the gRPC status. Successful
responses, Protobuf messages and HLA algorithms are unchanged.

| Condition | Standard exception |
|---|---|
| Unknown object class name (10.6) | `NameNotFound`, existing behavior retained |
| Unknown attribute in a valid object class (10.11) | `NameNotFound` |
| Invalid object class for attribute lookup (10.11) | `InvalidObjectClassHandle` |
| Unknown interaction class name (10.15) | `NameNotFound` |
| Unknown parameter in a valid interaction class (10.17) | `NameNotFound` |
| Invalid interaction class for parameter lookup (10.17) | `InvalidInteractionClassHandle` |
| Unknown dimension name (10.25) | `NameNotFound` |

The three previously reproduced Java failures no longer fall back to
`RTIinternalError`. Dimension lookup had the same missing-trailer condition
and was corrected as well. The invalid-class check runs only after a failed
attribute/parameter lookup; successful lookups do not add a reverse lookup.

Python now defines and publicly exports `InvalidObjectClassHandle` and
`InvalidInteractionClassHandle`, allowing its existing metadata-first
translator to retain these distinctions. No heuristic was added to Java's
general error translator, and unrelated transport failures are not reclassified
as missing names.

## Regression Cases

- `rti/internal/adapters/grpc/support_name_error_test.go`: actual gRPC
  serialization and trailers for five name-lookup services; known, missing,
  empty and known-again names; zero and unknown parent-class handles.
- `javasdk/client/src/test/java/org/gorti/rti/LiveSupportNameErrorsTest.java`:
  nine actual-server Java API cases with exact named exceptions and successful
  lookup controls before/after rejection.
- `pysdk/tests/test_support_lookup_errors_live.py`: nine actual-server Python
  cases verifying exact public exception classes, original server trailers
  and unchanged successful lookups.
- `pysdk/tests/test_support_name_error.py`: three additional metadata/export
  checks; existing legacy-server and unrelated-error mappings are retained.

## Results

Tests were added before the production fix. The new Go test failed against
the old handlers. Python's old-server run recorded one pass and eight failures:
four missing-name mappings and four cases exposing absent invalid-class SDK
exception definitions. Original red output is retained separately.

| Final campaign | Passed | Failed/error | Skipped |
|---|---:|---:|---:|
| Go gRPC adapter, Support domain, Go SDK and M25 integration | 639 | 0 | 0 |
| Python Support and exception-mapping regression | 52 | 0 | 0 |
| Java Support, exchange, DDM and FOM regression | 74 | 0 | 0 |
| Linux focused race, 18 tests repeated twice | 36 | 0 | 0 |

Go additionally recorded 1,166 nested passes; they are not added to the
top-level count. Both language campaigns include the nine new live cases.
These are focused regressions, not a rerun of the complete conformance suite.
The changed Python files pass Ruff; scoped `git diff --check` passes.

Linux focused race verification passed with 158 nested invocations and no
race warning. It used `-race -count=2 -run Support` for the gRPC adapter,
Support domain and M25 packages, with exit code 0. The first launch had a
WSL shell-quoting error; the final invocation above completed successfully.

The initial Python launch omitted the asyncio plugin required by repository
configuration and collected no tests. The corrected red run above is separate.
The first Java attempt passed 73 cases but lacked `gorti.fom` for one existing
test. A fresh run of the same 74-case selection with that property set passed
without failures or skips; no assertion or test selection was weakened.

## Execution Evidence

The Windows RTID built for the live checks has SHA-256:

`2c02745b1a873dd434d82e90e8d76fe58127433233408479c3c2c9d09b5ba621`

Raw output and executables stay outside Git:

- `D:/workspace/gorti/support-lookup-fix-20261004/go-red.jsonl`
- `D:/workspace/gorti/support-lookup-fix-20261004/go-green.jsonl`
- `D:/workspace/gorti/support-lookup-fix-20261004/go-regression.jsonl`
- `D:/workspace/gorti/support-lookup-fix-20261004/go-race-final.jsonl`
- `D:/workspace/gorti/support-lookup-fix-20261004/go-race-final.stderr.log`
- `D:/workspace/gorti/support-lookup-fix-20261004/python-red-r2.xml`
- `D:/workspace/gorti/support-lookup-fix-20261004/python-final.xml`
- `D:/workspace/gorti/applicable-fixes-20261004/java/support-lookup-green-r2/`

The Java completion record confirms its server stopped. All eleven Python
independent-server completion records report closed processes. The MIM source
diagnostic was not rerun or reclassified as a passing test in this repair.
