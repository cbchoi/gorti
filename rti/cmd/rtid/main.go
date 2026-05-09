// Command rtid runs the gorti RTI server.
//
// Wires the federation manager, declaration manager, object registry,
// event log multiplexer, and gRPC service handlers into a runnable
// server, plus a Prometheus metrics endpoint on a separate listener.
// See docs/agent-a-rti-core.md §4 for the M2 deliverable definition.
//
// # Connect URL convention (M6 W1B)
//
// rtid speaks one wire protocol — gRPC over HTTP/2 — and accepts two
// transport modes selected at startup:
//
//   - Insecure (default): no --tls-cert / --tls-key flags. Clients
//     dial with the URL form “grpc://host:port“ (Python SDK
//     transport strips the scheme and uses
//     “grpc.aio.insecure_channel“).
//   - Server-side TLS: --tls-cert + --tls-key both set. Clients dial
//     with the URL form “grpcs://host:port“ and supply a CA bundle
//     (or trust the system roots when the cert chains to one). mTLS
//     and cert rotation are explicitly out of scope for this cut and
//     are tracked as M7 follow-ups.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/cbchoi/gorti/rti/internal/buildinfo"
	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/ddm"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
	"github.com/cbchoi/gorti/rti/internal/federation"
	"github.com/cbchoi/gorti/rti/internal/mom"
	"github.com/cbchoi/gorti/rti/internal/object"
	"github.com/cbchoi/gorti/rti/internal/ownership"
	"github.com/cbchoi/gorti/rti/internal/research"
	"github.com/cbchoi/gorti/rti/internal/savepoint"
	syncpkg "github.com/cbchoi/gorti/rti/internal/sync"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"

	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	showVersion := flag.Bool("version", false, "print rtid version and exit")
	listen := flag.String("listen", ":8442", "gRPC listen address")
	metricsListen := flag.String("metrics-listen", ":9090", "Prometheus HTTP listen")
	// rtid-TUI Phase 1 (docs/rtid-tui.md §2.5 PINNED): a SEPARATE gRPC
	// listener that serves the read-only AdminService (Snapshot /
	// TailEvents / Status). Default localhost:8443 — admin is
	// loopback-only by default. Empty string disables admin (no second
	// listener constructed). TLS for admin is plaintext only in Phase
	// 1; the existing --tls-cert / --tls-key apply only to --listen.
	adminListen := flag.String("admin-listen", "localhost:8443", "AdminService gRPC listen address (read-only TUI / rti-top backend); empty disables")
	// rtid-TUI Phase 5: gate the MutatingService behind --admin-mutating
	// (default false). When true, the composition root REFUSES to start
	// unless --admin-listen resolves to a loopback address — the
	// operator can override with --admin-mutating-allow-non-loopback=true
	// if they really know what they're doing.
	//
	// See docs/rtid-tui.md §7.5 (Phase 5 unblocked under opt-in flag).
	adminMutating := flag.Bool("admin-mutating", false, "register MutatingService (ForceResign / DestroyFederation) on the admin port. DANGEROUS — requires loopback bind unless --admin-mutating-allow-non-loopback is also set.")
	adminMutatingAllowNonLoopback := flag.Bool("admin-mutating-allow-non-loopback", false, "override the loopback bind requirement for --admin-mutating. Operators that enable this MUST front the admin port with their own ACL.")
	logDir := flag.String("log-dir", "", "directory for per-federation event log files (empty = require explicit set)")
	logLevel := flag.String("log-level", "info", "log level: debug|info|warn|error")
	logFormat := flag.String("log-format", "json", "log format: json|text")
	mode := flag.String("mode", "server", "rtid mode: server|pingpong-demo|timed-demo|replay-from-log")
	federationMode := flag.String("federation-mode", "verbose", "federation operating mode for created federations: verbose|best-effort (TASK-076)")
	pingpongRounds := flag.Int("pingpong-rounds", 1000, "rounds for pingpong-demo mode")
	pingpongFederation := flag.String("pingpong-federation", "pingpong", "federation name for pingpong-demo mode")
	pingpongDeterministic := flag.Bool("pingpong-deterministic", false, "use FakeClock so the event-log body is byte-deterministic across runs")
	timedTicks := flag.Int("timed-ticks", 100, "ticks (per-federate NER count) for timed-demo mode")
	timedFederation := flag.String("timed-federation", "timed", "federation name for timed-demo mode")
	timedDeterministic := flag.Bool("timed-deterministic", false, "use FakeClock so the event-log body is byte-deterministic across runs")
	timedStallSkipFederate := flag.Int("timed-stall-skip", 0, "1-based federate index to skip NER for, provoking a stall (0 = no skip)")
	timedStallTimeoutMs := flag.Int("timed-stall-timeout-ms", 5000, "stall timeout in milliseconds for timed-demo mode (only effective when -timed-stall-skip is set)")
	timedStallAdvanceMs := flag.Int("timed-stall-advance-ms", 0, "fake-clock advance in milliseconds AFTER the demo loop, before CheckStalls (0 = skip stall check)")
	replayInput := flag.String("replay-input", "", "source event-log file path for replay-from-log mode")
	tlsCert := flag.String("tls-cert", "", "path to TLS server cert PEM (enables TLS when set; clients dial grpcs://host:port). Requires --tls-key.")
	tlsKey := flag.String("tls-key", "", "path to TLS server key PEM. Required when --tls-cert is set.")
	saveDir := flag.String("save-dir", "./gorti-saves", "directory under which federation save bundles are written + read (M9: FR-SR-1..5)")
	researchConfig := flag.String("research-config", "", "path to a TOML research-config file (Phase 3 of docs/research-platform.md). Server mode only; absent → default strategies + per-impl-opt-in determinism. Honors GORTI_RESEARCH_CONFIG as fallback when flag is empty; GORTI_DETERMINISM overrides the determinism field if set.")
	// M19 Phase 1a (docs/m19-dds-adapter.md §4.4): DDS data-plane
	// opt-in flags. These flags exist in EVERY rtid build — both the
	// default CGo-free build and the build-tag-gated `rtid-dds`
	// variant — so the CLI surface stays uniform. Their EFFECTIVE
	// behavior depends on whether the binary was built with
	// `-tags=dds`:
	//   - default build: --enable-dds=true is accepted by flag
	//     parsing but the rti/internal/transport/dds package's
	//     stubs return errors.ErrUnsupported on every primitive,
	//     so even with the flag set, CreateFederation with
	//     transport_mode=DDS will eventually fail when the
	//     federation tries to instantiate a DomainParticipant.
	//   - dds-tagged build: --enable-dds=true unlocks DDS-mode
	//     federations end-to-end (Phase 1b).
	// Default false keeps the cut-2 wire path untouched.
	enableDDS := flag.Bool("enable-dds", false, "accept CreateFederation requests with transport_mode=DDS. Requires the rtid binary to have been built with -tags=dds; the default CGo-free build rejects DDS even when this flag is set. See docs/m19-dds-adapter.md.")
	ddsDomainID := flag.Int("dds-domain-id", 0, "default DDS domain ID for federations created in DDS mode. Only meaningful when --enable-dds=true. Zero is the DDS default domain.")
	flag.Parse()

	if *showVersion {
		fmt.Println("rtid", buildinfo.String())
		return
	}

	logger := buildLogger(*logLevel, *logFormat)
	slog.SetDefault(logger)

	fedMode, err := parseFederationMode(*federationMode)
	if err != nil {
		logger.Error("invalid --federation-mode", "value", *federationMode, "err", err)
		os.Exit(2)
	}

	switch *mode {
	case "pingpong-demo":
		runPingpongMain(logger, *pingpongFederation, *pingpongRounds, *logDir, *pingpongDeterministic, fedMode)
		return
	case "timed-demo":
		runTimedMain(logger, timedRunArgs{
			Federation:        *timedFederation,
			Ticks:             *timedTicks,
			LogDir:            *logDir,
			Deterministic:     *timedDeterministic,
			StallSkipFederate: *timedStallSkipFederate,
			StallTimeoutMs:    *timedStallTimeoutMs,
			StallAdvanceMs:    *timedStallAdvanceMs,
			FederationMode:    fedMode,
		})
		return
	case "replay-from-log":
		runReplayMain(logger, *replayInput, *logDir)
		return
	case "server", "":
		runServerMain(logger, serverMainArgs{
			Listen:                        *listen,
			AdminListen:                   *adminListen,
			MetricsListen:                 *metricsListen,
			LogDir:                        *logDir,
			TLSCert:                       *tlsCert,
			TLSKey:                        *tlsKey,
			SaveDir:                       *saveDir,
			ResearchConfigPath:            *researchConfig,
			AdminMutating:                 *adminMutating,
			AdminMutatingAllowNonLoopback: *adminMutatingAllowNonLoopback,
			EnableDDS:                     *enableDDS,
			DDSDomainID:                   int32(*ddsDomainID),
		})
	default:
		logger.Error("unknown --mode", "mode", *mode)
		os.Exit(2)
	}
}

