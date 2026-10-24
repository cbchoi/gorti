# Development and educational use

These exercises connect the service modules, SDKs, examples, and tests to the
project's design documents.

## Read the design with the code

| Document | Use it to answer |
|---|---|
| [SRS](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/SRS.md) | What behavior and constraints does the software require? |
| [SDD](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/SDD.md) | Which module owns a state transition, algorithm, or invariant? |
| [IDD](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/IDD.md) | Which service, wire message, or callback crosses a boundary? |
| [STD](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/STD.md) | Which input and observation test that behavior? |

Current specifications live under `engineering/specifications/current`;
[historical versions](https://github.com/cbchoi/gorti/tree/main/engineering/specifications/history)
preserve earlier baselines. Use the version associated with the source being
studied.

## Module responsibilities

| Location | Responsibility | Suitable study or change |
|---|---|---|
| `rti/internal/hla/` | Federation state and service behavior | Time bounds, ownership transitions, declaration and DDM rules |
| `rti/internal/application/` | Configuration, composition, and lifecycle | Select and connect implementations for a server process |
| `rti/internal/adapters/grpc/` and `protobuf/` | Requests, wire conversion, callback transport | Follow an SDK call to a domain operation |
| `rti/internal/adapters/delivery/` | Reservations, outboxes, batching, and backpressure | Follow accepted callbacks into transport queues |
| `rti/internal/adapters/savebundle/` and `sdkstate/` | Persisted federation and SDK state | Study recovery separately from model-state persistence |
| `rti/pkg/federate/`, `pysdk/`, `javasdk/` | Language interfaces and dispatch | Compare the same interaction in different languages |
| `rti/experiments/algorithms/` | Experimental strategies | Add an alternative implementation for an explicit experiment |

Dependencies point toward the HLA service layer: service code does not import
gRPC or generated Protobuf types to implement its decisions.
See [architecture](architecture.md) for module boundaries and dependency checks.

## Exercise 1: explain a delayed time grant

Run the [two-federate quickstart](quickstart.md). Identify the time requested
by the waiter, the peer's regulating bound, and the point at which the peer
allows progress. Change the peer delay and check that wall-clock waiting
changes while the requested logical time remains 5.

Deliverable: a sequence diagram of requests and grants, with logical time
distinguished from elapsed time.

## Exercise 2: reuse a model across languages

Run the [Python-Go queue](model-integration.md), then change a service-duration
profile. Compare the source and sink transition functions before and after
integration, and locate the adapter code that performs the HLA mapping.

Explain why duration 2 grows the queue, how lookahead constrains sends, and
why the one-unit transfer delay is a model assumption.

Deliverable: a FOM mapping table and observed/reference job trajectories.

## Exercise 3: derive a boundary test

Follow the [lookahead test-case example](test-cases.md). Write down the
preconditions, the invalid timestamp, the accepted timestamp, and the expected
exception or return value before reading the assertions.

Add an input near another representation boundary. Distinguish a requirement
interpretation from a bug in its implementation, and keep the test's oracle
independent of the function being tested.

Deliverable: a requirement record, named test, and recorded result.

## Exercise 4: change one service algorithm

Choose a strategy in the responsible HLA service. Preserve the public interface
and state which service invariants the alternative must retain. Use application
composition to select the implementation; keep experimental strategies outside
the default service path.

Run the relevant package and architecture checks from the repository root.
For a time-management change, a starting selection is:

```text
go test ./rti/internal/hla/time
go test ./tests/architecture
```

Then select affected SDK and live contract cases using the
[STD](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/STD.md)
and [verification guide](verification.md).

Deliverable: the algorithm change, its invariant tests, and a before/after
experiment with the same workload and measurement boundary.

## Add an example

Keep model code, federate adaptation, and result validation distinguishable.
Provide a FOM, prerequisites, Windows and Linux entry points, expected results,
and cleanup that stops only processes started by the example. Store source,
configuration, and maintained summaries in Git; keep raw runs outside it.

See the [example catalog](https://github.com/cbchoi/gorti/blob/main/examples/README.md)
and [contribution guide](https://github.com/cbchoi/gorti/blob/main/CONTRIBUTING.md).
