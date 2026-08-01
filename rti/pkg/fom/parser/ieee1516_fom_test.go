package parser

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCanonicalCommercialRtiVerifierFOMParsesWithoutDiagnostics(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller did not return the test source path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	path := filepath.Join(repoRoot, "verification", "commercial-rti", "fom", "CommercialRtiVerifier.xml")
	xml, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Parse([]Module{{Path: path, XML: xml}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("canonical reference_rti FOM diagnostics: %+v", result.Diagnostics)
	}
	if result.FOM == nil {
		t.Fatal("canonical reference_rti FOM produced no model")
	}
}
