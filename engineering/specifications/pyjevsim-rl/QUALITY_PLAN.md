# pyjevsim RL Quality Plan

Version: 0.1-draft  
Updated: 2026-07-19

This plan applies ISO 9001:2015 quality-management principles, including the
2024 climate-action amendment where relevant to organizational context. It is
a development-process alignment statement, not an ISO certification claim.

## Controls

- SRS, SDD, IDD and STD are controlled design inputs. Approval is recorded
  before promotion into `engineering/specifications/current/`.
- Requirements, design elements, interfaces, tasks, tests and evidence use
  stable IDs and bidirectional traceability.
- A task moves through `planned -> ready -> doing -> review -> reflect -> done`.
  `done` requires acceptance evidence and no blocking review finding.
- Changes to a requirement or interface include impact analysis, affected
  tests, compatibility treatment, reviewer and effective version.
- Nonconformance creates a finding with severity, containment, root cause,
  corrective action, owner, due date and verification. Repetition or systemic
  impact creates a CAPA record.

## Review cycle

Each implementation slice records:

1. **Plan** — objective, requirement/task IDs, design decision, risks,
   acceptance tests and rollback.
2. **Do** — changed artifacts, assumptions, commands and generated evidence.
3. **Review** — independent findings, test outcomes, semantic and trace audits.
4. **Reflect** — gap to plan, root cause, lessons, risk/ADR/CAPA changes and the
   next bounded slice.

## Quality objectives

- 100% of mandatory implemented requirements have executable evidence.
- Identical seed/configuration/choreography has one semantic digest.
- No silent event, transition or experience loss is accepted.
- A third-party model follows the documented factory/binding workflow without
  changes to framework core.
- Local and federation backends share the public environment/transition
  contract and publish explicit completion semantics.
