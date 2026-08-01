# Engineering Documents

This directory contains the maintained engineering baseline and maintainer
procedures that sit behind the published user manual.

## Start here

- [Development guide](development.md): repository architecture, setup, build,
  code generation, test tiers, and CI mapping
- [Specifications](specifications/README.md): authoritative current SRS, SDD,
  IDD, and STD plus historical summaries
- [Design records](design/README.md): design authority and retention policy
- [Verification records](verification/README.md): relationship between the STD,
  executable fixtures, and evidence
- [Maintenance guides](maintenance/README.md): documentation publishing and
  release procedures

## Architecture orientation

`rtid` is a standalone Go process that owns authoritative federation state.
Go, Python, and C++ federate SDKs translate language APIs to the shared gRPC
contract under `proto/`. The composition root in `rti/cmd/rtid` wires transport
handlers to service managers under `rti/internal/`; accepted transitions flow
through per-federate outboxes and ordered callback streams, with event-log and
save/restore support where required.

Use the current [SDD](specifications/current/SDD.md) for architecture and state
ownership, the [IDD](specifications/current/IDD.md) for interface contracts,
and the [STD](specifications/current/STD.md) for acceptance criteria. Source and
tests remain the executable detail beneath those documents.

## Choose the right document

| Change | Primary orientation | Evidence or follow-up |
|---|---|---|
| RTI service behavior | Current SRS and SDD | Package tests and current STD |
| Wire or SDK surface | Current IDD and `proto/` | Generated bindings and cross-language tests |
| Performance claim | `docs/reproducibility.md` | `verification/fair-comparison/` |
| User workflow | `docs/` | Strict MkDocs build |
| Release or publishing process | `maintenance/` | Workflow configuration |

`docs/` is reserved for installation, usage, operations, public verification
summaries, reproducibility, release notes, and citation. Engineering material
is repository-facing and is not part of the MkDocs navigation unless a page in
`docs/` explicitly includes or links it.
