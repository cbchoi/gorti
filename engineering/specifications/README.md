# Engineering Specifications

The [current specifications](current/) are gorti's authoritative formal
engineering baseline. The history directory contains reconstructed,
non-normative summaries of earlier documented stages. Stage labels identify
documentation context, not release tags or release dates.

## Current formal baseline

Status: Authoritative

Context: v4 conformance scope

| Document | Purpose |
|---|---|
| [SRS](current/SRS.md) | Required behavior, constraints, and supported scope |
| [SDD](current/SDD.md) | Architecture, state ownership, ordering, and failure design |
| [IDD](current/IDD.md) | Public SDK, wire, callback, and internal interface contracts |
| [STD](current/STD.md) | Test scenarios, acceptance rules, and evidence boundaries |

## Historical summaries

Status: Historical summaries; non-normative

Reconstructed: 2026-07-17 from repository history and surviving specification
content; no release date is asserted

Authority: The [current specifications](current/) define the formal engineering
baseline.

| Stage | Scope summarized | Documents |
|---|---|---|
| v1-mvp | Federation lifecycle, pub/sub, object exchange, basic time management, Go/Python | [SRS](history/v1-mvp/SRS.md), [SDD](history/v1-mvp/SDD.md), [IDD](history/v1-mvp/IDD.md), [STD](history/v1-mvp/STD.md) |
| v2-services | Complete service groups, save/restore, ownership, DDM, MOM, security | [SRS](history/v2-services/SRS.md), [SDD](history/v2-services/SDD.md), [IDD](history/v2-services/IDD.md), [STD](history/v2-services/STD.md) |
| v3-research | Research alternatives, C++ SDK, observability, reproducible comparison | [SRS](history/v3-research/SRS.md), [SDD](history/v3-research/SDD.md), [IDD](history/v3-research/IDD.md), [STD](history/v3-research/STD.md) |

The v1-v3 summaries are cumulative descriptions reconstructed from the
available record. They do not claim byte-for-byte recovery of documents as
they existed at a particular time.

## Draft extensions

The [pyjevsim reinforcement-learning baseline](pyjevsim-rl/) is a draft
extension with its own SRS, SDD, IDD, STD, traceability, risk, and milestone
records. It is not part of the authoritative v4 baseline until it is reviewed
and promoted through change control.
