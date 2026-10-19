# Python Dependency and Lint Repair

## Scope

Follow-up to `ieee1516-2010-applicable-fixes-20261004.md`. This work addresses
the missing Python packages, the SDK lint/type-check failures, and regressions
directly related to those repairs. It does not repeat or supersede the full
Go, Java, Python or conformance campaigns.

## Environment

The existing environment is `D:/workspace/gorti/n04-venv` (Python 3.14.0).
Installed dependencies follow the existing `pysdk/uv.lock` versions:

- NumPy 2.5.2
- mypy 2.3.0
- Ruff 0.15.22
- types-protobuf 7.34.1.20260518
- pyjevsim 2.1.2 at Git revision
  `9893099b47aae89a3432c668b18ec4b6b15c043e`

`pip check` reports no broken requirements. The existing grpcio and Protobuf
runtime installations were not replaced. Package versions and the pyjevsim
installation origin are recorded in `packages-after.json` in the evidence
directory below.

mypy now uses the active interpreter version, matching the installed optional
dependency profile. A fixed Python 3.11 target could not parse the NumPy stubs
selected for Python 3.14. Strict mode and existing lint rules remain enabled.
This run does not establish Python 3.11 runtime compatibility.

## Repairs and Regression Results

Added explicit types for checkpoint state, local callback notifications,
restore bookkeeping, service metadata and logical-time results. Integer64
times remain integers; annotations and casts do not convert them to floats.
Callback collection annotations accept the existing list, tuple and set
values. Import ordering, long lines and test-only fixture/name diagnostics
were corrected without disabling a rule for an entire source package.

The local generated Python bindings were older than the current schema and
lacked `callback_recovery`. Regenerated the ignored outputs using the existing
`python -m rti1516e._proto` command; no `.proto` definitions changed in this
follow-up. The SDK verification instructions now include dependency setup and
code generation.

The Windows lint subprocesses now both emit and decode UTF-8. This preserves
Korean tool diagnostics instead of failing in a CP949 reader thread. The
process-worker serialization test compares the reported remote exception
against the current interpreter's own pickle error for the same fixture;
Python 3.14 uses `PicklingError` where earlier CPython used `AttributeError`.
The malformed-MIM test checks the MIM validation detail and the bounded
SDK/gRPC exception families instead of accepting any exception.

Final scoped results:

| Check | Result | Evidence |
| --- | --- | --- |
| Dependency consistency | Pass | `pip-check-final.txt` |
| Strict mypy, original 89 source files | Pass | `mypy-scoped-final.txt`, `mypy-scoped-paths.json` |
| Ruff, original source/test repair scope | Pass | `ruff-scoped-r2.txt`, `ruff-scoped-paths.json` |
| SDK affected files and directly related contracts, local generated bindings | 790 passed, no failures/skips | `sdk-local-r2.xml`, `sdk-local-paths.json` |
| pyjevsim/RL affected components and dependency-unblocked joined replay | 311 passed, 2 failed, no skips | `rl-r2.xml` |
| Existing whole-SDK lint/type pytest checks | 2 failed, no warnings | `lint-gate-r2.xml` |

The SDK selection includes actual TCP server and restart tests using the
existing evaluated RTID binary, SHA-256
`c4272e212e51dcd44fb4d4d1dad44520b0f8ada55c1f5d3ada64553d1dfdda10`.
No Go/Java/C++ suite or performance campaign was rerun. A prior 486-case
SDK pass overlaps the final selection and is not added to its total.

The final RL selection comprises branch runner/optimization, local process,
queue control/snapshot/review, rollout scope, reference PPO/qualification/
runtime identity, PPO features and joined-reference PPO tests.

Typing-only edits to `queue_control.py` changed its source hash. The internal
queue snapshot pin was updated from
`0ee12df83484b073ea797123bf01dab58da7f17e1cd29d5eda375dc7c87f1e3e`
to `9ae3023a1684fbbc40d21263e8232307f55ab048d86427c53fc440549a7d2d3d`.
Queue snapshot source identity and serialized bytes therefore change. Exact
source checks remain enforced; this is not an old-snapshot migration and does
not silently admit snapshots from the previous source profile. Saved PPO
qualification artifacts were not rewritten.

During this work, new `pyjevsim_bridge/rl/continuation` feature files appeared
in the shared checkout. They introduce additional whole-SDK lint/type errors
outside the starting repair inventory. Their implementation is preserved;
the original repair scope and the changing whole-tree result are recorded
separately. No permanent exclusion is added for that package.

At the final whole-SDK gate invocation, mypy reported 126 errors in five new
continuation files (`boundary`, `contracts`, `envelope`, `references`, and
`registry`). Ruff reported 40 findings in the new continuation source/tests.
Those counts are the recorded invocation, not a claim that a concurrently
edited checkout remains unchanged. Thus the whole-SDK gates are still red;
the scoped passing results above must not be presented as a whole-tree pass.

## Reference PPO Baseline Mismatch

Installing the missing dependencies exposes a separate incompatibility in
the two previously blocked joined-reference PPO tests. The committed
qualification artifacts declare algorithm version `1`, while the current
implementation declares version `2`. The policy loader and learner reject
that mismatch before replay or training. The original fixture, policy bytes,
integrity checks and algorithm identity checks have not been relabeled or
weakened to produce a passing result.

The failures are:

- `test_one_episode_actual_pyjevsim_replay_after_activation`
- `test_two_joined_oracle_batches_reproduce_learner_bytes`

A separately reviewed version-2 qualification baseline is needed before these
legacy replay checks can demonstrate current-version reproduction. This is
not an unresolved package installation failure.

## Evidence

Raw output remains outside the repository at
`D:/workspace/gorti/python-lint-fixes-20261004`. Earlier failed outputs are
preserved, including the dependency-unblocked PPO failures. Historical full
campaign counts in the preceding report remain unchanged.
