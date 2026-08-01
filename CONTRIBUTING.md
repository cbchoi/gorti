# Contributing to gorti

Thank you for helping improve gorti. Changes are welcome as focused issues or
pull requests. The detailed repository map and command reference live in the
[development guide](engineering/development.md).

## Before you start

1. Search the issue tracker for related work.
2. Open an issue before changing the public protocol, an SDK contract, or HLA
   service semantics.
3. State compatibility, ordering, determinism, and performance boundaries in
   proposals that affect observable behavior.
4. Read the current [SRS](engineering/specifications/current/SRS.md),
   [SDD](engineering/specifications/current/SDD.md),
   [IDD](engineering/specifications/current/IDD.md), and
   [STD](engineering/specifications/current/STD.md) for the affected service.

## Development setup

The common toolchain is Go 1.22 or later, Python 3.11 or later, and Buf. The
Makefile and conformance scripts use POSIX shell utilities; use Linux, macOS,
WSL, or Git Bash for those entry points. Direct Go and Python commands also
work from PowerShell.

From the repository root:

```bash
git clone https://github.com/cbchoi/gorti.git
cd gorti

python -m venv .venv
. .venv/bin/activate
python -m pip install -e "./pysdk[dev]"

buf generate
make build
go test ./...
python -m pytest pysdk/tests
```

On PowerShell, activate the environment with
`.venv\Scripts\Activate.ps1`. Run `make help` for the maintained target list.
C++ SDK work additionally needs a C++17 compiler, CMake 3.18 or later, and the
Conan dependencies described in [`cppsdk/README.md`](cppsdk/README.md).

## Work locally before relying on CI

`make verify` formats the tree and runs Go/Python lint where tools are present,
race-enabled tests, and the determinism harness. Review its formatting changes.
It does not run Python type checking, protocol breaking checks, documentation,
or the full C++ conformance gate, so run the checks that match the change:

```bash
make py-typecheck
buf lint
buf breaking --against ".git#branch=main"
make docs
bash scripts/ci-gates.sh
```

The final command requires generated C++ stubs, a Conan toolchain, and the
other dependencies installed by `.github/workflows/conformance.yml`. It can be
run by stage, for example `bash scripts/ci-gates.sh stubs go static ivct`.

GitHub Actions workflows use different triggers:

- `.github/workflows/docs.yml` runs automatically for documentation-related
  pull requests and pushes to `main`.
- `.github/workflows/ci.yml` and `conformance.yml` are manually dispatched.
- `release.yml` and `pypi.yml` run for `v*` tags.

Run relevant checks locally and arrange manual workflow runs when required;
opening a pull request does not automatically execute every code gate.

## Documentation

Read the Docs builds the MkDocs site from `.readthedocs.yaml`. The build uses
the pinned packages in `docs/requirements.txt` and treats every MkDocs warning
as an error. Reproduce that build locally from the repository root:

```bash
python -m venv .docs-venv
.docs-venv/bin/python -m pip install -r docs/requirements.txt
.docs-venv/bin/python -m mkdocs build --strict --clean
```

On PowerShell, use `.docs-venv\Scripts\python.exe` instead of
`.docs-venv/bin/python`. Replace `build --strict --clean` with `serve` for a
live preview. Read the Docs supplies the canonical site URL during hosted
builds; no repository-specific URL needs to be hard-coded in `mkdocs.yml`.

## Protocol and generated code

Edit protocol definitions only under `proto/`, then run `make proto` (or
`buf generate`) from the repository root. Buf writes all language bindings:

- `rti/internal/genproto/` contains committed Go bindings. Include their diff
  in the same change and verify it with
  `git diff --exit-code -- rti/internal/genproto` after regeneration.
- `pysdk/rti1516e/_generated/` and `cppsdk/_generated/` are ignored build
  inputs. Regenerate them before Python integration tests, C++ builds, and
  package builds; do not commit them.

Do not edit generated files by hand. Python-only contributors may run
`python -m rti1516e._proto` (the `make py-codegen` equivalent) after installing
`pysdk[dev]`; its `grpcio-tools` version is compatible with the protobuf 7
runtime floor. Use Buf when regenerating all languages.

## Pull requests

- Keep the change focused and add tests at the lowest useful layer.
- Preserve the caller-visible ACK boundary, TSO-before-grant ordering,
  generation fencing, backpressure behavior, and deterministic event order.
- Run focused tests while iterating, then the applicable aggregate and
  cross-process checks from the [development guide](engineering/development.md).
- Update `docs/` for user-visible behavior, the current engineering baseline
  for contract changes, and `CHANGELOG.md` for release-facing changes.
- Do not commit benchmark output, local launcher paths, event logs, generated
  Python/C++ stubs, build directories, or save-state directories.

Performance claims must follow the fair-comparison protocol in
[`docs/reproducibility.md`](docs/reproducibility.md). A faster result is not
accepted if it changes the measured semantics.

## Reporting conduct or security issues

Community conduct is governed by [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).
Report security issues using the private process in [`SECURITY.md`](SECURITY.md).
