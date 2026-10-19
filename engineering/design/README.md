# Design Records

The current architecture is defined by the
[Software Design Description](../specifications/current/SDD.md). Historical
design changes are summarized in the versioned specification baselines.

Temporary implementation notes and abandoned proposals are intentionally not
retained as product design documents. Source history and issue discussions
remain the appropriate record for those details.

Implemented structural changes are recorded in the
[HLA/RTI refactoring report](hla-rti-refactoring-report.md).

The [consistent-performance improvement plan](consistent-performance-plan-20261007.md)
uses current profiles and cross-RTI observations to prioritize callback ownership,
batch ledger work, FOM/routing indexes and SDK reporting. The comparison-copy
optimization has a measured gain. Delivery-ledger batching lowers allocations,
but its consistent-throughput acceptance remains open. Outstanding milestones
and engineering targets are distinguished from completed work.

The proposed [IEEE 1516-2010 feature completion plan](ieee1516-2010-feature-completion-plan.md)
defines the remaining implementation milestones, dependencies and release gates.
Its completion criteria distinguish product functionality from whole-standard
conformance evidence; the plan itself closes no implementation requirement.

The current [conformance execution plan](ieee1516-2010-conformance-execution-plan.md)
continues from the Python-first corrections with ordered work packages,
parallel ownership, TDD acceptance cases and release-specific evidence criteria.
It supersedes the older batch sequence, not the standard's completion obligations.
