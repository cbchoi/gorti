package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRunMetadataSeparatesWorkloadFromRunLocation(t *testing.T) {
	rtidPath := filepath.Join(t.TempDir(), "rtid.exe")
	if err := os.WriteFile(rtidPath, []byte("rtid-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	fomXML := []byte("<objectModel/>")
	base := commandConfig{
		workloadConfig: workloadConfig{
			Address:    "127.0.0.1:8801",
			Federation: "metadata-test",
			FOMPath:    `C:\runs\one\fom.xml`,
			Count:      10,
			Seed:       1516,
			Timeout:    time.Second,
		},
		RTIDPath:     rtidPath,
		RTIDVersion:  "test",
		SourceCommit: "test-commit",
		ServerArgs:   []string{"--listen=127.0.0.1:8801", `--log-dir=C:\runs\one\logs`},
	}
	other := base
	other.Address = "127.0.0.1:8802"
	other.FOMPath = `D:\runs\two\fom.xml`
	other.ServerArgs = []string{"--listen=127.0.0.1:8802", `--log-dir=D:\runs\two\logs`}

	startedAt := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	first, err := buildRunMetadata(base, startedAt, fomXML)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildRunMetadata(other, startedAt, fomXML)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(first.Workload, second.Workload) {
		t.Fatalf("run location changed workload fingerprint:\nfirst=%v\nsecond=%v", first.Workload, second.Workload)
	}
	if first.Environment["address"] == second.Environment["address"] {
		t.Fatal("runtime endpoint was not retained in environment provenance")
	}
	if reflect.DeepEqual(first.Provenance.BuildFlags, second.Provenance.BuildFlags) {
		t.Fatal("exact server arguments were not retained in provenance")
	}
}
