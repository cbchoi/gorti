# Publishing documentation

The repository is configured for Read the Docs and GitHub Pages. The local and
CI builds are strict and use the same pinned dependencies. The public hosting
projects must still be enabled in their respective service settings.

## Local build

```bash
python -m venv .docs-venv
python -m pip install -r docs/requirements.txt
python -m mkdocs build --strict
```

## Read the Docs

1. Import `cbchoi/gorti` in Read the Docs.
2. Keep the configuration file path at `.readthedocs.yaml`.
3. Build the default branch and the eventual v0.9 release tag.
4. Enable pull-request builds if repository permissions allow it.
5. Set the canonical documentation domain only after the project URL exists.

`.readthedocs.yaml` selects Ubuntu 22.04, Python 3.11, `mkdocs.yml`, and the
pinned requirements in `docs/requirements.txt`.

## GitHub Pages

The documentation workflow runs `mkdocs build --strict` on pull requests and
pushes. Deployment runs only for `main` after GitHub Pages is configured to use
GitHub Actions. Keep links relative so the same content renders correctly on
both hosts.
