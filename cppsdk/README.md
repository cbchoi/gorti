# Legacy gorti C++ SDK

This directory retains the earlier C++ implementation and fixtures. C++ is no
longer a supported gorti SDK. Current development uses Go and Python, with
[Java](../javasdk/README.md) as the IEEE 1516.1-2010 language mapping.
The build notes below describe the retained legacy source.

## Historical profile

The tested profile covers connection and federation lifecycle, declarations,
Object Management, Time Management, synchronization, ownership, save/restore,
DDM, MOM, callback evocation, typed handles and collections, standard exception
classes, and Annex B encoding helpers. Build and conformance tests define the
precise supported overloads.

These historical tests do not define the current Go/Python/Java release selection.

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

The current `buf generate` configuration no longer produces C++ bindings.
These historical build instructions require compatible bindings from the
earlier C++ generation setup; current release commands validate Go, Python,
and Java only.

## Layout

- `include/RTI/`: strict public header surface
- `include/rti1516e/`: compatibility headers
- `src/`: ambassador and transport implementation
- `tests/dlc/lockfile/`: compile-time API checks
- `tests/dlc/conformance/`: behavioral fixtures

The current language-profile contract is summarized in
`engineering/specifications/current/IDD.md` and the test acceptance rules are
in `engineering/specifications/current/STD.md`.
