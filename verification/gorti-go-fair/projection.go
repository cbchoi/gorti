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
	grants := make([]int, cfg.Count+1)
	for index, item := range payloads {
		attributes[index] = item.attribute
		interactions[index] = item.interaction
		grants[index] = index + 1
	}
	grants[cfg.Count] = cfg.Count + 1
	rows := []semanticRecord{
		{Kind: "semantic", Seq: 0, Service: "FM", Event: "federation_lifecycle_verified", Actor: "verifier", Data: map[string]any{
			"federates": 2, "sync_labels": []string{readySync, doneSync},
		}},
		{Kind: "semantic", Seq: 1, Service: "DM", Event: "declarations_verified", Actor: "verifier", Data: map[string]any{
			"object_class": objectClass, "interaction_class": interactionClass,
		}},
		{Kind: "semantic", Seq: 2, Service: "OM", Event: "timestamped_workload_verified", Actor: "verifier", Data: map[string]any{
			"count": cfg.Count, "named_instance": true, "removed": true,
			"attribute_payloads": attributes, "interaction_payloads": interactions,
		}},
		{Kind: "semantic", Seq: 3, Service: "TM", Event: "time_management_verified", Actor: "verifier", Data: map[string]any{
			"lookahead": 1, "order": "TimeStamp", "grants": grants,
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
