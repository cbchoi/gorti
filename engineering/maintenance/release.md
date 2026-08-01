# Release and distribution

## Distribution matrix

| Artifact | Platforms | Architectures or format | Channel |
|---|---|---|---|
| `rtid` and `rti-top` | Linux, macOS, Windows | amd64 and arm64 | GitHub Releases |
| Python `rti1516e` | Platform independent | wheel and source distribution | PyPI |
| C++ SDK | Source build | C++17 | Repository source |

GoReleaser builds the Go binaries with `CGO_ENABLED=0`; the experimental DDS
binary is not released. Each platform archive contains both Go binaries plus
`LICENSE`, `README.md`, and `CHANGELOG.md`. Windows uses zip and other platforms
use tar.gz. The Python wheel is pure Python, while `grpcio` and `protobuf`
resolve their own runtime distributions.

## Version and release content

Use an `X.Y.Z` package version and a matching `vX.Y.Z` tag. The metadata check
requires the same version in:

- `pysdk/pyproject.toml`;
- `cppsdk/CMakeLists.txt`;
- `CITATION.cff`; and
- `codemeta.json`.

Also update `CHANGELOG.md`, the applicable page under `docs/releases/`, and the
`mkdocs.yml` release navigation before tagging. Update other version examples
only when they describe the current release rather than historical output.

Verify metadata early:

```bash
python scripts/check_release_metadata.py
python scripts/check_release_metadata.py --tag vX.Y.Z
```

## Preflight

Work from a clean checkout with full tag history. Generate all bindings first,
then run the release workflow's local equivalents:

```bash
buf generate
git diff --exit-code -- rti/internal/genproto

go test ./...

python -m pip install -e "./pysdk[dev]"
python -m pytest pysdk/tests
(cd pysdk && python -m mypy --strict .)

python -m pip install -r docs/requirements.txt
python -m mkdocs build --strict

python scripts/check_release_metadata.py --tag vX.Y.Z
goreleaser release --snapshot --clean
```

Run the manually dispatched validation and conformance workflows for the
release commit, or reproduce the full conformance gate locally:

```bash
bash scripts/ci-gates.sh
```

The conformance path requires Conan-generated C++ dependencies and can take
substantially longer on an empty cache. Review any change to
`EXPECTED_VERDICTS.txt` rather than accepting it as routine test output.

The PyPI workflow copies the root `LICENSE` into `pysdk/`, runs
`python -m build pysdk --outdir dist`, and inspects the wheel for generated
stubs and accidental recursive paths. Reproduce that exact package build in a
disposable checkout when package layout changes; `pysdk/LICENSE` is a temporary
workflow input and must not be committed.

## Tag and publish

Do not push the tag until the release commit is reviewed, the worktree is
clean, metadata matches, and the required manual workflows are green.

```bash
git tag -a vX.Y.Z -m "gorti vX.Y.Z"
git push origin vX.Y.Z
```

A pushed `v*` tag starts two independent workflows:

1. `release.yml` regenerates bindings, checks tag metadata, tests all Go
   packages, tests and type-checks the Python SDK, builds docs, and then runs
   GoReleaser v2.
2. `pypi.yml` regenerates bindings, checks the tag against the Python version,
   builds and inspects the sdist and wheel, and publishes through the `pypi`
   environment using trusted publishing.

GoReleaser creates a non-draft GitHub Release, marks prereleases automatically,
builds six platform/architecture archives, and uploads
`gorti_X.Y.Z_SHA256SUMS`. The PyPI trusted publisher must be registered for the
repository, `pypi.yml` workflow, and `pypi` environment before the tag is
pushed.

## Post-release verification

- Confirm both tag workflows completed successfully.
- Confirm all six archives exist and each contains `rtid`, `rti-top`, and the
  three repository documents.
- Verify `gorti_X.Y.Z_SHA256SUMS` against downloads on a clean host.
- Run `rtid --version` and `rti-top --version` from an archive and confirm the
  tag-derived build information.
- Install `rti1516e==X.Y.Z` into a clean Python 3.11+ environment, import it,
  and run a live connection to the released `rtid`.
- Inspect the wheel to confirm `rti1516e/_generated/rti/v1/*.py` and gRPC stubs
  are present.
- Confirm Windows can stop `rtid.exe` cleanly with Ctrl+C.
- Confirm the release notes and hosted documentation describe the shipped
  behavior and known limitations.

If publishing fails, diagnose and rerun the failed workflow against the same
immutable tag. Do not move a tag that users may already have fetched. PyPI
versions cannot be replaced; a defect in a published package requires a new
version.
