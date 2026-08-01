package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type stringListFlag []string

func (values *stringListFlag) String() string { return strings.Join(*values, " ") }

func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type commandConfig struct {
	workloadConfig
	OutputPath   string
	RTIDPath     string
	RTIDVersion  string
	SourceCommit string
	SourceBranch string
	SourceDirty  bool
	ServerArgs   stringListFlag
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gorti-go benchmark:", err)
		os.Exit(1)
	}
}

func execute() error {
	cfg, err := parseCommandLine()
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC()

	fomXML, err := os.ReadFile(cfg.FOMPath)
	if err != nil {
		return fmt.Errorf("read FOM: %w", err)
	}
	cfg.FOMXML = fomXML
	payloads, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		return err
	}
	metadata, err := buildRunMetadata(cfg, startedAt, fomXML)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(cfg.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	recorder := &sampleRecorder{}
	runner := newLiveRunner(cfg.workloadConfig, recorder, payloads)
	runErr := runner.run(ctx)

	expected := 2 * cfg.Count
	accounted := runner.delivered + runner.explicitlyRejected
	if accounted > expected {
		return fmt.Errorf("delivery accounting exceeded expected fanout: %d > %d", accounted, expected)
	}
	artifact, artifactErr := newBenchmarkArtifact(metadata, recorder.snapshot(), deliveryAccounting{
		ExpectedFanout:     expected,
		Delivered:          runner.delivered,
		ExplicitlyRejected: runner.explicitlyRejected,
		Dropped:            expected - accounted,
	})
	if artifactErr != nil {
		return errors.Join(runErr, artifactErr)
	}
	writeErr := writeBenchmark(cfg.OutputPath, artifact)
	if runErr != nil {
		return errors.Join(runErr, writeErr)
	}
	if writeErr != nil {
		return writeErr
	}

	fmt.Printf("PASS: %s\n", cfg.OutputPath)
	fmt.Printf("Deliveries: %d/%d\n", runner.delivered, expected)
	return nil
}

func parseCommandLine() (commandConfig, error) {
	var cfg commandConfig
	flag.StringVar(&cfg.Address, "address", "127.0.0.1:8442", "rtid host:port")
	flag.StringVar(&cfg.Federation, "federation", "gorti-go-production-benchmark", "federation name")
	flag.StringVar(&cfg.FOMPath, "fom", "verification/gorti/federation.fom.xml", "FOM XML path")
	flag.IntVar(&cfg.Count, "count", 100, "lockstep iteration count")
	flag.Uint64Var(&cfg.Seed, "seed", 20260712, "deterministic payload seed")
	flag.DurationVar(&cfg.Timeout, "timeout", 15*time.Second, "timeout for each setup or lockstep phase")
	flag.StringVar(&cfg.OutputPath, "output", "verification/gorti-go/artifacts/benchmark.json", "benchmark JSON path")
	flag.StringVar(&cfg.RTIDPath, "rtid-path", "", "path to the RTID binary under test")
	flag.StringVar(&cfg.RTIDVersion, "rtid-version", "unknown", "exact RTID version output")
	flag.StringVar(&cfg.SourceCommit, "source-commit", "unknown", "source commit for provenance")
	flag.StringVar(&cfg.SourceBranch, "source-branch", "unknown", "source branch for provenance")
	flag.BoolVar(&cfg.SourceDirty, "source-dirty", false, "whether the source worktree is dirty")
	flag.Var(&cfg.ServerArgs, "server-arg", "exact RTID argument; repeat for every argument")
	flag.Parse()

	if strings.TrimSpace(cfg.Address) == "" || strings.Contains(cfg.Address, "://") {
		return commandConfig{}, errors.New("--address must be a host:port without a URI scheme")
	}
	if strings.TrimSpace(cfg.Federation) == "" {
		return commandConfig{}, errors.New("--federation must be non-empty")
	}
	if cfg.Count < 1 {
		return commandConfig{}, errors.New("--count must be at least 1")
	}
	if cfg.Timeout <= 0 {
		return commandConfig{}, errors.New("--timeout must be positive")
	}
	if strings.TrimSpace(cfg.RTIDPath) == "" {
		return commandConfig{}, errors.New("--rtid-path is required so the benchmark can hash the server binary")
	}

	var err error
	cfg.FOMPath, err = filepath.Abs(cfg.FOMPath)
	if err != nil {
		return commandConfig{}, fmt.Errorf("resolve FOM path: %w", err)
	}
	cfg.RTIDPath, err = filepath.Abs(cfg.RTIDPath)
	if err != nil {
		return commandConfig{}, fmt.Errorf("resolve RTID path: %w", err)
	}
	cfg.OutputPath, err = filepath.Abs(cfg.OutputPath)
	if err != nil {
		return commandConfig{}, fmt.Errorf("resolve output path: %w", err)
	}
	return cfg, nil
}

func buildRunMetadata(cfg commandConfig, startedAt time.Time, fomXML []byte) (runMetadata, error) {
	rtidSHA, err := hashFile(cfg.RTIDPath)
	if err != nil {
		return runMetadata{}, fmt.Errorf("hash RTID binary: %w", err)
	}
	fomDigest := sha256.Sum256(fomXML)
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	clientPath, err := os.Executable()
	if err != nil {
		clientPath = "unknown"
	}
	clientSHA := "unknown"
	if clientPath != "unknown" {
		if digest, hashErr := hashFile(clientPath); hashErr == nil {
			clientSHA = digest
		}
	}

	serverArgs := append([]string(nil), cfg.ServerArgs...)
	metadata := runMetadata{
		RunID:     fmt.Sprintf("gorti-go-%d-%s", cfg.Seed, startedAt.Format("20060102T150405.000000000Z")),
		Benchmark: "gorti-tso-lockstep",
		StartedAt: startedAt.Format(time.RFC3339Nano),
		Environment: map[string]any{
			"host":           hostname,
			"os":             runtime.GOOS,
			"architecture":   runtime.GOARCH,
			"logical_cpus":   runtime.NumCPU(),
			"gomaxprocs":     runtime.GOMAXPROCS(0),
			"branch":         cfg.SourceBranch,
			"dirty":          cfg.SourceDirty,
			"address":        cfg.Address,
			"fom_path":       cfg.FOMPath,
			"rtid_path":      cfg.RTIDPath,
			"rtid_arguments": serverArgs,
			"client_path":    clientPath,
			"client_sha256":  clientSHA,
		},
		Workload: map[string]any{
			"federation":        cfg.Federation,
			"count":             cfg.Count,
			"seed":              cfg.Seed,
			"timeout_ns":        cfg.Timeout.Nanoseconds(),
			"lookahead":         benchmarkLookahead,
			"fom_sha256":        hex.EncodeToString(fomDigest[:]),
			"object_class":      objectClass,
			"interaction_class": interactionClass,
			"payload_encoding":  "HLAinteger32BE+HLAASCIIstring",
			"expected_fanout":   2 * cfg.Count,
		},
		Provenance: provenance{
			Commit:          cfg.SourceCommit,
			BinarySHA256:    rtidSHA,
			RuntimeVersions: map[string]string{"go": runtime.Version(), "rtid": cfg.RTIDVersion},
			BuildFlags:      serverArgs,
		},
	}
	if err := metadata.validate(); err != nil {
		return runMetadata{}, err
	}
	return metadata, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
