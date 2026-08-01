# Timestamp-order interaction ordering

This fixture verifies same-timestamp tie breaking and callback-before-grant
behavior for three regulating publishers and one constrained subscriber.

## Scenario

1. The subscriber joins, subscribes to `Tick`, enables time constrained mode,
   and waits until regulating time is defined.
2. Publishers `alice`, `bob`, and `carol` join and enable time regulation with
   lookahead 1.
3. Each publisher sends one timestamped `Tick` interaction at logical time 1.
4. The subscriber advances and records the three timestamped callbacks followed
   by the exact grant.

The launcher starts the subscriber before the publishers. The subscriber gates
its NER on a defined GALT so an early request cannot advance beyond the staged
timestamped interactions.

Traceability: timestamp-order interaction callbacks use // §6.13; GALT, NER,
and Time Advance Grant behavior use // §8.13.

## Files

- `federation.fom.xml`: shared Time Management FOM
- publisher and subscriber fixture sources
- expected logs for the subscriber and all three publishers

## Required ordering

All three callbacks carry timestamp 1 and arrive before the subscriber's grant.
Within the timestamp bucket, the deterministic order is ascending source
federate handle. A final grant completes the requested advance without an
extra interim grant.

## Expected result

The accepted canonical result is FULL: subscriber 10/10 records and each
publisher 5/5 records. Any callback after the related grant, missing final
grant, or source-order inversion rejects the fixture.