// parseFederationMode converts the --federation-mode CLI value into a
// core.Mode. Unknown values produce an error so a typo surfaces at
// startup rather than as a silent default. Per TASK-076: only verbose
// and best-effort are accepted at the CLI layer.
func parseFederationMode(s string) (core.Mode, error) {
	switch s {
	case "verbose", "":
		return core.ModeVerbose, nil
	case "best-effort":
		return core.ModeBestEffort, nil
	default:
		return core.ModeUnspecified, fmt.Errorf("unknown federation mode %q (want verbose|best-effort)", s)
	}
}

// runReplayMain feeds an existing event log through eventlog.NewReplayer
// and writes the captured stream into logDir. Used by the
// examples/go-pingpong/replay_test.go harness to satisfy the M2 gate's
// "feed log back through fresh RTI; assert byte-identical" contract.
func runReplayMain(logger *slog.Logger, inputPath, logDir string) {
	if inputPath == "" {
		logger.Error("replay-from-log requires -replay-input")
		os.Exit(2)
	}
	if logDir == "" {
		logger.Error("replay-from-log requires -log-dir")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := runReplayFromFile(ctx, inputPath, logDir)
	cancel()
	if err != nil {
		logger.Error("replay failed", "err", err)
		os.Exit(1)
	}
	logger.Info("replay complete", "input", inputPath, "output_dir", logDir)
}

// serverMainArgs bundles the runServerMain CLI inputs. Extracted so
// the call site stays one-line per flag and a future flag can land
// without churning the function signature.
type serverMainArgs struct {
	Listen                        string
	AdminListen                   string
	MetricsListen                 string
	LogDir                        string
	TLSCert                       string
	TLSKey                        string
	SaveDir                       string
	ResearchConfigPath            string
	AdminMutating                 bool
	AdminMutatingAllowNonLoopback bool
	// M19 Phase 1a (docs/m19-dds-adapter.md §4.4): plumbed all the
	// way down to the transport/grpc handler so CreateFederation
	// requests with transport_mode=DDS are accepted only when both
	// the build tag and the operator have opted in.
	EnableDDS   bool
	DDSDomainID int32
}

// runServerMain boots the gRPC server + metrics endpoint and blocks until
// SIGINT/SIGTERM. Extracted so main can dispatch on --mode.
func runServerMain(logger *slog.Logger, args serverMainArgs) {
	if args.LogDir == "" {
		logger.Warn("--log-dir not set; event logs will not be persisted")
	}

	tlsConfig, err := buildServerTLS(args.TLSCert, args.TLSKey)
	if err != nil {
		logger.Error("rtid TLS configuration failed", "err", err)
		os.Exit(2)
	}

	// Phase 3 research-platform: resolve the research-config (if any)
	// before constructing the runtime so any error halts startup with
	// exit 2 (config error) rather than a half-started rtid. Path
	// resolution priority: --research-config flag > GORTI_RESEARCH_CONFIG
	// env > "" (defaults). The GORTI_DETERMINISM env var, when set,
	// overrides whatever determinism mode the file selected.
	resolved, err := resolveResearchConfig(args.ResearchConfigPath, os.Getenv("GORTI_RESEARCH_CONFIG"), os.Getenv("GORTI_DETERMINISM"))
	if err != nil {
		logger.Error("rtid research-config invalid", "err", err)
		os.Exit(2)
	}

	// rtid-TUI Phase 5: enforce the loopback safety gate BEFORE
	// constructing the runtime so an attempted mis-bind exits cleanly
	// without standing up half a daemon. The gate fires only when
	// the operator opted into mutating ops; default-off rtid keeps
	// the existing read-only admin contract untouched.
	if args.AdminMutating {
		if args.AdminListen == "" {
			logger.Error("rtid: --admin-mutating requires --admin-listen to be set")
			os.Exit(2)
		}
		loopback := isLoopbackBind(args.AdminListen)
		if !loopback && !args.AdminMutatingAllowNonLoopback {
			logger.Error(
				"rtid: --admin-mutating refuses to start on a non-loopback bind; "+
					"either set --admin-listen to localhost:PORT or pass "+
					"--admin-mutating-allow-non-loopback=true (and front the port with an ACL)",
				"admin_listen", args.AdminListen,
			)
			os.Exit(2)
		}
		// Prominent warning either way — even on loopback, anyone with
		// shell access can resign federates and destroy federations.
		logger.Warn(
			"rtid: MUTATING ADMIN OPS ENABLED — anyone with admin-port access "+
				"can resign federates and destroy federations",
			"admin_listen", args.AdminListen,
			"loopback", loopback,
		)
	}

	srv, err := newRTID(rtidConfig{
		ListenAddr:                    args.Listen,
		AdminListenAddr:               args.AdminListen,
		MetricsListenAddr:             args.MetricsListen,
		LogDir:                        args.LogDir,
		Logger:                        logger,
		TLSConfig:                     tlsConfig,
		SaveDir:                       args.SaveDir,
		Research:                      resolved,
		AdminMutating:                 args.AdminMutating,
		AdminMutatingAllowNonLoopback: args.AdminMutatingAllowNonLoopback,
		EnableDDS:                     args.EnableDDS,
		DDSDomainID:                   args.DDSDomainID,
	})
	if err != nil {
		logger.Error("rtid initialization failed", "err", err)
		os.Exit(1)
	}
	if tlsConfig != nil {
		logger.Info("rtid: TLS enabled (clients should dial grpcs://...)", "cert", args.TLSCert)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	serveErr := srv.Serve(ctx)
	cancel()
	if serveErr != nil && !errors.Is(serveErr, context.Canceled) {
		logger.Error("rtid serve exited with error", "err", serveErr)
		os.Exit(1)
	}
}

// isLoopbackBind reports whether addr (host:port) resolves to a
// loopback address. Accepts the literal hosts "localhost", "127.0.0.1",
// and "::1"; an unspecified host (":PORT" or "0.0.0.0:PORT") returns
// false because it accepts non-loopback connections. Any host that
// fails to parse falls back to false (safer to reject than accept).
//
// Only the literal forms are recognised — DNS resolution is
// intentionally avoided because a name that today resolves to
// loopback may resolve to something else after a config push, and we
// want the safety gate to be byte-deterministic from the flag.
func isLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

// timedRunArgs bundles the flags for runTimedMain. Extracted so flags
// stay grouped at the call site and a future "stall" mode can add
// fields without growing the function signature.
type timedRunArgs struct {
	Federation        string
	Ticks             int
	LogDir            string
	Deterministic     bool
	StallSkipFederate int       // 1-based federate index to skip NER for (0 = none)
	StallTimeoutMs    int       // stall timeout in ms; only used when StallSkipFederate>0
	StallAdvanceMs    int       // post-loop FakeClock advance, then CheckStalls (0 = skip)
	FederationMode    core.Mode // TASK-076: federation operating mode for the created federation
}

// runTimedMain runs the M3 timed demo and exits. logDir, when set,
// gets per-federation .log files written into it (same format as server
// mode); empty means the demo runs without persistence.
//
// Mirrors runPingpongMain's shape so the W4 integration is symmetric
// with M2's W4-equivalent. The actual runner lives in timed.go.
//
// Stall mode (StallSkipFederate > 0): the demo enables every federate
// but skips NER for the chosen one, then advances the FakeClock by
// StallAdvanceMs and calls CheckStalls. The result (halt count + the
// stalled federate handle) is emitted on stdout as a single line of
// the form "TIMED_HALT halts=N stalled_federate=H" so the example
// stall_test.go can capture and assert without parsing the log file.
func runTimedMain(logger *slog.Logger, args timedRunArgs) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cfg := timedConfig{
		FederationName:    core.FederationName(args.Federation),
		Ticks:             args.Ticks,
		LogDir:            args.LogDir,
		Deterministic:     args.Deterministic,
		SkipFederateIndex: args.StallSkipFederate,
	}
	if args.StallSkipFederate > 0 && args.StallTimeoutMs > 0 {
		cfg.StallTimeout = time.Duration(args.StallTimeoutMs) * time.Millisecond
	}
	stats, err := runTimedDemo(ctx, cfg)
	if err != nil {
		cancel()
		logger.Error("timed-demo failed", "err", err)
		os.Exit(1)
	}
	logger.Info("timed-demo complete",
		"ticks", stats.TicksCompleted,
		"federates", stats.FederateCount,
		"grants", stats.GrantsObserved,
		"halts", stats.HaltsObserved,
		"elapsed_ms", stats.Elapsed.Milliseconds(),
	)

	// Stall harness path: build a fresh runtime that we can drive
	// post-loop (advance FakeClock + CheckStalls). The runTimedDemo
	// above doesn't expose the runtime so we stand up a second one
	// dedicated to the stall scenario when StallAdvanceMs is set.
	// This keeps the "normal demo" code path untouched.
	if args.StallSkipFederate > 0 && args.StallAdvanceMs > 0 {
		halts, stalledFed, err := runTimedStall(ctx, args)
		if err != nil {
			cancel()
			logger.Error("timed-stall failed", "err", err)
			os.Exit(1)
		}
		// Machine-readable signal for stall_test.go. Emitted on
		// stdout (the logger writes to stderr); the example test
		// captures stdout and parses the line.
		emitTimedHaltLine(halts, stalledFed)
	}
	cancel()
}

// emitTimedHaltLine writes the single TIMED_HALT line to stdout. Lives
// in its own helper so the forbidigo allowlist for fmt.Print* can be
// scoped to just this function via a //nolint comment (we avoid using
// the logger because the line MUST be on stdout — the example
// stall_test.go captures stdout independently of the logger handler).
func emitTimedHaltLine(halts int, stalled core.FederateHandle) {
	// Use os.Stdout.WriteString to bypass the forbidigo fmt.Print
	// ban while still emitting a deterministic single-line signal.
	_, _ = os.Stdout.WriteString(
		"TIMED_HALT halts=" + strconv.Itoa(halts) +
			" stalled_federate=" + strconv.FormatUint(uint64(stalled), 10) + "\n",
	)
}

// runTimedStall stands up a dedicated runtime for the stall scenario,
// runs the abbreviated NER pattern (every federate enables; only the
// non-skipped ones NER once), advances the FakeClock past the
// configured stall timeout, then invokes CheckStalls. Returns the halt
// count and the recorded stalled-federate handle (0 if none).
//
// Lives here (not in timed.go) so the M2-style runtime helpers stay
// single-purpose; stall is a one-shot affair, not a loop.
func runTimedStall(ctx context.Context, args timedRunArgs) (int, core.FederateHandle, error) {
	clk := core.NewFakeClock(time.Unix(0, 0))
	rt, cleanup, err := buildTimedRuntime(timedConfig{
		FederationName: core.FederationName(args.Federation),
		Ticks:          1,
		Lookaheads:     []core.LogicalTime{1.0, 1.0, 1.0},
		StallTimeout:   time.Duration(args.StallTimeoutMs) * time.Millisecond,
		Clock:          clk,
	})
	if err != nil {
		return 0, 0, err
	}
	defer cleanup()

	for i := 0; i < 3; i++ {
		h := core.FederateHandle(uint64(i + 1))
		if err := rt.tm.EnableRegulation(ctx, core.FederationName(args.Federation), h, core.LogicalTime(1.0)); err != nil {
			return 0, 0, fmt.Errorf("EnableRegulation %d: %w", h, err)
		}
	}
	skip := uint64(0)
	if args.StallSkipFederate > 0 {
		skip = uint64(args.StallSkipFederate)
	}
	for i := 0; i < 3; i++ {
		h := core.FederateHandle(uint64(i + 1))
		if uint64(h) == skip {
			continue
		}
		// NER to a target peers can't satisfy without the skipped
		// federate also advancing.
		if err := rt.tm.NextMessageRequest(ctx, core.FederationName(args.Federation), h, core.LogicalTime(10.0)); err != nil {
			return 0, 0, fmt.Errorf("NER %d: %w", h, err)
		}
	}
	halts := rt.AdvanceClockAndCheckStalls(ctx, time.Duration(args.StallAdvanceMs)*time.Millisecond)
	var stalledFed core.FederateHandle
	for _, s := range rt.outbox.Sent() {
		if fh := stalledFromEvent(s.Event); fh != 0 {
			stalledFed = fh
			break
		}
	}
	return halts, stalledFed, nil
}

// stalledFromEvent extracts the StalledFederate handle from a
// *timepkg.FederationHalted event, or 0 for any other event type.
func stalledFromEvent(evt core.OutboundEvent) core.FederateHandle {
	if hv, ok := evt.(*timepkg.FederationHalted); ok {
		return hv.StalledFederate
	}
	return 0
}

// runPingpongMain runs the pingpong demo and exits. logDir, when set,
// gets per-federation .log files written into it (same format as server
// mode); empty means the demo runs without persistence.
func runPingpongMain(logger *slog.Logger, federation string, rounds int, logDir string, deterministic bool, fedMode core.Mode) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	stats, err := runPingpongDemo(ctx, pingpongConfig{
		FederationName: core.FederationName(federation),
		Rounds:         rounds,
		LogDir:         logDir,
		Deterministic:  deterministic,
		FederationMode: fedMode,
	})
	cancel()
	if err != nil {
		logger.Error("pingpong-demo failed", "err", err)
		os.Exit(1)
	}
	logger.Info("pingpong-demo complete",
		"rounds", stats.RoundsCompleted,
		"elapsed_ms", stats.Elapsed.Milliseconds(),
	)
}

