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

## parity-CF verdict (M35 wave 2)

**BLOCKED(MOM object-instance fan-out to standard pub/sub unimplemented
server-side)** — line-level PARTIAL 10/17 (observer **4/11**, alice
**3/3**, bob **3/3**).

What works (traced end-to-end against a fresh rtid):
- The standard MIM is auto-merged into every federation FOM at load
  (`rti/cmd/rtid/foms.go:61-65` → `rti/pkg/fom/mim/`), so
  `getObjectClassHandle("HLAobjectRoot.HLAmanager.HLAfederate")` and both
  attribute handle lookups **resolve**, and
  `subscribeObjectClassAttributes(..., active=true)` **succeeds** — the
  §11 "MOM via ordinary pub/sub" front door is open.
- Member goldens match exactly (join/resign wire path clean).

Exact missing events (all 7 §11 observer lines):
`DISCOVER name=HLAfederate` ×3, `REFLECT HLAfederateName=observer`,
`REFLECT HLAfederateName=alice`, `REFLECT HLAfederateName=bob`,
`REMOVE` (alice's resign). The observer receives nothing between
SUBSCRIBE and RESIGN.

Root cause (server-side, named): `rti/internal/mom` is a
snapshot-recorder + interaction responder, not an object publisher.
- `Manager.FederateJoined` (manager.go:151) / `FederateResigned`
  (manager.go:189) only mutate the in-memory `federateSnapshot` map —
  they emit no discover/reflect/remove through the Outbox.
- The MOM's Outbox use is limited to HLArequest→HLAreport
  *interactions* (emitter.go / handlers_request.go) and bespoke Query
  accessors; manager.go:66-71 documents this as the M11 cut-1
  simplification: "real federate-side subscription via the standard
  pub/sub APIs requires the object.Registry to be aware of MOM classes
  as subscribable ... the subscriber fan-out is a follow-up".
- `rti/internal/object/registry.go` is MOM-aware only for interaction
  dispatch (registry.go:109), never registers HLAfederate object
  instances.

Catalogue row 16.1's *negative* half (no bespoke federate API needed)
holds; the *positive* half (MOM data arrives via 4.19/4.20/4.22
callbacks) is blocked until the M11-deferred fan-out lands: register an
HLAfederate instance per join in the object registry, reflect
HLAfederateHandle/HLAfederateName to subscribers, delete on resign.

Fixture code untouched (observer already used the suite-standard
evoke-drain `evokeMultipleCallbacks(0.05, 0.1)`). Run sequencing:
observer joins/subscribes first; alice joins; bob joins while alice is
still a member; alice resigns before bob (golden step order preserved).

## M36 DD re-verdict (2026-07-02)

**SPEC-FULL — 18/18 lines** (observer **12/12**, alice **3/3**, bob
**3/3**), up from BLOCKED / PARTIAL 10/17 (observer 4/11). Run: fresh
worktree rtid (M36 DD-2 + DA-1..5 C++ layer merged), manual driver
sequencing identical to `test_mom_federation_lifecycle.cpp` (observer
joins+subscribes first; alice joins, dwells 300 ms, resigns; then bob;
observer pumps throughout). Canonicalized with `_harness/normalize.py`;
inline `#` citations stripped from the golden before diff.

What landed (M36 DD-2, `rti/internal/mom/instances.go`):
- HLAmanager.HLAfederation registered per federation and
  HLAmanager.HLAfederate per joined federate through the STANDARD
  `object.Registry` path — Discover/Reflect/Remove ride the normal
  subscriber fan-out (the M11 cut-1 deferred follow-up in
  `mom/manager.go`).
- Late subscribers to the MOM classes get retroactive Discover+Reflect
  (declaration.Manager post-subscribe hook → MOM), which is what makes
  the observer see its OWN HLAfederate record after subscribing.
- `HLAfederateName` reflected as HLAunicodeString (uint32BE count +
  UTF-16BE units — matches the DLC decoder);
  `HLAfederatesInFederation` maintained on the federation instance as
  HLAvariableArray of HLAhandle.
- REMOVE delivery additionally required the C++ side's
  `removeObjectInstance` wire→callback chain (Agent DA-2, merged).

Golden amendments (documented in expected.observer.log header):
instance names `HLAfederate.<H>` (IEEE §6.2 uniqueness makes the
skeleton's three identical `name=HLAfederate` lines unrealizable;
normalize.py/log_diff.h now canonicalize the RTI-assigned handle
suffix), alice's REMOVE moved before bob's DISCOVER (matches the
driver's sequencing), and bob's REMOVE added (he also resigns while
the observer still pumps).

Fixture-code fix: `federate_observer.cpp` bound
`HLAunicodeString::get()` (returns std::wstring BY VALUE per the DLC
API) to a local before taking begin()/end() — the skeleton called
`.get()` twice, mixing iterators of two temporaries (UB; crashed with
std::length_error the moment reflects actually arrived).

Residual: none at fixture scope. (MOM attribute coverage beyond
HLAfederateHandle/HLAfederateName/HLAfederateType +
HLAfederationName/HLAfederatesInFederation — e.g. time-state, counters
— is not exercised by this fixture and remains snapshot-only.)
