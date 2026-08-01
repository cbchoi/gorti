package main

import (
	"os"
	"strings"
	"testing"
)

func TestLauncherPinsFairServerAndMetadataContract(t *testing.T) {
	content, err := os.ReadFile("run.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	for _, required := range []string{
		"[ValidateSet('off', 'file')]",
		"'--log-dir='",
		`"--log-dir=$EventLogDirectory"`,
		"--outbox-event-capacity=$OutboxEventCapacity",
		"--local-lrc-batch-size=$LocalLRCBatchSize",
		"--callback-representation=$CallbackRepresentation",
		"[switch]$Confirmed",
		"[string]$TransportConfig",
		"--transport-config=",
		"if ($ReceiveOrder -and -not $Confirmed)",
		"if ($Confirmed) { $Common += '--confirmed=true' }",
		"gorti.fair-comparison/workload-v1",
		"delivery_boundary",
		"callback = 'immediate'",
		"server_event_log = $ServerEventLog",
		"duplicates = 0; invalid = 0",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("launcher is missing %q", required)
		}
	}
}
