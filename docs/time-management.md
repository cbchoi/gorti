# Time management

## Logical time and lookahead

A time-regulating federate promises not to send a timestamp-order message
earlier than its current logical time plus lookahead. For example, at logical
time 3 with lookahead 0.5, the earliest valid timestamp is 3.5. A timestamp of
3 is rejected as an invalid logical time.

Time-constrained federates receive timestamp-order callbacks in logical-time
order. gorti buffers those callbacks and emits them before the grant that makes
the timestamp reachable.

## Why one federate can block another

A time-advance request is evaluated against all time-regulating members. In
the quickstart, the waiter requests time 5 while the peer remains at time 0
with lookahead 2. The peer's state constrains the federation lower bound, so
the waiter cannot receive a grant. When the peer also requests time 5, the
lower bound advances and both requests become grantable.

This is the expected HLA behavior and is why a two-federate example is needed
to verify waiting rather than a locally immediate grant.

## Example sequence

```text
waiter                         peer
  | join, regulate, constrain    | join, regulate, constrain
  | <--------- ready ----------- |
  | TAR(5)                       | time 0, sleep 3 seconds
  | ... pending ...              |
  |                              | TAR(5)
  | <------ TAG(5) ------------- |
  |                    TAG(5) -> |
```

Run the sequence using the [quickstart](quickstart.md). The example exits after
the grant so automated checks can distinguish success from a hung federation.

## Safety properties

The time-management tests enforce:

- one atomic reservation for timestamped recipients;
- timestamp-order delivery before a grant;
- withholding a grant when delivery fails;
- generation checks that prevent callbacks from an old federation instance;
- teardown that drains or cancels pending streams without duplicate grants.
