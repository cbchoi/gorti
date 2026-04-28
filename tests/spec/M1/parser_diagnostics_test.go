package m1spec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// fomFixturesDir returns the absolute path to tests/conformance/foms/.
// Computed relative to this test file so it works regardless of cwd.
func fomFixturesDir(t *testing.T) string {
	t.Helper()
	// Walk up to the repo root (where go.mod lives), then descend.
	d, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return filepath.Join(d, "tests", "conformance", "foms")
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatalf("could not find repo root from %s", d)
		}
		d = parent
	}
}

func loadFOM(t *testing.T, rel string) []byte {
	t.Helper()
	path := filepath.Join(fomFixturesDir(t), rel)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return b
}

// TestSpec_M1_ParseMinimalGoodFOM_NoDiagnostics asserts that the canonical
// minimal user FOM parses without diagnostics.
//
// Implements: FR-FOM-1 (acceptance path).
func TestSpec_M1_ParseMinimalGoodFOM_NoDiagnostics(t *testing.T) {
	xml := loadFOM(t, "good/minimal.xml")

	res, err := parser.Parse([]parser.Module{{Path: "good/minimal.xml", XML: xml}})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if res.FOM == nil {
		t.Fatal("Parse returned nil FOM for valid input")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %+v", len(res.Diagnostics), res.Diagnostics)
	}
}

// TestSpec_M1_ParsePyjevsimBridgeFOM_NoDiagnostics asserts the pyjevsim-bridge
// FOM (used by Agent C in M4) parses cleanly.
//
// Implements: FR-FOM-1.
func TestSpec_M1_ParsePyjevsimBridgeFOM_NoDiagnostics(t *testing.T) {
	xml := loadFOM(t, "good/pyjevsim-bridge.xml")

	res, err := parser.Parse([]parser.Module{{Path: "good/pyjevsim-bridge.xml", XML: xml}})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if res.FOM == nil {
		t.Fatal("Parse returned nil FOM for valid input")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %+v", len(res.Diagnostics), res.Diagnostics)
	}
}

// TestSpec_M1_BadFOMDiagnostics asserts each canned bad FOM is rejected with
// the expected diagnostic code. Table-driven so failures localize per case.
//
// Implements: FR-FOM-1 (rejection path), per-code list in docs/idd.md §1.2.1.
func TestSpec_M1_BadFOMDiagnostics(t *testing.T) {
	cases := []struct {
		name        string // fixture filename under foms/bad/
		expectCode  string // FOM-NNN diagnostic that must appear at least once
		description string // for failure messages
	}{
		{
			name:        "FOM-001-undefined-datatype.xml",
			expectCode:  "FOM-001",
			description: "attribute references an undeclared dataType",
		},
		{
			name:        "FOM-002-cyclic-class-hierarchy.xml",
			expectCode:  "FOM-002",
			description: "object class hierarchy contains a cycle",
		},
		{
			name:        "FOM-004-duplicate-attribute.xml",
			expectCode:  "FOM-004",
			description: "attribute name duplicated within a class",
		},
		{
			name:        "FOM-009-unknown-element.xml",
			expectCode:  "FOM-009",
			description: "unknown XML element (strict mode)",
		},
		{
			name:        "FOM-011-missing-parent-class.xml",
			expectCode:  "FOM-011",
			description: "class references non-existent parent",
		},
		{
			name:        "FOM-101-redefines-mim-type.xml",
			expectCode:  "FOM-101",
			description: "user module attempts to redefine an MIM type",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.expectCode, func(t *testing.T) {
			t.Parallel()
			xml := loadFOM(t, "bad/"+tc.name)

			res, err := parser.Parse([]parser.Module{{Path: tc.name, XML: xml}})
			if err != nil {
				// I/O errors only — content errors should be diagnostics, not Go errors.
				t.Fatalf("Parse returned error: %v (description: %s)", err, tc.description)
			}
			if !res.HasCode(tc.expectCode) {
				t.Fatalf(
					"expected diagnostic %s for %s; got %v (description: %s)",
					tc.expectCode, tc.name, res.CodeCounts(), tc.description,
				)
			}
			if res.FOM != nil {
				t.Errorf("%s: expected FOM nil on rejection, got non-nil", tc.name)
			}
		})
	}
}
