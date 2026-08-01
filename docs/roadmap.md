# Roadmap

## Near-term project work

- Stabilize the v0.9 public API and publish repeatable release artifacts.
- Expand Linux, macOS, and Windows SDK integration coverage.
- Keep conformance fixtures and canonical logs versioned with behavioral
  changes.
- Complete publication metadata and archive a reproducibility bundle.

## Future performance track

LocalLRC already provides bounded local admission, a pipelined stream, and
cumulative ACKs for Go receive-order traffic; Python also has a bounded
pipelined extension. Further performance work is tracked separately from the
v0.9 release. A five-warm-up, 30-pair receive-order campaign completed with
exact semantic evidence. Its central estimates favored Portico, while the
predeclared practical-superiority decision was inconclusive. The next
investigation will evaluate:

- a consistent public asynchronous API across SDKs;
- ordered completion handles and explicit server-failure reporting;
- a dedicated lighter transport only if it preserves the strict API boundary;
- equivalent asynchronous reference RTI and gorti choreography;
- server-to-subscriber callback serialization and stream scheduling;
- allocation-aware strict journal encoding that preserves the current
  write-ahead error boundary;
- real per-federation journal-sequence metrics and profile status;
- a paired multi-process Time Management workload; and
- repetition of the 5+30 AB/BA campaign from a clean release commit.

The synchronous API will continue to return only after the server ACK.
Any alternative must preserve TSO-before-grant ordering, atomic recipient
reservation, federation-generation fencing, teardown, and delivery accounting.

## Out of scope for the current release

- Formal IVCT certification.
- A Java federate SDK.
- HLA 1.3 and IEEE 1516-2000 compatibility.
- Production support for clustered or failover RTI deployment.
