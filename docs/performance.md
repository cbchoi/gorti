# Performance

## Measurements

The comparison harness reports caller latency for `sendInteraction` and
`updateAttributeValues`, completed delivery latency to the subscriber, object
attribute synchronization time, and interaction/object throughput. Semantic
and accounting checks gate every performance sample.

The publication benchmark uses the [DEVStone-HLA profile](benchmark.md). Its
results describe the complete tested stacks and deployment topologies. They
must not be reported as a standard DEVStone simulator-engine score.

## Current Portico comparison

The July 23, 2026 DEVStone-HLA run used five warmup and thirty measured AB/BA
pairs, two independent federate processes, identical FOM and workload-plan
bytes, and a common subscriber callback-arrival boundary. For the tested
receive-order OM profile, Portico 2.1.4 completed the 81,200-callback batch in a
median 0.696 s (95% CI 0.664--0.734 s) and gorti LocalLRC in 0.808 s
(95% CI 0.791--0.820 s). Median throughput was 116,606 deliveries/s for Portico
and 100,552 deliveries/s for gorti.

The paired median gorti/Portico completion-time ratio was 1.167 (deterministic
bootstrap 95% CI 1.097--1.194); the order-adjusted ratio was 1.161 (95% CI
1.076--1.200). The central estimates favor Portico, and Portico was faster in
29 of 30 pairs. The predeclared decision rule nevertheless classifies the
result as inconclusive because the paired confidence interval crosses the 1.1
practical-superiority boundary. All 60 measured runs completed on their first
attempt with exact callback accounting and matching callback and terminal-state
digests.

The result is bounded to the same-host, two-federate DEVStone-HLA OM workload.
It does not rank TM, DDM, larger federations, resource efficiency, or caller API
latency. The LocalLRC queue-admission boundary is intentionally excluded from
the published comparison because it is not equivalent to Portico's service
return boundary.

This run supersedes the July 22 comparison. The verifier and callback transport
changed during the intervening performance milestones, so the two campaigns
should not be treated as an uninterrupted timing series or used to attribute
the complete difference to one code change.

## Separate reference RTI result

The five-warmup, twenty-pair v14 result in the
[verification summary](https://github.com/cbchoi/gorti/blob/main/verification/RESULTS.md)
compares gorti with an anonymized commercial reference RTI, not Portico. It also
uses a smaller service-level workload and different lifecycle controls. Its
favorable completed-delivery result for gorti is valid only for that contract
and cannot be used to infer gorti/Portico performance.

## Synchronous interaction latency

gorti uses Go's structured `slog` package for human-readable diagnostic
records. The optional audit/replay plugin is a separate mechanism: it
constructs semantic records, copies payloads, assigns sequence numbers,
encodes Protobuf, and writes through a per-federation sink.

The default `gorti-hla-core` profile does not load this plugin. Local registry
screens show materially fewer allocations for receive-order updates and
interactions on the core path. Plugin-enabled results are therefore reported
as `gorti-audit-replay` and are not mixed with the core benchmark profile.

Most remaining synchronous caller latency comes from the persistent gRPC
request/ACK round trip and scheduler interaction with callbacks and time
advance traffic. Go execution speed alone does not remove that transport and
return-boundary cost.

## Future optimization

The strict synchronous API remains unchanged because its return boundary
provides exact error and ACK semantics. Go LocalLRC and the Python pipelined
extension already provide bounded asynchronous admission for selected
receive-order paths. Future work is to expose consistent ordered completion
handles and server-failure reporting across SDKs. It must be compared only
with an equivalent asynchronous reference RTI choreography.

Runtime tuning, shared gRPC write buffers, and experimental prepared ACK frames
were screened and rejected because they did not produce a stable gain. The
remaining improvement directions are summarized in the [roadmap](roadmap.md).
