# pyjevsim dashboard example — Sensor → Dashboard (object instances)

Two-federate object-class example running on the in-process pysdk
transport:

```text
Sensor ──register_object_instance(SensorReading)──▶
       ──update_attributes(value=...) per tick──▶ Dashboard
                                                   (reflects received)
```

## What you'll learn

- The **OBJECT INSTANCE half** of HLA: ``register_object_instance`` +
  ``update_attributes`` on the publish side, ``subscribe_object_class``
  + ``ReflectAttributeValues`` callbacks on the subscribe side.
- How a runner stages ``DiscoverObjectInstance`` and
  ``ReflectAttributeValues`` events explicitly when running on the
  in-process transport (which is a recorder, not a router).
- The pedagogical contrast with ``examples/pyjevsim/`` and
  ``examples/pyjevsim-relay/``, which exercise interactions only.

## Federates

### Sensor (object publisher)

Registers a ``SensorReading`` instance once, then calls
``update_attributes(value=...)`` every tick. Defaults publish a
monotonic sequence ``0, 1, 2, ...``; ``--mode sine`` publishes a
quantised 8-step sine wave with configurable amplitude.

### Dashboard (object subscriber)

Subscribes to ``SensorReading.value`` and accumulates the reflected
sequence. Records both ``DiscoverObjectInstance`` (exactly once,
when the sensor's instance becomes visible) and
``ReflectAttributeValues`` (once per sensor update).

## Why this example bypasses the bridge

This is the most important pedagogical point of the example.

The ``pyjevsim_bridge`` is **interaction-only** at this cut:

- ``port_mapping.py`` infers direction from ``out_*`` / ``in_*``
  port-name prefixes and binds each to a FOM **interaction class**.
- ``time_advance.py::_run_internal_cycle`` calls ``send_interaction``
  on every output the coupled model emits, wrapping the payload as
  ``parameters={"_payload": payload}``.
- There is no equivalent path for object-class
  ``register_object_instance`` / ``update_attributes`` /
  ``ReflectAttributeValues``.

A ``pyjevsim`` coupled model whose semantics are object-attribute
updates therefore can't be wired through the bridge as-is. Two paths
were considered:

- **Path A (taken)** — Use the lower-level ``RtiConnection`` +
  ``Federate`` API directly. The federate models in this example
  (``Sensor``, ``Dashboard``) are plain Python objects whose state
  the runner manipulates; the runner owns the ``register`` and
  ``update_attributes`` calls and the synthesis of reflect events.
  This is the same surface the M12 spec tests use (see
  ``pysdk/tests/spec/m12/test_spec_m12_sdk_exposure.py``); it's
  pedagogically closer to "real HLA" than to "DEVS-via-bridge".

- **Path B (deferred)** — Extend the bridge to recognise an
  object-class output convention (e.g. ``coupled_model.attributes()``
  returning ``{class: {attr: value}}``). Out of scope here; the
  bridge contract change would touch every existing user of the
  bridge and the cut-1 design intentionally narrowed to interactions
  to keep the first-pass surface small.

A future cut that grows the bridge into Path B can keep this
example as the "what it looked like before" reference; the verify()
checks here are the same checks a Path-B variant would ship.

## Run it

```bash
# From the repo root
python3 examples/pyjevsim-dashboard/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim-dashboard/runner.py \
    --ticks 10 \
    --mode sequence \
    --amplitude 100 \
    --verbose
```

Default-config output:

```text
runner: published=10  received=10  discovered=1  updates=10  verify=ok
```

## Verification invariants

1. ``sensor.published == dashboard.received`` (ordered equality —
   no reflects are lost or reordered).
2. Exactly one ``DiscoverObjectInstance`` event reached the
   dashboard (one instance was registered).
3. ``update_attributes`` wire-call count equals the number of
   ``sensor.published`` values.

## Tuning knobs

| Knob | Effect |
|---|---|
| ``--ticks N`` | Number of sensor updates (default 10) |
| ``--mode sequence`` | Publish ``i`` on tick ``i`` (default; trivial verify) |
| ``--mode sine`` | Publish a quantised 8-step sine wave |
| ``--amplitude A`` | For sine mode: integer amplitude (default 100) |

## Wire shape

One object class, one attribute. See ``dashboard-fom.xml``:

```xml
<objectClass>
  <name>SensorReading</name>
  <attribute>
    <name>value</name>
    <dataType>HLAinteger32BE</dataType>
  </attribute>
</objectClass>
```

## What's deferred

- **Bridge extension for object classes** (Path B above). Would
  require a new ``CoupledModelProtocol`` output convention plus
  ``port_mapping.py`` rules for object-class binding; not done at
  this cut.
- **Cross-process variant.** This runs on the in-process transport;
  a real ``rtid`` subprocess + two ``grpc://`` federates would route
  the discover/reflect events automatically (the rtid Registry has
  the OnRegister/OnUpdate hooks; see
  ``rti/internal/object/registry.go``). Use the M12 helper at
  ``pysdk/tests/spec/m12/_helpers.py::RtidProcess`` as the cross-
  process scaffold.
- **Time-stamped reflects vs receive-order.** The runner stamps
  ``timestamp=float(tick)`` on every update so a future variant can
  exercise TSO ordering; this example doesn't enable time
  regulation on either federate (no ``enable_time_regulation`` /
  ``next_message_request`` calls), so the dashboard processes the
  reflects in arrival order. See ``examples/pyjevsim-time-advance/``
  for the time-management example.
- **Multi-instance.** Sensor registers one instance; production
  topologies often have many. Verify() asserts ``discovered == 1``
  to keep this example crisp.
