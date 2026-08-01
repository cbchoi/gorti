# C++ IEEE 1516e smoke

This C++17 publisher uses the gorti IEEE 1516-2010 ambassador-shaped SDK. It
connects to `rtid`, creates and joins a federation, resolves FOM handles,
publishes `Vehicle` attributes and `Honk`, registers `cpp-car-1`, updates its
attributes, sends the interaction, resigns, and exits.

## Prerequisites

- Go 1.22 or later for `rtid`.
- CMake 3.18 or later and a C++17 compiler.
- gRPC++ and protobuf, available as system packages or through Conan/vcpkg.
- `buf` when `cppsdk/_generated` has not been generated yet.

One Conan setup is:

```bash
conan install cppsdk --build=missing --output-folder=cppsdk/build
```

The entry scripts use `cppsdk/build/conan_toolchain.cmake` automatically when
it exists. `CMAKE_TOOLCHAIN_FILE` and `CMAKE_PREFIX_PATH` can select another
dependency setup.

## Run and verify

From any working directory:

```bash
bash examples/cpp-ieee1516e-smoke/run.sh
```

```powershell
.\examples\cpp-ieee1516e-smoke\run.ps1
```

The launcher generates missing bindings when possible, configures and builds
the publisher, builds a temporary `rtid`, waits for its listener, runs the
finite publisher flow, checks its completion marker, and always stops the
daemon. `PUBLISHER_BINARY` and `RTID_BINARY` can select existing binaries;
`CPP_BUILD_DIR`, `RTID_PORT`, and `HOLD_SECONDS` are also overridable. The
PowerShell script exposes equivalent named parameters.

For stronger cross-language verification after the publisher is built, run:

```bash
python -m pytest -q -s \
  pysdk/tests/spec/m17/test_cpp_python_interop.py::test_spec_m17_7_cpp_publisher_python_subscriber
```

That test starts a Python subscriber first and verifies discovery, reflection,
and interaction delivery from this C++ publisher.
