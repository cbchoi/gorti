# Observed cross-RTI service-test results

These runs transfer five existing Java test invocations, plus one separately
written interaction control. They do not run the whole IEEE-derived inventory.
No Portico service assertion was removed to obtain a passing result.

## Retained runs

| Run | Purpose | Passed | Failed |
|---|---|---:|---:|
| [attempt-01](results/attempt-01/results.json) | Portico, incomplete TCP discovery list | 0 | 5 |
| [attempt-02](results/attempt-02/results.json) | Portico, corrected discovery list, original assertions | 0 | 5 |
| [gorti-control-01](results/gorti-control-01/results.json) | Same five adapted test invocations against gorti | 5 | 0 |
| [portico-control-01](results/portico-control-01/results.json) | Separate receive-order interaction control | 1 | 0 |
| [gorti-delivery-control-01](results/gorti-delivery-control-01/results.json) | Same interaction control against gorti | 1 | 0 |

All listed invocations ran; none was skipped. Attempts are not pooled or treated
as repeated independent estimates. The first attempt exposed a configuration
problem: discovery listed only the base port, whereas channels bound to later
ports. Its join failures cannot establish service behavior. Attempt 2 lists all
16 candidate ports and reaches the observations below. The successful control
on both RTIs demonstrates a working connection and one data exchange, not the
correctness of every other fixture assumption.

## First failure in each retained case

| Case | Portico observation in attempt 2 | What was not established |
|---|---|---|
| Federation/time lifecycle | Duplicate-name join expected `FederateNameAlreadyInUse`; received `FederateAlreadyExecutionMember`. | Later regulation and peer-dependent grant assertions were not reached. The fixture uses multiple ambassadors in one JVM; a process-isolated check is needed before attributing the exception to a general RTI defect. |
| Object/interaction exchange | Transportation-name lookup returned `null` instead of `HLAreliable`. | The subsequent object/interaction lifecycle was not exercised by this invocation. |
| Empty-participant synchronization | The callback guard detected `announceSynchronizationPoint` outside the participant's own `evoke` window/caller-thread contract. | The full synchronization completion sequence was not established. This report alone does not distinguish a different thread from same-thread reentrant dispatch. |
| Lookahead at time 1 | `queryGALT` raised `RTIinternalError` with a null-response diagnostic. | The numeric GALT value, timestamp rejection, and retraction assertions were not reached. |
| Lookahead at time 2^53 | Same GALT query failure. | The test does not show that Portico accepted an inadmissible timestamp or rounded its bound incorrectly. |

The transportation-name failure has a source-level explanation: the
[Portico 2.1.4 ambassador](https://github.com/openlvc/portico/blob/portico-2.1.4/codebase/src/java/portico/org/portico/impl/hla1516e/Rti1516eAmbassador.java#L4911-L4930)
marks transportation handle/name queries unsupported and returns `null`. Inspection
of the executed JAR's bytecode confirms that path. The other observations remain
reported differences or failures under this fixture; they have not been adjudicated
as IEEE violations. In particular, the numerical lookahead oracle is an explicit
representable-bound interpretation, not a direct prescription of `Math.nextUp`
in the standard.

## Configuration and identity

- gorti service/SDK source: `cb9d28e7c7c95626ebe78a95d90fd06a01e3d2f8`.
  No production code was changed for these runs. Adapter and generated-test hashes
  are in each `adaptations.json`; diffs show every original-test change.
- Server: Go 1.26.5, Windows/amd64, CGO disabled, no `callback_async` tag.
  Build command: `go build -buildvcs=false -o <rtid.exe> ./rti/cmd/rtid`.
  VCS stamping was disabled after the toolchain could not read worktree VCS state;
  the source commit and binary SHA-256 are recorded independently.
- Java SDK: `mvn -f javasdk/pom.xml -Dmaven.test.skip=true install`, then
  `dependency:build-classpath` for the client. The retained JAR hashes identify
  the actual classpath, including dependencies; no earlier dirty SDK build is used.
- Portico JAR SHA-256:
  `146dc0a7916b168396c6e595c42aaed0bdc69d64c5c54e728b7cb53fdd94ea84`.
- Windows 11, Temurin 11.0.31+11, JUnit Platform 1.11.4, loopback TCP.
  Portico logging OFF, response timeout 5,000 ms; gorti error-level logging.
- One JVM per class, two sessions for exchange/time cases and up to three for
  the federation duplicate-name case. gorti has a separate server process.
  The control checks 4 payload bytes and a 4-byte tag. These are not timed trials.

## Interpretation

The transfer demonstrates that the selected test bodies compile against a second
RTI without gorti implementation imports and expose differences at specific calls
or callbacks. The successful control establishes basic exchange under the adapter.
It does **not** demonstrate that Portico passed the five retained regressions,
that all inventory cases are portable, or that gorti is a fully conformant RTI.
Later assertions in a failed method need separate cases before their coverage
can be compared. A separate-process reproduction and clause-by-clause adjudication
are follow-up work, not completed results of this run.
