// Command rtid runs the gorti RTI server.
//
// Wires the federation manager, declaration manager, object registry,
// and gRPC service handlers into a runnable server, plus a Prometheus
// metrics endpoint and optional runtime plugins.
// See engineering/specifications/current/SDD.md for the runtime design.
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
package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cbchoi/gorti/rti/experiments/cluster/manager"
	"github.com/cbchoi/gorti/rti/internal/adapters/auth/oidc"
	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"github.com/cbchoi/gorti/rti/internal/adapters/eventlog"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/adapters/observability"
	"github.com/cbchoi/gorti/rti/internal/application/buildinfo"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/application/configuration"
	"github.com/cbchoi/gorti/rti/internal/application/lifecycle"
	"github.com/cbchoi/gorti/rti/internal/application/runtimeplugin"
	"github.com/cbchoi/gorti/rti/internal/compatibility/researchconfig"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"github.com/cbchoi/gorti/rti/plugins/auditreplay"

	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Main runs the rtid process using the command-line arguments and environment.
// The cmd/rtid package intentionally delegates here so process wiring remains
// separate from the executable entry point.
func Main() {
	showVersion := flag.Bool("version", false, "print rtid version and exit")
	listen := flag.String("listen", ":8442", "gRPC listen address")
	metricsListen := flag.String("metrics-listen", ":9090", "Prometheus HTTP listen")
	outboxEventCapacity := flag.Int("outbox-event-capacity", defaultMultiEventCapacity, "buffered outbound events per federate (1..1048576)")
	outboxBatchSize := flag.Int("outbox-batch-size", defaultMultiBatchSize, "outbound event batch size (1..1024)")
	outboxFlushInterval := flag.Duration("outbox-flush-interval", defaultMultiFlushInterval, "maximum delay before flushing a partial outbound event batch (>0)")
	// W8 — gRPC transport buffer knobs for the federate-facing listener.
	// 0 = product default, -1 = gRPC library default (32KB), >0 = bytes.
	grpcWriteBuffer := flag.Int("grpc-write-buffer", defaultGRPCWriteBufferSize, "per-connection gRPC write buffer in bytes (0 = product default, -1 = gRPC library default)")
	grpcReadBuffer := flag.Int("grpc-read-buffer", defaultGRPCReadBufferSize, "per-connection gRPC read buffer in bytes (0 = product default, -1 = gRPC library default)")
	// Round-2 — Go runtime GC tuning (goruntime.go). Precedence:
	// explicit flag > GOGC env > product default 400. The flag default
	// shown here (400) only applies when GOGC is NOT set; passing the
	// flag explicitly always wins, including over a set GOGC.
	gcPercent := flag.Int("gc-percent", configuration.DefaultGCPercent, "Go GC target percentage (runtime/debug.SetGCPercent). Explicit flag beats the GOGC env var; without the flag, a set GOGC beats this default. -1 leaves the Go runtime default untouched.")
	goMemLimit := flag.Int64("go-mem-limit", 0, "Go soft memory limit in bytes (runtime/debug.SetMemoryLimit). 0 = no limit call (Go runtime default).")
	// M15 cut-2 demo flags. --cluster-peers is a comma-separated
	// list of node_id=host:port pairs identifying the other rtid
	// nodes in the cluster. --node-id is this node's stable ID; if
	// empty, falls back to --listen value. Single-node deployments
	// leave --cluster-peers empty and the cluster behaves
	// identically to pre-M15 (cut-1 semantics).
	nodeID := flag.String("node-id", "", "stable opaque ID for this node in the cluster (empty = use --listen value)")
	clusterPeers := flag.String("cluster-peers", "", "comma-separated list of node_id=host:port entries for other rtid nodes in the cluster (M15 cut-2 demo)")
	clusterAdvertise := flag.String("cluster-advertise", "", "host:port this node advertises to peers as its dial address (empty = use --listen value)")
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
	logDir := flag.String("log-dir", "", "directory for audit/replay plugin event logs")
	stateDir := flag.String("state-dir", "", "directory for persistent RTI core state such as federation generation fencing (empty = process-local epoch)")
	eventLogProtobufValidation := flag.Bool("event-log-protobuf-validation", true, "audit/replay plugin: check required Protobuf fields while marshaling records")
	auditReplayPlugin := flag.String("audit-replay-plugin", auditReplayDisabledName, "optional runtime plugin: none|event-journal")
	logLevel := flag.String("log-level", "info", "log level: debug|info|warn|error")
	logFormat := flag.String("log-format", "json", "log format: json|text")
	mode := flag.String("mode", serverRunMode, "rtid mode: server|pingpong-demo|timed-demo|replay-from-log")
	federationMode := flag.String("federation-mode", "verbose", "federation operating mode for created federations: verbose|best-effort (TASK-076)")
	pingpongRounds := flag.Int("pingpong-rounds", 1000, "rounds for pingpong-demo mode")
	pingpongFederation := flag.String("pingpong-federation", "pingpong", "federation name for pingpong-demo mode")
	pingpongDeterministic := flag.Bool("pingpong-deterministic", false, "use ManualClock so the event-log body is byte-deterministic across runs")
	timedTicks := flag.Int("timed-ticks", 100, "ticks (per-federate NER count) for timed-demo mode")
	timedFederation := flag.String("timed-federation", "timed", "federation name for timed-demo mode")
	timedDeterministic := flag.Bool("timed-deterministic", false, "use ManualClock so the event-log body is byte-deterministic across runs")
	timedStallSkipFederate := flag.Int("timed-stall-skip", 0, "1-based federate index to skip NER for, provoking a stall (0 = no skip)")
	timedStallTimeoutMs := flag.Int("timed-stall-timeout-ms", 5000, "stall timeout in milliseconds for timed-demo mode (only effective when -timed-stall-skip is set)")
	timedStallAdvanceMs := flag.Int("timed-stall-advance-ms", 0, "fake-clock advance in milliseconds AFTER the demo loop, before CheckStalls (0 = skip stall check)")
	replayInput := flag.String("replay-input", "", "source event-log file path for replay-from-log mode")
	tlsCert := flag.String("tls-cert", "", "path to TLS server cert PEM (enables TLS when set; clients dial grpcs://host:port). Requires --tls-key.")
	tlsKey := flag.String("tls-key", "", "path to TLS server key PEM. Required when --tls-cert is set.")
	// M14 W1 — mTLS: client cert verification.
	tlsClientCA := flag.String("tls-client-ca", "", "path to PEM bundle for verifying client certs (mTLS). When set, clients MUST present a cert signed by one of these CAs.")
	tlsClientCNAllow := flag.String("tls-client-cn-allow", "", "comma-separated list of client cert CommonNames to allow (M14). Empty → any cert signed by --tls-client-ca passes.")
	// M14 W4 — OIDC bearer token verification.
	oidcIssuer := flag.String("oidc-issuer", "", "OIDC issuer URL. Server fetches JWKS from <url>/.well-known/openid-configuration. Empty disables OIDC.")
	oidcAudience := flag.String("oidc-audience", "", "Required `aud` claim on incoming JWTs.")
	oidcJWKSPem := flag.String("oidc-jwks-pem", "", "Pre-pinned JWKS as PEM (alternative to --oidc-issuer for air-gapped deployments).")
	saveDir := flag.String("save-dir", "./gorti-saves", "directory under which federation save bundles are written + read (M9: FR-SR-1..5)")
	enableInteger64 := flag.Bool("enable-integer64", true, "enable explicit HLAinteger64Time federations on the single-server gRPC backend; omitted factory selection remains HLAfloat64Time")
	autoResignGrace := flag.Duration("auto-resign-grace", 5*time.Second, "reconnection grace for authenticated callback streams")
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
		//nolint:forbidigo // The --version CLI contract writes its result to stdout, not the log sink.
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
	pluginMode, err := parseAuditReplayPluginMode(*auditReplayPlugin)
	if err != nil {
		logger.Error("invalid --audit-replay-plugin", "value", *auditReplayPlugin, "err", err)
		os.Exit(2)
	}
	if err := validateAuditReplayPluginConfig(*mode, pluginMode, *logDir); err != nil {
		logger.Error("invalid audit/replay plugin configuration", "err", err)
		os.Exit(2)
	}

	// Round-2 GC tuning: translate the flag into the rtidConfig value.
	// flag.Visit only visits flags that were actually passed, so an
	// absent --gc-percent maps to 0 = "product-default policy" (a set
	// GOGC env wins; unset GOGC installs defaultGCPercent), while an
	// explicit flag value passes through verbatim and beats GOGC.
	// Explicit 0 is rejected up front — it is reserved for the policy
	// sentinel, and SetGCPercent(0) is never an intentional production
	// setting.
	cfgGCPercent := 0
	if flagExplicitlySet("gc-percent") {
		if *gcPercent == 0 {
			logger.Error("invalid --gc-percent", "value", 0,
				"err", "must be positive or -1 (0 is reserved for the product-default policy)")
			os.Exit(2)
		}
		cfgGCPercent = *gcPercent
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
	case serverRunMode, "":
		if *ddsDomainID < math.MinInt32 || *ddsDomainID > math.MaxInt32 {
			logger.Error("invalid --dds-domain-id", "value", *ddsDomainID)
			os.Exit(2)
		}
		runServerMain(logger, serverMainArgs{
			Listen:                        *listen,
			AdminListen:                   *adminListen,
			MetricsListen:                 *metricsListen,
			LogDir:                        *logDir,
			StateDir:                      *stateDir,
			AllowPartialEventLogProto:     !*eventLogProtobufValidation,
			AuditReplayPlugin:             pluginMode,
			TLSCert:                       *tlsCert,
			TLSKey:                        *tlsKey,
			TLSClientCA:                   *tlsClientCA,
			TLSClientCNAllow:              *tlsClientCNAllow,
			OIDCIssuer:                    *oidcIssuer,
			OIDCAudience:                  *oidcAudience,
			OIDCJWKSPem:                   *oidcJWKSPem,
			SaveDir:                       *saveDir,
			AutoResignGrace:               *autoResignGrace,
			ResearchConfigPath:            *researchConfig,
			AdminMutating:                 *adminMutating,
			AdminMutatingAllowNonLoopback: *adminMutatingAllowNonLoopback,
			EnableDDS:                     *enableDDS,
			EnableInteger64:               *enableInteger64,
			DDSDomainID:                   int32(*ddsDomainID), //nolint:gosec // G115: checked against MinInt32 and MaxInt32 before bootstrap.
			NodeID:                        *nodeID,
			ClusterPeers:                  *clusterPeers,
			ClusterAdvertise:              *clusterAdvertise,
			OutboxEventCapacity:           *outboxEventCapacity,
			OutboxBatchSize:               *outboxBatchSize,
			OutboxFlushInterval:           *outboxFlushInterval,
			GRPCWriteBufferSize:           *grpcWriteBuffer,
			GRPCReadBufferSize:            *grpcReadBuffer,
			GCPercent:                     cfgGCPercent,
			GoMemLimitBytes:               *goMemLimit,
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

type auditReplayPluginMode uint8

const (
	auditReplayPluginNone auditReplayPluginMode = iota
	auditReplayPluginEventJournal
)

func parseAuditReplayPluginMode(s string) (auditReplayPluginMode, error) {
	switch s {
	case auditReplayDisabledName, "":
		return auditReplayPluginNone, nil
	case "event-journal":
		return auditReplayPluginEventJournal, nil
	default:
		return auditReplayPluginNone, fmt.Errorf("unknown audit/replay plugin %q (want none|event-journal)", s)
	}
}

func (m auditReplayPluginMode) String() string {
	switch m {
	case auditReplayPluginNone:
		return auditReplayDisabledName
	case auditReplayPluginEventJournal:
		return "event-journal"
	default:
		return fmt.Sprintf("unknown(%d)", m)
	}
}

func validateAuditReplayPluginConfig(runMode string, plugin auditReplayPluginMode, logDir string) error {
	if plugin != auditReplayPluginNone && plugin != auditReplayPluginEventJournal {
		return fmt.Errorf("unknown audit/replay plugin %s", plugin)
	}
	if plugin == auditReplayPluginEventJournal {
		if runMode != serverRunMode && runMode != "" {
			return fmt.Errorf("--audit-replay-plugin=%s is supported only with --mode=server", plugin)
		}
		if logDir == "" {
			return errors.New("--audit-replay-plugin=event-journal requires --log-dir")
		}
	}
	if (runMode == serverRunMode || runMode == "") && plugin == auditReplayPluginNone && logDir != "" {
		return errors.New("--log-dir requires --audit-replay-plugin=event-journal in server mode")
	}
	return nil
}

func pluginFactories(mode auditReplayPluginMode, logDir string, allowPartialProto bool) []runtimeplugin.Factory {
	if mode != auditReplayPluginEventJournal {
		return nil
	}
	return []runtimeplugin.Factory{
		auditreplay.NewFactory(auditreplay.Config{
			Dir:               logDir,
			AllowPartialProto: allowPartialProto,
		}),
	}
}

// runReplayMain invokes the audit/replay plugin's replay tool and writes the
// captured stream into logDir. Used by the
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
	Listen        string
	AdminListen   string
	MetricsListen string
	LogDir        string
	StateDir      string
	// AllowPartialEventLogProto skips only the Protobuf required-field
	// initialization check. Event-log encoding and writes still run.
	AllowPartialEventLogProto bool
	AuditReplayPlugin         auditReplayPluginMode
	TLSCert                   string
	TLSKey                    string
	// M14 W1 — mTLS.
	TLSClientCA      string
	TLSClientCNAllow string
	// M14 W4 — OIDC.
	OIDCIssuer                    string
	OIDCAudience                  string
	OIDCJWKSPem                   string
	SaveDir                       string
	AutoResignGrace               time.Duration
	ResearchConfigPath            string
	AdminMutating                 bool
	AdminMutatingAllowNonLoopback bool
	// M19 Phase 1a (docs/m19-dds-adapter.md §4.4): plumbed all the
	// way down to the transport/grpc handler so CreateFederation
	// requests with transport_mode=DDS are accepted only when both
	// the build tag and the operator have opted in.
	EnableInteger64 bool
	EnableDDS       bool
	DDSDomainID     int32
	// M15 cut-2 demo cluster flags.
	NodeID              string
	ClusterPeers        string // comma-separated node_id=host:port
	ClusterAdvertise    string // host:port advertised to peers
	OutboxEventCapacity int
	OutboxBatchSize     int
	OutboxFlushInterval time.Duration
	// W8 — gRPC buffer knobs (0 = product default, -1 = library
	// default, >0 = bytes). Validated in newRTID.
	GRPCWriteBufferSize int
	GRPCReadBufferSize  int
	// Round-2 — Go runtime GC tuning, already translated by main()
	// (0 = product-default policy, -1 = leave runtime default, >0 =
	// explicit percentage; see goruntime.go). Validated again in
	// newRTID for non-CLI composers.
	GCPercent       int
	GoMemLimitBytes int64
}

// flagExplicitlySet reports whether the named flag was actually passed
// on the command line (flag.Visit only visits flags that were set).
// Used by the round-2 GC tuning to distinguish "operator explicitly
// chose --gc-percent=400" (beats a set GOGC env) from "flag left at
// its default 400" (a set GOGC env wins).
func flagExplicitlySet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func validateOutboxCLIConfig(eventCapacity, batchSize int, flushInterval time.Duration) error {
	if eventCapacity < 1 || eventCapacity > maxMultiEventCapacity {
		return fmt.Errorf("--outbox-event-capacity must be between 1 and %d (got %d)", maxMultiEventCapacity, eventCapacity)
	}
	if batchSize < 1 || batchSize > maxMultiBatchSize {
		return fmt.Errorf("--outbox-batch-size must be between 1 and %d (got %d)", maxMultiBatchSize, batchSize)
	}
	if eventCapacity < batchSize {
		return fmt.Errorf("--outbox-event-capacity must be at least --outbox-batch-size (got %d < %d)", eventCapacity, batchSize)
	}
	if flushInterval <= 0 {
		return fmt.Errorf("--outbox-flush-interval must be greater than zero (got %s)", flushInterval)
	}
	return nil
}

// runServerMain boots the gRPC server + metrics endpoint and blocks until
// SIGINT/SIGTERM. Extracted so main can dispatch on --mode.
func runServerMain(logger *slog.Logger, args serverMainArgs) {
	if err := validateOutboxCLIConfig(args.OutboxEventCapacity, args.OutboxBatchSize, args.OutboxFlushInterval); err != nil {
		logger.Error("rtid outbox configuration failed", "err", err)
		os.Exit(2)
	}
	if err := configuration.ValidateGoRuntimeCLIConfig(args.GCPercent, args.GoMemLimitBytes); err != nil {
		logger.Error("rtid Go runtime tuning configuration failed", "err", err)
		os.Exit(2)
	}
	if args.AuditReplayPlugin == auditReplayPluginNone {
		logger.Info(
			"rtid HLA core profile active; audit journal, replay, and AdminService.TailEvents are not loaded",
		)
	}

	tlsConfig, err := buildServerTLSWithMTLS(args.TLSCert, args.TLSKey, args.TLSClientCA, args.TLSClientCNAllow)
	if err != nil {
		logger.Error("rtid TLS configuration failed", "err", err)
		os.Exit(2)
	}

	// M14 W4 — OIDC bearer-token verifier. Built at startup; if any
	// flag misconfigures the verifier (bad PEM, unreachable issuer),
	// fail fast.
	oidcVerifier, err := buildOIDCVerifier(args.OIDCJWKSPem, args.OIDCAudience, args.OIDCIssuer)
	if err != nil {
		logger.Error("rtid OIDC configuration failed", "err", err)
		os.Exit(2)
	}
	if oidcVerifier != nil {
		logger.Info("rtid: OIDC bearer-token authentication enabled",
			"audience", args.OIDCAudience, "issuer", args.OIDCIssuer)
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
	validateMutatingAdminCLI(logger, args)

	generationEpoch, err := nextFederationGenerationEpoch(args.StateDir)
	if err != nil {
		logger.Error("rtid: federation generation epoch initialization failed", "err", err)
		os.Exit(1)
	}
	srv, err := newRTID(rtidConfig{
		ListenAddr:                    args.Listen,
		AdminListenAddr:               args.AdminListen,
		MetricsListenAddr:             args.MetricsListen,
		PluginFactories:               pluginFactories(args.AuditReplayPlugin, args.LogDir, args.AllowPartialEventLogProto),
		Logger:                        logger,
		TLSConfig:                     tlsConfig,
		OIDCVerifier:                  oidcVerifier,
		SaveDir:                       args.SaveDir,
		AutoResignGrace:               args.AutoResignGrace,
		Research:                      resolved,
		AdminMutating:                 args.AdminMutating,
		AdminMutatingAllowNonLoopback: args.AdminMutatingAllowNonLoopback,
		EnableDDS:                     args.EnableDDS,
		EnableInteger64:               args.EnableInteger64,
		DDSDomainID:                   args.DDSDomainID,
		NodeID:                        args.NodeID,
		ClusterPeers:                  args.ClusterPeers,
		ClusterAdvertise:              args.ClusterAdvertise,
		OutboxEventCapacity:           args.OutboxEventCapacity,
		OutboxBatchSize:               args.OutboxBatchSize,
		OutboxFlushInterval:           args.OutboxFlushInterval,
		GRPCWriteBufferSize:           args.GRPCWriteBufferSize,
		GRPCReadBufferSize:            args.GRPCReadBufferSize,
		GCPercent:                     args.GCPercent,
		GoMemLimitBytes:               args.GoMemLimitBytes,
		GenerationEpoch:               generationEpoch,
		GenerationSpan:                generationReservationSpan,
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

const federationGenerationEpochFilename = ".federation-generation-epoch"

func nextFederationGenerationEpoch(logDir string) (epoch uint32, err error) {
	if logDir == "" {
		var raw [4]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, err
		}
		maxBase := ^uint32(0) - generationReservationSpan + 1
		epoch := 1 + binary.LittleEndian.Uint32(raw[:])%maxBase
		return epoch, nil
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return 0, err
	}
	path := filepath.Join(logDir, federationGenerationEpochFilename)
	release, err := lockFederationGenerationEpoch(path + ".lock")
	if err != nil {
		return 0, fmt.Errorf("lock %s: %w", path, err)
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil {
			epoch = 0
			err = errors.Join(err, fmt.Errorf("unlock %s: %w", path, releaseErr))
		}
	}()

	var previous uint64
	contents, err := os.ReadFile(path)
	switch {
	case err == nil:
		previous, err = strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 32)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return 0, err
	}
	if previous > uint64(^uint32(0)-generationReservationSpan) {
		return 0, errors.New("federation generation epoch exhausted")
	}
	epoch = uint32(previous) + 1
	highWater := uint32(previous) + generationReservationSpan
	tmp, err := os.CreateTemp(logDir, ".federation-generation-epoch-*")
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := fmt.Fprintf(tmp, "%d\n", highWater); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := replaceFederationGenerationEpoch(tmpPath, path, logDir); err != nil {
		return 0, err
	}
	return epoch, nil
}

// Each persistent RTI process reserves a disjoint generation block before it
// starts. Crashes may leave gaps, but a later process cannot reuse a generation
// already published by an earlier process.
const generationReservationSpan uint32 = 1 << 16

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
	StallAdvanceMs    int       // post-loop ManualClock advance, then CheckStalls (0 = skip)
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
// but skips NER for the chosen one, then advances the ManualClock by
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
	// post-loop (advance ManualClock + CheckStalls). The runTimedDemo
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
// non-skipped ones NER once), advances the ManualClock past the
// configured stall timeout, then invokes CheckStalls. Returns the halt
// count and the recorded stalled-federate handle (0 if none).
//
// Lives here (not in timed.go) so the M2-style runtime helpers stay
// single-purpose; stall is a one-shot affair, not a loop.
func runTimedStall(ctx context.Context, args timedRunArgs) (int, core.FederateHandle, error) {
	clk := core.NewManualClock(time.Unix(0, 0))
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

	for h := core.FederateHandle(1); h <= 3; h++ {
		if err := rt.tm.EnableRegulation(ctx, core.FederationName(args.Federation), h, core.LogicalTime(1.0)); err != nil {
			return 0, 0, fmt.Errorf("EnableRegulation %d: %w", h, err)
		}
	}
	skip := core.FederateHandle(0)
	if args.StallSkipFederate > 0 {
		//nolint:gosec // G115: the positive int branch fits the uint64 handle representation.
		skip = core.FederateHandle(args.StallSkipFederate)
	}
	for h := core.FederateHandle(1); h <= 3; h++ {
		if h == skip {
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
	// PluginFactories is empty for the default HLA core profile.
	PluginFactories []runtimeplugin.Factory
	Logger          *slog.Logger
	// Zero values retain the production defaults for tests and internal
	// callers that predate these settings.
	OutboxEventCapacity int
	OutboxBatchSize     int
	OutboxFlushInterval time.Duration
	GenerationEpoch     uint32
	GenerationSpan      uint32
	// EnableInteger64 is opt-in until the independent SDK acceptance campaign
	// has verified the complete production composition.
	EnableInteger64 bool

	// W8 — per-connection gRPC transport buffers for the
	// federate-facing listener. Zero selects the product defaults
	// (defaultGRPCWriteBufferSize / defaultGRPCReadBufferSize), -1
	// forces the gRPC library default (no ServerOption appended),
	// positive values are bytes. See grpcbuffers.go.
	GRPCWriteBufferSize int
	GRPCReadBufferSize  int

	// Round-2 — Go runtime GC tuning, applied by newRTID via
	// applyGoRuntimeTuning (goruntime.go) so the real binary and every
	// newRTID composer (lrcbench harness, tests) inherit the same
	// product default. GCPercent: 0 = product-default policy (a set
	// GOGC env wins; unset GOGC installs defaultGCPercent=400), -1 =
	// never call debug.SetGCPercent, >0 = install that value (explicit
	// operator choice; beats GOGC). GoMemLimitBytes: 0 = no
	// debug.SetMemoryLimit call, >0 = soft memory limit in bytes.
	GCPercent       int
	GoMemLimitBytes int64

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
	// local/dev workloads). Construct via buildServerTLSWithMTLS or
	// by hand in tests; M14 W1 added mTLS support via the
	// --tls-client-ca flag.
	TLSConfig *tls.Config

	// OIDCVerifier, when non-nil, installs a bearer-token gRPC
	// interceptor that validates `authorization: Bearer <jwt>` on
	// every RPC. M14 W4 — supports RS256 against a pre-pinned PEM
	// public key (--oidc-jwks-pem); JWKS HTTP discovery is a future
	// cut. Stackable with TLSConfig (mTLS + OIDC both required when
	// both configured).
	OIDCVerifier *oidc.Verifier

	// SaveDir is the directory under which the savepoint.Manager
	// writes + reads federation save bundles (M9: FR-SR-1..5). Empty
	// string disables persistence (the manager is still composed but
	// any RequestFederationSave will fail with a stat-error wrapped
	// in core.ErrSaveBundleCorrupt — production callers should set
	// this; tests may leave it empty when they construct the manager
	// directly).
	SaveDir         string
	AutoResignGrace time.Duration

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

	// M15 cut-2 demo cluster fields.
	//
	// NodeID is this node's stable opaque identifier in the
	// cluster. Empty = fall back to ListenAddr.
	NodeID string
	// ClusterPeers is a comma-separated list of node_id=host:port
	// entries identifying peer rtid nodes. Empty = single-node
	// (cut-1 behavior).
	ClusterPeers string
	// ClusterAdvertise is the host:port this node advertises to
	// peers as its dial address. Empty = use ListenAddr.
	ClusterAdvertise string
}

// rtid is the composed runtime: gRPC server + metrics handler + the
// underlying core components. Construct via newRTID; run via Serve.
type rtid struct {
	cfg            rtidConfig
	logger         *slog.Logger
	startedAt      time.Time
	fedMgr         *federation.Manager
	declMgr        core.DeclarationManagement
	objReg         *object.Registry
	syncMgr        core.SyncCoordinator
	ownMgr         core.OwnershipCoordinator
	momMgr         core.ManagementObjectModel
	ddmMgr         core.DataDistributionManagement
	saveMgr        core.SavepointCoordinator
	timeMgr        core.TimeManager
	integerTime    *timepkg.ManagerOf[int64]
	integerObjects *object.RegistryOf[int64]
	integerSave    *savepoint.ManagerOf[int64]
	plugins        *runtimeplugin.Manager
	sessions       *session.Manager
	outbox         *multiOutbox
	grpcS          *stdgrpc.Server
	// adminS is the dedicated AdminService gRPC server bound to
	// cfg.AdminListenAddr. nil when admin is disabled.
	adminS  *stdgrpc.Server
	metrics *observability.Handler
	foms    *composition.Repository
	// M15 cut-2 demo cluster wiring. Always non-nil; single-node
	// mode keeps the manager empty of peers and behaves identically
	// to cut-1.
	clusterMgr *cluster.Manager
	clusterSvc *grpcsvc.ClusterService

	deliveryRetries          *deliveryRetryWorker
	synchronizationLifecycle *synchronizationLifecycle
	startOwnershipRetries    func(context.Context) func()
}

// newRTID composes all the components and returns a runnable rtid.
//
// Wiring order matters: the multiplex writer is shared by the federation
// manager and the object registry (both write to the same per-federation
// log files); the FOM repository is wired into the federation manager;
// the gRPC server is the last composer.
func newRTID(cfg rtidConfig) (*rtid, error) {
	a := &rtidAssembly{cfg: cfg}
	complete := false
	defer func() {
		if complete {
			return
		}
		if a.sessions != nil {
			a.sessions.Close()
		}
		if a.plugins != nil {
			_ = a.plugins.Close()
		}
	}()
	for _, initialize := range []func() error{
		a.initializeCore,
		a.initializeTime,
		a.initializeFederation,
		a.initializeOwnership,
		a.initializeRegions,
		a.initializeObjects,
		a.configureObjectLifecycle,
		a.initializeSave,
		a.initializeSessions,
		a.initializeCluster,
		a.configureMetrics,
		a.initializeObservers,
		a.configureMOMExecution,
		a.initializeGRPCServices,
		a.initializeGRPCTransport,
		a.initializeAdministration,
	} {
		if err := initialize(); err != nil {
			return nil, err
		}
	}
	complete = true
	return a.runtime(), nil
}

// rtidAssembly retains late-bound dependencies until all lifecycle hooks are wired.
// Its phases run once, in order, before any listener or background worker starts.
type rtidAssembly struct {
	cfg                       rtidConfig
	clock                     core.Clock
	plugins                   *runtimeplugin.Manager
	runtimeEventLog           core.EventLog
	foms                      *composition.Repository
	declMgr                   *declaration.Manager
	outbox                    *multiOutbox
	callbackReceipts          *grpcsvc.CallbackReceipts
	conveyedRegions           conveyedRegionLifecycle
	callbackOutbox            *conveyedCallbackOutbox
	momMgr                    *mom.Manager
	timeMgr                   *timepkg.Manager
	integerTime               *timepkg.ManagerOf[int64]
	timeLifecycle             *runtimeTimeLifecycle
	resolveTimeImplementation func(string) (string, error)
	fedMgr                    *federation.Manager
	prepareResign             func(context.Context, core.FederationName, core.FederateHandle, core.ResignAction) (federation.ResignPreparation, error)
	reportLostFederate        func(context.Context, core.MemberIdentity) error
	destroyingDispatch        func(context.Context, core.FederationName) error
	reservationResignHook     func(core.FederationName, core.FederateHandle)
	updateRates               *updateRateBridge
	sessions                  *session.Manager
	syncLifecycle             *synchronizationLifecycle
	lifecycleMOMObserver      *grpcsvc.MOMObserver
	momDispatcher             *mom.Dispatcher
	objServices               *runtimeObjects
	ownMgr                    *ownership.Manager
	syncMgr                   *syncpkg.Manager
	ddmMgr                    *ddm.Manager
	traffic                   *mom.TrafficAccumulator
	objReg                    *object.Registry
	integerObjects            *object.RegistryOf[int64]
	saveMgr                   *savepoint.Manager
	integerSave               *savepoint.ManagerOf[int64]
	saveAdmission             *runtimeSaveAdmission
	observationAdmission      saveObservationAdmission
	saveCoordinator           core.SavepointCoordinator
	checkpointAdmission       grpcsvc.CheckpointAdmission
	clusterMgr                *cluster.Manager
	clusterSvc                *grpcsvc.ClusterService
	acquireCallbackService    func(context.Context, core.FederationName) (func(), error)
	momObserver               *grpcsvc.MOMObserver
	ownershipReceipts         *grpcsvc.OwnershipCallbackReceipts
	integerBackends           *grpcsvc.TypedBackends[int64]
	grpcSrv                   *grpcsvc.Server
	gs                        *stdgrpc.Server
	metrics                   *observability.Handler
	startedAt                 time.Time
	adminGS                   *stdgrpc.Server
}

func (a *rtidAssembly) initializeCore() error {
	var err error
	if a.cfg.Logger == nil {
		a.cfg.Logger = slog.Default()
	}
	// Round-2 GC product default. Lives HERE — not in main() — so the
	// lrcbench harness and every other newRTID composer runs with the
	// exact GC configuration an untouched production rtid runs with.
	// A set GOGC env var always wins over the product default; an
	// explicit --gc-percent (translated by main()) wins over both.
	if err := configuration.ApplyGoRuntimeTuning(a.cfg.GCPercent, a.cfg.GoMemLimitBytes, a.cfg.Logger); err != nil {
		return fmt.Errorf("rtid: invalid Go runtime tuning: %w", err)
	}
	batchSize, flushInterval, err := resolveMultiOutboxConfig(a.cfg.OutboxBatchSize, a.cfg.OutboxFlushInterval)
	if err != nil {
		return fmt.Errorf("rtid: invalid outbox configuration: %w", err)
	}
	eventCapacity, err := resolveMultiOutboxCapacity(a.cfg.OutboxEventCapacity)
	if err != nil {
		return fmt.Errorf("rtid: invalid outbox configuration: %w", err)
	}
	if eventCapacity < batchSize {
		return fmt.Errorf(
			"rtid: invalid outbox configuration: event capacity must be at least batch size (got %d < %d)",
			eventCapacity,
			batchSize,
		)
	}
	a.clock = core.NewRealClock()

	// Plugins are constructed before the federation manager, but audit writers
	// resolve metadata lazily on their first event, after fedMgr is assigned.
	eventLogMetadata := func(fed core.FederationName) (uint64, core.Mode, uint64, bool) {
		if a.fedMgr == nil {
			return 0, core.ModeUnspecified, 0, false
		}
		return a.fedMgr.EventLogMetadataFor(fed)
	}
	a.plugins, err = runtimeplugin.Open(a.cfg.PluginFactories, runtimeplugin.Host{
		Clock:    a.clock,
		Logger:   a.cfg.Logger,
		Metadata: eventLogMetadata,
	})
	if err != nil {
		return err
	}
	a.runtimeEventLog = a.plugins.EventLog()

	a.foms = composition.NewFOMRepository()
	a.declMgr = declaration.New()
	a.declMgr.SetFOMRepository(a.foms)
	a.outbox = newRuntimeOutbox(a.cfg.Logger, eventCapacity, batchSize, flushInterval)
	a.callbackOutbox = newConveyedCallbackOutbox(a.outbox, func() *grpcsvc.CallbackReceipts { return a.callbackReceipts }, func() conveyedRegionLifecycle { return a.conveyedRegions })
	// M37 — §5.10-§5.13 registration / interaction advisories
	// flow through the shared outbox.
	a.declMgr.SetAdvisoryOutbox(a.callbackOutbox)

	// M11: MOM manager constructed BEFORE federation manager so the
	// federation manager's OnFederateJoined / OnFederateResigned hooks
	// can dispatch to MOM. The Outbox is shared with sync/ownership/
	// object — MOM uses it for the (cut-2 deferred) subscriber fan-out.
	a.momMgr, err = mom.New(mom.Options{
		Outbox:   a.callbackOutbox,
		EventLog: a.runtimeEventLog,
	})
	if err != nil {
		return err
	}
	return nil
}

func (a *rtidAssembly) initializeTime() error {
	var err error
	// M21 TASK-204: time.Manager is composed BEFORE federation.Manager
	// so the federation's OnFederateResigned hook can call into
	// timeMgr.OnFederateResign (TASK-204c) — pending NER/TAR/TARA/
	// NMRA/FQR state must drop when the federate leaves.
	// M17.26 — capture momMgr (constructed earlier) in the
	// OnTimeStateChanged hook so HLAfederate.HLAtimeRegulating /
	// HLAtimeConstrained / HLAlookahead / HLAlogicalTime mirror
	// the time.Manager's state.
	a.timeMgr, err = timepkg.New(timepkg.Options{
		Clock:    a.clock,
		Outbox:   a.callbackOutbox,
		EventLog: a.runtimeEventLog,
		OnTimeStateChanged: func(
			ctx context.Context,
			fed core.FederationName,
			h core.FederateHandle,
			regulating bool,
			constrained bool,
			lookahead core.LogicalTime,
			logicalTime core.LogicalTime,
		) {
			_ = a.momMgr.TimeStateChanged(
				ctx, fed, h, regulating, constrained,
				lookahead, logicalTime,
			)
		},
	})
	if err != nil {
		return fmt.Errorf("rtid: time manager init: %w", err)
	}
	a.integerTime, err = newIntegerTimeManager(a.clock, a.callbackOutbox, a.runtimeEventLog, a.momMgr)
	if err != nil {
		return fmt.Errorf("rtid: Integer64 time manager init: %w", err)
	}
	a.timeLifecycle = &runtimeTimeLifecycle{floating: a.timeMgr, integer: a.integerTime}
	a.resolveTimeImplementation = core.ResolveLogicalTimeImplementationName
	if a.cfg.EnableInteger64 {
		a.resolveTimeImplementation = func(name string) (string, error) {
			if name == integer64TimeName && (a.cfg.EnableDDS || a.cfg.ClusterPeers != "" || !supportsIntegerTimeStrategies(a.cfg.Research.Time.LBTS, a.cfg.Research.Time.Grant)) {
				return "", fmt.Errorf("%w: Integer64 requires the single-server gRPC standard-algorithm profile", core.ErrCouldNotCreateLogicalTimeFactory)
			}
			return core.ResolveLogicalTimeImplementationCapabilities(name, float64TimeName, integer64TimeName)
		}
	}
	return nil
}

func (a *rtidAssembly) initializeFederation() error {
	var err error
	// M24 W1+W2 — resign hooks are set after ownMgr/objReg are constructed
	// (a few lines down). The fedMgr captures function-pointer indirections
	// so dispatch resolves at runtime, after the dependencies are wired in.
	// M26 Phase F — same indirection pattern for the object registry's
	// reservation-cleanup hook (objReg is constructed below).
	a.fedMgr, err = federation.New(federation.Options{
		ResolveTimeImplementation: a.resolveTimeImplementation,
		Clock:                     a.clock,
		EventLog:                  a.runtimeEventLog,
		FOMs:                      a.foms,
		PrepareJoinFOM:            a.foms.PrepareLiveExtension,
		GenerationEpoch:           a.cfg.GenerationEpoch,
		GenerationSpan:            a.cfg.GenerationSpan,
		OnFederateJoined: lifecycle.ChainFederateJoined(
			// M27 Phase A — pre-bind the outbox channel for the
			// federate so events emitted by a service-group RPC fired
			// immediately after JoinFederation returns are buffered
			// rather than dropped. Runs before MOM so the channel
			// exists by the time the join RPC returns.
			func(_ context.Context, fed core.FederationName, h core.FederateHandle, _ string, _ string) {
				a.outbox.Bind(fed, h)
				if err := a.timeLifecycle.OnFederateJoined(fed, h); err != nil {
					a.cfg.Logger.Warn("MOM elapsed-time origin unavailable", "federation", fed, "federate", h, "error", err)
				}
			},
			momFederateJoinedHook(a.momMgr, a.cfg.Logger),
			func(ctx context.Context, fed core.FederationName, h core.FederateHandle, _ string, _ string) {
				if a.timeLifecycle.isInteger(fed) {
					observeIntegerTime(ctx, a.momMgr, fed, h, false, false, 0, 0)
				}
				if a.syncLifecycle != nil {
					_ = a.syncLifecycle.Joined(ctx, fed, h)
				}
			},
		),
		OnFederateResigned: lifecycle.ChainFederateResigned(
			momFederateResignedHook(a.momMgr, a.cfg.Logger),
			func(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
				if a.syncLifecycle != nil {
					_ = a.syncLifecycle.Resigned(ctx, fed, h)
				}
			},
			func(_ context.Context, fed core.FederationName, h core.FederateHandle) {
				a.declMgr.OnFederateResign(fed, h)
				if a.updateRates != nil {
					a.updateRates.FederateResigned(fed, h)
				}
			},
			// M27 Phase A — drop the outbox state if the federate
			// resigned without ever opening its Events stream. The
			// stream subscribe path's cancel func handles the case
			// where a stream WAS opened (idempotent vs Unbind).
			func(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
				retireOutbox(ctx, a.outbox, a.cfg.Logger, fed, h)
			},
			// M21 TASK-204c: drop time-mgr pending state on resign so a
			// pending NER/TAR/TARA/NMRA/FQR doesn't leak in nerStore.
			a.timeLifecycle.OnFederateResign,
			// M26 Phase F — drop object instance name reservations
			// owned by the resigning federate.
			func(_ context.Context, fed core.FederationName, h core.FederateHandle) {
				if a.reservationResignHook != nil {
					a.reservationResignHook(fed, h)
				}
			},
		),
		OnFederateResignOutcome: a.onFederateResignOutcome,
		// M24 W2 — action-aware pre-resign dispatch. Runs BEFORE the
		// roster mutation so ownership/object cleanup observes the
		// federate as still-joined. Indirection resolved below.
		PrepareResign: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction) (federation.ResignPreparation, error) {
			return a.prepareResign(ctx, fed, h, action)
		},
		OnFederateLost:         func(ctx context.Context, id core.MemberIdentity) error { return a.reportLostFederate(ctx, id) },
		OnFederateResigning:    a.onFederateResigning,
		OnFederationDestroying: a.onFederationDestroying,
	})
	if err != nil {
		return err
	}
	a.timeLifecycle.implementations = a.fedMgr
	return nil
}

func (a *rtidAssembly) initializeOwnership() error {
	var err error
	// Ownership assumptions target eligible publishers. The registry is wired
	// after the ownership manager, so resolve the object class at call time.
	ownershipClassOf := func(fed core.FederationName, obj core.ObjectHandle) core.ObjectClassHandle {
		if a.objServices == nil {
			return 0
		}
		return a.objServices.ClassOf(fed, obj)
	}
	subscribersResolver := ownership.NewPublisherResolver(ownershipClassOf, a.declMgr, a.foms)
	a.ownMgr, err = ownership.New(ownership.Options{
		Outbox: ownershipOutbox{next: a.callbackOutbox, project: func(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
			if a.objServices == nil {
				return nil, errors.New("ownership object registry unavailable")
			}
			_, mapped, err := a.objServices.selected(fed).ProjectKnownObjectAttributes(ctx, fed, recipient, obj, attrs)
			return mapped, err
		}, capture: func(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (object.KnownAttributeProjection, error) {
			if a.objServices == nil {
				return object.KnownAttributeProjection{}, errors.New("ownership object registry unavailable")
			}
			return a.objServices.selected(fed).CaptureKnownObjectAttributes(ctx, fed, recipient, obj, attrs)
		}},
		EventLog:              a.runtimeEventLog,
		Subscribers:           subscribersResolver,
		PublicationAttributes: ownership.NewPublicationAttributeResolver(ownershipClassOf, a.foms),
		PrepareAcquisition: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			if a.objServices == nil {
				return nil, errors.New("ownership object registry unavailable")
			}
			return a.objServices.selected(fed).PrepareOwnershipAcquisition(ctx, fed, caller, obj, attrs)
		},
		PrepareKnownAcquisition: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
			if a.objServices == nil {
				return nil, nil, errors.New("ownership object registry unavailable")
			}
			return a.objServices.selected(fed).PrepareKnownOwnershipAcquisition(ctx, fed, caller, obj, attrs)
		},
		PrepareKnownOperation: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
			if a.objServices == nil {
				return nil, nil, errors.New("ownership object registry unavailable")
			}
			return a.objServices.selected(fed).PrepareKnownOwnershipOperation(ctx, fed, caller, obj, attrs)
		},
		PrepareReleaseDenied: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			if a.objServices == nil {
				return nil, errors.New("ownership object registry unavailable")
			}
			return a.objServices.selected(fed).PrepareOwnershipReleaseDenied(ctx, fed, caller, obj, attrs)
		},
		PrepareDivestiture: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			if a.objServices == nil {
				return nil, errors.New("ownership object registry unavailable")
			}
			return a.objServices.selected(fed).PrepareOwnershipReleaseDenied(ctx, fed, caller, obj, attrs)
		},
		// Phase 3 research-platform: thread the resolved
		// NegotiationStrategy when --research-config is set; nil →
		// ownership.New falls back to defaultNegotiation, identical
		// to today's behavior.
		Strategy: a.cfg.Research.Ownership.Negotiation,
	})
	if err != nil {
		return err
	}
	a.declMgr.SetOnPublishObjectClass(a.ownMgr.ObjectClassPublished)
	a.syncMgr, err = syncpkg.New(syncpkg.Options{
		Outbox:             grpcsvc.NewMandatoryCallbackOutbox(a.outbox, func() *grpcsvc.CallbackReceipts { return a.callbackReceipts }),
		EventLog:           a.runtimeEventLog,
		Members:            a.fedMgr.MembersOf,
		RequireInvocations: true,
	})
	if err != nil {
		return err
	}
	a.syncLifecycle = newSynchronizationLifecycle(a.syncMgr, func(err error) {
		a.cfg.Logger.Warn("synchronization callback admission pending", "error", err)
	})

	// M21 TASK-204: timeMgr is constructed earlier (above the federation
	// manager) so the OnFederateResigned chain can call into it; see
	// the construction site above for rationale.
	return nil
}

func (a *rtidAssembly) initializeRegions() error {
	var err error
	// M10 W1: Data Distribution Management. The Manager is composed
	// here so the object.Registry can consult it on every update;
	// gRPC handlers for the DDMService are deferred to M10 W2 (the
	// proto definition is FROZEN at this cut, so DDM operations are
	// reachable only via the in-process API for now).
	a.ddmMgr, err = ddm.New(ddm.Options{
		ValidateSubscriptionOptions:       a.declMgr.ValidateSubscriptionOptions,
		OnObjectSubscriptionInterest:      a.declMgr.SetRegionalObjectSubscriptionInterest,
		OnInteractionSubscriptionInterest: a.declMgr.SetRegionalInteractionSubscriptionInterest,
		OnObjectSubscribed: func(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) {
			a.momMgr.ObjectClassSubscribed(ctx, fed, sub, cls, attrs)
			a.objServices.ObjectClassSubscribed(ctx, fed, sub, cls, attrs)
			if err := refreshMOMClassMetrics(ctx, fed, cls, a.momMgr, a.foms, a.fedMgr.MembersOf); err != nil {
				a.cfg.Logger.Warn("MOM regional subscribed metrics unavailable", "federation", fed, "federate", sub, "error", err)
			}
		},
		Outbox:   a.callbackOutbox,
		EventLog: a.runtimeEventLog,
		FOMs:     a.foms,
		Objects: regionAssociationValidatorFunc(func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, func(), error) {
			if a.objServices == nil {
				return 0, nil, core.ErrObjectHandleInvalid
			}
			return a.objServices.selected(fed).PrepareRegionAssociation(ctx, fed, caller, obj, attrs)
		}),
		PrepareKnownAssociation: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, []core.AttributeHandle, map[core.AttributeHandle]core.FederateHandle, func(), error) {
			if a.objServices == nil {
				return 0, nil, nil, nil, core.ErrObjectHandleInvalid
			}
			return a.objServices.selected(fed).PrepareKnownRegionAssociation(ctx, fed, caller, obj, attrs)
		},
		FederationExists: func(fed core.FederationName) bool {
			_, ok := a.fedMgr.GenerationFor(fed)
			return ok
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func (a *rtidAssembly) initializeObjects() error {
	var err error
	// M20.3 — HLAmanager interaction dispatch. The dispatcher
	// intercepts incoming sendInteraction calls whose class is in
	// the HLAmanager subtree and routes to the matching handler
	// (HLAsetSwitches, HLArequest*, etc.) instead of broadcasting.
	// M20.6 wires the production ResponseEmitter so HLAreport*
	// responses from HLArequest* handlers reach the wire via the
	// shared outbox.
	a.conveyedRegions = a.ddmMgr
	a.momDispatcher = mom.NewDispatcher(a.momMgr)
	a.traffic = mom.NewTrafficAccumulator()
	if err := a.momMgr.SetTrafficAccumulator(a.traffic); err != nil {
		return err
	}
	a.momDispatcher.SetCounterSource(a.traffic.Snapshot)
	a.momDispatcher.SetEmitter(mom.NewProductionEmitter(a.callbackOutbox))
	objectOptions := object.Options{
		EventLog:           a.runtimeEventLog,
		Declarations:       a.declMgr,
		Outbox:             a.callbackOutbox,
		FOMs:               a.foms,
		Clock:              a.clock,
		ManagementDispatch: a.momDispatcher,
		// TASK-077: wire mode + per-attribute order lookup so the
		// registry can choose RO over TSO when both the federation
		// is best-effort AND the FOM declares the attribute as
		// Receive-order. The production fomHandle does not yet
		// implement FOMOrderResolver, so FOMRepoOrderLookup returns
		// "unknown" for every attribute and the registry stays on
		// the TSO path until a future cut upgrades the handle.
		Federations: a.fedMgr,
		Orders:      grpcsvc.FOMRepoOrderLookup{Repo: a.foms},
		// M8 W1: notify ownership.Manager of every successful object
		// registration so QueryOwnership / IsOwnedBy reflect the
		// producing federate as the initial owner of all class
		// attributes (FR-OWN-5).
		OnRegister: func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			a.ownMgr.RegisterInitialOwnership(fed, owner, obj, attrs)
		},
		OnObjectRegistered: func(ctx context.Context, fed core.FederationName, _ core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, _ []core.AttributeHandle) {
			if a.updateRates != nil {
				a.updateRates.ObjectChanged(ctx, fed, obj)
			}
		},
		OnDelete: func(fed core.FederationName, obj core.ObjectHandle) {
			a.ownMgr.OnObjectDeleted(fed, obj)
			if a.updateRates != nil {
				a.updateRates.ObjectDeleted(context.Background(), fed, obj)
			}
		},
		// M38 GB — §6.6 per-instance ownership gate: UpdateAttributes
		// requires the producer to be the CURRENT §7 owner of every
		// updated attribute (publication stays the §5 class-level
		// precondition, necessary but not sufficient). ownMgr
		// satisfies object.InstanceAttributeOwnership via
		// core.OwnershipCoordinator.IsOwnedBy; state is seeded by the
		// OnRegister hook above and mutated by the §7 divest/acquire
		// flows. The internal MOM producer is exempt at the gate.
		Ownership: a.ownMgr,
		// M11: per-federate MOM counters (FR-MOM-1). See momCounterHooks.
		OnUpdateSent:           a.momMgr.IncrementUpdatesSent,
		OnInteractionSent:      a.momMgr.IncrementInteractionsSent,
		OnReflectDelivered:     a.momMgr.IncrementReflectionsReceived,
		OnInteractionDelivered: a.momMgr.IncrementInteractionsReceived,
		OnObjectSentTransport: func(fed core.FederationName, h core.FederateHandle, cls core.ObjectClassHandle, transport core.TransportType) {
			_ = a.traffic.Record(fed, h, mom.UpdatesSent, uint64(cls), transport)
		},
		OnObjectInstanceTraffic: func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, incoming bool) {
			if incoming {
				return
			}
			_ = a.traffic.RecordObjectTraffic(fed, h, obj, cls, incoming)
		},
		OnInteractionSentTransport: func(fed core.FederationName, h core.FederateHandle, cls core.InteractionClassHandle, transport core.TransportType) {
			_ = a.traffic.Record(fed, h, mom.InteractionsSent, uint64(cls), transport)
		},
		// M10: DDM-aware fan-out filter. The adapter wraps
		// ddm.Manager into the object.DDMFilter shape (see
		// ddmFilterAdapter below for the handle-conversion glue).
		DDM:                 ddmFilterAdapter{m: a.ddmMgr},
		RegistrationRegions: regionRegistrar(a.ddmMgr),
		// M22 W2 — TSO delivery gate. The time manager satisfies
		// core.TSODeliveryGate; when nil the registry falls back to
		// pre-M22 always-async behavior. Wiring it here makes the
		// IEEE 1516.1 §8.16-8.17 default (async OFF) observable
		// cross-process.
		TSOGate:     a.timeMgr,
		Retractions: a.timeMgr,
		// M37 EB-3 — outgoing-TSO timestamp validation (§8.1.2):
		// regulating senders may not stamp TSO messages below
		// currentTime + lookahead. The time manager satisfies
		// object.OutgoingTSOValidator directly.
		TSOValidator: a.timeMgr,
	}
	a.objReg, err = object.New(objectOptions)
	if err != nil {
		return err
	}
	a.integerObjects, err = newIntegerObjectRegistry(objectOptions, a.integerTime)
	if err != nil {
		return fmt.Errorf("rtid: Integer64 object registry init: %w", err)
	}
	a.objServices = &runtimeObjects{floating: a.objReg, integer: a.integerObjects, implementations: a.fedMgr}
	a.declMgr.SetObjectUnpublication(a.objServices, a.ownMgr)

	a.timeMgr.SetDeletionLifecycle(a.objReg)
	a.timeMgr.SetQueuedMessageFilter(a.objReg)
	if err := configureMOMCatalog(a.momDispatcher, momCatalogDependencies{
		Manager: a.momMgr, Traffic: a.traffic, Declarations: a.declMgr, Objects: a.objReg, ObjectServices: a.objServices,
		Ownership: a.ownMgr, Synchronization: a.syncMgr, Regions: a.ddmMgr,
		FOMs: a.foms, Federations: a.fedMgr,
		JoinedModules: func(ctx context.Context, fed core.FederationName, h core.FederateHandle) ([]mom.FOMModule, error) {
			modules, err := a.fedMgr.JoinedModules(ctx, fed, h)
			if err != nil {
				return nil, err
			}
			return momModulesFromSource(modules)
		},
	}); err != nil {
		return err
	}
	return nil
}

func (a *rtidAssembly) configureObjectLifecycle() error {
	a.updateRates = newUpdateRateBridge(a.foms, a.declMgr, a.objServices, a.ddmMgr, a.ownMgr)

	// M36 — MOM object-instance fan-out: the MOM manager registers
	// HLAfederation/HLAfederate instances through the standard object
	// registry (discover/reflect/remove ride the normal fan-out), and
	// the declaration manager's post-subscribe hook delivers
	// retroactive Discover+Reflect to late subscribers of the MOM
	// object classes. Wired here, AFTER objReg exists (the MOM
	// manager itself is constructed before the registry).
	a.momMgr.EnableInstanceFanout(a.objServices, a.declMgr, a.foms)
	publisher, err := newMOMFederateAttributePublisher(a.objServices, a.foms, a.fedMgr.MembersOf)
	if err != nil {
		return err
	}
	if err := a.momMgr.SetFederateAttributePublisher(publisher); err != nil {
		return err
	}
	// M37 EB-4 — chain the post-subscribe hook: the MOM manager sends
	// retroactive Discover+Reflect for its own (internal-producer)
	// instances; the object registry sends retroactive §6.9 Discover
	// for federate-registered instances (subscribe-after-register).
	// The registry path skips internal-producer instances so late MOM
	// subscribers never see double discovers.
	a.declMgr.SetOnSubscribeObjectClass(func(
		ctx context.Context,
		fed core.FederationName,
		sub core.FederateHandle,
		cls core.ObjectClassHandle,
		attrs []core.AttributeHandle,
	) {
		a.momMgr.ObjectClassSubscribed(ctx, fed, sub, cls, attrs)
		a.objServices.ObjectClassSubscribed(ctx, fed, sub, cls, attrs)
		if err := refreshMOMClassMetrics(ctx, fed, cls, a.momMgr, a.foms, a.fedMgr.MembersOf); err != nil {
			a.cfg.Logger.Warn("MOM subscribed metrics unavailable", "federation", fed, "federate", sub, "error", err)
		}
	})

	// M26 Phase F — bind the reservation-resign indirection now that
	// objReg exists.
	a.reservationResignHook = a.objServices.OnFederateResign

	a.prepareResign = func(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction) (federation.ResignPreparation, error) {
		if a.timeLifecycle.isInteger(fed) {
			return lifecycle.PrepareResignOf[int64](ctx, fed, h, action, a.integerObjects, a.ownMgr, a.outbox, a.integerTime)
		}
		return lifecycle.PrepareResign(ctx, fed, h, action, a.objReg, a.ownMgr, a.outbox, a.timeMgr)
	}
	return nil
}

func (a *rtidAssembly) initializeSave() error {
	var err error
	// Compose checkpoints after all state-owning services exist. Restore is
	// snapshot-based; an audit event log is not a substitute for missing state.
	if a.cfg.SaveDir != "" {
		saveDependencies := runtimeSaveDependencies{
			directory: a.cfg.SaveDir, logger: a.cfg.Logger, outbox: a.outbox, journal: a.runtimeEventLog,
			foms: a.foms, federations: a.fedMgr, synchronization: a.syncMgr, ownership: a.ownMgr,
			management: a.momMgr, regions: a.ddmMgr, declarations: a.declMgr,
			receipts: func() *grpcsvc.CallbackReceipts { return a.callbackReceipts },
		}
		a.saveMgr, err = newTypedSaveManager[core.LogicalTime](saveDependencies, a.objReg, a.timeMgr)
		if err != nil {
			return err
		}
		a.integerSave, err = newTypedSaveManager[int64](saveDependencies, a.integerObjects, a.integerTime)
		if err != nil {
			return err
		}
		a.saveAdmission = &runtimeSaveAdmission{implementations: a.fedMgr, floating: a.saveMgr, integer: a.integerSave}
		a.observationAdmission = a.saveAdmission
		a.checkpointAdmission = newScheduledCheckpointAdmission(a.saveAdmission, a.cfg.Logger)
		a.saveCoordinator = a.saveMgr
	}
	a.momMgr.SetBackgroundAdmission(func(_ context.Context, fed core.FederationName) (func(), error) {
		checkpointRelease := func() {}
		if a.checkpointAdmission != nil {
			var err error
			checkpointRelease, err = a.checkpointAdmission.AcquireService(fed)
			if err != nil {
				return nil, err
			}
		}
		generation, exists := a.fedMgr.GenerationFor(fed)
		if !exists {
			checkpointRelease()
			return nil, core.ErrFederationNotFound
		}
		release, err := a.fedMgr.AcquireGeneration(fed, generation)
		if err != nil {
			checkpointRelease()
			return nil, err
		}
		return func() { release(); checkpointRelease() }, nil
	})
	a.reportLostFederate = func(ctx context.Context, id core.MemberIdentity) error {
		model, err := a.foms.Get(ctx, id.Federation)
		if err != nil {
			return err
		}
		if family, _ := a.fedMgr.LogicalTimeImplementationFor(id.Federation); family == core.ScalarRepresentation[int64]().String() {
			return reportLostFederateOf(ctx, id, a.integerTime, a.integerSave, a.momDispatcher, model)
		}
		return reportLostFederateOf(ctx, id, a.timeMgr, a.saveMgr, a.momDispatcher, model)
	}
	return nil
}

func (a *rtidAssembly) initializeSessions() error {
	var err error
	a.sessions, err = session.New(session.Options{Membership: a.fedMgr, Grace: a.cfg.AutoResignGrace, AcquireLoss: func(fed core.FederationName) (func(), error) {
		if a.saveAdmission != nil {
			release, err := a.saveAdmission.AcquireLoss(fed)
			if err != nil {
				return nil, err
			}
			return func() {
				release()
				if err := a.saveAdmission.ProgressScheduledSave(context.Background(), fed); err != nil {
					a.cfg.Logger.Error("fault recovery time progress failed", "federation", fed, "error", err)
				}
			}, nil
		}
		return func() {}, nil
	}, AcquireService: func(fed core.FederationName) (func(), error) {
		if a.checkpointAdmission != nil {
			return a.checkpointAdmission.AcquireService(fed)
		}
		return func() {}, nil
	}, OnError: func(err error) { a.cfg.Logger.Error("automatic resign failed", "error", err) }})
	if err != nil {
		return err
	}
	return nil
}

func (a *rtidAssembly) initializeCluster() error {
	// M15 cut-2 — construct cluster manager + service BEFORE the
	// grpc handlers so the federation-creation hook can reference
	// them. The service is registered on the gs server later in
	// this function (after gs is created).
	clusterAdvAddr := a.cfg.ClusterAdvertise
	if clusterAdvAddr == "" {
		clusterAdvAddr = a.cfg.ListenAddr
	}
	a.clusterMgr = cluster.New(a.cfg.NodeID, clusterAdvAddr)
	peers, err := parseClusterPeers(a.cfg.ClusterPeers)
	if err != nil {
		return fmt.Errorf("rtid: --cluster-peers: %w", err)
	}
	for nodeID, address := range peers {
		a.clusterMgr.RegisterPeer(nodeID, address)
	}
	a.clusterSvc = grpcsvc.NewClusterService(a.clusterMgr, nil)
	a.clusterSvc.SetGenerationResolver(a.fedMgr.GenerationFor)
	a.destroyingDispatch = func(ctx context.Context, fed core.FederationName) error {
		var cleanupErrors []error
		a.sessions.ClearFederation(fed)
		if a.callbackReceipts != nil {
			a.callbackReceipts.ClearFederation(fed)
		}
		if err := a.momMgr.FederationDestroyed(ctx, fed); err != nil {
			a.cfg.Logger.Error("rtid: MOM federation cleanup failed", "federation", fed, "error", err)
			cleanupErrors = append(cleanupErrors, err)
		}
		a.declMgr.OnFederationDestroyed(fed)
		a.objReg.OnFederationDestroyed(fed)
		a.integerObjects.OnFederationDestroyed(fed)
		a.timeMgr.OnFederationDestroyed(fed)
		a.integerTime.OnFederationDestroyed(fed)
		a.syncMgr.OnFederationDestroyed(fed)
		a.ownMgr.OnFederationDestroyed(fed)
		a.ddmMgr.OnFederationDestroyed(fed)
		if a.saveMgr != nil {
			a.saveMgr.OnFederationDestroyed(fed)
			a.integerSave.OnFederationDestroyed(fed)
		}
		a.foms.ForgetFor(fed)
		a.clusterMgr.UnassignFederation(fed)
		a.outbox.UnbindFederation(fed)
		// Optional observers close last. Their failures are logged by the
		// plugin manager and never change federation teardown semantics.
		a.plugins.CloseFederation(fed)
		return errors.Join(cleanupErrors...)
	}
	return nil
}

func (a *rtidAssembly) configureMetrics() error {
	if err := configureMOMMetrics(a.momMgr, momMetricDependencies{
		Catalog: momCatalogDependencies{Manager: a.momMgr, Traffic: a.traffic, Declarations: a.declMgr, Objects: a.objReg, ObjectServices: a.objServices, Ownership: a.ownMgr, Synchronization: a.syncMgr, Regions: a.ddmMgr, FOMs: a.foms, Federations: a.fedMgr},
		Time:    a.timeMgr, Save: a.saveMgr, IntegerTime: a.integerTime, IntegerSave: a.integerSave, RTIVersion: rtidVersion(), TimeImplementation: a.fedMgr.LogicalTimeImplementationFor,
		Host:          a.sessions.Host,
		CurrentFDD:    a.foms.CurrentFDD,
		MIMDesignator: a.foms.MIMDesignator,
		DataQueues: func(ctx context.Context, fed core.FederationName, h core.FederateHandle) (core.DataQueueCounts, error) {
			if a.callbackReceipts == nil {
				return core.DataQueueCounts{}, fmt.Errorf("MOM: callback queue source unavailable")
			}
			if a.timeLifecycle.isInteger(fed) {
				return a.integerTime.DataQueueMetrics(ctx, fed, h, a.callbackReceipts)
			}
			return a.timeMgr.DataQueueMetrics(ctx, fed, h, a.callbackReceipts)
		},
	}); err != nil {
		return err
	}
	a.acquireCallbackService = func(_ context.Context, fed core.FederationName) (func(), error) {
		checkpointRelease := func() {}
		if a.saveMgr != nil {
			checkpointRelease = a.saveAdmission.AcquireObservation(fed)
		}
		generation, exists := a.fedMgr.GenerationFor(fed)
		if !exists {
			checkpointRelease()
			return nil, core.ErrFederationNotFound
		}
		release, err := a.fedMgr.AcquireGeneration(fed, generation)
		if err != nil {
			checkpointRelease()
			return nil, err
		}
		return func() { release(); checkpointRelease() }, nil
	}
	a.syncLifecycle.retryAdmission = a.acquireCallbackService
	return nil
}

func (a *rtidAssembly) initializeObservers() error {
	var err error
	a.momObserver, a.callbackReceipts, err = newMOMRuntimeObservers(momObservationDependencies{
		Manager: a.momMgr, Dispatcher: a.momDispatcher, FOMs: a.foms, Authenticate: a.sessions.Authenticate,
		CallbackEntry: momDataCallbackEntryObserver(a.momMgr, conveyedCallbackEntryObserver(a.ddmMgr, syncCheckpointEntryObserver(a.saveAdmission, syncCallbackEntryObserver(a.syncMgr))), func(err error) {
			a.cfg.Logger.Debug("MOM callback entry measurement unavailable", "error", err)
		}),
		DataCallbacksCountedAtEntry: true,
		LocalDataCallbackEntry:      true,
		DataCallbackReports:         newDataCallbackReportBatch(a.momMgr, a.fedMgr, a.observationAdmission, a.sessions.Authenticate),
		CallbackInvoked:             conveyedCallbackOutcomeObserver(a.ddmMgr, syncCallbackInvocationObserver(a.syncMgr)),
		ValidateRequiredRecipient: func(_ context.Context, fed core.FederationName, h core.FederateHandle) error {
			_, err := a.fedMgr.MemberIdentity(fed, h)
			return err
		},
		ResolveCaller: newMOMCallerResolver(a.sessions, a.fedMgr, func(fed core.FederationName) func() {
			if a.saveMgr != nil {
				return a.saveAdmission.AcquireObservation(fed)
			}
			return func() {}
		}),
		AcquireService: a.acquireCallbackService,
		OnError:        func(err error) { a.cfg.Logger.Debug("MOM service observation unavailable", "error", err) },
	})
	if err != nil {
		return err
	}
	return nil
}

func (a *rtidAssembly) configureMOMExecution() error {
	var err error
	momExecution := momServiceExecutionDependencies{
		Federations: a.fedMgr, Declarations: a.declMgr, FOMs: a.foms, Ownership: a.ownMgr,
		Synchronization: a.syncMgr, FloatTime: a.timeMgr, IntegerTime: a.integerTime,
		FloatObjects: a.objReg, IntegerObjects: a.integerObjects,
		Admission: admittedMOMServiceExecution, Resign: a.fedMgr.ResignFederationIdentity,
	}
	momExecution.TransportationConfirmed, err = newMOMTransportationConfirmation(momTransportationConfirmationDependencies{
		Federations: a.fedMgr, Float: a.objReg, Integer: a.integerObjects,
		CallbackEpoch: a.outbox.CallbackEpoch,
		Outbox:        grpcsvc.NewMandatoryCallbackOutbox(a.outbox, func() *grpcsvc.CallbackReceipts { return a.callbackReceipts }),
	})
	if err != nil {
		return err
	}
	if a.saveMgr != nil {
		momExecution.FloatSave = a.saveMgr
	}
	if a.integerSave != nil {
		momExecution.IntegerSave = a.integerSave
	}
	if err := configureMOMServiceExecution(a.momDispatcher, momExecution); err != nil {
		return err
	}
	a.lifecycleMOMObserver = a.momObserver
	a.ownershipReceipts, err = newMandatoryOwnershipReceipts(a.ownMgr, a.fedMgr, a.outbox, a.observationAdmission, a.sessions.Authenticate)
	if err != nil {
		return err
	}
	a.integerBackends = &grpcsvc.TypedBackends[int64]{Objects: a.integerObjects, Time: a.integerTime}
	if a.integerSave != nil {
		a.integerBackends.Savepoint = a.integerSave
	}
	return nil
}

func (a *rtidAssembly) initializeGRPCServices() error {
	var err error
	a.grpcSrv, err = grpcsvc.NewServer(grpcsvc.Options{
		Integer64: a.integerBackends, TimeImplementations: a.fedMgr,
		ResolveTimeImplementation: a.resolveTimeImplementation,
		CallbackReceipts:          a.callbackReceipts, MOMObserver: a.momObserver,
		OwnershipReceipts:    a.ownershipReceipts,
		Sessions:             a.sessions,
		LocalValidationState: newLocalValidationSource(a.fedMgr, a.foms, a.declMgr, a.objReg, a.ownMgr, a.outbox, a.sessions.Authenticate),
		NormalizationMembers: a.fedMgr.MembersOf,
		Federations:          a.fedMgr,
		Membership: newMOMLocalLRCMembership(momServiceAdmissionDependencies{
			Dispatcher: a.momDispatcher, FOMs: a.foms, Federations: a.fedMgr,
			Declarations: a.declMgr, Authenticate: a.sessions.Authenticate, Checkpoint: a.checkpointAdmission,
		}),
		CheckpointAdmission:         a.checkpointAdmission,
		Declarations:                a.declMgr,
		Objects:                     a.objReg,
		Outbox:                      a.outbox,
		EnableInteractionStream:     a.cfg.OIDCVerifier == nil,
		EnableConfirmedObjectStream: true,
		EnableLocalLRC:              a.cfg.OIDCVerifier == nil,
		OnCreateFederationSuccess:   createFederationHook(a.fedMgr, a.foms, a.momMgr, a.clusterMgr, a.clusterSvc, a.cfg.Logger),
		PrepareFederation: func(_ context.Context, fed core.FederationName) error {
			if a.saveMgr == nil {
				return nil
			}
			return a.saveAdmission.InitializeSaveHistory(fed)
		},
		OnJoinFederationSuccess: func(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
			release, err := a.fedMgr.AcquireMember(fed, h)
			if err != nil {
				a.cfg.Logger.Warn("MOM joined-member update unavailable", "federation", fed, "federate", h, "error", err)
				return
			}
			defer release()
			for _, target := range []core.FederateHandle{h, 0} {
				if err := a.momMgr.RefreshFOMModules(ctx, fed, target); err != nil {
					a.cfg.Logger.Warn("MOM FOM module report unavailable", "federation", fed, "federate", target, "error", err)
				}
				if err := a.momMgr.RefreshMetrics(ctx, fed, target); err != nil {
					a.cfg.Logger.Warn("MOM joined-member metrics unavailable", "federation", fed, "federate", target, "error", err)
				}
			}
		},
		// M21 TASK-204: TimeService gRPC. Composed unconditionally
		// in server mode (vs cut-1's nil placeholder) so federates
		// can drive HLA time advance cross-process.
		Time: a.timeMgr,
		// M12 W1: cut-3 gRPC services. Save manager is optional —
		// only wired when --save-dir is set (saveMgr == nil otherwise).
		Sync:      a.syncMgr,
		Ownership: a.ownMgr,
		DDM:       a.ddmMgr,
		Savepoint: a.saveCoordinator,
		// M12 W3: MomService — federates introspect MOM-tracked
		// state (HLAfederation / HLAfederate object snapshots +
		// per-federate counters). Read-only; lives on the federate
		// port (this --listen) since it is federate-facing
		// introspection, not operator-facing observability.
		MOM: a.momMgr,
		// M19 Phase 1a (docs/m19-dds-adapter.md §4.4): wire the
		// DDS opt-in flags into the federation handler so
		// CreateFederation rejects transport_mode=DDS unless the
		// operator opted in. TransportLookup feeds the manager's
		// recorded mode back to JoinFederationResponse so federates
		// pick the right wire path.
		DDSEnabled:         a.cfg.EnableDDS,
		DDSDefaultDomainID: a.cfg.DDSDomainID,
		TransportLookup:    a.fedMgr.TransportFor,
		// M25 Phase B (§10.2): SupportService — read-only handle /
		// name / dimension / order / transport lookups against the
		// per-federation FOM. Same repository the federation manager
		// already uses.
		FOMs: a.foms,
	})
	if err != nil {
		return err
	}
	return nil
}

func (a *rtidAssembly) initializeGRPCTransport() error {
	var serverOpts []stdgrpc.ServerOption
	// W8 — gRPC buffer knobs. Zero config selects the tuned product
	// defaults (1MB write / 1MB read, adopted from the isolated A/B
	// in grpcbuffers.go); -1 per knob appends NOTHING, restoring the
	// byte-identical pre-W8 ServerOption set (unit-tested in
	// grpcbuffers_test.go).
	bufferOpts, err := grpcBufferServerOptions(a.cfg.GRPCWriteBufferSize, a.cfg.GRPCReadBufferSize)
	if err != nil {
		return fmt.Errorf("rtid: invalid gRPC buffer configuration: %w", err)
	}
	serverOpts = append(serverOpts, bufferOpts...)
	if a.cfg.TLSConfig != nil {
		// credentials.NewTLS clones the *tls.Config internally, so the
		// caller is free to keep mutating its copy without affecting
		// the live server. Static-cert only at this cut: cfg.TLSConfig
		// is built from --tls-cert/--tls-key and not reloaded.
		serverOpts = append(serverOpts, stdgrpc.Creds(credentials.NewTLS(a.cfg.TLSConfig)))
	}
	membershipInterceptor, err := newMOMServiceAdmissionInterceptor(a.grpcSrv.UnaryMembershipInterceptor(), momServiceAdmissionDependencies{
		Dispatcher: a.momDispatcher, FOMs: a.foms, Federations: a.fedMgr, Declarations: a.declMgr,
		Authenticate: a.sessions.Authenticate, Checkpoint: a.checkpointAdmission,
	})
	if err != nil {
		return err
	}
	unaryInterceptors := []stdgrpc.UnaryServerInterceptor{membershipInterceptor, a.momObserver.UnaryInterceptor()}
	momStreamAdmission, err := newMOMServiceStreamAdmissionInterceptor(a.grpcSrv.StreamMembershipInterceptor(), momServiceAdmissionDependencies{
		Dispatcher: a.momDispatcher, FOMs: a.foms, Federations: a.fedMgr, Declarations: a.declMgr,
		Authenticate: a.sessions.Authenticate, Checkpoint: a.checkpointAdmission,
	})
	if err != nil {
		return err
	}
	streamInterceptors := []stdgrpc.StreamServerInterceptor{momStreamAdmission}
	if a.cfg.OIDCVerifier != nil {
		// M14 W4 — OIDC bearer-token interceptor. Validates the
		// `authorization: Bearer <jwt>` metadata before any service
		// handler runs. Stackable with mTLS: a deployment configured
		// with both must satisfy both.
		unaryInterceptors = append([]stdgrpc.UnaryServerInterceptor{oidc.UnaryServerInterceptor(a.cfg.OIDCVerifier)}, unaryInterceptors...)
		streamInterceptors = append([]stdgrpc.StreamServerInterceptor{oidc.StreamServerInterceptor(a.cfg.OIDCVerifier)}, streamInterceptors...)
	}
	serverOpts = append(serverOpts,
		stdgrpc.ChainUnaryInterceptor(unaryInterceptors...),
		stdgrpc.ChainStreamInterceptor(streamInterceptors...),
	)
	a.gs = stdgrpc.NewServer(serverOpts...)
	if err := a.grpcSrv.Register(a.gs); err != nil {
		return err
	}
	return nil
}

func (a *rtidAssembly) initializeAdministration() error {
	a.metrics = observability.NewHandler(
		a.fedMgr,
		objectCounterFor(a.objReg),
		zeroSeqSource{},
	)

	// rtid-TUI Phase 1: AdminService gRPC server. Bound to
	// cfg.AdminListenAddr — a SEPARATE listener so admin traffic does
	// not contend with federate traffic on --listen, and the cfg.TLSConfig
	// (server-side TLS for federates) does NOT apply (Phase 1 admin is
	// plaintext only; production deployments front it with a reverse
	// proxy + ACL when network-exposed). Empty AdminListenAddr means
	// admin is disabled — adminS stays nil and Serve skips the
	// listener.
	a.startedAt = a.clock.Now()
	if a.cfg.AdminListenAddr != "" {
		a.adminGS = stdgrpc.NewServer()
		if err := grpcsvc.RegisterAdminService(a.adminGS, grpcsvc.AdminOptions{
			Integer64: a.integerBackends, TimeImplementations: a.fedMgr,
			Federations:  a.fedMgr,
			Declarations: a.declMgr,
			Sync:         a.syncMgr,
			Ownership:    a.ownMgr,
			DDM:          a.ddmMgr,
			Savepoint:    a.saveCoordinator,
			MOM:          a.momMgr,
			// M21 TASK-204: time manager is composed in server mode
			// (the same instance the TimeServiceServer wraps), so the
			// AdminService Snapshot includes per-federation time state.
			Time:      a.timeMgr,
			Objects:   a.objReg,
			Outbox:    a.outbox,
			EventLog:  a.plugins.AdminEventLog(),
			Version:   rtidVersion(),
			StartedAt: a.startedAt,
		}); err != nil {
			return fmt.Errorf("rtid: admin service register: %w", err)
		}

		// rtid-TUI Phase 5: register MutatingService ONLY when the
		// composition-root flag is set. AdminService stays read-only.
		// See docs/rtid-tui.md §7.5 for the safety contract.
		if a.cfg.AdminMutating {
			if err := grpcsvc.RegisterMutatingService(a.adminGS, grpcsvc.MutatingOptions{
				Federations:         a.fedMgr,
				Membership:          a.fedMgr,
				CheckpointAdmission: a.checkpointAdmission,
				Version:             rtidVersion(),
				Cluster:             a.clusterMgr,
				ClusterPeers:        a.clusterSvc,
			}); err != nil {
				return fmt.Errorf("rtid: mutating service register: %w", err)
			}
		}
	}

	// M15 cut-2 — register the cluster service on the federate-facing
	// gRPC server (constructed earlier in this function alongside the
	// other handlers).
	a.clusterSvc.RegisterWith(a.gs)
	return nil
}

func (a *rtidAssembly) runtime() *rtid {
	return &rtid{
		sessions:       a.sessions,
		cfg:            a.cfg,
		logger:         a.cfg.Logger,
		startedAt:      a.startedAt,
		fedMgr:         a.fedMgr,
		declMgr:        a.declMgr,
		objReg:         a.objReg,
		syncMgr:        a.syncMgr,
		ownMgr:         a.ownMgr,
		momMgr:         a.momMgr,
		ddmMgr:         a.ddmMgr,
		saveMgr:        a.saveCoordinator,
		timeMgr:        a.timeMgr,
		integerTime:    a.integerTime,
		integerObjects: a.integerObjects,
		integerSave:    a.integerSave,
		plugins:        a.plugins,
		outbox:         a.outbox,
		grpcS:          a.gs,
		adminS:         a.adminGS,
		metrics:        a.metrics,
		foms:           a.foms,
		clusterMgr:     a.clusterMgr,
		clusterSvc:     a.clusterSvc,

		deliveryRetries:          newDeliveryRetryWorker(a.timeLifecycle, a.fedMgr, a.checkpointAdmission),
		synchronizationLifecycle: a.syncLifecycle,
		startOwnershipRetries: func(ctx context.Context) func() {
			return startOwnershipCallbackRetries(ctx, a.ownMgr, a.fedMgr, a.observationAdmission, 100*time.Millisecond)
		},
	}
}

// parseClusterPeers splits a comma-separated --cluster-peers value
// into a node_id→address map. Each entry is "node_id=host:port";
// duplicate node_ids are rejected. Empty input yields an empty map
// (single-node mode).
func parseClusterPeers(s string) (map[string]string, error) {
	out := map[string]string{}
	if s == "" {
		return out, nil
	}
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		eq := strings.Index(entry, "=")
		if eq <= 0 || eq == len(entry)-1 {
			return nil, fmt.Errorf("invalid peer %q (want node_id=host:port)", entry)
		}
		id := entry[:eq]
		addr := entry[eq+1:]
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("duplicate peer node_id %q", id)
		}
		out[id] = addr
	}
	return out, nil
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

// RegionSubscribersFor — M36 DC-1. Pass-through (no handle conversion
// needed: the result is federate handles).
func (a ddmFilterAdapter) RegionSubscribersFor(
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attr core.AttributeHandle,
) []core.FederateHandle {
	return a.m.RegionSubscribersFor(fed, cls, attr)
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
	if r.sessions != nil {
		defer r.sessions.Close()
	}
	gln, mln, aln, err := r.listen()
	if err != nil {
		return err
	}

	logArgs := []any{"grpc", gln.Addr().String(), "metrics", mln.Addr().String()}
	if aln != nil {
		logArgs = append(logArgs, "admin", aln.Addr().String())
		r.logger.Info("rtid: admin gRPC listening (plaintext, read-only)", "addr", aln.Addr().String())
	}
	r.logger.Info("rtid serving", logArgs...)
	r.deliveryRetries.Start(ctx)
	defer r.deliveryRetries.Close()
	if r.startOwnershipRetries != nil {
		stop := r.startOwnershipRetries(ctx)
		defer stop()
	}
	if r.synchronizationLifecycle != nil {
		retryContext, cancelRetries := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); r.synchronizationLifecycle.Run(retryContext) }()
		defer func() { cancelRetries(); <-done }()
	}

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
		r.deliveryRetries.Close()
		r.grpcS.GracefulStop()
		if r.adminS != nil {
			r.adminS.GracefulStop()
		}
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = metricsSrv.Shutdown(shutCtx)
		shutCancel()
		_ = r.plugins.Close()
		return ctx.Err()
	case err := <-errCh:
		r.deliveryRetries.Close()
		_ = gln.Close()
		_ = mln.Close()
		if aln != nil {
			_ = aln.Close()
		}
		_ = r.plugins.Close()
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

