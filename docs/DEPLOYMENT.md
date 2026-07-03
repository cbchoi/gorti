# gorti deployment plan

Target environments: **Linux** and **Windows** (x86-64 and arm64).
License: MIT. First deployment target: **v0.9.0**.

This document is the operational plan for shipping gorti to those two
platforms. It covers what ships, how a release is cut, how to install on each
OS, how to verify a download, and the one deliberately-deferred piece (the C++
federate SDK on Windows).

---

## 1. Artifact matrix

| Artifact | What it is | Linux | Windows | Channel |
|---|---|:---:|:---:|---|
| `rtid` | RTI server daemon (the thing federates connect to) | ✅ `rtid` | ✅ `rtid.exe` | GitHub Releases |
| `rti-top` | Read-only live-federation TUI | ✅ `rti-top` | ✅ `rti-top.exe` | GitHub Releases |
| `rti1516e` | Python federate SDK (+ pyjevsim bridge) | ✅ wheel | ✅ same wheel | PyPI |
| `cppsdk` | IEEE 1516.1-2010 DLC C++ federate SDK | ✅ source build (conan+CMake) | ⏳ **future plan** — see §7 | (source) |

**Who runs what on Windows (v0.9.0 scope):** operators run `rtid.exe`;
federate developers write **Python** federates against the `rti1516e` wheel.
Windows C++ federate development is a documented future milestone (§7).

Both Go binaries are **CGo-free** (`CGO_ENABLED=0`), listen on **TCP** (no Unix
domain sockets), and contain no OS-specific source files — so Windows and macOS
targets are mechanical cross-compiles from the Linux CI host. Verified building
for `windows/amd64` and `windows/arm64`.

The Python wheel is **pure-Python** (`rti1516e-<ver>-py3-none-any.whl`): one
artifact serves every OS. Its only native dependencies — `grpcio` and
`protobuf` — ship their own platform wheels via pip.

---

## 2. Release procedure (one tag ships everything)

A release is cut by pushing an annotated `v*` tag. Two workflows fire in
parallel off that tag:

```
git tag -a v0.9.0 -m "gorti v0.9.0"
git push origin v0.9.0
```

| Workflow | File | Produces | Channel |
|---|---|---|---|
| Release | `.github/workflows/release.yml` → goreleaser (`.goreleaser.yaml`) | `rtid`/`rti-top` for linux, darwin, windows × amd64, arm64 + `SHA256SUMS` | GitHub Releases |
| PyPI | `.github/workflows/pypi.yml` | `rti1516e` sdist + wheel | PyPI |

Both are idempotent per tag. **Before tagging**, bump the version in **two**
places so they stay in lockstep — the PyPI workflow hard-fails if the tag and
the Python package version disagree:

- `pysdk/pyproject.toml` → `version = "0.9.0"`
- the tag itself → `v0.9.0`

(Go binaries take their version from the tag via goreleaser `ldflags`; no source
edit needed.)

### Archive formats
- Linux/macOS: `gorti_<ver>_<os>_<arch>.tar.gz`
- Windows: `gorti_<ver>_windows_<arch>.zip` (goreleaser `format_overrides`)

Each archive bundles `LICENSE`, `README.md`, and `CHANGELOG-MASTERPLAN.md`.

### Local dry run (no publish)
```bash
goreleaser release --snapshot --clean --skip=publish   # inspect ./dist/
python -m build pysdk                                   # after `buf generate`
```

---

## 3. Linux deployment

### Server (`rtid`)
```bash
curl -LO https://github.com/cbchoi/gorti/releases/download/v0.9.0/gorti_0.9.0_linux_amd64.tar.gz
curl -LO https://github.com/cbchoi/gorti/releases/download/v0.9.0/gorti_0.9.0_SHA256SUMS
sha256sum -c gorti_0.9.0_SHA256SUMS --ignore-missing
tar xzf gorti_0.9.0_linux_amd64.tar.gz
./rtid --listen :8442 --metrics-listen :9090
```

Optional: run under systemd (unit file ships nothing OS-specific — a plain
`ExecStart=/opt/gorti/rtid --listen :8442` service works; `SIGTERM` triggers
graceful shutdown).

### Python federates
```bash
pip install rti1516e
```

---

## 4. Windows deployment

### Server (`rtid.exe`)
```powershell
# PowerShell
Invoke-WebRequest -Uri https://github.com/cbchoi/gorti/releases/download/v0.9.0/gorti_0.9.0_windows_amd64.zip -OutFile gorti.zip
Expand-Archive gorti.zip -DestinationPath gorti
.\gorti\rtid.exe --listen :8442 --metrics-listen :9090
```

Verify the download against `SHA256SUMS`:
```powershell
(Get-FileHash gorti_0.9.0_windows_amd64.zip -Algorithm SHA256).Hash.ToLower()
# compare to the matching line in gorti_0.9.0_SHA256SUMS
```

### Python federates
```powershell
pip install rti1516e
```
The wheel is pure-Python; pip pulls Windows wheels for `grpcio`/`protobuf`
automatically. Python 3.11+.

