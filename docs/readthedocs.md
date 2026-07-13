# Publishing documentation

The repository is configured for Read the Docs. Local and CI builds are strict
and use the same pinned dependencies. The public hosting project must still be
enabled in the Read the Docs service settings.

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

The `Docs` GitHub Actions workflow validates the same strict build on pull
requests and pushes. Read the Docs owns publication; GitHub Actions does not
deploy a second copy of the site.
