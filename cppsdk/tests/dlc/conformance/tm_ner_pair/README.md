# Time Management NER pair

This fixture verifies a regulating publisher and a constrained subscriber using
Next Event Request with timestamped interactions.

## Scenario

- The regulator publishes `Tick`, enables time regulation with lookahead 1,
  sends timestamped interactions at logical times 1 through 5, and advances by
  NER.
- The constrained federate subscribes to `Tick`, enables time constrained mode,
  advances by NER, and records each timestamped callback and grant.
- Both federates resign after the final grant.

Every expected log line cites the relevant IEEE service section.
`scripts/check-spec-traceability.sh` verifies those citations.

Traceability: timestamped interaction delivery uses // §6.13; NER and Time
Advance Grant ordering use // §8.13.

## Files

- `federation.fom.xml`: strict HLA Evolved FOM
- `test_tm_ner_pair.cpp`: two-process fixture driver
- `expected.regulator.log`, `expected.constrained.log`: canonical expected logs

## Required ordering

For every logical time `N`, the constrained federate receives the timestamped
`Tick` callback at `N` before `GRANT time=N`. The grant sequence is exact and
contains no duplicate or interim grants.

## Expected result

The accepted current result is FULL 15/15 for both regulator and constrained
roles when run against the implementation that provides TSO-before-grant
delivery and outgoing timestamp validation.
