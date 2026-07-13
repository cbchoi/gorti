# Roadmap

## Near-term project work

- Stabilize the v0.9 public API and publish repeatable release artifacts.
- Expand Linux, macOS, and Windows SDK integration coverage.
- Keep conformance fixtures and canonical logs versioned with behavioral
  changes.
- Complete publication metadata and archive a SoftwareX reproduction bundle.

## Future performance track

Further interaction performance work is deliberately separate from the
current release cleanup. The next investigation will evaluate:

- a bounded asynchronous interaction API with backpressure;
- ordered completion handles and explicit failure reporting;
- a dedicated lighter transport only if it preserves the strict API boundary;
- equivalent asynchronous Pitch and gorti choreography; and
- promotion to claim-grade 5+20 AB/BA measurement only after a smaller screen
  shows a stable improvement without tail or semantic regression.

The synchronous API will not return success before the existing server ACK.
TSO-before-grant ordering, atomic recipient reservation, federation-generation
fencing, complete teardown, and delivery accounting remain non-negotiable.

## Out of scope for the current release

- Formal IVCT certification.
- A Java federate SDK.
- HLA 1.3 and IEEE 1516-2000 compatibility.
- Production support for clustered or failover RTI deployment.
