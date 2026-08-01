// Package runtimeplugin defines optional observers that can be attached to
// the RTI composition root without changing HLA service results.
package runtimeplugin

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// MetadataResolver returns immutable metadata for one federation execution.
type MetadataResolver func(core.FederationName) (generation uint64, mode core.Mode, seed uint64, ok bool)

// Host contains the RTI services exposed to plugin factories.
type Host struct {
	Clock    core.Clock
	Logger   *slog.Logger
	Metadata MetadataResolver
}

// Plugin is the lifecycle contract for an optional RTI observer.
//
// EventLog is injected into HLA service managers. Its implementation must not
// return observation failures from Append or Sync because a plugin must not
// change the result of an HLA service. AdminEventLog is the explicit
// diagnostics surface and may return plugin errors to its caller.
type Plugin interface {
	Name() string
	EventLog() core.EventLog
	AdminEventLog() core.EventLog
	CloseFederation(core.FederationName) error
	Close() error
}

// Factory creates a plugin after the composition root has established the
// clock and lazy federation metadata resolver.
type Factory func(Host) (Plugin, error)

// Manager owns the configured plugins and their lifecycle.
type Manager struct {
	logger        *slog.Logger
	plugins       []Plugin
	eventLog      core.EventLog
	adminEventLog core.EventLog
}

// Open constructs all configured plugins. At most one plugin may provide the
// event-log hook because event sequence assignment has a single owner.
func Open(factories []Factory, host Host) (*Manager, error) {
	if host.Logger == nil {
		host.Logger = slog.Default()
	}
	m := &Manager{logger: host.Logger}
	for i, factory := range factories {
		if factory == nil {
			_ = m.Close()
			return nil, fmt.Errorf("runtimeplugin: factory %d is nil", i)
		}
		plugin, err := factory(host)
		if err != nil {
			_ = m.Close()
			return nil, fmt.Errorf("runtimeplugin: open factory %d: %w", i, err)
		}
		if plugin == nil {
			_ = m.Close()
			return nil, fmt.Errorf("runtimeplugin: factory %d returned a nil plugin", i)
		}
		if provider := plugin.EventLog(); provider != nil {
			if m.eventLog != nil {
				_ = plugin.Close()
				_ = m.Close()
				return nil, fmt.Errorf("runtimeplugin: plugin %q conflicts with an existing event-log provider", plugin.Name())
			}
			m.eventLog = provider
			m.adminEventLog = plugin.AdminEventLog()
		}
		m.plugins = append(m.plugins, plugin)
		host.Logger.Info("rtid plugin enabled", "plugin", plugin.Name())
	}
	return m, nil
}

// EventLog returns the non-interfering hook injected into HLA services.
func (m *Manager) EventLog() core.EventLog {
	if m == nil {
		return nil
	}
	return m.eventLog
}

// AdminEventLog returns the diagnostics surface exposed to AdminService.
func (m *Manager) AdminEventLog() core.EventLog {
	if m == nil {
		return nil
	}
	return m.adminEventLog
}

// CloseFederation notifies every plugin after core federation teardown.
// Plugin failures are observable in logs but do not change the HLA result.
func (m *Manager) CloseFederation(fed core.FederationName) {
	if m == nil {
		return
	}
	for _, plugin := range m.plugins {
		if err := plugin.CloseFederation(fed); err != nil {
			m.logger.Warn("rtid plugin federation close failed",
				"plugin", plugin.Name(),
				"federation", fed,
				"err", err,
			)
		}
	}
}

// Close releases plugins in reverse construction order.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	var errs []error
	for i := len(m.plugins) - 1; i >= 0; i-- {
		plugin := m.plugins[i]
		if err := plugin.Close(); err != nil {
			m.logger.Warn("rtid plugin close failed", "plugin", plugin.Name(), "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", plugin.Name(), err))
		}
	}
	m.plugins = nil
	m.eventLog = nil
	m.adminEventLog = nil
	return errors.Join(errs...)
}
