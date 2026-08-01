# Python publisher and C++ subscriber

This fixture verifies cross-language FOM handles, HLA value bytes, discovery,
reflection, interaction delivery, and removal between a Python publisher and a
C++ subscriber connected to the same `rtid`.

## Scenario

1. The C++ subscriber joins, publishes its readiness point, and subscribes to
   the object attributes and interaction class.
2. The Python publisher joins, waits for readiness, registers an object, sends
   an attribute update and interaction, deletes the object, and resigns.
3. The subscriber records discovery, byte-identical attribute values,
   interaction parameters, and removal, then resigns.

## Traceability

- Join and resign: // §4.9 and // §4.10
- Publish and subscribe: // §5.2
- Register, discover, reflect, interact, and remove: // §6.8, // §6.9,
  // §6.11, // §6.13, and // §6.15

## Prerequisites

Generate Python and C++ `rti.v1` bindings from the same schema before building:

```bash
buf generate
```

Use Python 3.11 or later and a C++17 build of the SDK. When running from source,
set `PYTHONPATH=<repo>/pysdk` and build the C++ fixture through its CMake target.

## Files

- `federation.fom.xml`: shared object model
- `python_pub.py`: Python publisher
- `federate_subscriber.cpp`: C++ subscriber
- `expected.python_pub.log`, `expected.cpp_sub.log`: canonical expected records

## Expected result

The accepted canonical result is SPEC-FULL: Python publisher 8/8 records and
C++ subscriber 9/9 records, including `SUB: REMOVE`. The fixture compares value
bytes and observable callback order; it does not claim support for Python API
fields that are not currently exposed by the selected adapter.
