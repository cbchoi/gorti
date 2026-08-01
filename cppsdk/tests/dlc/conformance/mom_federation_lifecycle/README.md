# MOM federation lifecycle

This fixture verifies standard MOM lifecycle observation through normal HLA
object subscription. It does not call a gorti-specific MOM helper.

## Scenario

1. An observer joins and subscribes to
   `HLAobjectRoot.HLAmanager.HLAfederate` attributes
   `HLAfederateHandle` and `HLAfederateName`.
2. The observer discovers and receives its own MOM federate record.
3. Federates `alice` and `bob` join in sequence; the observer discovers and
   receives each record.
4. `alice` then `bob` resign; the observer receives the corresponding removal
   callbacks.
5. The observer resigns and the federation is destroyed.

The driver keeps the observer callback pump active throughout. Instance names
include the RTI-assigned handle so every discovered MOM object name is unique.

## Traceability

- MOM class and attribute discovery: // §16
- Federate join and resign lifecycle: // §4.9 and // §4.10
- Object discovery, reflection, and removal callbacks: // §6.9,
  // §6.11, and // §6.15

## Files

- `federation.fom.xml`: MOM-capable federation model
- `test_mom_federation_lifecycle.cpp`: fixture driver
- `expected.observer.log`, `expected.alice.log`, `expected.bob.log`: canonical
  expected records

## Expected result

The accepted canonical result is SPEC-FULL: observer 12/12 records, alice 3/3,
and bob 3/3. Attribute values use standard HLA encodings and each resign emits
one `removeObjectInstance` callback for the corresponding MOM record.
