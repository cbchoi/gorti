package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
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
	var plan *workloadPlan
	var payloads []encodedIteration
	if cfg.WorkloadPlan != "" {
		plan, err = loadWorkloadPlan(cfg.WorkloadPlan, cfg.Count, cfg.Seed)
		if err != nil {
			return err
		}
		if err := verifyWorkloadPlanDigest(plan, cfg.WorkloadPlanSHA256); err != nil {
			return err
		}
		payloads, err = preencodePlanWorkload(plan)
	} else {
		payloads, err = preencodeWorkload(cfg.Seed, cfg.Count)
	}
	if err != nil {
		return err
	}
	warmupPayloads, err := preencodeWorkloadRange(cfg.Seed, cfg.Count, cfg.OperationWarmup)
	if err != nil {
		return err
	}
	if err := prepareOutputDirectory(cfg); err != nil {
		return err
	}
	log, err := newRunLog(cfg)
	if err != nil {
		return err
	}
	defer log.close()

	runStarted := counterNow()
	_ = log.event("FM", "phase", map[string]any{
		"count": cfg.Count, "phase": "plan", "seed": cfg.Seed, "status": "complete",
	})
	_ = log.event("FM", "phase", map[string]any{"phase": "do", "status": "start"})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	p := &participant{cfg: cfg, log: log, payloads: payloads, warmupPayloads: warmupPayloads}
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
		localStats := p.fed.LocalLRCStats()
		stats := p.fed.ConfirmedObjectTransportStats()
		metricErr = errors.Join(metricErr,
			log.metric("OM", "local_lrc.enqueued", "operations", float64(localStats.Enqueued)),
			log.metric("OM", "local_lrc.sent", "operations", float64(localStats.Sent)),
			log.metric("OM", "local_lrc.acked", "operations", float64(localStats.Acked)),
			log.metric("OM", "local_lrc.operation_frames", "frames", float64(localStats.OperationFrames)),
			log.metric("OM", "local_lrc.max_frame_operations", "operations", float64(localStats.MaxFrameOperations)),
			log.metric("OM", "local_lrc.requested_batch_size", "operations", float64(localStats.RequestedBatchSize)),
			log.metric("OM", "local_lrc.peer_batch_limit", "operations", float64(localStats.PeerBatchLimit)),
			log.metric("OM", "local_lrc.batch_size", "operations", float64(localStats.BatchSize)),
			log.metric("OM", "local_lrc.flushes", "count", float64(localStats.Flushes)),
			log.metric("OM", "local_lrc.failures", "count", float64(localStats.Failures)),
			log.metric("OM", "confirmed_object_transport.total", "calls", float64(stats.Total)),
			log.metric("OM", "confirmed_object_transport.stream_sent", "calls", float64(stats.StreamSent)),
			log.metric("OM", "confirmed_object_transport.stream_acked", "calls", float64(stats.StreamAcked)),
			log.metric("OM", "confirmed_object_transport.unary_sent", "calls", float64(stats.UnarySent)),
			log.metric("OM", "confirmed_object_transport.unary_acked", "calls", float64(stats.UnaryAcked)),
			log.metric("OM", "confirmed_object_transport.open_attempts", "count", float64(stats.OpenAttempts)),
			log.metric("OM", "confirmed_object_transport.open_successes", "count", float64(stats.OpenSuccesses)),
			log.metric("OM", "confirmed_object_transport.resets", "count", float64(stats.Resets)),
			log.metric("OM", "confirmed_object_transport.indeterminate", "count", float64(stats.Indeterminate)),
			log.metric("OM", "confirmed_object_transport.fallbacks", "count", float64(stats.FallbackDisabled+stats.FallbackMetadata+stats.FallbackUnsupported)),
		)
	}
	if runErr != nil {
		return errors.Join(runErr, metricErr)
	}
	if metricErr != nil {
		return metricErr
	}
	if cfg.CompactSummary {
		if err := writeParticipantSummary(cfg, plan, p.state, log); err != nil {
			return err
		}
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