// rtidConfig bundles the runnable configuration. main.go translates flags
// into this struct; tests construct it directly.
type rtidConfig struct {
	ListenAddr        string
	MetricsListenAddr string
	LogDir            string
	Logger            *slog.Logger

	// AdminListenAddr is the bind address for the read-only
	// AdminService gRPC server (rtid-TUI Phase 1 — docs/rtid-tui.md
	// §2.5 PINNED). Default localhost:8443 in main.go's flag binding.
	// Empty string disables admin (no second listener / server is
	// constructed). Plaintext only in Phase 1 — TLSConfig DOES NOT
	// apply here; mTLS / RBAC for admin are deferred. Production
	// deployments that need network-exposed admin should front it
	// with a reverse proxy + ACL.
	AdminListenAddr string

	// TLSConfig, when non-nil, enables server-side TLS on the gRPC
	// listener. nil keeps the listener insecure (the default for
	// local/dev workloads). Construct via buildServerTLS or by hand
	// in tests; the M6 W1B cut supports the static-cert path only —
	// mTLS / cert rotation are M7 follow-ups.
	TLSConfig *tls.Config

	// SaveDir is the directory under which the savepoint.Manager
	// writes + reads federation save bundles (M9: FR-SR-1..5). Empty
	// string disables persistence (the manager is still composed but
	// any RequestFederationSave will fail with a stat-error wrapped
	// in core.ErrSaveBundleCorrupt — production callers should set
	// this; tests may leave it empty when they construct the manager
	// directly).
	SaveDir string

	// Research carries the resolved research-platform strategies +
	// determinism mode (Phase 3 of docs/research-platform.md). Zero
	// value (Resolved{} with all-nil strategies) means "no
	// research-config wired"; newRTID falls back to package defaults
	// for every Manager so behavior is identical to today's
	// hand-wired runtime. When non-zero, the resolved
	// Time.LBTS/Time.Grant/Ownership.Negotiation strategies thread
	// into the corresponding Options fields at Manager construction.
	Research research.Resolved

	// AdminMutating, when true, registers the MutatingService
	// (ForceResign / DestroyFederation) on the admin gRPC server.
	// rtid-TUI Phase 5 — DANGEROUS; only set after the loopback safety
	// gate has fired (see runServerMain).
	AdminMutating bool

	// AdminMutatingAllowNonLoopback is the "yes I really know what
	// I'm doing" override for the loopback bind requirement. Surfaced
	// here only so newRTID can log it; the gate logic lives in
	// runServerMain so an early exit doesn't construct the runtime.
	AdminMutatingAllowNonLoopback bool

	// EnableDDS, when true, accepts CreateFederation requests with
	// transport_mode=DDS. M19 Phase 1a (docs/m19-dds-adapter.md §4.4).
	// In the default CGo-free build, the transport/dds package's
	// stubs return errors.ErrUnsupported on every primitive — so the
	// flag exists in every build but is effectively a no-op without
	// the dds build tag. The dds-tagged build (Phase 1b) wires real
	// CGo-backed implementations.
	EnableDDS bool

	// DDSDomainID is the default DDS domain ID stamped into a
	// federation when CreateFederation is accepted in DDS mode.
	// Zero is the DDS default domain. Only meaningful when
	// EnableDDS=true.
	DDSDomainID int32
}

