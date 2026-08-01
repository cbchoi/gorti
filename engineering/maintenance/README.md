# Maintenance guides

Repository maintenance procedures are kept separate from the user manual:

- [Documentation publishing](documentation.md): document ownership, local
  preview, strict build, CI artifact, and Read the Docs configuration
- [Release and distribution](release.md): version alignment, preflight,
  tagging, GitHub artifacts, and PyPI publishing

For day-to-day architecture, build, code generation, tests, and CI, use the
[development guide](../development.md).

## Sources of truth

| Concern | Authoritative file or directory |
|---|---|
| User documentation and navigation | `docs/` and `mkdocs.yml` |
| Formal behavior and acceptance baseline | `engineering/specifications/current/` |
| Protocol generation | `proto/`, `buf.yaml`, and `buf.gen.yaml` |
| Local command composition | `Makefile` and `scripts/` |
| Automated triggers and steps | `.github/workflows/` |
| Go release artifacts | `.goreleaser.yaml` |
| Python package metadata | `pysdk/pyproject.toml` and `pysdk/MANIFEST.in` |

When a guide and automation differ, stop and reconcile them before the next
release. Do not silently document an aspirational command as if CI runs it.
