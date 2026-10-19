# Join FOM Preparation And FDD Metadata

Date: 2026-09-20. Bounded engineering evidence, not IEEE conformance.
Full normative texts remain unavailable. No new normative clause claims are made.

## Delivered Boundary

`composition.Repository.PrepareExtension` retains and re-parses the exact
creation modules plus additional bytes without changing the repository,
membership, counters, or service caches. The result provides a merged immutable
model preview, source digests, affected existing class/member/dimension handles,
and a dimension-cache refresh requirement. It has no commit operation and is
not a successful join or permission to publish the preview. Missing retained
source, cancelled requests, malformed modules and incompatible modeled
definitions fail without changing the live repository.

Python's standard-shaped `additional_fom_modules` argument now rejects nonempty
input before entering the lifecycle context or calling the transport. Its
`NotImplementedError` explains the missing live-composition protocol and the
new-federation workaround. Empty and omitted arguments preserve prior behavior.
This is an input-honesty correction, **not additional-module runtime support**.
The C++ owner independently handles that binding's typed fail-closed guard.
No federation protobuf field, RPC, generated file, or descriptor golden changed
in this scope. Go's existing creation-module API is unchanged.

## Architectural Blocker

Two independent negative probes exercise production composition and the real
DDM manager, not mocked merge algorithms:

- Reloading a valid additive object-class module changes an already assigned
  `Vehicle` handle from 5 to 6 because handles are sorted positional indices.
- Replacing the repository after DDM initialization leaves the existing manager
  unable to resolve newly introduced dimension `A`.

The committed preparation tests also expose inherited attribute and parameter
renumbering when an empty ancestor or child declaration gains members. A sorted
reload cannot be used as an additive live update.

A complete implementation requires an explicit, coordinated design:

1. Maintain append-only identity tables for classes, per-class inherited members
   and dimensions; preserve every already-issued handle, not just cached names.
2. Prepare all merged validation and consumer updates under generation-aware
   federation lifecycle/operation admission before consuming a join handle or
   changing the roster. Revalidate the original execution and model identity.
3. Update DDM dimension tables and retained FOM references, including management
   interaction caches, without exposing a partially updated federation.
4. Supply authoritative mappings to new and already-joined clients. Go/Python
   creation-time parsed tables and C++ remote-lookup caches are different paths;
   adding module bytes to the join request alone does not update them.
5. Define durable module/revision records and checkpoint identity compatibility.
   The existing join journal has no module payload or FOM revision. Commit FOM,
   roster, journal and consumer-visible state at one recoverable boundary, with
   validation/delivery/journal failure preserving prior state.

Parser acceptance and a zero-length handle-change list establish neither full
modular-FOM compatibility nor safe live admission. Unmodeled OMT metadata,
cross-module datatype/reference rules, consumer caches and persistence remain
separate obligations. Runtime join extension is still incomplete.

## Named Rates And Advisory Metadata

The FOM model/parser now retain named update rates, their descriptions and
decimal identity, and four advisory-switch declarations. MIM composition retains
the new metadata. The production FOM handle implements these optional ports:

- `core.FOMUpdateRates.UpdateRateValue(string) (float64, bool)`.
- `core.FOMAdvisoryDefaults.AdvisorySwitchDefault(kind) (bool, bool)`.
- `core.FOMClassAncestry`, nearest ancestor first, including the queried class.

Missing names/switches remain absent, not invented defaults. For a present
switch element, explicit `true`, `false`, `1`, and `0` are parsed; absent
`isEnabled` takes the supplied XSD's false attribute default. An absent entire
switch declaration still returns presence=false. Other switch categories are
not implemented by these four advisory ports.

Named-rate compatibility compares exact decimals before float64 rounding can
hide conflicts. Duplicate equivalent values are collapsed; conflicting modeled
rates/descriptions or switch values are rejected. Rates are converted to
float64 for the existing consumer API; overflow and nonzero underflow are
explicit representation limitations. No positive-value restriction is invented.
This remains a bounded decoder, not complete XML Schema/OMT validation.

## Source Proof

The supplied official `IEEE1516-FDD-2010.xsd` was read locally at
`C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee1516-audit-20260918/interface/1516.1-2010_downloads/IEEE1516-FDD-2010.xsd`.
SHA256: `8846b46dc0ee8ee1299c0e854e65630cf4de18375f6ce1200abd904e37f5a3c1`.

`updateRatesType`, `updateRateType`, and `RateType` specify name, decimal rate,
optional semantics; `switchType` declares `isEnabled` as XML boolean with a
false attribute default. These schema facts do not establish universal RTI
service defaults or complete modular merge rules.

The committed `parser/testdata/fdd2010-rates-switches.xml` fixture was validated
against that exact schema using .NET's validating XML reader. It includes
zero/negative decimal values and present switches with/without `isEnabled`.
The parser regression also confirms the modeled values are retained.

## Verification

Genuine pre-fix failures were recorded for discarded named rates, silently
accepted rate conflicts and invalid decimals, dropped advisory metadata,
invalid XML booleans, and Python nonempty additional-module arguments. The two
unsafe live-reload probes intentionally still fail: they document unresolved
architecture, not implemented runtime functionality.

Windows focused command passed with the requested external Go caches:

```text
go test ./rti/internal/application/composition ./rti/pkg/fom/model ./rti/pkg/fom/parser ./rti/pkg/fom/mim ./rti/pkg/federate ./rti/tests/unit/ddm ./tests/spec/M1 -count=1
```

Python `test_join_additional_fom.py`, `test_standard_ambassador.py`, and
`spec/m4/test_spec_m4_standard_ambassador.py`: 8 passed. Ruff and scoped
`git diff --check` passed. No real-server runtime-extension campaign is claimed.

Raw evidence and negative-probe overlays are outside the repository under
`D:/workspace/gorti/`: `join-fom-architecture-red.log`,
`join-fom-update-rates-red.log`, `join-fom-advisory-model-red.log`,
`join-fom-python-red.log`, `join-fom-python-green.log`,
`join-fom-final-go.log`, and `join-fom-schema-proof.log`.

## Independent Read-Only Review

The requested review of the concurrently implemented local-delete/support
paths reproduced two issues without modifying their owner files:

- `discover.go` exposes Discover through the outbox before recording caller
  knowledge. A lookup during delivery therefore observes an unknown object.
- Failed immediate rediscovery after LocalDelete leaves the object unknown,
  but the next attribute update still delivers Reflect to that subscriber.

Both failing probes and their overlay remain outside the repository in
`local-known-review-probe_test.go`, `local-known-review-overlay.json`, and
`local-known-review-red.log`. Findings were sent to the main owner; later fixes
must be assessed by fresh execution. The explicitly retained caller-free
administrative support lookup is a compatibility boundary, not evidence of
per-federate knowledge semantics. Ownership acquisition/query use the new
knowledge helper; divest/cancel routes still rely on owner/pending-state checks,
so their complete known-object/error-precedence coverage remains unestablished.
