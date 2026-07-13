# Contributing to gorti

Thank you for helping improve gorti. Changes are welcome as focused issues or
pull requests.

## Before you start

1. Search the issue tracker for related work.
2. Open an issue before changing a public protocol or HLA service semantic.
3. Keep compatibility and determinism requirements explicit in the proposal.

## Development setup

The core requires Go 1.22 or later. Python SDK development requires Python
3.11 or later.

```bash
git clone https://github.com/cbchoi/gorti.git
cd gorti
go test ./...

python -m venv .venv
python -m pip install -e "./pysdk[dev]"
python -m pytest pysdk/tests
```

Build the documentation with:

```bash
python -m pip install -r docs/requirements.txt
python -m mkdocs build --strict
```

## Pull requests

- Add focused tests for behavioral changes.
- Preserve the caller-visible ACK boundary, TSO-before-grant ordering,
  generation fencing, and deterministic event ordering.
- Run `go test ./...` and the affected Python tests.
- Update user documentation and `CHANGELOG-MASTERPLAN.md` when behavior changes.
- Do not commit generated benchmark output, local launcher paths, event logs, or
  save-state directories.

Performance claims need the fair-comparison protocol described in
[`docs/reproducibility.md`](docs/reproducibility.md). A faster result is not
accepted if it changes the measured semantics.

## Reporting conduct or security issues

Community conduct is governed by [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).
Report security issues using the private process in [`SECURITY.md`](SECURITY.md).
