# Documentation publishing

## Document ownership

Choose the document set by audience:

- `docs/` is the MkDocs source for installation, use, operations, public
  verification summaries, reproducibility, performance, releases, and citation.
- `engineering/` holds repository-facing architecture, formal specifications,
  verification records, and maintainer procedures. It is not copied into the
  published site by default.
- Root files such as `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, and
  `SUPPORT.md` are repository entry points.
- SDK and example READMEs document their local build or execution path.

Avoid duplicating normative behavior. Link user explanations to the current
engineering baseline and keep command details near the automation that owns
them.

## Local build and preview

The site uses dependencies pinned in `docs/requirements.txt`. From the
repository root:

```bash
python -m venv .docs-venv
. .docs-venv/bin/activate
python -m pip install -r docs/requirements.txt
python -m mkdocs build --strict
python -m mkdocs serve
```

The equivalent Make targets are `make docs-deps`, `make docs`, and
`make docs-serve`. They use the active `pip` and `mkdocs` executables. On
PowerShell, activate with `.docs-venv\Scripts\Activate.ps1`; the direct
`python -m mkdocs` commands do not require a POSIX shell.

Strict mode treats warnings as failures and writes the site to `site/`. Confirm
that `site/index.html` is non-empty after the build. `site/` and `.docs-venv/`
are ignored and must not be committed.

## Adding or changing a page

1. Put a public page under `docs/` and add it to the explicit `nav` in
   `mkdocs.yml`. A page omitted from navigation causes a strict-build warning.
2. Use links relative to the current page for other pages under `docs/`. A
   `../` link to a repository file outside `docs/` is not a MkDocs page and
   produces a strict-build warning; use that file's absolute GitHub URL or add
   a public page under `docs/`. In repository-only Markdown, use
   repository-relative links. The snippets plugin searches both the repository
   root and `docs/` and fails when a configured include is missing.
3. Update examples and commands from the actual Makefile, script, or workflow;
   state platform and optional dependency requirements.
4. Update the current SRS, SDD, IDD, or STD when user documentation reflects a
   changed requirement, interface, design invariant, or acceptance rule.
5. Run the strict site build and the no-emoji source check before review:

```bash
make docs
bash scripts/check-no-emojis.sh
```

MkDocs only builds the `docs/` tree. A change limited to `engineering/` still
triggers the docs workflow, but its Markdown links are not validated by MkDocs;
review those relative paths directly.

## GitHub Actions behavior

`.github/workflows/docs.yml` runs for documentation-related pull requests and
pushes to `main`, including changes under `docs/`, `engineering/`, the MkDocs
and Read the Docs configuration, and citation metadata. It:

1. installs `docs/requirements.txt` with Python 3.11;
2. runs `mkdocs build --strict`;
3. verifies `site/index.html`; and
4. uploads a GitHub Pages-compatible artifact.

The workflow validates and packages the site but does not deploy it. There is
no Pages deployment job in the repository.

## Read the Docs

`.readthedocs.yaml` selects Ubuntu 22.04, Python 3.11, `mkdocs.yml`, the pinned
requirements file, and warning-as-error behavior. In the Read the Docs project:

1. use `.readthedocs.yaml` as the configuration path;
2. build the default branch and release tags;
3. enable pull-request builds when repository permissions allow it; and
4. set a canonical domain only after the hosted project URL is active.

Read the Docs deployment is configured in the hosted service, not by the
GitHub Pages readiness workflow.

## Release documentation

Prepare the public release page under `docs/releases/`, add it to `mkdocs.yml`,
and update `CHANGELOG.md` before tagging. Keep the package/version metadata
workflow in the [release guide](release.md); documentation publication alone
does not make a release ready.
