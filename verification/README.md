# Pitch versus gorti verification

This suite runs equivalent two-federate IEEE 1516-2010 workloads against
Pitch pRTI and gorti.

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File verification\run_all.ps1 `
  -Seed 1516 -Count 100 -MaxIterations 3
```

The workload covers:

- **FM:** create, join, two synchronization barriers, resign, and destroy;
- **DM:** object/attribute and interaction publication/subscription;
- **OM:** name reservation, named registration/discovery, timestamped updates
  and interactions, timestamped deletion/removal;
- **TM:** regulation and constrained mode on both federates, paired TAR calls,
  exact grants, and TSO callback logical times.

Every payload is the first 16 lowercase SHA-256 hex characters of
`seed:channel:index`, where the channels are `attribute` and `interaction`.

Each implementation retains a detailed raw semantic log and a performance
log. `project_semantics.py` emits the identical four-record cross-runtime log
only after validating all required calls, callbacks, payloads, synchronization
points, and logical times. Performance values are reported separately and do
not decide semantic pass/fail.

The bounded Ralph runner records PLAN, DO, REVIEW, and REFLECT artifacts for
every iteration. It retries only within `-MaxIterations`; configuration errors
stop immediately.

Pitch service use is black-box evidence from successful standard API returns
and callbacks. gorti additionally retains server event logs under each run
directory. The Python ambassador does not expose Pitch's enable-time callbacks
or full callback order/transport/retraction metadata, so parity is asserted on
the common observable contract, not Java/Python source-level API identity.
