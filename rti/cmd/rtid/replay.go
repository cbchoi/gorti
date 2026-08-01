package main

import (
	"context"

	"github.com/cbchoi/gorti/rti/plugins/auditreplay"
)

// runReplayFromFile preserves the command's test seam while delegating replay
// implementation to the optional audit/replay module.
func runReplayFromFile(ctx context.Context, inputPath, outputDir string) error {
	return auditreplay.ReplayFile(ctx, inputPath, outputDir)
}
