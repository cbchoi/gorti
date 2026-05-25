# gorti C++ SDK (rti1516e::)

IEEE 1516.1-2010 Layer-2 ambassador (Pitch-style) for the gorti RTI.
Federate code ported from Pitch / Portico / MAK should compile against
this SDK with minimal call-site change.

**Status:** M17 Cut-4 closed (2026-05-25). See `docs/PITCH_PARITY.md`
for the API divergence table.

## Build

The SDK is CMake-based and depends on:
- C++17 compiler (g++ 12+, clang++ 15+)
- CMake 3.18+
- gRPC++ + protobuf (system or via Conan/vcpkg)
- GoogleTest (for the test target)

Two supported build paths:

### Via Conan

```bash
cd cppsdk
conan install . --build=missing --output-folder=build
cmake -B build -DCMAKE_TOOLCHAIN_FILE=build/conan_toolchain.cmake \
               -DCMAKE_PREFIX_PATH=$(pwd)/build
cmake --build build -j
ctest --test-dir build --output-on-failure
```

### Via system packages

```bash
# Debian/Ubuntu
sudo apt-get install -y cmake libgrpc++-dev libprotobuf-dev \
                        protobuf-compiler-grpc libgtest-dev pkg-config
# macOS
brew install cmake grpc protobuf googletest

cd cppsdk
cmake -B build
cmake --build build -j
ctest --test-dir build --output-on-failure
```

## Generated proto stubs

The proto stubs live under `cppsdk/_generated/rti/v1/`. They are
gitignored — regenerate via `buf generate` from the repository root.
The C++ plugins are configured in `buf.gen.yaml`.

## Layout

```
cppsdk/
├── CMakeLists.txt              top-level build
├── conanfile.txt               Conan deps (Cut-1: gRPC + protobuf + gtest)
├── include/rti1516e/
│   ├── RtiAmbassador.h         Layer-2 ambassador
│   ├── FederateAmbassador.h    callback override slots
│   ├── Encoding.h              Annex B basic encoding (Cut-2)
│   ├── Exceptions.h            Annex C exception hierarchy
│   └── Types.h                 strong handles + value maps
├── src/
│   └── RtiAmbassador.cpp       pimpl impl
├── _generated/                 buf-generated proto stubs (gitignored)
└── tests/
    └── test_ambassador_unit.cpp
```

## Milestones

Cut-1 (closed 2026-05-23):
- M17.1 — scaffold + gRPC plumbing. Connection lifecycle.
- M17.2 — federation lifecycle (createFederationExecution, join, resign).
- M17.3 — §10.2 handle services (getObjectClassHandle, etc.).
- M17.4 — §5 publish/subscribe declarations.
- M17.5 — §6 register/update/send.
- M17.6 — §10.4 tickCallback + FederateAmbassador callback dispatch.
- M17.7 — cross-language Pitch smoke (C++ pub ↔ Python sub).

Cut-2 (closed 2026-05-21):
- M17.8 — HLA Annex B basic encoding (`<rti1516e/Encoding.h>`).
- M17.9 — §6.30/§6.31 runtime instance handle services.
- M17.10 — §6.1-5 object instance name reservation flow.
- M17.11 — §8 Time Management (TAR/TARA/NER/NMRA/FQR + queries).

Cut-3 (closed 2026-05-24):
- M17.13 — §11 MOM ambassador delegates (queryFederation /
  Federate Attributes, enumerateMomInstances).
- M17.14 — §4.7 Federation synchronization points.
- M17.15 — §7 Ownership Management (8 RPCs + 3 callbacks).
- M17.16 — §4.8-15 Save / Restore.
- M17.17 — §9 DDM region surface (16 RPCs).
- M17.18 — strict HLA_EVOKED aliases + enableCallbacks /
  disableCallbacks toggle.
- M17.19 — Advanced encodings (enum, fixed/variable array,
  fixed record).

Cut-4 (closed 2026-05-25):
- M17.21 — refactor dispatchOneEvent helper.
- M17.22 — strict at-most-one `evokeCallback`.
- M17.23 — variable-width `HLAvariableArray<T>`.
- M17.24 — `HLAfixedRecord` auto-alignment.
- M17.25 — save/restore event-stream callbacks (server + SDK).
- M17.26 — MOM `TimeStateChanged` hook wired in rtid.
- M17.27 — two-federate ownership transfer (Subscribers
  resolver wired + Acquire-of-unowned semantics + xfed tests).

## Out of scope

- Async / non-blocking ambassador variants — separate
  ambassador shape, not in M17 scope
- Java SDK (M18 — separate milestone)
