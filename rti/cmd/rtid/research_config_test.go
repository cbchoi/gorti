package main

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/research"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// testFakeLBTS is a minimal LBTSStrategy used only by the strict-gate
// integration test below. Lives in _test.go so it never touches
// production code.
type testFakeLBTS struct{}

func (testFakeLBTS) LBTS(_ []timepkg.RegulatingFederate) core.LogicalTime { return 0 }
func (testFakeLBTS) Name() string                                         { return "fake-nondet" }
func (testFakeLBTS) DeterminismPreserving() bool                          { return false }

// TestResolveResearchConfig_AbsentReturnsDefaults: with both flag and
// env unset, resolveResearchConfig produces the all-defaults bundle
// and behavior is identical to today's hand-wired runtime.
func TestResolveResearchConfig_AbsentReturnsDefaults(t *testing.T) {
	t.Setenv("GORTI_RESEARCH_CONFIG", "")
	t.Setenv("GORTI_DETERMINISM", "")

	r, err := resolveResearchConfig("", "", "")
	if err != nil {
		t.Fatalf("resolveResearchConfig: unexpected err %v", err)
	}
	if r.Time.LBTS == nil || r.Time.LBTS.Name() != "default" {
		t.Errorf("Time.LBTS: want default, got %v", r.Time.LBTS)
	}
	if r.Time.Grant == nil || r.Time.Grant.Name() != "default" {
		t.Errorf("Time.Grant: want default, got %v", r.Time.Grant)
	}
	if r.Ownership.Negotiation == nil || r.Ownership.Negotiation.Name() != "default" {
		t.Errorf("Ownership.Negotiation: want default, got %v", r.Ownership.Negotiation)
	}
	if r.Determinism != research.DeterminismPerImplOptIn {
		t.Errorf("Determinism: want per-impl-opt-in, got %v", r.Determinism)
	}
}

// TestResolveResearchConfig_FlagPathReadsFile: --research-config wins
// over the env var fallback when both are set.
func TestResolveResearchConfig_FlagPathReadsFile(t *testing.T) {
	dir := t.TempDir()
	flagPath := filepath.Join(dir, "flag.toml")
	envPath := filepath.Join(dir, "env.toml")
	if err := os.WriteFile(flagPath, []byte(`determinism = "off"`), 0o600); err != nil {
		t.Fatalf("WriteFile flagPath: %v", err)
	}
	if err := os.WriteFile(envPath, []byte(`determinism = "strict"`), 0o600); err != nil {
		t.Fatalf("WriteFile envPath: %v", err)
	}
	r, err := resolveResearchConfig(flagPath, envPath, "")
	if err != nil {
		t.Fatalf("resolveResearchConfig: %v", err)
	}
	if r.Determinism != research.DeterminismOff {
		t.Errorf("Determinism: want off (flag wins), got %v", r.Determinism)
	}
}

// TestResolveResearchConfig_EnvPathFallback: when --research-config is
// empty, GORTI_RESEARCH_CONFIG is consulted.
func TestResolveResearchConfig_EnvPathFallback(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.toml")
	if err := os.WriteFile(envPath, []byte(`determinism = "strict"`), 0o600); err != nil {
		t.Fatalf("WriteFile envPath: %v", err)
	}
	r, err := resolveResearchConfig("", envPath, "")
	if err != nil {
		t.Fatalf("resolveResearchConfig: %v", err)
	}
	if r.Determinism != research.DeterminismStrict {
		t.Errorf("Determinism: want strict (env fallback), got %v", r.Determinism)
	}
}

// TestResolveResearchConfig_DeterminismOverride: GORTI_DETERMINISM
// overrides whatever the TOML file said.
func TestResolveResearchConfig_DeterminismOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "research.toml")
	if err := os.WriteFile(path, []byte(`determinism = "strict"`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r, err := resolveResearchConfig(path, "", "off")
	if err != nil {
		t.Fatalf("resolveResearchConfig: %v", err)
	}
	if r.Determinism != research.DeterminismOff {
		t.Errorf("Determinism: want off (env override), got %v", r.Determinism)
	}
}

// TestResolveResearchConfig_BadOverride: an unknown determinism
// override surfaces a clear error so a typo halts startup.
func TestResolveResearchConfig_BadOverride(t *testing.T) {
	_, err := resolveResearchConfig("", "", "yolo")
	if err == nil {
		t.Fatalf("resolveResearchConfig(yolo): want error, got nil")
	}
	if !strings.Contains(err.Error(), "GORTI_DETERMINISM") {
		t.Errorf("err: should name GORTI_DETERMINISM, got %v", err)
	}
}

// TestResolveResearchConfig_BadFile: a missing config file surfaces a
// clear error so the operator notices typos before the rtid serves
// any RPC.
func TestResolveResearchConfig_BadFile(t *testing.T) {
	_, err := resolveResearchConfig("/nonexistent/path.toml", "", "")
	if err == nil {
		t.Fatalf("resolveResearchConfig(missing file): want error, got nil")
	}
}

// TestNewRTID_WithDefaultsResearchConfig: rtid boots cleanly when the
// research config (an empty TOML) resolves to all-defaults — the
// resulting runtime is identical to the no-flag path.
func TestNewRTID_WithDefaultsResearchConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "research.toml")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	resolved, err := resolveResearchConfig(path, "", "")
	if err != nil {
		t.Fatalf("resolveResearchConfig: %v", err)
	}
	srv, err := newRTID(rtidConfig{
		ListenAddr:        ":0",
		MetricsListenAddr: ":0",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		Research:          resolved,
	})
	if err != nil {
		t.Fatalf("newRTID with defaults research config: %v", err)
	}
	if srv.fedMgr == nil || srv.ownMgr == nil {
		t.Errorf("newRTID: incompletely wired runtime: %+v", srv)
	}
}

// TestResolveResearchConfig_StrictRejectsNonPreserving: the strict
// gate fires on any non-preserving impl. Exercised here at the
// research package level (the cmd/rtid path can't register fakes
// without dragging test-only types into production code).
func TestResolveResearchConfig_StrictRejectsNonPreserving(t *testing.T) {
	cfg := research.DefaultConfig()
	cfg.Determinism = research.DeterminismStrict
	cfg.Time.LBTS = "fake-nondet"

	reg := research.Default()
	if err := reg.RegisterLBTS("fake-nondet", testFakeLBTS{}); err != nil {
		t.Fatalf("RegisterLBTS: %v", err)
	}
	_, err := research.Apply(cfg, reg)
	if err == nil {
		t.Fatalf("Apply(strict + non-preserving): want error, got nil")
	}
	var npe *research.NonPreservingError
	if !errors.As(err, &npe) {
		t.Fatalf("err: want *research.NonPreservingError, got %T: %v", err, err)
	}
	if npe.Category != research.CategoryTimeLBTS {
		t.Errorf("Category: want %s, got %s", research.CategoryTimeLBTS, npe.Category)
	}
}
