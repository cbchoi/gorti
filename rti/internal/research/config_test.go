package research_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/research"
)

func TestLoadConfigEmptyPathReturnsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := research.LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\"): unexpected err %v", err)
	}
	if cfg.Determinism != research.DeterminismPerImplOptIn {
		t.Errorf("Determinism: want per-impl-opt-in, got %v", cfg.Determinism)
	}
	if cfg.Time.LBTS != "default" || cfg.Time.Grant != "default" {
		t.Errorf("Time: want default/default, got %+v", cfg.Time)
	}
	if cfg.Ownership.Negotiation != "default" {
		t.Errorf("Ownership.Negotiation: want default, got %q", cfg.Ownership.Negotiation)
	}
}

func TestParseConfigEmptyBodyReturnsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := research.ParseConfig([]byte(""))
	if err != nil {
		t.Fatalf("ParseConfig(empty): unexpected err %v", err)
	}
	if cfg.Determinism != research.DeterminismPerImplOptIn {
		t.Errorf("Determinism: want per-impl-opt-in, got %v", cfg.Determinism)
	}
	if cfg.Time.LBTS != "default" {
		t.Errorf("Time.LBTS: want default, got %q", cfg.Time.LBTS)
	}
}

func TestParseConfigRoundtrip(t *testing.T) {
	t.Parallel()

	body := []byte(`
determinism = "strict"

[time]
lbts = "alt-lbts"
grant = "alt-grant"

[ownership]
negotiation = "alt-neg"
`)
	cfg, err := research.ParseConfig(body)
	if err != nil {
		t.Fatalf("ParseConfig: unexpected err %v", err)
	}
	if cfg.Determinism != research.DeterminismStrict {
		t.Errorf("Determinism: want strict, got %v", cfg.Determinism)
	}
	if cfg.Time.LBTS != "alt-lbts" {
		t.Errorf("Time.LBTS: want alt-lbts, got %q", cfg.Time.LBTS)
	}
	if cfg.Time.Grant != "alt-grant" {
		t.Errorf("Time.Grant: want alt-grant, got %q", cfg.Time.Grant)
	}
	if cfg.Ownership.Negotiation != "alt-neg" {
		t.Errorf("Ownership.Negotiation: want alt-neg, got %q", cfg.Ownership.Negotiation)
	}
}

func TestParseConfigPartialFieldsApplyDefaultsForAbsent(t *testing.T) {
	t.Parallel()

	body := []byte(`
[time]
lbts = "alt"
`)
	cfg, err := research.ParseConfig(body)
	if err != nil {
		t.Fatalf("ParseConfig: unexpected err %v", err)
	}
	if cfg.Determinism != research.DeterminismPerImplOptIn {
		t.Errorf("Determinism: want default per-impl-opt-in, got %v", cfg.Determinism)
	}
	if cfg.Time.LBTS != "alt" {
		t.Errorf("Time.LBTS: want alt, got %q", cfg.Time.LBTS)
	}
	if cfg.Time.Grant != "default" {
		t.Errorf("Time.Grant: want default fallback, got %q", cfg.Time.Grant)
	}
	if cfg.Ownership.Negotiation != "default" {
		t.Errorf("Ownership.Negotiation: want default fallback, got %q", cfg.Ownership.Negotiation)
	}
}

func TestParseConfigRejectsUnknownDeterminism(t *testing.T) {
	t.Parallel()

	body := []byte(`determinism = "yolo"`)
	_, err := research.ParseConfig(body)
	if err == nil {
		t.Fatalf("ParseConfig(yolo): want error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown determinism mode") {
		t.Errorf("err: want 'unknown determinism mode', got %v", err)
	}
}

func TestParseConfigRejectsUnknownTimeKey(t *testing.T) {
	t.Parallel()

	body := []byte(`
[time]
lbtss = "default"
`)
	_, err := research.ParseConfig(body)
	if err == nil {
		t.Fatalf("ParseConfig(unknown time key): want error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("err: want 'unknown config key', got %v", err)
	}
}

func TestParseConfigRejectsUnknownOwnershipKey(t *testing.T) {
	t.Parallel()

	body := []byte(`
[ownership]
negotiations = "default"
`)
	_, err := research.ParseConfig(body)
	if err == nil {
		t.Fatalf("ParseConfig(unknown ownership key): want error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("err: want 'unknown config key', got %v", err)
	}
}

func TestParseConfigRejectsUnknownTopLevelKey(t *testing.T) {
	t.Parallel()

	body := []byte(`
ddm = "default"
`)
	_, err := research.ParseConfig(body)
	if err == nil {
		t.Fatalf("ParseConfig(unknown top key): want error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("err: want 'unknown config key', got %v", err)
	}
}

func TestParseConfigInvalidTOMLSyntax(t *testing.T) {
	t.Parallel()

	body := []byte(`determinism = `)
	_, err := research.ParseConfig(body)
	if err == nil {
		t.Fatalf("ParseConfig(bad TOML): want error, got nil")
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "research.toml")
	body := `determinism = "off"
[time]
lbts = "x"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := research.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: unexpected err %v", err)
	}
	if cfg.Determinism != research.DeterminismOff {
		t.Errorf("Determinism: want off, got %v", cfg.Determinism)
	}
	if cfg.Time.LBTS != "x" {
		t.Errorf("Time.LBTS: want x, got %q", cfg.Time.LBTS)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	t.Parallel()

	_, err := research.LoadConfig("/nonexistent/research.toml")
	if err == nil {
		t.Fatalf("LoadConfig(missing): want error, got nil")
	}
}

func TestDeterminismModeString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mode research.DeterminismMode
		want string
	}{
		{research.DeterminismStrict, "strict"},
		{research.DeterminismPerImplOptIn, "per-impl-opt-in"},
		{research.DeterminismOff, "off"},
	}
	for _, tc := range cases {
		if got := tc.mode.String(); got != tc.want {
			t.Errorf("DeterminismMode(%d).String() = %q, want %q", tc.mode, got, tc.want)
		}
	}
}