func newMultiplexLog(logDir string, clock core.Clock, metadata ...eventlog.MetadataResolver) (*eventlog.MultiplexWriter, error) {
	return newMultiplexLogConfigured(logDir, clock, false, metadata...)
}

func newMultiplexLogConfigured(
	logDir string,
	clock core.Clock,
	allowPartialProto bool,
	metadata ...eventlog.MetadataResolver,
) (*eventlog.MultiplexWriter, error) {
	var resolver eventlog.MetadataResolver
	if len(metadata) > 0 {
		resolver = metadata[0]
	}
	if logDir == "" {
		return eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
			Clock:             clock,
			AllowPartialProto: allowPartialProto,
			Mode:              core.ModeVerbose,
			Metadata:          resolver,
			Factory:           discardWriterFactory(clock),
		})
	}
	return eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
		Clock:             clock,
		AllowPartialProto: allowPartialProto,
		Mode:              core.ModeVerbose,
		Metadata:          resolver,
		Dir:               logDir,
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
	return buildServerTLSWithMTLS(certPath, keyPath, "", "")
}

// buildOIDCVerifier builds an *oidc.Verifier from the M14 W4 flags.
// Returns (nil, nil) when no OIDC flags are set (i.e., OIDC is
// disabled, the default). Returns an error when --oidc-issuer is set
// but --oidc-jwks-pem isn't (M14 doesn't yet implement OIDC discovery).
func buildOIDCVerifier(jwksPemPath, audience, issuer string) (*oidc.Verifier, error) {
	if jwksPemPath == "" && issuer == "" && audience == "" {
		return nil, nil
	}
	if jwksPemPath == "" {
		return nil, fmt.Errorf("rtid: --oidc-jwks-pem is required (M14: OIDC discovery via --oidc-issuer is a future cut)")
	}
	pemBytes, err := os.ReadFile(jwksPemPath)
	if err != nil {
		return nil, fmt.Errorf("rtid: read --oidc-jwks-pem: %w", err)
	}
	return oidc.NewFromPEM(pemBytes, audience, issuer)
}

