// Package auditreplay provides the optional binary audit journal and replay
// tooling for rtid. It is not part of the HLA core execution profile.
package auditreplay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
	"github.com/cbchoi/gorti/rti/internal/runtimeplugin"
)

const Name = "audit-replay"

// Config selects persistent journal behavior.
type Config struct {
	Dir               string
	AllowPartialProto bool
}

// Status summarizes runtime recording health.
type Status struct {
	ErrorCount uint64
	LastError  error
}

// Plugin records service events using the versioned binary event-log format.
type Plugin struct {
	writer *eventlog.MultiplexWriter
	logger *slog.Logger

	mu     sync.Mutex
	status Status
}

// NewFactory returns a runtime plugin factory for cfg.
func NewFactory(cfg Config) runtimeplugin.Factory {
	return func(host runtimeplugin.Host) (runtimeplugin.Plugin, error) {
		return New(cfg, host)
	}
}

// New constructs the audit/replay plugin.
func New(cfg Config, host runtimeplugin.Host) (*Plugin, error) {
	if cfg.Dir == "" {
		return nil, errors.New("auditreplay: Config.Dir is required")
	}
	if host.Clock == nil {
		return nil, errors.New("auditreplay: Host.Clock is required")
	}
	if host.Logger == nil {
		host.Logger = slog.Default()
	}
	writer, err := eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
		Clock:             host.Clock,
		AllowPartialProto: cfg.AllowPartialProto,
		Mode:              core.ModeVerbose,
		Metadata: eventlog.MetadataResolver(func(fed core.FederationName) (uint64, core.Mode, uint64, bool) {
			if host.Metadata == nil {
				return 0, core.ModeUnspecified, 0, false
			}
			return host.Metadata(fed)
		}),
		Dir: cfg.Dir,
	})
	if err != nil {
		return nil, fmt.Errorf("auditreplay: open writer: %w", err)
	}
	return &Plugin{writer: writer, logger: host.Logger}, nil
}

func (*Plugin) Name() string { return Name }

// EventLog returns a non-interfering observer for HLA service managers.
func (p *Plugin) EventLog() core.EventLog { return observer{plugin: p} }

// AdminEventLog returns the raw diagnostics surface. Explicit admin and replay
// operations receive storage errors rather than having them hidden.
func (p *Plugin) AdminEventLog() core.EventLog { return p.writer }

func (p *Plugin) CloseFederation(fed core.FederationName) error {
	return p.writer.CloseFederation(fed)
}

func (p *Plugin) Close() error { return p.writer.Close() }

// Status returns a snapshot of recording failures observed by the plugin.
func (p *Plugin) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *Plugin) recordFailure(operation string, fed core.FederationName, err error) {
	if err == nil {
		return
	}
	p.mu.Lock()
	p.status.ErrorCount++
	p.status.LastError = err
	p.mu.Unlock()
	p.logger.Warn("audit/replay plugin operation failed",
		"operation", operation,
		"federation", fed,
		"err", err,
	)
}

type observer struct {
	plugin *Plugin
}

func (o observer) Append(ctx context.Context, fed core.FederationName, event core.EventRecord) error {
	err := o.plugin.writer.Append(ctx, fed, event)
	o.plugin.recordFailure("append", fed, err)
	return nil
}

func (o observer) Sync(ctx context.Context, fed core.FederationName) error {
	err := o.plugin.writer.Sync(ctx, fed)
	o.plugin.recordFailure("sync", fed, err)
	return nil
}

func (o observer) OpenReader(ctx context.Context, path string) (core.EventLogReader, error) {
	return o.plugin.writer.OpenReader(ctx, path)
}
