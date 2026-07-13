package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gorti-go-fair:", err)
		os.Exit(1)
	}
}

func execute(args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	fom, err := os.ReadFile(cfg.FOMPath)
	if err != nil {
		return fmt.Errorf("read caller-supplied FOM: %w", err)
	}
	payloads, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	semantic, err := newNDJSONLogger(filepath.Join(cfg.OutputDir, string(cfg.Role)+"-semantic.ndjson"), string(cfg.Role))
	if err != nil {
		return err
	}
	metrics, err := newNDJSONLogger(filepath.Join(cfg.OutputDir, string(cfg.Role)+"-metrics.ndjson"), string(cfg.Role))
	if err != nil {
		_ = semantic.close()
		return err
	}
	samples, err := newNDJSONLogger(filepath.Join(cfg.OutputDir, string(cfg.Role)+"-samples.ndjson"), string(cfg.Role))
	if err != nil {
		_ = semantic.close()
		_ = metrics.close()
		return err
	}
	log := &runLog{semantic: semantic, metrics: metrics, samples: samples}
	defer log.close()

	runStarted := counterNow()
	_ = log.event("FM", "phase", map[string]any{
		"count": cfg.Count, "phase": "plan", "seed": cfg.Seed, "status": "complete",
	})
	_ = log.event("FM", "phase", map[string]any{"phase": "do", "status": "start"})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	p := &participant{cfg: cfg, log: log, payloads: payloads}
	runErr := p.run(ctx, fom)
	if runErr == nil {
		_ = log.event("FM", "phase", map[string]any{"phase": "reflect", "result": "pass", "status": "complete"})
	} else {
		_ = log.event("FM", "phase", map[string]any{
			"error": runErr.Error(), "phase": "reflect", "result": "fail", "status": "complete",
		})
	}

	metricErr := log.metric("FM", "run_duration", "nanoseconds", float64(counterElapsed(runStarted)))
	metricErr = errors.Join(metricErr, log.metric("FM", "verification_result", "boolean", boolMetric(runErr == nil)))
	if cfg.Role == roleSubscriber && p.state != nil {
		accounting := p.state.accounting()
		metricErr = errors.Join(metricErr,
			log.metric("OM", "delivery_accounting.expected", "deliveries", float64(accounting.Expected)),
			log.metric("OM", "delivery_accounting.delivered", "deliveries", float64(accounting.Delivered)),
			log.metric("OM", "delivery_accounting.explicitly_rejected", "deliveries", float64(accounting.Rejected)),
			log.metric("OM", "delivery_accounting.dropped", "deliveries", float64(accounting.Dropped)),
			log.metric("OM", "delivery_accounting.unexpected", "callbacks", float64(accounting.Unexpected)),
			log.metric("OM", "delivery_accounting.duplicates", "callbacks", float64(accounting.Duplicates)),
			log.metric("OM", "delivery_accounting.invalid", "callbacks", float64(accounting.Invalid)),
		)
	}
	if cfg.Role == rolePublisher && p.fed != nil {
		stats := p.fed.InteractionTransportStats()
		metricErr = errors.Join(metricErr,
			log.metric("OM", "interaction_transport.total", "calls", float64(stats.Total)),
			log.metric("OM", "interaction_transport.stream_sent", "calls", float64(stats.StreamSent)),
			log.metric("OM", "interaction_transport.stream_acked", "calls", float64(stats.StreamAcked)),
			log.metric("OM", "interaction_transport.unary_sent", "calls", float64(stats.UnarySent)),
			log.metric("OM", "interaction_transport.unary_acked", "calls", float64(stats.UnaryAcked)),
			log.metric("OM", "interaction_transport.open_attempts", "count", float64(stats.OpenAttempts)),
			log.metric("OM", "interaction_transport.open_successes", "count", float64(stats.OpenSuccesses)),
			log.metric("OM", "interaction_transport.resets", "count", float64(stats.Resets)),
			log.metric("OM", "interaction_transport.indeterminate", "count", float64(stats.Indeterminate)),
			log.metric("OM", "interaction_transport.fallbacks", "count", float64(stats.FallbackDisabled+stats.FallbackMetadata+stats.FallbackUnsupported)),
		)
	}
	if runErr != nil {
		return errors.Join(runErr, metricErr)
	}
	if metricErr != nil {
		return metricErr
	}
	fmt.Printf("PASS: %s (%d iterations)\n", cfg.Role, cfg.Count)
	return nil
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
