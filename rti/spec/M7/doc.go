// Package m7spec contains the orchestrator-frozen specification tests for
// milestone M7 — Complete time-advance primitives (TAR + TARA + FQR + NMRA).
// See docs/srs.md §10.3 (cut 2) for the milestone gate.
//
// M3 (cut 1) implemented NER (NextMessageRequest). M7 (cut 2) adds the
// other three primitives from IEEE 1516.1-2010 §8:
//
//   - NextMessageRequestAvailable (NMRA)  — §8.12
//   - TimeAdvanceRequest          (TAR)   — §8.10
//   - TimeAdvanceRequestAvailable (TARA)  — §8.11
//   - FlushQueueRequest           (FQR)   — §8.13
//
// All four primitives MUST share the LBTS computation + grant-emission
// machinery from M3. The semantic differences (Available variants
// permit grants AT LBTS; FQR drains the TSO queue without strict
// advance) are exercised by the per-primitive tests.
//
// Per docs/TDD.md §5, these tests are committed RED before the
// milestone is dispatched. Agent A turns them green incrementally per
// the M7 wave model (TBD; see CHANGELOG-MASTERPLAN entry once dispatched).
//
// Agents may ADD tests here but must NEVER weaken or delete existing
// assertions.
package m7spec
