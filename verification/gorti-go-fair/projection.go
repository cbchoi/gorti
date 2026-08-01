package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func writeProjection(cfg config, payloads []encodedIteration) error {
	attributes := make([]string, len(payloads))
	interactions := make([]string, len(payloads))
	grants := make([]int, 0, cfg.Count+1)
	for index, item := range payloads {
		attributes[index] = item.attribute
		interactions[index] = item.interaction
		if !cfg.ReceiveOrder {
			grants = append(grants, index+1)
		}
	}
	if !cfg.ReceiveOrder {
		grants = append(grants, cfg.Count+1)
	}
	omEvent := "timestamped_workload_verified"
	tmEvent := "time_management_verified"
	order := "TimeStamp"
	if cfg.ReceiveOrder {
		omEvent = "receive_order_workload_verified"
		tmEvent = "time_management_excluded"
		order = "Receive"
	}
	var lookahead any = 1
	if cfg.ReceiveOrder {
		lookahead = nil
	}
	syncLabels := []string{readySync}
	if cfg.OperationWarmup > 0 || !cfg.ReceiveOrder {
		syncLabels = append(syncLabels, measureSync)
	}
	if cfg.ReceiveOrder && cfg.devstoneMode() {
		if cfg.OperationWarmup == 0 {
			syncLabels = append(syncLabels, measureSync)
		}
		syncLabels = append(syncLabels, startSync)
	}
	syncLabels = append(syncLabels, doneSync)
	rows := []semanticRecord{
		{Kind: "semantic", Seq: 0, Service: "FM", Event: "federation_lifecycle_verified", Actor: "verifier", Data: map[string]any{
			"federates": 2, "sync_labels": syncLabels,
		}},
		{Kind: "semantic", Seq: 1, Service: "DM", Event: "declarations_verified", Actor: "verifier", Data: map[string]any{
			"object_class": cfg.ObjectClass, "interaction_class": cfg.InteractionClass,
		}},
		{Kind: "semantic", Seq: 2, Service: "OM", Event: omEvent, Actor: "verifier", Data: map[string]any{
			"count": cfg.Count, "named_instance": true, "removed": true,
			"attribute_payloads": attributes, "interaction_payloads": interactions,
		}},
		{Kind: "semantic", Seq: 3, Service: "TM", Event: tmEvent, Actor: "verifier", Data: map[string]any{
			"lookahead": lookahead, "order": order, "grants": grants,
		}},
	}
	file, err := os.Create(filepath.Join(cfg.OutputDir, "projected-canonical.ndjson"))
	if err != nil {
		return fmt.Errorf("create projected canonical log: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			_ = file.Close()
			return err
		}
	}
	return file.Close()
}

