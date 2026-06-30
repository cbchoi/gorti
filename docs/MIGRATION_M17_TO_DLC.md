# Migration: M17 cppsdk → IEEE 1516.1-2010 DLC strict surface

**Status:** SKELETON — published in M35 W4 per `docs/DLC_COMPLIANCE_PROGRAM.md §5.5`. M31 lands this skeleton so the document path is reserved.

Full content lands when the strict DLC surface deprecates the M17-era `rti1516e/*.h` shim. Until then, M17-era federate source continues to compile unchanged.

---

## Scope

When the DLC compliance track closes (M35), this document will guide federates written against M17's `cppsdk/include/rti1516e/*.h` headers through a ~4-line migration to the strict DLC surface at `cppsdk/include/RTI/*.h`. The M17 headers stay buildable through M34; M35 marks them `[[deprecated]]`; a future major (v2.0) removes them.

Before/after sketch (full version lands in M35):

**Before (M17-era):**
```cpp
#include "rti1516e/RtiAmbassador.h"
#include "rti1516e/FederateAmbassador.h"

rti1516e::RTIambassador amb;
amb.connect("grpc://127.0.0.1:8080");
amb.joinFederationExecution("alice", "demo");
auto h = amb.getObjectClassHandle("Vehicle");
amb.resignFederationExecution();
```

**After (DLC-strict):**
```cpp
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>

class MyFed : public rti1516e::NullFederateAmbassador { /* overrides */ };

auto factory = rti1516e::RTIambassadorFactory();
auto amb = factory.createRTIambassador();          // rti1516e::auto_ptr
MyFed fed;
amb->connect(fed, rti1516e::HLA_IMMEDIATE,
             L"crcAddress=127.0.0.1:8989");        // wstring, address vector
amb->joinFederationExecution(L"alice", L"demo");   // wstring
auto h = amb->getObjectClassHandle(L"Vehicle");    // wstring; returns ObjectClassHandle class
amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
```

---

## What M35 will add to this document

(Tracked in `docs/M35_DISPATCH_PLAN.md` W4. Skeleton placeholders below.)

### 1. Header migration mapping

`#include "rti1516e/RtiAmbassador.h"` → `#include <RTI/RTIambassadorFactory.h>` + `<RTI/NullFederateAmbassador.h>` + `<RTI/Enums.h>` (3 includes replace the 1 M17 include).

Full filename mapping table for the 5 M17 headers → 30 DLC headers will land in M35.

### 2. Construction site rewrites

The single biggest change. M17 federates construct `RTIambassador` directly; DLC requires the factory + `auto_ptr`. ~5 sed-style hints for the most-common patterns.

### 3. String type migration

`std::string` → `std::wstring` everywhere on the federate↔RTI boundary. Federate-internal strings can stay narrow; only ambassador-call arguments and return values switch. The M17 shim auto-translates; the strict surface is wstring-only.

### 4. Callback overload set

Federates that derive from M17's `rti1516e::FederateAmbassador` (concrete, all callbacks no-op default) must switch to `rti1516e::NullFederateAmbassador` (concrete subclass) under DLC. Federates that derived from the M17 abstract `FederateAmbassador` need to add the new overloads (3x reflect/receive/remove per FR-DLC-5).

### 5. Time argument migration

`std::optional<double>` → `LogicalTime const&` reference. Federate must pick `HLAfloat64Time` or `HLAinteger64Time` via factory at startup.

### 6. ResignAction migration

`amb.resignFederationExecution()` → `amb->resignFederationExecution(CANCEL_THEN_DELETE_THEN_DIVEST)` (mandatory arg per FR-DLC-10).

### 7. Exception migration

M17's `std::runtime_error`-derived hierarchy → spec `rti1516e::Exception`-derived hierarchy. `catch(std::exception&)` clauses continue to work via inheritance bridge; `catch(rti1516e::RTIinternalError const&)` previously caught everything and now catches only the actual `RTIinternalError` leaf.

### 8. CMake / build system migration

`find_package(rti1516e CONFIG)` continues to find both M17 and DLC headers via the same package. Header path switch is automatic via the back-compat shim re-export. Federates that want to opt **out** of the shim set `-DGORTI_NO_M17_SHIM` to get compile errors on M17-shape usage.

### 9. Worked example: `examples/cpp-pitch-smoke/`

The existing `publisher.cpp` gets a `publisher-strict.cpp` sibling compiled against DLC headers. Both compile through M34; at M35 the M17 variant is renamed `-old`.

### 10. Migration timing recommendation

- Existing federates that don't link gorti's headers (use Pitch/MAK/Portico): no action — the DLC compliance program means your code compiles unchanged against gorti.
- Existing federates that link gorti's M17 headers: migrate at your convenience through M34; at M35 your build emits `[[deprecated]]` warnings.
- New federates: start with DLC.

---

## References

- `docs/DLC_COMPLIANCE_PROGRAM.md §5.5` — recipe outline.
- `docs/DLC_DIVERGENCE_CATALOGUE.md` — the 153 row-by-row catalogue that drives the migration. Every BLOCKING row is a federate-source-touch point.
- `docs/srs.md §5.14` — FR-DLC-1..18.
- `docs/idd.md §1.8` — DLC C++ API public-interface map.
- `cppsdk/tests/dlc/lockfile/` — the assertions that lock the strict surface in place.
- `docs/agent-d-cppsdk.md` — owner brief for the migration.
