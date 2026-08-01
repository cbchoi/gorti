package main

// Go runtime GC tuning knobs (perf round-2).
//
// The lrcbench GC A/B (5 process-isolated runs per config on the
// post-W8 TCP loopback bench) measured GOGC=400 as a -22.1% total-time
// median (137318476 ns vs the 179715186 ns post-W8 reference), so 400
// is adopted as the PRODUCT DEFAULT GC target percentage. The default
// is applied inside newRTID — the composition root shared by the real
// rtid binary and the lrcbench harness — so a bench run measures
// exactly what an untouched production rtid runs with.
//
// Precedence (highest wins):
//
//	explicit operator flag (--gc-percent / --go-mem-limit)
//	  > GOGC environment variable
//	    > product default (400)
//
// An operator-set GOGC is NEVER overridden by the product default:
// when GOGC is present in the environment and no explicit flag was
// passed, rtid does not call debug.SetGCPercent at all, so the Go
// runtime's own GOGC handling stands. --gc-percent=-1 likewise leaves
// the runtime default untouched (restores pre-round-2 behavior).
//
// rtidConfig.GCPercent / flag value semantics:
//
//	 0 → product-default policy (a set GOGC env wins; unset GOGC
//	     installs defaultGCPercent). This is the zero-config path the
//	     lrcbench harness and tests compose.
//	-1 → never call debug.SetGCPercent (Go runtime / GOGC defaults).
//	>0 → install that value (explicit operator choice; beats GOGC).
//
// Any other negative value is a configuration error. The explicit
// value 0 is also rejected at the CLI layer (main.go) — SetGCPercent(0)
// would force a collection at every heap growth, which is never an
// intentional production setting; 0 is reserved for "policy".

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
)

const (
	// defaultGCPercent is the PRODUCT DEFAULT GC target percentage,
	// adopted from the round-2 GC A/B evidence quoted above.
	defaultGCPercent = 400

	// gcPercentUnmanaged forces "leave the Go runtime default alone"
	// explicitly (no debug.SetGCPercent call), overriding the product
	// default.
	gcPercentUnmanaged = -1

	// maxGCPercent caps operator input at 100000 (heap may grow 1000x
	// between collections) — anything larger is certainly a typo or a
	// unit mistake, and "effectively off" is spelled GOGC=off or a
	// --go-mem-limit-driven configuration instead.
	maxGCPercent = 100_000
)

// gogcEnvSet reports whether the GOGC environment variable carries an
// operator value. An empty GOGC counts as unset — the Go runtime
// itself ignores an empty GOGC and applies its built-in default.
func gogcEnvSet() bool {
	v, ok := os.LookupEnv("GOGC")
	return ok && v != ""
}

// resolveGCPercent maps a flag/config GC-percent value plus the GOGC
// env presence to the effective percentage to install. A return of 0
// means "do not call debug.SetGCPercent" (operator env or explicit
// opt-out wins).
func resolveGCPercent(v int, gogcSet bool) (int, error) {
	switch {
	case v == 0:
		if gogcSet {
			return 0, nil // operator GOGC wins over the product default
		}
		return defaultGCPercent, nil
	case v == gcPercentUnmanaged:
		return 0, nil
	case v < 0:
		return 0, fmt.Errorf(
			"GC percent must be positive, 0 (product-default policy), or -1 (leave Go runtime default); got %d", v)
	case v > maxGCPercent:
		return 0, fmt.Errorf("GC percent must be at most %d; got %d", maxGCPercent, v)
	default:
		return v, nil
	}
}

// validateGoMemLimit rejects nonsense soft-memory-limit values. 0 means
// "do not call debug.SetMemoryLimit" (the Go runtime keeps its
// math.MaxInt64 = no-limit default); positive values are bytes.
func validateGoMemLimit(bytes int64) error {
	if bytes < 0 {
		return fmt.Errorf("Go memory limit must be >= 0 bytes (0 = no limit call); got %d", bytes)
	}
	return nil
}

// validateGoRuntimeCLIConfig is the fail-fast CLI-layer check mirroring
// validateOutboxCLIConfig: it resolves without installing anything so a
// nonsense value exits 2 before the runtime is half-composed. newRTID
// validates again for non-CLI composers.
func validateGoRuntimeCLIConfig(gcPercent int, memLimitBytes int64) error {
	if _, err := resolveGCPercent(gcPercent, gogcEnvSet()); err != nil {
		return fmt.Errorf("--gc-percent: %w", err)
	}
	if err := validateGoMemLimit(memLimitBytes); err != nil {
		return fmt.Errorf("--go-mem-limit: %w", err)
	}
	return nil
}

// applyGoRuntimeTuning resolves and installs the GC percent and the
// optional soft memory limit. Called from newRTID so the real binary
// and every newRTID composer (lrcbench harness, tests) inherit the
// same product defaults. Returns the first configuration error without
// having installed anything for the erroneous knob.
func applyGoRuntimeTuning(gcPercent int, memLimitBytes int64, logger *slog.Logger) error {
	effective, err := resolveGCPercent(gcPercent, gogcEnvSet())
	if err != nil {
		return fmt.Errorf("--gc-percent: %w", err)
	}
	if err := validateGoMemLimit(memLimitBytes); err != nil {
		return fmt.Errorf("--go-mem-limit: %w", err)
	}
	if effective > 0 {
		previous := debug.SetGCPercent(effective)
		if logger != nil {
			logger.Info("rtid: Go GC target percentage set",
				"gc_percent", effective, "previous", previous, "gogc_env", gogcEnvSet())
		}
	}
	if memLimitBytes > 0 {
		previous := debug.SetMemoryLimit(memLimitBytes)
		if logger != nil {
			logger.Info("rtid: Go soft memory limit set",
				"limit_bytes", memLimitBytes, "previous", previous)
		}
	}
	return nil
}

// currentGCPercent reads the process GC target percentage without
// changing it (SetGCPercent(-1) returns the previous value; the second
// call restores it). Used by the lrcbench report line and unit tests
// so "the default engaged" is an executable observation, not trust.
func currentGCPercent() int {
	v := debug.SetGCPercent(-1)
	debug.SetGCPercent(v)
	return v
}
