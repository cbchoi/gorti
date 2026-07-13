package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

type role string

const (
	rolePublisher  role = "publisher"
	roleSubscriber role = "subscriber"
)

type config struct {
	Role       role
	Address    string
	Federation string
	FOMPath    string
	Seed       string
	Count      int
	OutputDir  string
	Timeout    time.Duration
}

func parseConfig(args []string) (config, error) {
	var cfg config
	var roleValue string
	set := flag.NewFlagSet("gorti-go-fair", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&roleValue, "role", "", "publisher or subscriber")
	set.StringVar(&cfg.Address, "address", "127.0.0.1:8442", "rtid host:port")
	set.StringVar(&cfg.Federation, "federation", "", "federation name")
	set.StringVar(&cfg.FOMPath, "fom", "", "caller-supplied FOM XML path")
	set.StringVar(&cfg.Seed, "seed", "", "caller-supplied deterministic payload seed")
	set.IntVar(&cfg.Count, "count", 0, "caller-supplied iteration count")
	set.StringVar(&cfg.OutputDir, "output", "", "output directory")
	set.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "per-phase timeout")
	if err := set.Parse(args); err != nil {
		return config{}, err
	}
	if set.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(set.Args(), " "))
	}
	cfg.Role = role(strings.ToLower(strings.TrimSpace(roleValue)))
	if cfg.Role != rolePublisher && cfg.Role != roleSubscriber {
		return config{}, errors.New("--role must be publisher or subscriber")
	}
	if strings.TrimSpace(cfg.Address) == "" || strings.Contains(cfg.Address, "://") {
		return config{}, errors.New("--address must be a host:port without a URI scheme")
	}
	if strings.TrimSpace(cfg.Federation) == "" {
		return config{}, errors.New("--federation is required")
	}
	if strings.TrimSpace(cfg.FOMPath) == "" {
		return config{}, errors.New("--fom is required")
	}
	if cfg.Seed == "" {
		return config{}, errors.New("--seed is required")
	}
	if cfg.Count < 1 {
		return config{}, errors.New("--count must be at least 1")
	}
	if strings.TrimSpace(cfg.OutputDir) == "" {
		return config{}, errors.New("--output is required")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("--timeout must be positive")
	}

	var err error
	cfg.FOMPath, err = filepath.Abs(cfg.FOMPath)
	if err != nil {
		return config{}, fmt.Errorf("resolve FOM path: %w", err)
	}
	cfg.OutputDir, err = filepath.Abs(cfg.OutputDir)
	if err != nil {
		return config{}, fmt.Errorf("resolve output directory: %w", err)
	}
	return cfg, nil
}
