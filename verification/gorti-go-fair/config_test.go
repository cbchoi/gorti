package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRequiresFairComparisonInputs(t *testing.T) {
	base := []string{"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516", "--count=2", "--output=out"}
	if _, err := parseConfig(base); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"--federation=f", "--fom=f.xml", "--seed=1516", "--count=2", "--output=out"} {
		args := make([]string, 0, len(base)-1)
		for _, value := range base {
			if value != required {
				args = append(args, value)
			}
		}
		if _, err := parseConfig(args); err == nil {
			t.Fatalf("missing %s was accepted", strings.Split(required, "=")[0])
		}
	}
}

func TestConfigUsesPublisherAndSubscriberActors(t *testing.T) {
	for _, actor := range []string{"publisher", "subscriber"} {
		cfg, err := parseConfig([]string{
			"--role=" + actor, "--federation=f", "--fom=f.xml", "--seed=s", "--count=1", "--output=out",
		})
		if err != nil || string(cfg.Role) != actor {
			t.Fatalf("role %s: cfg=%+v err=%v", actor, cfg, err)
		}
	}
}

func TestConfigEnablesReceiveOrderWorkload(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out", "--receive-order=true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ReceiveOrder || !cfg.LocalLRC || cfg.Confirmed {
		t.Fatalf("receive-order defaults = %+v, want LocalLRC without confirmed", cfg)
	}
}

func TestConfigConfirmedIsExplicitAndExclusive(t *testing.T) {
	base := []string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out", "--receive-order=true",
	}
	cfg, err := parseConfig(append(base, "--confirmed=true"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LocalLRC || !cfg.Confirmed {
		t.Fatalf("confirmed config = %+v", cfg)
	}
	if _, err := parseConfig(append(base, "--confirmed=true", "--local-lrc=true")); err == nil ||
		!strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("combined transport options error = %v", err)
	}
	if _, err := parseConfig([]string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out", "--confirmed=true",
	}); err == nil || !strings.Contains(err.Error(), "requires --receive-order") {
		t.Fatalf("confirmed without receive order error = %v", err)
	}
}

func TestConfigLoadsTransportProfileAndCLIOverridesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transport.json")
	content := `{
		"schema_version":"gorti.receive-order-transport/v1",
		"receive_order_transport":"confirmed",
		"local_lrc_queue_capacity":2048,
		"local_lrc_ack_every":64,
		"local_lrc_batch_size":128,
		"callback_representation":"handles"
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out", "--receive-order=true", "--config=" + path,
	}
	cfg, err := parseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Confirmed || cfg.LocalLRC || cfg.LocalLRCQueueCapacity != 2048 ||
		cfg.LocalLRCAckEvery != 64 || cfg.LocalLRCBatchSize != 128 ||
		cfg.CallbackRepresentation != "handles" {
		t.Fatalf("profile config = %+v", cfg)
	}

	cfg, err = parseConfig(append(base, "--local-lrc=true", "--local-lrc-queue=4096"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Confirmed || !cfg.LocalLRC || cfg.LocalLRCQueueCapacity != 4096 {
		t.Fatalf("CLI override config = %+v", cfg)
	}
}

func TestConfigRejectsUnknownTransportProfileField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transport.json")
	content := `{
		"schema_version":"gorti.receive-order-transport/v1",
		"receive_order_transport":"local-lrc",
		"unknown":true
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := parseConfig([]string{"--transport-config=" + path})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown transport field error = %v", err)
	}
}

func TestConfigEnablesLocalLRCOnlyForReceiveOrder(t *testing.T) {
	base := []string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out", "--local-lrc=true",
	}
	if _, err := parseConfig(base); err == nil || !strings.Contains(err.Error(), "requires --receive-order") {
		t.Fatalf("LocalLRC without receive order error = %v", err)
	}
	cfg, err := parseConfig(append(base,
		"--receive-order=true", "--local-lrc-queue=64", "--local-lrc-ack-every=16",
		"--local-lrc-batch-size=64",
	))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LocalLRC || cfg.LocalLRCQueueCapacity != 64 || cfg.LocalLRCAckEvery != 16 ||
		cfg.LocalLRCBatchSize != 64 {
		t.Fatalf("LocalLRC config = %+v", cfg)
	}
}