// rtid is the composed runtime: gRPC server + metrics handler + the
// underlying core components. Construct via newRTID; run via Serve.
type rtid struct {
	cfg       rtidConfig
	logger    *slog.Logger
	startedAt time.Time
	fedMgr    *federation.Manager
	declMgr   core.DeclarationManagement
	objReg    *object.Registry
	syncMgr   core.SyncCoordinator
	ownMgr    core.OwnershipCoordinator
	momMgr    core.ManagementObjectModel
	ddmMgr    core.DataDistributionManagement
	saveMgr   core.SavepointCoordinator
	timeMgr   core.TimeManager
	multi     *eventlog.MultiplexWriter
	outbox    *multiOutbox
	grpcS     *stdgrpc.Server
	// adminS is the dedicated AdminService gRPC server bound to
	// cfg.AdminListenAddr. nil when admin is disabled.
	adminS  *stdgrpc.Server
	metrics *metricsHandler
	foms    *fomRepository
}

// newRTID composes all the components and returns a runnable rtid.
//
// Wiring order matters: the multiplex writer is shared by the federation
// manager and the object registry (both write to the same per-federation
// log files); the FOM repository is wired into the federation manager;
// the gRPC server is the last composer.
func newRTID(cfg rtidConfig) (*rtid, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	clock := core.NewRealClock()

	multi, err := newMultiplexLog(cfg.LogDir, clock)
	if err != nil {
		return nil, err
	}

	foms := newFOMRepository()
	declMgr := declaration.New()
	outbox := newMultiOutbox(1024)

	// M11: MOM manager constructed BEFORE federation manager so the
	// federation manager's OnFederateJoined / OnFederateResigned hooks
	// can dispatch to MOM. The Outbox is shared with sync/ownership/
	// object — MOM uses it for the (cut-2 deferred) subscriber fan-out.
	momMgr, err := mom.New(mom.Options{
		Outbox:   outbox,
		EventLog: multi,
	})
	if err != nil {
		return nil, err
	}

	// M21 TASK-204: time.Manager is composed BEFORE federation.Manager
	// so the federation's OnFederateResigned hook can call into
	// timeMgr.OnFederateResign (TASK-204c) — pending NER/TAR/TARA/
	// NMRA/FQR state must drop when the federate leaves.
	timeMgr, err := timepkg.New(timepkg.Options{
		Clock:    clock,
		Outbox:   outbox,
		EventLog: multi,
	})
	if err != nil {
		return nil, fmt.Errorf("rtid: time manager init: %w", err)
	}

	// M24 W1 — ownership-release hook is set after ownMgr is constructed
	// (a few lines down). The fedMgr's OnFederateResigned chain captures
	// a function-pointer indirection so the resign call dispatches into
	// ownMgr.ReleaseAllOwnedBy at runtime, after ownMgr is wired in.
	var ownResignHook func(context.Context, core.FederationName, core.FederateHandle)
	fedMgr, err := federation.New(federation.Options{
		Clock:              clock,
		EventLog:           multi,
		FOMs:               foms,
		OnFederateJoined: momFederateJoinedHook(momMgr, cfg.Logger),
		OnFederateResigned: chainOnFederateResigned(
			momFederateResignedHook(momMgr, cfg.Logger),
			// M21 TASK-204c: drop time-mgr pending state on resign so a
			// pending NER/TAR/TARA/NMRA/FQR doesn't leak in nerStore.
			timeMgr.OnFederateResign,
			// M24 W1: indirection — resolved when ownResignHook is set
			// after ownMgr construction.
			func(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
				if ownResignHook != nil {
					ownResignHook(ctx, fed, h)
				}
			},
		),
	})
	if err != nil {
		return nil, err
	}

	// M8 W1: ownership + sync managers. Sync uses Outbox for
	// announceSynchronizationPoint / federationSynchronized; ownership
	// uses Outbox for the §7 transfer notifications. Neither manager
	// is exposed via gRPC in cut-1 (proto Service definitions for
	// sync/ownership are not yet defined — M8 W2 follow-up); they are
	// composed here so the runtime owns canonical state and so the
	// object.Registry's OnRegister hook can populate initial ownership.
	ownMgr, err := ownership.New(ownership.Options{
		Outbox:   outbox,
		EventLog: multi,
		// Phase 3 research-platform: thread the resolved
		// NegotiationStrategy when --research-config is set; nil →
		// ownership.New falls back to defaultNegotiation, identical
		// to today's behavior.
		Strategy: cfg.Research.Ownership.Negotiation,
	})
	if err != nil {
		return nil, err
	}
	// M24 W1 — wire the ownership release into the resign chain. The
	// federation manager's OnFederateResigned was constructed earlier
	// with a closure that defers to ownResignHook; assigning here
	// activates it. ReleaseAllOwnedBy is silent at the manager level —
	// no peer notifications — which matches the cut-1 simplification
	// documented in rti/internal/ownership/release.go.
	ownResignHook = func(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
		ownMgr.ReleaseAllOwnedBy(ctx, fed, h)
	}
	syncMgr, err := syncpkg.New(syncpkg.Options{
		Outbox:   outbox,
		EventLog: multi,
		// M13 thread A (docs/srs.md §10.4): wire the federation
		// manager's joined-federate snapshot as the sync-point
		// required-set resolver. Register calls with nil
		// requiredFederates now materialize the implicit "all
		// joined federates" set at request-time instead of falling
		// back to dynamic-mode aggregation. Unknown federation
		// returns an empty slice (no joined federates) which leaves
		// the sync point announced-but-never-achieved — same as the
		// caller-supplied empty list.
		Members: fedMgr.MembersOf,
	})
	if err != nil {
		return nil, err
	}

	// M21 TASK-204: timeMgr is constructed earlier (above the federation
	// manager) so the OnFederateResigned chain can call into it; see
	// the construction site above for rationale.

	// M10 W1: Data Distribution Management. The Manager is composed
	// here so the object.Registry can consult it on every update;
	// gRPC handlers for the DDMService are deferred to M10 W2 (the
	// proto definition is FROZEN at this cut, so DDM operations are
	// reachable only via the in-process API for now).
	ddmMgr, err := ddm.New(ddm.Options{
		Outbox:   outbox,
		EventLog: multi,
		FOMs:     foms,
	})
	if err != nil {
		return nil, err
	}

	// M9 W1: Federation Save/Restore. Composed here so the runtime
	// owns canonical save state and the Storage backend (filesystem
	// rooted at SaveDir). gRPC handlers for the save/restore services
	// are deferred to M9 W2 — the proto Service definition is FROZEN
	// at this cut and does not yet expose save/restore RPCs, so the
	// manager is reachable only via the in-process API for now. See
	// docs/reports/M9/agent-a.md for the deferral rationale.
	var saveMgr *savepoint.Manager
	if cfg.SaveDir != "" {
		fsStore, err := savepoint.NewFSStorage(cfg.SaveDir)
		if err != nil {
			return nil, fmt.Errorf("rtid: savepoint storage init: %w", err)
		}
		saveMgr, err = savepoint.New(savepoint.Options{
			Outbox:      outbox,
			EventLog:    multi,
			BundleStore: fsStore,
			// M13 thread A (docs/srs.md §10.4): wire
			// federation.Manager.MembersOf as the joined-federate
			// snapshot resolver. Closes the M26 deferral —
			// RequestFederationSave now fans out
			// initiateFederateSave to every joined federate via a
			// concrete recipient list instead of emitting a single
			// broadcast envelope addressed to InvalidFederateHandle
			// (which multiOutbox.Send drops). Federates therefore
			// receive the save-callback delivery that the M12 W2
			// proto FederateEvent variants made possible.
			Members: fedMgr.MembersOf,
			// M13 thread C (docs/srs.md §10.4): wire the four
			// service-group managers as snapshot participants. On
			// save, each Marshal(fed) result is bundled into the
			// manifest under its registered key; on restore, the
			// matching Unmarshal runs before the event-log replay
			// so state lands from structured bytes without sole
			// reliance on replay determinism. Old bundles that
			// pre-date M13 omit manager_snapshots — the restore
			// path is nil-safe and falls back to event-log replay.
			ManagerSnapshots: map[string]savepoint.ManagerSnapshotter{
				savepoint.ManagerSnapshotKeySync:      syncMgr,
				savepoint.ManagerSnapshotKeyOwnership: ownMgr,
				savepoint.ManagerSnapshotKeyMOM:       momMgr,
				savepoint.ManagerSnapshotKeyDDM:       ddmMgr,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("rtid: savepoint manager init: %w", err)
		}
	}

	objReg, err := object.New(object.Options{
		EventLog:     multi,
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         foms,
		Clock:        clock,
		// TASK-077: wire mode + per-attribute order lookup so the
		// registry can choose RO over TSO when both the federation
		// is best-effort AND the FOM declares the attribute as
		// Receive-order. The production fomHandle does not yet
		// implement FOMOrderResolver, so FOMRepoOrderLookup returns
		// "unknown" for every attribute and the registry stays on
		// the TSO path until a future cut upgrades the handle.
		Federations: fedMgr,
		Orders:      grpcsvc.FOMRepoOrderLookup{Repo: foms},
		// M8 W1: notify ownership.Manager of every successful object
		// registration so QueryOwnership / IsOwnedBy reflect the
		// producing federate as the initial owner of all class
		// attributes (FR-OWN-5).
		OnRegister: func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			ownMgr.RegisterInitialOwnership(fed, owner, obj, attrs)
		},
		// M11: per-federate MOM counters (FR-MOM-1). See momCounterHooks.
		OnUpdateSent:           momMgr.IncrementUpdatesSent,
		OnInteractionSent:      momMgr.IncrementInteractionsSent,
		OnReflectDelivered:     momMgr.IncrementReflectionsReceived,
		OnInteractionDelivered: momMgr.IncrementInteractionsReceived,
		// M10: DDM-aware fan-out filter. The adapter wraps
		// ddm.Manager into the object.DDMFilter shape (see
		// ddmFilterAdapter below for the handle-conversion glue).
		DDM: ddmFilterAdapter{m: ddmMgr},
		// M22 W2 — TSO delivery gate. The time manager satisfies
		// core.TSODeliveryGate; when nil the registry falls back to
		// pre-M22 always-async behavior. Wiring it here makes the
		// IEEE 1516.1 §8.16-8.17 default (async OFF) observable
		// cross-process.
		TSOGate: timeMgr,
	})
	if err != nil {
		return nil, err
	}

	grpcSrv, err := grpcsvc.NewServer(grpcsvc.Options{
		Federations:                fedMgr,
		Declarations:               declMgr,
		Objects:                    objReg,
		Outbox:                     outbox,
		OnCreateFederationSuccess:  createFederationHook(foms, momMgr, cfg.Logger),
		OnDestroyFederationSuccess: destroyFederationHook(momMgr, cfg.Logger),
		// M21 TASK-204: TimeService gRPC. Composed unconditionally
		// in server mode (vs cut-1's nil placeholder) so federates
		// can drive HLA time advance cross-process.
		Time: timeMgr,
		// M12 W1: cut-3 gRPC services. Save manager is optional —
		// only wired when --save-dir is set (saveMgr == nil otherwise).
		Sync:      syncMgr,
		Ownership: ownMgr,
		DDM:       ddmMgr,
		Savepoint: saveMgr,
		// M12 W3: MomService — federates introspect MOM-tracked
		// state (HLAfederation / HLAfederate object snapshots +
		// per-federate counters). Read-only; lives on the federate
		// port (this --listen) since it is federate-facing
		// introspection, not operator-facing observability.
		MOM: momMgr,
		// M19 Phase 1a (docs/m19-dds-adapter.md §4.4): wire the
		// DDS opt-in flags into the federation handler so
		// CreateFederation rejects transport_mode=DDS unless the
		// operator opted in. TransportLookup feeds the manager's
		// recorded mode back to JoinFederationResponse so federates
		// pick the right wire path.
		DDSEnabled:         cfg.EnableDDS,
		DDSDefaultDomainID: cfg.DDSDomainID,
		TransportLookup:    fedMgr.TransportFor,
	})
	if err != nil {
		return nil, err
	}
	var serverOpts []stdgrpc.ServerOption
	if cfg.TLSConfig != nil {
		// credentials.NewTLS clones the *tls.Config internally, so the
		// caller is free to keep mutating its copy without affecting
		// the live server. Static-cert only at this cut: cfg.TLSConfig
		// is built from --tls-cert/--tls-key and not reloaded.
		serverOpts = append(serverOpts, stdgrpc.Creds(credentials.NewTLS(cfg.TLSConfig)))
	}
	gs := stdgrpc.NewServer(serverOpts...)
	if err := grpcSrv.Register(gs); err != nil {
		return nil, err
	}

	metrics := newMetricsHandler(
		fedMgr,
		objectCounterFor(objReg),
		multiplexSeqSource(multi),
	)

	// rtid-TUI Phase 1: AdminService gRPC server. Bound to
	// cfg.AdminListenAddr — a SEPARATE listener so admin traffic does
	// not contend with federate traffic on --listen, and the cfg.TLSConfig
	// (server-side TLS for federates) does NOT apply (Phase 1 admin is
	// plaintext only; production deployments front it with a reverse
	// proxy + ACL when network-exposed). Empty AdminListenAddr means
	// admin is disabled — adminS stays nil and Serve skips the
	// listener.
	startedAt := time.Now()
	var adminGS *stdgrpc.Server
	if cfg.AdminListenAddr != "" {
		adminGS = stdgrpc.NewServer()
		if err := grpcsvc.RegisterAdminService(adminGS, grpcsvc.AdminOptions{
			Federations:  fedMgr,
			Declarations: declMgr,
			Sync:         syncMgr,
			Ownership:    ownMgr,
			DDM:          ddmMgr,
			Savepoint:    saveMgr,
			MOM:          momMgr,
			// M21 TASK-204: time manager is composed in server mode
			// (the same instance the TimeServiceServer wraps), so the
			// AdminService Snapshot includes per-federation time state.
			Time:      timeMgr,
			Objects:   objReg,
			Outbox:    outbox,
			EventLog:  multi,
			Version:   rtidVersion(),
			StartedAt: startedAt,
		}); err != nil {
			return nil, fmt.Errorf("rtid: admin service register: %w", err)
		}

		// rtid-TUI Phase 5: register MutatingService ONLY when the
		// composition-root flag is set. AdminService stays read-only.
		// See docs/rtid-tui.md §7.5 for the safety contract.
		if cfg.AdminMutating {
			if err := grpcsvc.RegisterMutatingService(adminGS, grpcsvc.MutatingOptions{
				Federations: fedMgr,
				Version:     rtidVersion(),
			}); err != nil {
				return nil, fmt.Errorf("rtid: mutating service register: %w", err)
			}
		}
	}

	return &rtid{
		cfg:       cfg,
		logger:    cfg.Logger,
		startedAt: startedAt,
		fedMgr:    fedMgr,
		declMgr:   declMgr,
		objReg:    objReg,
		syncMgr:   syncMgr,
		ownMgr:    ownMgr,
		momMgr:    momMgr,
		ddmMgr:    ddmMgr,
		saveMgr:   saveMgr,
		timeMgr:   timeMgr,
		multi:     multi,
		outbox:    outbox,
		grpcS:     gs,
		adminS:    adminGS,
		metrics:   metrics,
		foms:      foms,
	}, nil
}

// rtidVersion returns the build version used in the AdminService
// Status / Snapshot responses. Defaults to the buildinfo "dev"
// sentinel; release builds override via -ldflags injection (see
// .goreleaser.yaml).
func rtidVersion() string {
	return buildinfo.String()
}

// ddmFilterAdapter bridges the core.DataDistributionManagement API
// into the object.DDMFilter contract. The two packages use distinct
// typed handles (core.DDMRegionHandleCore vs object.DDMRegionHandle,
// both uint64) so the adapter performs the trivial conversion at the
// boundary. Defined here (cmd/rtid composition) rather than inside ddm
// so the ddm package stays free of an object-package import.
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.4): the field type is the interface so alternative DDM impls
// composed at the rtid root flow through unchanged.
type ddmFilterAdapter struct {
	m core.DataDistributionManagement
}

