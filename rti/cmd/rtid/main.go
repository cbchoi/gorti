// Command rtid runs the gorti RTI server.
//
// Wires the federation manager, declaration manager, object registry,
// event log multiplexer, and gRPC service handlers into a runnable
// server, plus a Prometheus metrics endpoint on a separate listener.
// See docs/agent-a-rti-core.md §4 for the M2 deliverable definition.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
	"github.com/cbchoi/gorti/rti/internal/federation"
	"github.com/cbchoi/gorti/rti/internal/object"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"

	stdgrpc "google.golang.org/grpc"
)

func main() {
	listen := flag.String("listen", ":8442", "gRPC listen address")
	metricsListen := flag.String("metrics-listen", ":9090", "Prometheus HTTP listen")
	logDir := flag.String("log-dir", "", "directory for per-federation event log files (empty = require explicit set)")
	logLevel := flag.String("log-level", "info", "log level: debug|info|warn|error")
	logFormat := flag.String("log-format", "json", "log format: json|text")
	mode := flag.String("mode", "server", "rtid mode: server|pingpong-demo|replay-from-log")
	pingpongRounds := flag.Int("pingpong-rounds", 1000, "rounds for pingpong-demo mode")
	pingpongFederation := flag.String("pingpong-federation", "pingpong", "federation name for pingpong-demo mode")
	pingpongDeterministic := flag.Bool("pingpong-deterministic", false, "use FakeClock so the event-log body is byte-deterministic across runs")
	replayInput := flag.String("replay-input", "", "source event-log file path for replay-from-log mode")
	flag.Parse()

	logger := buildLogger(*logLevel, *logFormat)
	slog.SetDefault(logger)

	switch *mode {
	case "pingpong-demo":
		runPingpongMain(logger, *pingpongFederation, *pingpongRounds, *logDir, *pingpongDeterministic)
		return
	case "replay-from-log":
		runReplayMain(logger, *replayInput, *logDir)
		return
	case "server", "":
		runServerMain(logger, *listen, *metricsListen, *logDir)
	default:
		logger.Error("unknown --mode", "mode", *mode)
		os.Exit(2)
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
	defer cancel()
	if err := runReplayFromFile(ctx, inputPath, logDir); err != nil {
		logger.Error("replay failed", "err", err)
		os.Exit(1)
	}
	logger.Info("replay complete", "input", inputPath, "output_dir", logDir)
}

// runServerMain boots the gRPC server + metrics endpoint and blocks until
// SIGINT/SIGTERM. Extracted so main can dispatch on --mode.
func runServerMain(logger *slog.Logger, listen, metricsListen, logDir string) {
	if logDir == "" {
		logger.Warn("--log-dir not set; event logs will not be persisted")
	}

	srv, err := newRTID(rtidConfig{
		ListenAddr:        listen,
		MetricsListenAddr: metricsListen,
		LogDir:            logDir,
		Logger:            logger,
	})
	if err != nil {
		logger.Error("rtid initialization failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("rtid serve exited with error", "err", err)
		os.Exit(1)
	}
}

// runPingpongMain runs the pingpong demo and exits. logDir, when set,
// gets per-federation .log files written into it (same format as server
// mode); empty means the demo runs without persistence.
func runPingpongMain(logger *slog.Logger, federation string, rounds int, logDir string, deterministic bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stats, err := runPingpongDemo(ctx, pingpongConfig{
		FederationName: core.FederationName(federation),
		Rounds:         rounds,
		LogDir:         logDir,
		Deterministic:  deterministic,
	})
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
}

// rtid is the composed runtime: gRPC server + metrics handler + the
// underlying core components. Construct via newRTID; run via Serve.
type rtid struct {
	cfg     rtidConfig
	logger  *slog.Logger
	fedMgr  *federation.Manager
	declMgr *declaration.Manager
	objReg  *object.Registry
	multi   *eventlog.MultiplexWriter
	outbox  *multiOutbox
	grpcS   *stdgrpc.Server
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
	fedMgr, err := federation.New(federation.Options{
		Clock:    clock,
		EventLog: multi,
		FOMs:     foms,
	})
	if err != nil {
		return nil, err
	}

	declMgr := declaration.New()
	outbox := newMultiOutbox(1024)

	objReg, err := object.New(object.Options{
		EventLog:     multi,
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         foms,
		Clock:        clock,
	})
	if err != nil {
		return nil, err
	}

	grpcSrv, err := grpcsvc.NewServer(grpcsvc.Options{
		Federations:  fedMgr,
		Declarations: declMgr,
		Objects:      objReg,
		Outbox:       outbox,
	})
	if err != nil {
		return nil, err
	}
	gs := stdgrpc.NewServer()
	if err := grpcSrv.Register(gs); err != nil {
		return nil, err
	}

	metrics := newMetricsHandler(
		fedMgr,
		objectCounterFor(objReg),
		multiplexSeqSource(multi),
	)

	return &rtid{
		cfg:     cfg,
		logger:  cfg.Logger,
		fedMgr:  fedMgr,
		declMgr: declMgr,
		objReg:  objReg,
		multi:   multi,
		outbox:  outbox,
		grpcS:   gs,
		metrics: metrics,
		foms:    foms,
	}, nil
}

// Serve runs the gRPC + metrics listeners until ctx is canceled. Returns
// the first non-graceful error from either listener.
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

	r.logger.Info("rtid serving", "grpc", gln.Addr().String(), "metrics", mln.Addr().String())

	metricsSrv := &http.Server{
		Handler:           r.metrics,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() { errCh <- r.grpcS.Serve(gln) }()
	go func() { errCh <- metricsSrv.Serve(mln) }()

	select {
	case <-ctx.Done():
		r.logger.Info("rtid shutting down", "cause", ctx.Err())
		r.grpcS.GracefulStop()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = metricsSrv.Shutdown(shutCtx)
		shutCancel()
		_ = r.multi.Close()
		return ctx.Err()
	case err := <-errCh:
		_ = gln.Close()
		_ = mln.Close()
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
