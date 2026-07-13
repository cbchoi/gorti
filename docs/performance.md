# Performance

## What is measured

The comparison harness reports caller latency for `sendInteraction` and
`updateAttributeValues`, completed delivery latency to the subscriber, object
attribute synchronization time, and interaction/object throughput. Semantic
and accounting checks gate every performance sample.

## Current result

The retained claim-grade session used five warmup and twenty measured AB/BA
pairs with identical two-process choreography. Its paired median ratio for
synchronous `sendInteraction` was 1.218, meaning gorti remained about 22%
behind Pitch on that specific caller boundary. gorti was faster on the
completed-delivery boundary and competitive on object updates.

These values are machine- and workload-specific. They should not be read as a
general vendor ranking. See `probe_result.md` for attribution details and the
archived `analysis.json` for the complete statistics.

## Why synchronous interaction is the remaining gap

Microbenchmarks place registry work and file event-log append at only a few
microseconds. Most synchronous caller latency comes from the persistent gRPC
request/ACK round trip and scheduler interaction with callbacks and time
advance traffic. Go execution speed alone does not remove that transport and
return-boundary cost.

## Future optimization

The strict synchronous API remains unchanged because its return boundary
provides exact error and ACK semantics. A future, separate bounded asynchronous
interaction API may pipeline requests and expose ordered completion handles.
It must be compared only with an equivalent asynchronous Pitch choreography.

Runtime tuning, shared gRPC write buffers, and experimental prepared ACK frames
were screened and rejected because they did not produce a stable gain. The
decision record is maintained in `improvement.md` and summarized in the
[roadmap](roadmap.md).
