# _runtime — gorti-only DLC runtime suites (GREEN, WILL_FAIL=OFF)

Unlike the sibling conformance fixtures, `_runtime` holds no federate
binaries and no goldens: these are in-process gtest suites over the DLC
library itself (mock-based — no rtid subprocess, no gRPC). They are the
"3+1 C++ runtime suites" regression gate that every milestone must keep
GREEN alongside `go test ./rti/...` and the lockfile suite.

| Suite (ctest name) | File | What it pins |
|---|---|---|
| `conformance__runtime_exceptions` | `test_exceptions_throw_and_catch.cpp` | IEEE 1516.1-2010 Annex C exception hierarchy — every `<RTI/Exception.h>` type throws, catches by base `rti1516e::Exception` (// §10.4 error surfacing contract). M33-I. |
| `conformance__runtime_encoding` | `test_encoding_roundtrip.cpp` | HLA 1516.2 Annex B encoders — encode/decode round-trips plus byte-level pins against the cross-language golden vectors (// §14.3 of the compliance catalogue). M34-AF. |
| `conformance__runtime_mom_surface` | `test_mom_surface.cpp` | // §16 MOM name surface — `HLAobjectRoot.HLAmanager.*` class/attribute name resolution the MOM fixtures depend on. |
| `conformance__runtime_callback_bridge` | `test_callback_bridge.cpp` | `DLCFederateAmbassadorBridge` dispatch — M17-wire → DLC-typed callback conversion for the // §4, §5, §6, §7, §8 federate services (discover/reflect/remove, sync §4.12-§4.15, save/restore §4.25-§4.26, ownership §7.4-§7.11, time §8.13-§8.22, advisories §5.10/§6.17-§6.18). 53 tests as of M37-EC. |

Run: `ctest -R conformance__runtime` from `cppsdk/build`.