func (a ddmFilterAdapter) HasObjectAssociations(fed core.FederationName, obj core.ObjectHandle) bool {
	return a.m.HasObjectAssociations(fed, obj)
}

func (a ddmFilterAdapter) PublisherRegionsFor(fed core.FederationName, obj core.ObjectHandle, attr core.AttributeHandle) []object.DDMRegionHandle {
	rs := a.m.PublisherRegionsFor(fed, obj, attr)
	if len(rs) == 0 {
		return nil
	}
	out := make([]object.DDMRegionHandle, len(rs))
	for i, r := range rs {
		out[i] = object.DDMRegionHandle(r)
	}
	return out
}

func (a ddmFilterAdapter) SubscribersForUpdate(
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attr core.AttributeHandle,
	publisherRegions []object.DDMRegionHandle,
) []core.FederateHandle {
	if len(publisherRegions) == 0 {
		return nil
	}
	rs := make([]core.DDMRegionHandleCore, len(publisherRegions))
	for i, r := range publisherRegions {
		rs[i] = core.DDMRegionHandleCore(r)
	}
	return a.m.SubscribersForUpdate(fed, cls, attr, rs)
}

// Serve runs the gRPC + metrics listeners until ctx is canceled. Returns
// the first non-graceful error from either listener.
//
// rtid-TUI Phase 1: when r.adminS is non-nil, also binds a third
// listener on cfg.AdminListenAddr serving the read-only AdminService.
// Admin is plaintext only — the cfg.TLSConfig set by --tls-cert
// does NOT apply (separate listener; production deployments add an
// ACL via mTLS or a reverse proxy when network-exposed).
func (r *rtid) Serve(ctx context.Context) error {
	gln, err := net.Listen("tcp", r.cfg.ListenAddr)
	if err != nil {
		return err
	}
	mln, err := net.Listen("tcp", r.cfg.MetricsListenAddr)
	if err != nil {
		_ = gln.Close()
		return err
	}
	var aln net.Listener
	if r.adminS != nil {
		aln, err = net.Listen("tcp", r.cfg.AdminListenAddr)
		if err != nil {
			_ = gln.Close()
			_ = mln.Close()
			return err
		}
	}

	logArgs := []any{"grpc", gln.Addr().String(), "metrics", mln.Addr().String()}
	if aln != nil {
		logArgs = append(logArgs, "admin", aln.Addr().String())
		r.logger.Info("rtid: admin gRPC listening (plaintext, read-only)", "addr", aln.Addr().String())
	}
	r.logger.Info("rtid serving", logArgs...)

	metricsSrv := &http.Server{
		Handler:           r.metrics,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Channel sized for the worst case: federate gRPC + metrics +
	// admin gRPC.
	errCh := make(chan error, 3)
	go func() { errCh <- r.grpcS.Serve(gln) }()
	go func() { errCh <- metricsSrv.Serve(mln) }()
	if r.adminS != nil {
		go func() { errCh <- r.adminS.Serve(aln) }()
	}

	select {
	case <-ctx.Done():
		r.logger.Info("rtid shutting down", "cause", ctx.Err())
		r.grpcS.GracefulStop()
		if r.adminS != nil {
			r.adminS.GracefulStop()
		}
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = metricsSrv.Shutdown(shutCtx)
		shutCancel()
		_ = r.multi.Close()
		return ctx.Err()
	case err := <-errCh:
		_ = gln.Close()
		_ = mln.Close()
		if aln != nil {
			_ = aln.Close()
		}
		_ = r.multi.Close()
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, stdgrpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}

// newMultiplexLog returns an event-log MultiplexWriter rooted at logDir,
// or an in-memory factory when logDir is empty. The in-memory factory
// drops bytes (no observable file) but keeps the writer happy so the
// rest of the pipeline runs.
func newMultiplexLog(logDir string, clock core.Clock) (*eventlog.MultiplexWriter, error) {
	if logDir == "" {
		return eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
			Clock:   clock,
			Mode:    core.ModeVerbose,
			Factory: discardWriterFactory(clock),
		})
	}
	return eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
		Clock: clock,
		Mode:  core.ModeVerbose,
		Dir:   logDir,
	})
}

