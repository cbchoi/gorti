# mom_federation_lifecycle — MOM via standard pub/sub, no bespoke API

**Spec:** IEEE 1516.1-2010 §11 (Management Object Model). Specifically: `HLAobjectRoot.HLAmanager.HLAfederate` is the per-federate MOM object class; the RTI publishes a new instance on each `joinFederationExecution` and removes it on `resignFederationExecution`. Federates observe federation membership through ordinary §5.6 `subscribeObjectClassAttributes` + §6.9 / §6.11 / §6.15 callbacks — **no MOM-specific federate API exists in the spec**.

**Owns catalogue rows:** **16.1** (MOM via standard pub/sub — REMOVE bespoke `queryFederationAttributes` / `queryFederateAttributes` / `enumerateMomInstances` from M17's RtiAmbassador.h:552-586). Also locks 11.9 (`subscribeObjectClassAttributes` with mandatory `active` flag) and 4.19/4.20/4.22 (discover/reflect/remove callback shapes).

## Why this fixture exists

M17's `RtiAmbassador.h` ships a bespoke MOM API (`queryFederationAttributes` etc., RtiAmbassador.h:552-586) returning custom structs. That API does not exist in the 1516.1 spec — federates that want MOM data must subscribe to `HLAobjectRoot.HLAmanager.*` like any other object class.

This fixture exercises **only** the standard path:

1. Observer subscribes `HLAobjectRoot.HLAmanager.HLAfederate` for `HLAfederateHandle`, `HLAfederateName`.
2. RTI fires `discoverObjectInstance` + `reflectAttributeValues` for the observer's own MOM record, then for each subsequent joiner.
3. When alice resigns, RTI fires `removeObjectInstance` for alice's MOM record.

The fixture **does not** call any non-standard MOM helper. After M32+ lands MOM, the lockfile under `cppsdk/tests/dlc/lockfile/core/test_rtiambassador_mom.cpp` (Agent A) will verify the bespoke API is *absent* from the strict surface.

## Scenario

| Step | Driver action | Expected observer event |
|---|---|---|
| 1 | Observer joins, subscribes HLAfederate | (subscribe) |
| 2 | (RTI publishes observer's own HLAfederate object) | DISCOVER + REFLECT name=observer |
| 3 | Alice joins | DISCOVER + REFLECT name=alice |
| 4 | Bob joins | DISCOVER + REFLECT name=bob |
| 5 | Alice resigns | REMOVE |
| 6 | Bob resigns, observer resigns | (driver-side teardown) |

## Files

- `federate_observer.cpp` — MOM-subscriber observer
- `federate_member.cpp` — passive joiner (used twice: alice + bob)
- `federation.fom.xml`
- `expected.observer.log`
- `expected.alice.log`
- `expected.bob.log`
- `test_mom_federation_lifecycle.cpp`