// buildServerTLSWithMTLS extends buildServerTLS with optional mTLS
// (M14 W1). When clientCAPath is non-empty, the returned config
// requires + verifies a client cert against the CA bundle. When
// clientCNAllow is non-empty, only certs whose Subject CN appears in
// the comma-separated list are accepted (the verification chain still
// runs first; the allow-list is the secondary filter).
func buildServerTLSWithMTLS(certPath, keyPath, clientCAPath, clientCNAllow string) (*tls.Config, error) {
	if certPath == "" && keyPath == "" {
		if clientCAPath != "" {
			return nil, fmt.Errorf("rtid: --tls-client-ca requires --tls-cert + --tls-key (mTLS implies TLS)")
		}
		return nil, nil
	}
	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("rtid: --tls-cert and --tls-key must both be set (got cert=%q key=%q)", certPath, keyPath)
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("rtid: load TLS keypair: %w", err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		// MinVersion: TLS 1.2 floor matches Go's secure-by-default
		// recommendation and avoids the gosec G402 lint. Clients
		// dialing with the Python SDK's default ssl.create_default_context
		// negotiate TLS 1.2 or higher.
		MinVersion: tls.VersionTLS12,
	}
	if clientCAPath != "" {
		caPEM, err := os.ReadFile(clientCAPath)
		if err != nil {
			return nil, fmt.Errorf("rtid: read --tls-client-ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("rtid: --tls-client-ca contains no valid PEM certs")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert

		if clientCNAllow != "" {
			allowList := strings.Split(clientCNAllow, ",")
			for i := range allowList {
				allowList[i] = strings.TrimSpace(allowList[i])
			}
			cfg.VerifyPeerCertificate = makeCNAllowVerifier(allowList)
		}
	}
	return cfg, nil
}

// makeCNAllowVerifier — M14 W1. Returns a tls.Config.VerifyPeerCertificate
// callback that succeeds only when the peer leaf cert's Subject CN is
// in allow. The standard chain verification has already passed by the
// time this fires (we set ClientAuth=RequireAndVerifyClientCert above).
func makeCNAllowVerifier(allow []string) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	allowSet := make(map[string]struct{}, len(allow))
	for _, cn := range allow {
		if cn != "" {
			allowSet[cn] = struct{}{}
		}
	}
	return func(_ [][]byte, verifiedChains [][]*x509.Certificate) error {
		if len(verifiedChains) == 0 || len(verifiedChains[0]) == 0 {
			return fmt.Errorf("rtid: no verified chain for client cert")
		}
		leaf := verifiedChains[0][0]
		if _, ok := allowSet[leaf.Subject.CommonName]; !ok {
			return fmt.Errorf("rtid: client cert CN %q not in --tls-client-cn-allow", leaf.Subject.CommonName)
		}
		return nil
	}
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
func objectCounterFor(_ *object.Registry) zeroObjectCounter {
	return zeroObjectCounter{}
}

type zeroObjectCounter struct{}

func (zeroObjectCounter) ObjectCount(core.FederationName) uint64 { return 0 }

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
// chainOnFederateJoined composes multiple OnFederateJoined hooks
// in declaration order. M27 Phase A — needed once the outbox-bind
// hook joined MOM as a second registrant on the join-side.
// createFederationHook returns the gRPC OnCreateFederationSuccess
// closure that (a) populates the FOM repository's per-federation map
// for FOMRepoOrderLookup and (b) registers the HLAfederation MOM
// instance. M15.2.3 also (c) records the federation→self assignment
// in the cluster manager and broadcasts NotifyAssignment to peers.
//
// Reuse the already validated model while creation admission is exclusive.
func createFederationHook(federations *federation.Manager, foms *composition.Repository, momMgr core.ManagementObjectModel, clusterMgr *cluster.Manager, clusterSvc *grpcsvc.ClusterService, logger *slog.Logger) func(context.Context, core.FederationName, []core.FOMModule) {
	return func(ctx context.Context, name core.FederationName, modules []core.FOMModule) {
		h, err := federations.FOMFor(name)
		if err != nil {
			logger.Error("rtid: validated federation model unavailable during creation",
				"federation", name, "err", err)
			return
		}
		foms.RememberFor(name, h)
		if err := momMgr.FederationCreated(ctx, name, modules); err != nil {
			logger.Warn("rtid: MOM FederationCreated hook failed",
				"federation", name, "err", err)
		}
		// M15 cut-2 — claim the federation locally and tell peers.
		if clusterMgr != nil {
			if clusterSvc != nil {
				clusterSvc.AssignLocalFederation(name)
				clusterSvc.BroadcastAssignment(
					ctx, name, clusterMgr.SelfID(), clusterMgr.SelfAddress(),
				)
			} else {
				clusterMgr.AssignFederation(name)
			}
		}
	}
}

func (a *rtidAssembly) onFederateResignOutcome(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction, outcome error) {
	if a.momDispatcher != nil {
		if err := a.momDispatcher.ObserveMOMResignOutcome(ctx, fed, h, outcome, grpcsvc.MOMServiceExceptionName(outcome)); err != nil {
			a.cfg.Logger.Warn("MOM resign outcome reporting unavailable", "federation", fed, "federate", h, "error", err)
		}
	}
	if a.lifecycleMOMObserver != nil {
		a.lifecycleMOMObserver.ObserveResignOutcome(ctx, fed, h, action, outcome)
	}
}

func (a *rtidAssembly) onFederateResigning(_ context.Context, fed core.FederationName, h core.FederateHandle, _ core.ResignAction) {
	if a.sessions != nil {
		a.sessions.ClearFederate(fed, h)
	}
	if a.callbackReceipts != nil {
		a.callbackReceipts.ClearFederate(fed, h)
	}
}

func (a *rtidAssembly) onFederationDestroying(ctx context.Context, fed core.FederationName) error {
	if a.destroyingDispatch != nil {
		return a.destroyingDispatch(ctx, fed)
	}
	return nil
}

func validateMutatingAdminCLI(logger *slog.Logger, args serverMainArgs) {
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
}

func (r *rtid) listen() (net.Listener, net.Listener, net.Listener, error) {
	gln, err := net.Listen("tcp", r.cfg.ListenAddr)
	if err != nil {
		return nil, nil, nil, err
	}
	mln, err := net.Listen("tcp", r.cfg.MetricsListenAddr)
	if err != nil {
		_ = gln.Close()
		return nil, nil, nil, err
	}
	var aln net.Listener
	if r.adminS != nil {
		aln, err = net.Listen("tcp", r.cfg.AdminListenAddr)
		if err != nil {
			_ = gln.Close()
			_ = mln.Close()
			return nil, nil, nil, err
		}
	}

	return gln, mln, aln, nil
}
