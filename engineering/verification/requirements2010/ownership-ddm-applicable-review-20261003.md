# Applicable DDM time/bounds review, 2026-10-03

## Plan
Review the one applicable-unlinked DDM row, DDM2010-TIME-INDEPENDENCE, S1516.1 9.1.4, PDF page 225. Add a bounded real-service oracle, retaining all untested facets and original failures.

## Do
Linked `rti/internal/hla/ddm/applicable_r1_time_independence_test.go` declaration `TestApplicableR1DDMCommitIndependentOfConstrainedLogicalTime`.
A Float64 time-constrained region owner exercises logical times 0 and 100 using the real TimeService. Draft changes remain invisible to owner-authenticated QueryBounds; commit immediately exposes changed bounds without another time advance; edits/commit leave QueryLogicalTime unchanged.

Ownership/DDM has 29 records, 26 applicable. Applicable-linked increased **25 -> 26**, unlinked **1 -> 0**. This is a link-count result, not complete verification. No new IDs or reviewed_units; coverage_complete remains false.

## Review
First `new-oracles.jsonl` failed because the test created region at federate index 1 but queried with index 0. The real server correctly returned PermissionDenied, region not owned by this federate. That admission failure occurred before evaluating the intended committed-bounds assertion: **fixture cause, not a reproduced DDM defect**.
Queries were corrected to owner index 1; the same draft/committed/logical-time assertions passed in `review-oracles.jsonl`. First red is retained independently.

Review execution contains 44 top-level pass, 4 fail, 0 skip; nested 43 pass, 2 fail, 0 skip. Its four top-level failures are declaration/object obligations, not this DDM test. Exact artifact counts and source/test snapshots are in `D:/workspace/gorti/applicable-requirements-20261003-r1/declaration/summary.json`.

Source PDF SHA256 `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4`.
Pinned server SHA256 `fc2bd04a5e64970e2dd0f6cb37f98741a257a578e273cae6458d4fd9602d75e8`.
Official supplementary FDD XSD SHA256 `8846b46dc0ee8ee1299c0e854e65630cf4de18375f6ce1200abd904e37f5a3c1`, configured with private Python/schema-control paths and offline Go caches.
No production, existing-test or Git edits. No race campaign or Java binding verdict.

## Reflect
Still open: immediate overlap/routing effect at multiple logical times, Integer64, pending blocked advance, timestamped callback timing and other DDM services. Two logical-time points and query visibility cannot establish the entire time-independence requirement. Projection redline/visual ambiguity is not promoted to complete clause review.

The same new file also hosts opt-in raw live OM name admission controls, whose four failures and Java/product distinctions are documented in the declaration/object review. They are not DDM failures.

All owned servers (323276, 29244, 160080, 118992) stopped and absence-checked; zero owned processes remain.
