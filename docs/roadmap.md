# Roadmap

## Service verification

- Resolve the 25 open interpretation records in the requirements inventory.
- Review clause coverage and obtain independent service and language-mapping
  assessment.
- Run the remaining external API archive, FDD-schema, and cross-binding cases.
- Extend platform coverage beyond the retained Linux campaign.

Current results are in [verification](verification.md).

## Performance

- Evaluate a sliding callback-report window.
- Measure long-lived participants across separate sessions.
- Add matched multi-host, larger-federation, and time-management workloads.
- Reduce callback serialization and scheduling costs while preserving ordering,
  error reporting, and checkpoint behavior.
- Evaluate asynchronous completion interfaces across SDKs.
- Measure audit/replay overhead separately from the core profile.

Compare each change with the previous build under the same workload and run
the affected service tests. Existing measurements are in
[performance](performance.md).

## Experimental algorithms

Cluster/failover deployment and alternative time/ownership algorithms remain
experimental. The supported deployment uses one authoritative server.
