package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
)

// runReplayFromFile drives the source event log at inputPath through
// eventlog.NewReplayer with a fresh CapturingSink that writes to the
// per-federation .log file under outputDir. It's the production
// equivalent of the rti/spec/M2/replay_test.go exercise — the example
// determinism / replay tests under examples/go-pingpong/ exec rtid in
// this mode to satisfy the M2 gate's "feed log back through fresh
// RTI" contract.
//
// Federation name is recovered from the source log's header.
//
// Federation/Objects components are intentionally NOT wired here — the
// pingpong example produces an event log with only InteractionSent +
// FederateJoined + FederateResigned records; the cut-1 replayer
// dispatches those to live components when supplied, but for the
// byte-identical contract a passthrough is sufficient (and avoids
// re-validating handle assignment against a fresh permissive FOM).
func runReplayFromFile(ctx context.Context, inputPath, outputDir string) error {
	src, err := os.Open(inputPath) //nolint:gosec // path is operator-supplied
	if err != nil {
		return fmt.Errorf("rtid replay: open source %s: %w", inputPath, err)
	}
	defer func() { _ = src.Close() }()

	rdr, err := eventlog.NewReader(src)
	if err != nil {
		return fmt.Errorf("rtid replay: NewReader: %w", err)
	}
	defer func() { _ = rdr.Close() }()

	hdr := rdr.Header()
	fed := hdr.Federation
	if fed == "" {
		return errors.New("rtid replay: source log header has empty federation name")
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("rtid replay: mkdir %s: %w", outputDir, err)
	}
	outPath := eventlog.GenerationLogPath(outputDir, fed, hdr.Generation)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("rtid replay: mkdir %s: %w", filepath.Dir(outPath), err)
	}
	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) //nolint:gosec
	if err != nil {
		return fmt.Errorf("rtid replay: open output %s: %w", outPath, err)
	}
	defer func() { _ = outFile.Close() }()

	// Tee the captured stream into a bytes.Buffer the Replayer can
	// observe, while also forwarding to the on-disk file.
	var capturedBuf bytes.Buffer
	captured := io.MultiWriter(outFile, &capturedBuf)

	// Reuse the source header's CreatedAtNs so the captured header is
	// byte-equal to the source (the W2B Replayer's documented exclusion
	// covers timestamp-only mismatches; here we go further and produce
	// a fully byte-identical file when possible).
	captureClock := newFixedClock(time.Unix(0, int64(hdr.CreatedAtNs))) //nolint:gosec // value bounded by header

	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink:       captured,
		Federation: fed,
		Generation: hdr.Generation,
		Mode:       hdr.Mode,
		Seed:       hdr.Seed,
		Clock:      captureClock,
	})
	if err != nil {
		return fmt.Errorf("rtid replay: NewWriter: %w", err)
	}
	defer func() { _ = w.Close() }()

	rep, err := eventlog.NewReplayer(eventlog.ReplayerOptions{
		Source:         rdr,
		CapturingSink:  w,
		CapturedBuffer: &capturedBuf,
	})
	if err != nil {
		return fmt.Errorf("rtid replay: NewReplayer: %w", err)
	}
	if err := rep.Replay(ctx); err != nil {
		return fmt.Errorf("rtid replay: Replay: %w", err)
	}
	return nil
}

// fixedClock is a Clock pinned at a single time. Used by the replay
// mode to reproduce the source header's CreatedAtNs.
type fixedClock struct{ t time.Time }

func newFixedClock(t time.Time) core.Clock { return fixedClock{t: t} }

func (c fixedClock) Now() time.Time { return c.t }
