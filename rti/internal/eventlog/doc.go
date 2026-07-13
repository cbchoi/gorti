// Package eventlog implements the binary event-log file format that
// underpins replay determinism. The log format is defined in
// proto/rti/v1/eventlog.proto and docs/idd.md §1.6.
//
// Owner: Agent A. Stubs in this package are part of the M2 contract;
// the public API surface (Writer, Reader, Replayer, Format constants,
// constructors) is FROZEN-SHAPE.
//
// # File layout
//
//	+----------------+----------------+----------------+
//	| MAGIC (8 B)    | length-prefixed Event records ...
//	|  KDRTI\0\1\0   | uint32-length + Protobuf bytes
//	+----------------+----------------+----------------+
//
// Magic identifies the file kind and version. The version field in the
// header (separate uint32) governs structural evolution; magic is
// purely a sentinel.
//
// # Test seams
//
// Writer takes its underlying io.Writer through Options so tests can pass
// a bytes.Buffer instead of a real file:
//
//	w, _ := eventlog.NewWriter(eventlog.WriterOptions{
//	    Sink: &bytes.Buffer{},
//	    Federation: "demo",
//	    Mode: core.ModeVerbose,
//	    Seed: 42,
//	    Generation: 1,
//	    Clock: core.NewFakeClock(time.Unix(0, 0)),
//	})
//
// Reader takes io.Reader; replay tests construct Reader directly off the
// Writer's buffer to verify byte-identical round-trips without disk I/O.
//
// # Determinism
//
// Sequence numbers are gapless and monotonic per federation. Writer assigns
// them at Append time. Version-2 headers identify the federation generation;
// version-1 CreatedAtNs and per-event wall_ns fields remain informational.
// Replay byte-equality compares the seq + body, not wall-clock stamps. See
// spec test tests/spec/M2/replay_test.go for the contract.
package eventlog
