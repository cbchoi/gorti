# Documentation review: 18 October 2026

## Scope

Reviewed repository entry points, public user guides, SDK READMEs, current
SRS/SDD/IDD/STD, development and release procedures, and benchmark entry points
against the working tree. Updated 28 existing Markdown files and added this
record. Production code, schemas, workflows, and test assertions were unchanged.
Existing worktree changes were preserved.

Base commit: `4852206bdfcfa770df4d34fb1a0251fb87b811e1`. The checkout has existing
uncommitted implementation changes, so this review concerns the working tree,
not that commit alone.

## Corrections

| Finding | Documentation correction | Implementation evidence |
|---|---|---|
| C++ remained in current requirements, architecture, and developer setup | Current interfaces are Go/Python/Java; retained C++ material is labeled legacy | `javasdk/`, `pysdk/`, `rti/pkg/federate/`; current service campaign selection |
| Java overview described only initial services and Float64 time | Added ownership, DDM, save/restore, encodings, exact Integer64, and callback recovery | `javasdk/service-coverage.tsv`, `TimeWire.java`, `RuntimeCheckpoint.java`, stateful service classes |
| Python README said the server enabled Float64 only | Documented both factories and the Integer64 CLI switch | `rti/internal/application/server/run.go`, typed adapters and managers |
| Java README rejected empty synchronization sets and targeted attribute removals | Empty synchronization set selects current members; explicit empty attribute removal is a no-op | `SynchronizationServices.java`, `SynchronizationServicesTest.java`, `ExchangeServices.java`, `declaration.proto` |
| DDM association lifecycle and delayed subscriptions were listed as future work | Described inactive/active associations, region copies, and delivery-time subscription evaluation | `hla/ddm/association_lifecycle.go`, `conveyed_lifecycle.go`, `hla/object/delayed_subscription.go` |
| MOM queue lengths were described as time-queue-only counts | Described waiting plus committed ready/transport/SDK data until authenticated entry | `hla/time/data_queue_metrics.go`, `adapters/grpc/callback_data_queue.go`, application MOM sources |
| Custom dial options were said to disable LocalLRC | Distinguished dial options from bearer/OIDC fallback; documented session-token authentication | `rti/pkg/federate/federate.go`, `local_lrc.go` |
| Generated Go bindings still pointed to `internal/genproto` | Corrected generation path and separated Buf from Maven generation | `buf.gen.yaml`, `javasdk/wire/pom.xml` |
| SDD claimed HLA services depended only on core | Documented the two current sibling-package dependencies | `hla/mom/handlers_request.go`, `hla/object/transportation_checkpoint_preparation.go` |
| Public verification omitted the later full service run and traceability | Added dated Go/Python/Java counts and requirement reconciliation | 4 October full service report and requirements inventory |
| July performance data was labeled current | Presented both October sessions, timing definitions, and SDK-only improvement separately | 7/8 October Portico reports and the isolated SDK experiment |
| Benchmark entry points described only the paired runner | Added crossover design and distinguished artifact policies | `benchmark/callback-reporting/compare_portico.py` and runner README |

## Editorial changes

Removed repeated generic disclaimers, conversational development notes, internal
task labels, and unsupported language-speed or competitor-maintenance assertions
from current-facing explanations. Replaced them with service behavior,
configuration choices, and dated evidence.

Kept operational warnings that affect security, correctness, or interpretation:
authentication boundaries, callback entry/completion, explicit drains, time
representation, certification status, and experimental cluster deployment.
Historical specification summaries and dated experiment reports retain their
original results. The earlier release-candidate page now directs readers to
current implementation and verification pages.

SRS is version 4.5; SDD, IDD, and STD are version 4.6. Requirement identifiers
and behavioral assertions remain in place; the supported encoding-language set
now excludes C++.

## Remaining automation differences

Follow-up: these differences were addressed in the subsequent supported-SDK
workflow cleanup. Buf now generates Go/Python only, Maven generates Java, and
manual CI, conformance stages and release preflight validate Go/Python/Java.
The metadata check validates Java reactor versions without reading C++ metadata.
The list below records what the documentation-only review found before that change.

The documentation now distinguishes current support from retained automation:

- `buf.gen.yaml` still generates legacy C++ bindings.
- `conformance.yml` and the default `scripts/ci-gates.sh` selection still run
  C++/DLC stages.
- `scripts/check_release_metadata.py` still checks the C++ CMake version and
  does not check Maven versions.
- The manual validation workflow has no Java Maven job.

These files were inspected but not changed in this documentation task. The
development and release guides give explicit Go/Python/Java commands instead
of presenting the legacy workflow as the current service campaign.

## Validation

- Strict MkDocs build passed with the pinned documentation dependencies.
- `go test ./tests/architecture` passed.
- Relative links in the edited Markdown files were checked against the checkout.
- `git diff --check` was used for whitespace validation.

The site output is outside the repository at
`D:/workspace/gorti/documentation-review-20261018/site/`.
No new performance measurements or full service/race campaign were run.
The documented October test and benchmark results are preserved prior executions.
No commit, push, release, or hosted-documentation deployment was performed.
