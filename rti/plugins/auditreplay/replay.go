package auditreplay

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

// ReplayFile replays one audit journal into a new generation log file.
func ReplayFile(ctx context.Context, inputPath, outputDir string) error {
	src, err := os.Open(inputPath) //nolint:gosec // operator-supplied path
	if err != nil {
		return fmt.Errorf("auditreplay: open source %s: %w", inputPath, err)
	}
	defer func() { _ = src.Close() }()

	reader, err := eventlog.NewReader(src)
	if err != nil {
		return fmt.Errorf("auditreplay: open reader: %w", err)
	}
	defer func() { _ = reader.Close() }()

	header := reader.Header()
	fed := header.Federation
	if fed == "" {
		return errors.New("auditreplay: source log header has empty federation name")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("auditreplay: mkdir %s: %w", outputDir, err)
	}
	outputPath := eventlog.GenerationLogPath(outputDir, fed, header.Generation)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("auditreplay: mkdir %s: %w", filepath.Dir(outputPath), err)
	}
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) //nolint:gosec
	if err != nil {
		return fmt.Errorf("auditreplay: open output %s: %w", outputPath, err)
	}
	defer func() { _ = output.Close() }()

	var capturedBuffer bytes.Buffer
	captured := io.MultiWriter(output, &capturedBuffer)
	writer, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink:       captured,
		Federation: fed,
		Generation: header.Generation,
		Mode:       header.Mode,
		Seed:       header.Seed,
		Clock:      fixedClock{t: time.Unix(0, int64(header.CreatedAtNs))}, //nolint:gosec
	})
	if err != nil {
		return fmt.Errorf("auditreplay: open writer: %w", err)
	}
	defer func() { _ = writer.Close() }()

	replayer, err := eventlog.NewReplayer(eventlog.ReplayerOptions{
		Source:         reader,
		CapturingSink:  writer,
		CapturedBuffer: &capturedBuffer,
	})
	if err != nil {
		return fmt.Errorf("auditreplay: open replayer: %w", err)
	}
	if err := replayer.Replay(ctx); err != nil {
		return fmt.Errorf("auditreplay: replay: %w", err)
	}
	return nil
}

type fixedClock struct {
	t time.Time
}

func (c fixedClock) Now() time.Time { return c.t }

var _ core.Clock = fixedClock{}
