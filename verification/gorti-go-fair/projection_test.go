package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectionIsCanonicalFourServiceSummary(t *testing.T) {
	cfg := config{Seed: "1516", Count: 2, OutputDir: t.TempDir()}
	payloads, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeProjection(cfg, payloads); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(cfg.OutputDir, "projected-canonical.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var rows []semanticRecord
	for scanner.Scan() {
		var row semanticRecord
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0].Event != "federation_lifecycle_verified" ||
		rows[3].Event != "time_management_verified" || rows[3].Actor != "verifier" {
		t.Fatalf("projection = %+v", rows)
	}
	grants, ok := rows[3].Data["grants"].([]any)
	if !ok || len(grants) != 3 {
		t.Fatalf("projected grants = %#v", rows[3].Data["grants"])
	}
}
