# Requirement-derived test cases

The test inventory links reviewed IEEE 1516-2010 requirements to named tests
and execution reports. Each case specifies its input conditions and expected
result.

## From a requirement to an observation

A record in the
[requirements inventory](https://github.com/cbchoi/gorti/tree/main/engineering/verification/requirements2010)
contains the standard and clause, applicability, implementation paths, test
targets, and an oracle. The oracle states what should be observed for a given
input: acceptance, rejection, callback ordering, state changes, or recovery.

```text
reviewed requirement
  -> preconditions and boundary inputs
  -> expected observable result (oracle)
  -> named executable test
  -> execution report and source identity
```

A requirement can link to several tests, and a test can check several
requirements.

## Worked example: positive lookahead at a Float64 boundary

Record `TM-LOOKAHEAD-SEND-FLOOR` refers to IEEE 1516.1-2010 clause 8.1.4.
The selected case checks that a positive lookahead remains visible when
floating-point addition would round back to the current logical time.

The implementation is
[`LiveTimeArithmeticTest.sendGaltAndRetractionUseTheSameRepresentableBound`](https://github.com/cbchoi/gorti/blob/main/javasdk/client/src/test/java/org/gorti/rti/LiveTimeArithmeticTest.java).

| Step | Test design |
|---|---|
| Arrange | Create a federation and two RTI sessions; publish and subscribe to `Pulse`. |
| Establish state | Enable time regulation, dispatch its callback, request the chosen current time, and wait for its grant. |
| Boundary inputs | Current time 1 with `Double.MIN_VALUE` lookahead; current time 2^53 with lookahead 1. |
| Oracle | Compute the next representable value with `Math.nextUp(current)`, independently of the RTI implementation. |
| Query | The peer's GALT is valid and equals the expected bound. |
| Reject | Sending at the current time throws `InvalidLogicalTime`. |
| Accept | Sending at the expected bound returns a valid, non-null retraction handle. |
| Clean up | Resign both sessions, destroy the federation, and disconnect. |

The test uses two RTI sessions in one Java process connected to a live server.
Both parameterized invocations passed in the retained Linux campaign. This
case checks Float64 query and send outcomes; it does not check delivery of the
interaction callback.

## Other observable behaviors

| Area | Example assertion | Named case |
|---|---|---|
| Model interoperability | Java/Python peers preserve opaque bytes, tags, producer identity, order, and evoked dispatch | `JavaLanguageTransparency20261004Test` |
| Timed save/restore | Save does not start early, pending advancement is retained, and restore resumes the saved advance | `LiveSaveRestoreTest.timedSaveDefersGrantsAndRestoreResumesTheSavedAdvance` |
| Object deletion | Reject an unknown or unowned object without mutation; allow the later valid owner operation | `test_delete_unknown_precedes_privilege_and_preserves_object` |
| DDM lifecycle | Reject deletion of an associated region; permit it after disassociation; reject later region queries | `LiveDdmTest.regionsRouteByAttributePairsAndCommittedBoundsAcrossThreeSessions` |

Paths, clause identifiers, and the complete assertions are listed in the
[technical test-case guide](https://github.com/cbchoi/gorti/blob/main/engineering/verification/ieee1516-2010-test-case-guide.md).

## Evidence classes

The
[classification manifest](https://github.com/cbchoi/gorti/blob/main/engineering/verification/requirements2010-evidence-classes-20261023.json)
assigns a primary class, rationale, and source/oracle hashes to each named target.

- **Service/SDK behavior:** controlled unit or SDK tests and live service cases.
- **API/structure:** interface shape, encodings, time/handle helpers, and
  FOM/MIM/XML parsing.
- **Documentation/reporting:** checks of the report tool, separate from RTI
  service tests.

The retained inventory has 464 distinct targets and 877 requirement-test links.
One reporting guard is counted separately from 388 behavioral and 75
API/structure targets. [Verification results](verification.md) report passing,
skipped, not-applicable, and unresolved records separately.

## Extend the collection

1. Identify the requirement and state any applicability conditions or unresolved
   interpretation.
2. Describe input state and the observation that would distinguish correct from
   incorrect behavior. Include negative cases and state preservation after
   rejection where relevant.
3. Implement a named test through the appropriate service or SDK interface.
   Avoid using the implementation under test to compute the expected result.
4. Link the case and oracle to the requirement, and classify its evidence.
5. Record the source, selection, environment, result, and skips. Rerun affected
   cases when their sources change.

Use [reproducibility](reproducibility.md) for result-table generation and
[verification](verification.md) for the latest retained service campaign.
The [STD](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/STD.md)
provides the broader test design.