// discardWriterFactory builds Writers whose sink discards bytes — used
// when --log-dir is empty so the system stays runnable without
// persisting logs (the rtid log emits a Warn at startup explaining the
// non-persistence).
func discardWriterFactory(clock core.Clock) eventlog.WriterFactory {
	return func(opts eventlog.WriterOptions) (*eventlog.Writer, error) {
		opts.Sink = discardSink{}
		opts.Clock = clock
		return eventlog.NewWriter(opts)
	}
}

// discardSink is io.Writer that swallows everything. Used by the
// no-log-dir factory.
type discardSink struct{}

func (discardSink) Write(p []byte) (int, error) { return len(p), nil }

// resolveResearchConfig produces a research.Resolved bundle from the
// --research-config flag value, the GORTI_RESEARCH_CONFIG env var
// fallback, and the GORTI_DETERMINISM env var override.
//
// Path resolution priority: flagPath > envPath > "". When the
// resolved path is "" the returned Resolved is the all-defaults
// bundle (default strategies + per-impl-opt-in determinism), which
// makes the code path through newRTID identical to today's
// hand-wired runtime — Options.Strategy on the resulting Manager is
// the package default, and behavior is bit-for-bit unchanged.
//
// The determinismOverride argument, when non-empty, replaces the
// determinism mode on the resolved Config before Apply runs. This is
// the documented escape hatch from design doc §8 step 2 for one-off
// determinism flips without editing the TOML file. Unknown override
// values are rejected with the same error LoadConfig would produce.
//
// Errors are returned wrapped so the caller can log them with
// context. The caller (runServerMain) exits 2 on any error so the
// rtid never starts in a misconfigured state.
func resolveResearchConfig(flagPath, envPath, determinismOverride string) (research.Resolved, error) {
	path := flagPath
	if path == "" {
		path = envPath
	}
	cfg, err := research.LoadConfig(path)
	if err != nil {
		return research.Resolved{}, err
	}
	if determinismOverride != "" {
		// Re-encode through ParseConfig's path validation so an
		// unknown override surfaces with the same error wording the
		// TOML field would. We synthesize a one-line TOML body to
		// reuse parseDeterminismMode without exposing it.
		var probe research.Config
		probe.Determinism = cfg.Determinism
		ovrCfg, err := research.ParseConfig([]byte("determinism = \"" + determinismOverride + "\""))
		if err != nil {
			return research.Resolved{}, fmt.Errorf("rtid: GORTI_DETERMINISM override: %w", err)
		}
		cfg.Determinism = ovrCfg.Determinism
	}
	reg := research.Default()
	resolved, err := research.Apply(cfg, reg)
	if err != nil {
		return research.Resolved{}, err
	}
	return resolved, nil
}