func TestConfigValidatesLocalLRCBatchSize(t *testing.T) {
	base := []string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out", "--receive-order=true", "--local-lrc=true",
	}
	for _, size := range []string{"32", "64", "128", "256"} {
		cfg, err := parseConfig(append(base, "--local-lrc-batch-size="+size))
		if err != nil {
			t.Fatalf("batch size %s: %v", size, err)
		}
		if fmt.Sprint(cfg.LocalLRCBatchSize) != size {
			t.Fatalf("batch size = %d, want %s", cfg.LocalLRCBatchSize, size)
		}
	}
	if _, err := parseConfig(append(base, "--local-lrc-batch-size=48")); err == nil {
		t.Fatal("unsupported LocalLRC batch size was accepted")
	}
}

func TestConfigValidatesCallbackRepresentation(t *testing.T) {
	base := []string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out",
	}
	for _, representation := range []string{"names", "handles"} {
		cfg, err := parseConfig(append(base, "--callback-representation="+representation))
		if err != nil {
			t.Fatalf("callback representation %s: %v", representation, err)
		}
		if cfg.CallbackRepresentation != representation {
			t.Fatalf("callback representation = %q, want %q", cfg.CallbackRepresentation, representation)
		}
	}
	if _, err := parseConfig(append(base, "--callback-representation=raw")); err == nil {
		t.Fatal("unsupported callback representation was accepted")
	}
}

func TestConfigEnablesPlanAndCompactSummaryWithoutChangingDefaults(t *testing.T) {
	base := []string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out", "--receive-order=true",
	}
	validDigest := strings.Repeat("a", sha256.Size*2)
	legacy, err := parseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.WorkloadPlan != "" || legacy.CompactSummary || legacy.devstoneMode() || !legacy.LocalLRC {
		t.Fatalf("legacy defaults changed: %+v", legacy)
	}

	cfg, err := parseConfig(append(
		base,
		"--workload-plan=plan.bin",
		"--workload-plan-sha256="+validDigest,
		"--compact-summary=true",
	))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkloadPlan == "" || cfg.WorkloadPlanSHA256 != validDigest ||
		!cfg.CompactSummary || !cfg.devstoneMode() {
		t.Fatalf("DEVStone config = %+v", cfg)
	}

	if _, err := parseConfig(append(base, "--compact-summary=true")); err == nil ||
		!strings.Contains(err.Error(), "requires --workload-plan") {
		t.Fatalf("compact summary without plan error = %v", err)
	}
	withoutRO := append([]string(nil), base[:len(base)-1]...)
	if _, err := parseConfig(append(withoutRO, "--workload-plan=plan.bin")); err == nil ||
		!strings.Contains(err.Error(), "requires --receive-order") {
		t.Fatalf("plan without receive order error = %v", err)
	}
	if _, err := parseConfig(append(
		base,
		"--workload-plan=plan.bin",
		"--compact-summary=true",
	)); err == nil || !strings.Contains(err.Error(), "requires --workload-plan-sha256") {
		t.Fatalf("compact summary without plan digest error = %v", err)
	}
}

func TestConfigAllowsCompactSummaryWithLocalLRC(t *testing.T) {
	digest := strings.Repeat("a", sha256.Size*2)
	cfg, err := parseConfig([]string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out", "--receive-order=true", "--local-lrc=true",
		"--workload-plan=plan.bin", "--workload-plan-sha256=" + digest,
		"--compact-summary=true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LocalLRC || !cfg.CompactSummary {
		t.Fatalf("compact LocalLRC config = %+v", cfg)
	}
}

func TestConfigValidatesWorkloadPlanSHA256(t *testing.T) {
	base := []string{
		"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516",
		"--count=1", "--output=out", "--receive-order=true", "--workload-plan=plan.bin",
	}
	for name, digest := range map[string]string{
		"uppercase": strings.Repeat("A", sha256.Size*2),
		"short":     strings.Repeat("a", sha256.Size*2-1),
		"non-hex":   strings.Repeat("g", sha256.Size*2),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseConfig(append(base, "--workload-plan-sha256="+digest))
			if err == nil || !strings.Contains(err.Error(), "64-character lowercase SHA-256") {
				t.Fatalf("invalid digest error = %v", err)
			}
		})
	}

	withoutPlan := base[:len(base)-1]
	_, err := parseConfig(append(
		withoutPlan,
		"--workload-plan-sha256="+strings.Repeat("a", sha256.Size*2),
	))
	if err == nil || !strings.Contains(err.Error(), "requires --workload-plan") {
		t.Fatalf("digest without plan error = %v", err)
	}
}
