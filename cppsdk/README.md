# gorti C++ SDK (rti1516e::)

IEEE 1516.1-2010 Layer-2 ambassador (Pitch-style) for the gorti RTI.
Federate code ported from Pitch / Portico / MAK should compile against
this SDK with minimal call-site change.

**Status:** M17 Cut-1 in progress. See `docs/PITCH_PARITY.md` for the
API divergence table.

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
│   ├── Exceptions.h            Annex C exception hierarchy
│   └── Types.h                 strong handles + value maps
├── src/
│   └── RtiAmbassador.cpp       pimpl impl
├── _generated/                 buf-generated proto stubs (gitignored)
└── tests/
    └── test_ambassador_unit.cpp
```

## Milestones

- **M17.1 — scaffold + gRPC plumbing.** Connection lifecycle (this file).
- M17.2 — federation lifecycle (createFederationExecution, join, resign).
- M17.3 — §10.2 handle services (getObjectClassHandle, etc.).
- M17.4 — §5 publish/subscribe declarations.
- M17.5 — §6 register/update/send.
- M17.6 — §10.4 tickCallback + FederateAmbassador callback dispatch.
- M17.7 — cross-language Pitch smoke (C++ pub ↔ Python sub).

## Out of scope for Cut-1

Cut-2/3+ deferrals:
- §8 Time Management (TAR/TARA/NER/NMRA/FQR)
- §7 Ownership Management
- §9 Data Distribution Management
- §4.8-15 Save/Restore
- §6.1-5 Object instance name reservation flow
- §11 MOM ambassador methods
- §10.4 strict HLA_EVOKED buffered-drain (M27 Phase E noted gorti's
  cheap-evoke divergence; that carries over to C++)