// buildServerTLS loads a server-side *tls.Config from the
// --tls-cert/--tls-key flag pair. Returns (nil, nil) when both are empty
// (insecure mode). Returns an error when one is set without the other,
// or when the keypair fails to load. M6 W1B cut: server-side TLS only,
// no mTLS, no rotation; clients dial grpcs://host:port and rely on a
// trusted CA bundle (or system roots when the cert chains to one).
func buildServerTLS(certPath, keyPath string) (*tls.Config, error) {
	if certPath == "" && keyPath == "" {
		return nil, nil
	}
	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("rtid: --tls-cert and --tls-key must both be set (got cert=%q key=%q)", certPath, keyPath)
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("rtid: load TLS keypair: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		// MinVersion: TLS 1.2 floor matches Go's secure-by-default
		// recommendation and avoids the gosec G402 lint. Clients
		// dialing with the Python SDK's default ssl.create_default_context
		// negotiate TLS 1.2 or higher.
		MinVersion: tls.VersionTLS12,
	}, nil
}

// buildLogger constructs the slog Logger from the level / format flags.
func buildLogger(levelStr, formatStr string) *slog.Logger {
	level := slog.LevelInfo
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	if formatStr == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

// objectCounterFor returns an objectCounter view over the object
// registry. The cut-1 stub returns 0 — the registry doesn't expose a
// public per-federation count yet (not required by an in-flight test).
// A future enhancement adds a Stats() method to the registry; for now
// the metrics line is present (so scrapers see the gauge) but always
// reports 0 in production. Tests inject their own counter.
func objectCounterFor(_ *object.Registry) objectCounter {
	return zeroObjectCounter{}
}

type zeroObjectCounter struct{}

func (zeroObjectCounter) ObjectCount(core.FederationName) uint64 { return 0 }

// multiplexSeqSource returns an eventLogSeqSource view over the
// multiplexer. Like objectCounterFor, the cut-1 stub returns 0; a
// future Stats() on MultiplexWriter wires real seqs.
func multiplexSeqSource(_ *eventlog.MultiplexWriter) eventLogSeqSource {
	return zeroSeqSource{}
}

type zeroSeqSource struct{}

func (zeroSeqSource) EventLogSeq(core.FederationName) uint64 { return 0 }

// momFederateJoinedHook returns the federation.Manager OnFederateJoined
// closure that forwards joins to MOM.FederateJoined. M13 thread B
// (docs/srs.md §10.4): the federate-type string the federate declared
// on its JoinFederationRequest is now plumbed through, so HLAfederate
// snapshots reflect HLAfederateType. Errors are logged but not
// propagated — MOM is a metric/introspection layer, not a
// federation-correctness gate.
func momFederateJoinedHook(momMgr core.ManagementObjectModel, logger *slog.Logger) func(context.Context, core.FederationName, core.FederateHandle, string, string) {
	return func(ctx context.Context, fed core.FederationName, h core.FederateHandle, federateName string, federateType string) {
		if err := momMgr.FederateJoined(ctx, fed, h, federateName, federateType); err != nil {
			logger.Warn("rtid: MOM FederateJoined hook failed",
				"federation", fed, "handle", h, "err", err)
		}
	}
}

// momFederateResignedHook is the resign-side analogue.
func momFederateResignedHook(momMgr core.ManagementObjectModel, logger *slog.Logger) func(context.Context, core.FederationName, core.FederateHandle) {
	return func(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
		if err := momMgr.FederateResigned(ctx, fed, h); err != nil {
			logger.Warn("rtid: MOM FederateResigned hook failed",
				"federation", fed, "handle", h, "err", err)
		}
	}
}

// chainOnFederateResigned composes multiple OnFederateResigned hooks
// into one. M21 TASK-204c: the federation.Manager exposes a single
// hook field, so cmd/rtid chains MOM's resign-side hook with the
// time-manager's pending-state cleanup.
func chainOnFederateResigned(
	hs ...func(context.Context, core.FederationName, core.FederateHandle),
) func(context.Context, core.FederationName, core.FederateHandle) {
	return func(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
		for _, h2 := range hs {
			if h2 != nil {
				h2(ctx, fed, h)
			}
		}
	}
}

// createFederationHook returns the gRPC OnCreateFederationSuccess
// closure that (a) populates the FOM repository's per-federation map
// for FOMRepoOrderLookup and (b) registers the HLAfederation MOM
// instance. The double parse on the FOM modules is intentional: the
// federation manager already validated them, so a Load failure here
// would be a programmer error — surfacing it would lie about the
// federation's existence (it has already been created).
func createFederationHook(foms *fomRepository, momMgr core.ManagementObjectModel, logger *slog.Logger) func(context.Context, core.FederationName, []core.FOMModule) {
	return func(ctx context.Context, name core.FederationName, modules []core.FOMModule) {
		h, err := foms.Load(ctx, modules)
		if err != nil {
			logger.Warn("rtid: post-CreateFederation FOM Load failed; "+
				"FOMRepoOrderLookup will default to TSO for this federation",
				"federation", name, "err", err)
			return
		}
		foms.RememberFor(name, h)
		if err := momMgr.FederationCreated(ctx, name, modules); err != nil {
			logger.Warn("rtid: MOM FederationCreated hook failed",
				"federation", name, "err", err)
		}
	}
}

// destroyFederationHook is the destroy-side analogue.
func destroyFederationHook(momMgr core.ManagementObjectModel, logger *slog.Logger) func(context.Context, core.FederationName) {
	return func(ctx context.Context, name core.FederationName) {
		if err := momMgr.FederationDestroyed(ctx, name); err != nil {
			logger.Warn("rtid: MOM FederationDestroyed hook failed",
				"federation", name, "err", err)
		}
	}
}
