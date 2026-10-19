package research

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// DeterminismMode is the runtime determinism contract from
// docs/research-platform.md §3.2 + §8. The default ("per-impl-opt-in")
// is applied when the TOML config omits the top-level "determinism"
// field; "strict" gates rtid startup on every wired impl satisfying
// DeterminismPreserving() == true; "off" disables replay-based
// gating entirely.
type DeterminismMode int

const (
	// DeterminismPerImplOptIn is the default mode: replay tests skip
	// when any wired impl reports DeterminismPreserving() == false,
	// and run normally when all wired impls preserve determinism.
	DeterminismPerImplOptIn DeterminismMode = iota
	// DeterminismStrict makes the rtid composition root reject any
	// non-preserving impl at boot. Replay tests run unchanged.
	DeterminismStrict
	// DeterminismOff disables replay gating entirely. Researchers
	// manage determinism manually.
	DeterminismOff
)

// String returns the canonical TOML token for the mode (the inverse of
// parseDeterminismMode). Used in error messages.
func (m DeterminismMode) String() string {
	switch m {
	case DeterminismStrict:
		return "strict"
	case DeterminismOff:
		return "off"
	case DeterminismPerImplOptIn:
		return "per-impl-opt-in"
	}
	return fmt.Sprintf("DeterminismMode(%d)", int(m))
}

// Config is the parsed research-config. All fields default to the
// "default" strategy / "per-impl-opt-in" determinism when the TOML
// document omits them, so a fully-empty file resolves to behavior
// identical to today's hand-wired runtime.
type Config struct {
	// Determinism is the active determinism mode. Apply consults this
	// when enforcing the strict-mode gate.
	Determinism DeterminismMode

	// Time selects the time-package strategies.
	Time TimeConfig

	// Ownership selects the ownership-package strategies.
	Ownership OwnershipConfig
}

// TimeConfig holds the selected names for the time-package strategies.
// Names are looked up in the registry at Apply-time.
type TimeConfig struct {
	// LBTS is the registry key for the LBTSStrategy slot.
	LBTS string
	// Grant is the registry key for the GrantStrategy slot.
	Grant string
}

// OwnershipConfig holds the selected names for the ownership-package
// strategies.
type OwnershipConfig struct {
	// Negotiation is the registry key for the NegotiationStrategy slot.
	Negotiation string
}

// rawConfig is the on-disk TOML schema. Distinct from Config so the
// public API does not expose toml-tagged fields and so the
// determinism-string → enum conversion happens during validation
// rather than via reflection.
type rawConfig struct {
	Determinism string             `toml:"determinism"`
	Time        rawTimeConfig      `toml:"time"`
	Ownership   rawOwnershipConfig `toml:"ownership"`
}

type rawTimeConfig struct {
	LBTS  string `toml:"lbts"`
	Grant string `toml:"grant"`
}

type rawOwnershipConfig struct {
	Negotiation string `toml:"negotiation"`
}

// DefaultConfig returns a Config populated with the same defaults a
// totally-empty TOML file would produce: "per-impl-opt-in" determinism,
// "default" strategies everywhere.
func DefaultConfig() *Config {
	return &Config{
		Determinism: DeterminismPerImplOptIn,
		Time:        TimeConfig{LBTS: "default", Grant: "default"},
		Ownership:   OwnershipConfig{Negotiation: "default"},
	}
}

// LoadConfig reads + parses a research-config TOML file at path.
//
// Behavior:
//   - path == ""  → returns a fully-defaulted Config (no file read).
//   - file missing/unreadable → returns a wrapped error.
//   - file parse error or unknown field → returns a wrapped error
//     naming the offending field/value.
//   - file present but with absent fields → defaults applied for the
//     absent fields only.
//
// Strict-field semantics: any TOML key NOT in the schema (e.g. a typo
// like `[time].lbtss = "..."`) is rejected with a clear error so silent
// misconfiguration is impossible.
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		return DefaultConfig(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("research: read config %q: %w", path, err)
	}
	return ParseConfig(data)
}

// ParseConfig parses a research-config TOML document from bytes.
// Same validation rules as LoadConfig; exposed as a separate entry
// point for tests and for callers that already have the bytes in hand.
func ParseConfig(data []byte) (*Config, error) {
	var raw rawConfig
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&raw)
	if err != nil {
		return nil, fmt.Errorf("research: parse config: %w", err)
	}
	// Strict-mode field check: any key the decoder did not consume is a
	// typo or an unsupported option. Reject loudly.
	if undec := md.Undecoded(); len(undec) > 0 {
		keys := make([]string, 0, len(undec))
		for _, k := range undec {
			keys = append(keys, k.String())
		}
		return nil, fmt.Errorf("research: unknown config key(s): %s", strings.Join(keys, ", "))
	}

	cfg := DefaultConfig()
	if raw.Determinism != "" {
		mode, err := parseDeterminismMode(raw.Determinism)
		if err != nil {
			return nil, err
		}
		cfg.Determinism = mode
	}
	if raw.Time.LBTS != "" {
		cfg.Time.LBTS = raw.Time.LBTS
	}
	if raw.Time.Grant != "" {
		cfg.Time.Grant = raw.Time.Grant
	}
	if raw.Ownership.Negotiation != "" {
		cfg.Ownership.Negotiation = raw.Ownership.Negotiation
	}
	return cfg, nil
}

// parseDeterminismMode maps a TOML string token onto the enum. Unknown
// values produce a clear error naming the accepted set.
func parseDeterminismMode(s string) (DeterminismMode, error) {
	switch s {
	case "strict":
		return DeterminismStrict, nil
	case "per-impl-opt-in":
		return DeterminismPerImplOptIn, nil
	case "off":
		return DeterminismOff, nil
	default:
		return 0, fmt.Errorf("research: unknown determinism mode %q (want strict|per-impl-opt-in|off)", s)
	}
}
