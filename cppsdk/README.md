# gorti C++ SDK

The C++ SDK is an IEEE 1516-2010 DLC-shaped federate API for gorti. It uses
C++17 and maps ambassador calls and callbacks to the gorti wire services.

## Supported profile

The tested profile covers connection and federation lifecycle, declarations,
Object Management, Time Management, synchronization, ownership, save/restore,
DDM, MOM, callback evocation, typed handles and collections, standard exception
classes, and Annex B encoding helpers. Build and conformance tests define the
precise supported overloads.

Asynchronous ambassador variants, a Java SDK, HLA 1.3, and IEEE 1516-2000 are
outside this SDK profile.

## Requirements

- C++17 compiler
- CMake 3.18 or later
- gRPC++ and protobuf
- GoogleTest for the test targets
- Conan, vcpkg, or compatible system packages

## Build with Conan

```bash
cd cppsdk
conan install . --build=missing --output-folder=build
cmake -B build -DCMAKE_TOOLCHAIN_FILE=build/conan_toolchain.cmake \
               -DCMAKE_PREFIX_PATH=$(pwd)/build
cmake --build build -j
ctest --test-dir build --output-on-failure
```

## Build with system packages

```bash
cd cppsdk
cmake -B build
cmake --build build -j
ctest --test-dir build --output-on-failure
```

Run `buf generate` from the repository root to regenerate protobuf and gRPC
bindings under `cppsdk/_generated/rti/v1/`.

## Layout

- `include/RTI/`: strict public header surface
- `include/rti1516e/`: compatibility headers
- `src/`: ambassador and transport implementation
- `tests/dlc/lockfile/`: compile-time API checks
- `tests/dlc/conformance/`: behavioral fixtures

The current language-profile contract is summarized in
`engineering/specifications/current/IDD.md` and the test acceptance rules are
in `engineering/specifications/current/STD.md`.
