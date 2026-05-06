# pyjevsim dashboard example — Path B (bridged)

Two-federate object-class example running on the in-process pysdk
transport, **wired through ``pyjevsim_bridge.HLAFederate`` instead
of raw ``Federate``**.

This is the Path-B variant of ``examples/pyjevsim-dashboard/``. The
sibling Path-A variant (``examples/pyjevsim-dashboard/``) drives
``Federate.register_object_instance`` / ``Federate.update_attributes``
directly and is documented as the bypass reference; this variant
shows how the same semantics are expressed once the bridge gains
object-class support via ``ObjectClassFederateProtocol``.

```text
Sensor coupled-model ─[bridge]─▶ register_object_instance(SensorReading)
                       ─[bridge]─▶ update_attributes(value=...) per tick
                                                                       │
Dashboard coupled-model ◀─[bridge]─◀ ReflectAttributeValues / Discover ┘
```

## What you'll learn

- How a coupled model **opts INTO** HLA object-class semantics via
  ``ObjectClassFederateProtocol``: declare publications /
  subscriptions / instance registrations as plain methods, return
  ``{instance: {attr: payload}}`` from ``attribute_update_handler``
  every cycle, and receive ``DiscoverObjectInstance`` /
  ``ReflectAttributeValues`` events as ``discover_handler`` /
  ``reflect_handler`` callbacks.
- How the bridge wires those methods to the SDK calls
  (``publish_object_class`` / ``subscribe_object_class`` /
  ``register_object_instance`` / ``update_attributes``) without
  changing the interaction-cycle contract — interactions and object
  classes coexist on the same ``HLAFederate``.
- How the **runner still synthesizes Discover + Reflect** events
  because ``InProcessTransport`` is a recorder, not a router. (The
  bridge accepts these via ``deliver_object_event`` exactly the way
  it accepts ``deliver_external`` for interactions.)

## Federates

### Sensor (object publisher) — ``sensor.py``

A bridged coupled model whose ``object_class_publications`` returns
``{"SensorReading": ["value"]}``, whose ``register_instances``
returns ``{"sensor-1": "SensorReading"}``, and whose
``attribute_update_handler`` returns ``{"sensor-1": {"value":
<bytes>}}`` every cycle (or ``{}`` once ``stop_after`` is reached).

### Dashboard (object subscriber) — ``dashboard.py``

A bridged coupled model whose ``object_class_subscriptions`` returns
``{"SensorReading": ["value"]}``, plus
``discover_handler(handle, class_name, instance_name)`` and
``reflect_handler(handle, attrs)`` to receive the events the bridge
routes here.

## Path A vs Path B — which should you pick?

| Concern | Path A (bypass) | Path B (bridged) |
|---|---|---|
| Federate code does pub/sub | Yes — explicit | No — bridge does it from declarations |
| Federate code calls ``register_object_instance`` | Yes | No |
| Federate code calls ``update_attributes`` | Yes | No — return from ``attribute_update_handler`` |
| Federate code receives ``Discover`` / ``Reflect`` | Yes — drains ``fed.events()`` | No — handler callbacks |
| Time-management cycle | Manual / runner | Bridge (DEVS ``ta``-driven NER) |
| Pedagogical closeness to "raw HLA" | Higher | Lower |
| Pedagogical closeness to "DEVS-via-bridge" | Lower | Higher |

**Pick Path A** when you want to learn the raw HLA SDK calls or
when you need fine-grained control over event ordering /
synchronization (e.g. to mix it with TimeService primitives that
the bridge doesn't yet expose).

**Pick Path B** when you have a coupled-model-shaped federate and
want the bridge to handle declarations + event routing for you.
This is the mode you'd use for a pyjevsim ``StructuralModel`` /
``BehaviorModel`` whose ``output()`` already produces a port-payload
dict that fits the protocol.

## Run it

```bash
# From the repo root
python3 examples/pyjevsim-dashboard-bridged/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim-dashboard-bridged/runner.py \
    --ticks 10 \
    --mode sequence \
    --amplitude 100 \
    --verbose
```

Default-config output:

```text
runner: published=10  received=10  discovered=1  updates=10  verify=ok
```

Sine mode:

```text
$ python3 examples/pyjevsim-dashboard-bridged/runner.py --mode sine --ticks 16
runner: published=16  received=16  discovered=1  updates=16  verify=ok
```

Should be byte-equivalent to the bypass variant under the same
flags — same ``published``/``received`` sequences, same
``update_attribute_calls`` counts.

## Verification invariants

1. ``sensor.published == dashboard.received`` (ordered equality).
2. Exactly one ``DiscoverObjectInstance`` event reached the
   dashboard (one instance was registered).
3. ``update_attributes`` wire-call count equals
   ``len(sensor.published)``.
4. **New invariants (bridge-specific)**:
   - The sensor's bridge issued exactly **one**
     ``publish_object_class`` (``SensorReading`` declared via
     ``object_class_publications``).
   - The dashboard's bridge issued exactly **one**
     ``subscribe_object_class``.
   - The sensor's bridge issued exactly **one**
     ``register_object_instance`` (``sensor-1`` declared via
     ``register_instances``).

## What's still deferred

- **Dynamic instance registration mid-run.** Models that need to
  register instances after startup can call
  ``HLAFederate.register_instance(class_name, local_name)`` from
  inside their ``external_transition`` (or via runner
  orchestration), but this example sticks to the
  startup-registration shape for clarity.
- **Cross-process variant.** This runs on the in-process
  transport so the runner has to synthesize Discover + Reflect.
  A real ``rtid`` subprocess + two ``grpc://`` federates would
  route those automatically (the rtid Registry has
  OnRegister/OnUpdate hooks; see
  ``rti/internal/object/registry.go``). The bridge's
  ``HLAFederate`` works unchanged against that transport — just
  flip the URL.
- **Multi-instance / multi-class.** Sensor registers one
  instance of one class. The protocol supports many
  (``register_instances`` returns a dict; the bridge stores all
  the handles and dispatches per local-name); a future variant
  can exercise that.

## Wire shape

Same as the bypass variant — one object class, one attribute. See
``dashboard-fom.xml``.
