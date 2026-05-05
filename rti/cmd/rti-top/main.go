// rti-top — top-style TUI for `rtid` (gorti's runtime infrastructure
// daemon). Phase 2 of docs/rtid-tui.md.
//
// Read-only observability tool: dials AdminService on rtid's admin
// listener (default localhost:8443), polls Snapshot at the configured
// refresh rate, and renders five views (federations / drill-down /
// time / wire / events) using bubbletea + lipgloss + bubbles.
//
// PINNED constraints from the design doc:
//
//   - §1: read-only; no mutating RPCs (§7.5).
//   - §2.4: default refresh 1s, range [100ms, 60s], cycled via the
//     `r` keybinding at runtime.
//   - §2.5: admin listener default localhost:8443.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cbchoi/gorti/rti/cmd/rti-top/internal/client"
)

const (
	// minRefresh / maxRefresh are the boot-time clamps for --refresh,
	// taken from docs/rtid-tui.md §2.4 PINNED. Anything outside this
	// range is rejected at startup with a clear error.
	minRefresh = 100 * time.Millisecond
	maxRefresh = 60 * time.Second
)

func main() {
	addr := flag.String("rtid-addr", "localhost:8443",
		"AdminService listener address on rtid (host:port). Plaintext only — Phase 1 admin listener is plaintext (mTLS deferred).")
	refresh := flag.Duration("refresh", 1*time.Second,
		"Snapshot polling interval; clamped at boot to [100ms, 60s]. Cycled at runtime with the `r` keybinding.")
	smoke := flag.Bool("smoke", false,
		"Smoke-test mode: dial + call Status + print the response, then exit. Used by CI; does not enter the TUI.")
	flag.Parse()

	if err := validateRefresh(*refresh); err != nil {
		fmt.Fprintf(os.Stderr, "rti-top: %v\n", err)
		os.Exit(2)
	}

	cli, err := client.Dial(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rti-top: dial: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = cli.Close() }()

	// Liveness probe — surfaces the most common misconfig (wrong port,
	// admin listener not enabled) before we drop into the TUI loop.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := cli.Status(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rti-top: Status: %v\n", err)
		fmt.Fprintf(os.Stderr, "rti-top: is rtid running with --admin-listen %s?\n", *addr)
		os.Exit(1)
	}

	if *smoke {
		fmt.Printf("rti-top smoke OK: rtid_version=%s uptime=%ds (admin=%s)\n",
			st.GetRtidVersion(), st.GetUptimeSeconds(), cli.Target())
		return
	}

	if err := runTUI(ctx, cli, st, *refresh); err != nil {
		fmt.Fprintf(os.Stderr, "rti-top: %v\n", err)
		os.Exit(1)
	}
}

// validateRefresh enforces the PINNED [100ms, 60s] range from
// docs/rtid-tui.md §2.4. Returns a user-facing error message; the
// caller exits with code 2 (usage error).
func validateRefresh(d time.Duration) error {
	if d < minRefresh {
		return fmt.Errorf("--refresh=%s is below the minimum %s; pick a value in [%s, %s]",
			d, minRefresh, minRefresh, maxRefresh)
	}
	if d > maxRefresh {
		return fmt.Errorf("--refresh=%s is above the maximum %s; pick a value in [%s, %s]",
			d, maxRefresh, minRefresh, maxRefresh)
	}
	return nil
}