### Windows runtime notes
- **Graceful shutdown:** `rtid.exe` shuts down cleanly on **Ctrl+C**
  (`os.Interrupt`). `SIGTERM` exists in the Go build but is never delivered on
  Windows — this is expected and does not affect Ctrl+C or console-close
  handling.
- **Running as a Windows Service:** not bundled in v0.9.0. Wrap `rtid.exe` with
  a service manager such as [NSSM](https://nssm.cc/) or WinSW if you need it to
  survive logout / start at boot. (A native `--service` mode is a candidate for
  a later release.)
- **Firewall:** `rtid` listens on TCP `:8442` (federates) and `:9090`
  (Prometheus metrics) by default — open or bind (`--listen 127.0.0.1:8442`) as
  your network policy requires. `--admin-listen` defaults to loopback.

---

## 5. Verification checklist (per release)

CI already gates correctness on every push/PR (`.github/workflows/conformance.yml`
+ `ci.yml`). At tag time, additionally confirm:

- [ ] `pysdk/pyproject.toml` version == tag (the PyPI workflow enforces this)
- [ ] GitHub Release contains 6 archives (linux/darwin/windows × amd64/arm64 ×
      the two-binary bundle) + `SHA256SUMS`
- [ ] `sha256sum -c` (Linux/macOS) / `Get-FileHash` (Windows) matches
- [ ] `pip install rti1516e==<ver>` in a clean venv imports and resolves the
      wire stubs:
      ```python
      import rti1516e
      from rti1516e.standard import Rti1516eAmbassador   # Layer-2 surface
      ```
- [ ] `rtid --version` / `rtid.exe --version` prints the tag

---

## 6. One-time channel setup

### GitHub Releases
Already wired. `release.yml` uses the default `GITHUB_TOKEN` (needs
`contents: write`, already granted). Nothing to configure.

### PyPI (Trusted Publishing — no API token)
The `pypi.yml` workflow publishes via OIDC trusted publishing. One-time setup on
PyPI:

1. Create/claim the `rti1516e` project on PyPI.
2. Project → **Settings → Publishing → Add a new trusted publisher**:
   - Publisher: **GitHub Actions**
   - Owner: `cbchoi`, Repository: `gorti`
   - Workflow name: `pypi.yml`
   - Environment: `pypi`
3. Create a GitHub environment named `pypi` (repo → Settings → Environments) if
   you want approval gates on publishes.

No secrets are stored in the repo. (If you prefer an API token instead, add
`password: ${{ secrets.PYPI_API_TOKEN }}` to the publish step and drop the
`id-token: write` permission.)

---

## 7. C++ federate SDK on Windows — deferred (future plan)

The `cppsdk` IEEE 1516.1-2010 DLC SDK builds today on **Linux** (and macOS) via
conan + CMake. It is **not** yet buildable on Windows/MSVC. This is a deliberate
v0.9.0 scoping decision: Windows federate developers use the Python SDK for now.

When Windows C++ support is picked up, the concrete blockers are:

1. **Compiler flags** — `cppsdk/CMakeLists.txt:38` hard-codes
   `add_compile_options(-Wall -Wextra -Wpedantic)`, which MSVC rejects. Gate it:
   ```cmake
   if(MSVC)
     add_compile_options(/W4)
   else()
     add_compile_options(-Wall -Wextra -Wpedantic)
   endif()
   ```
2. **DLL export macro** — `cppsdk/include/RTI/SpecificConfig.h:41` defines
   `RTI_EXPORT` only for GCC/Clang (`__attribute__((visibility("default")))`).
   Add an MSVC branch (`__declspec(dllexport)` / `__declspec(dllimport)`), as
   the IEEE 1516.1 `SpecificConfig.h` contract intends.
3. **conan profile** — add a Windows profile (MSVC or MinGW) and confirm the
   static grpc/protobuf/gtest dependency graph resolves there
   (`conanfile.txt` currently notes linux_x64 gcc/clang prebuilts only).
4. **CI** — add a `windows-latest` job to `conformance.yml` (or a dedicated
   matrix) that configures + builds the lib and runs the runtime suites.
5. **Distribution decision** — source-only (documented build) vs. prebuilt libs
   per (MSVC toolset, arch) as release artifacts, and optionally a conan/vcpkg
   recipe. Prebuilt C++ carries an ABI/runtime-matching maintenance burden per
   toolset; defer until there is Windows C++ federate demand.

The `PRTI_HOME`-gated conformance cross-check and the whole Go/Python surface are
unaffected — this is purely the C++ SDK's Windows build.

---

## 8. Optional future enhancements (not in v0.9.0 scope)

- **Container image** — `rtid` is CGo-free, so a `FROM scratch` / distroless
  image is ~15 MB. A `docker/Dockerfile` + a `ghcr.io/cbchoi/gorti/rtid` publish
  job would add a Linux container channel. (Windows containers are niche; skip.)
- **conan / vcpkg package** for `cppsdk` — only meaningful once §7 lands.
- **Windows Service mode** (`rtid --service`) or a signed MSI installer, if
  Windows becomes a primary server platform (code-signing cert required).
- **Homebrew tap / Scoop manifest** for one-line CLI installs.
