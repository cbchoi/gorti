# Tasks

Orchestrator-written task briefs. Each `TASK-NNN.md` is one unit of work assigned to exactly one agent.

See `docs/DISPATCH.md` for the protocol. See `TASK-template.md` for the file format.

## Index

This README is updated by the orchestrator after each task transition.

| ID | Title | Assignee | Milestone | Status |
|---|---|---|---|---|
| (none yet — M0 is orchestrator-led, no agent tasks dispatched until M1) | | | | |

## Status legend

- **DISPATCHED**: written and assigned; agent has not yet started.
- **IN_PROGRESS**: agent has acknowledged and is working.
- **IN_REVIEW**: agent has opened a PR with the completion sentinel; awaiting orchestrator review.
- **DONE**: PR merged to `main`.
- **CANCELLED**: task closed without merge; see Notes section in the file for reason.

## Subdirectories

- `signals/` — agent completion sentinels (`TASK-NNN.done` files). When an agent finishes a task, the final commit on the topic branch creates `signals/TASK-NNN.done`. Spec: `signals/README.md`. Protocol: `docs/DISPATCH.md` §10.
