package mim_test

import (
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// TestProperty_StandardMIMBytes_ParsesCleanly asserts the embedded standard
// MIM XML byte stream is non-empty and parses through parser.Parse with zero
// diagnostics. The MIM is the project-wide source of truth for the names
// FOM-101 protects against; if it cannot be parsed, every downstream
// validation that depends on it is broken.
//
// Implements: FR-FOM-2, FR-FOM-3.
func TestProperty_StandardMIMBytes_ParsesCleanly(t *testing.T) {
	t.Parallel()

	got := mim.StandardMIMBytes()
	if len(got) == 0 {
		t.Fatal("StandardMIMBytes returned 0 bytes; expected embedded MIM XML")
	}
	res, err := parser.Parse([]parser.Module{{Path: "mim/standard-mim.xml", XML: got}})
	if err != nil {
		t.Fatalf("Parse(StandardMIMBytes) returned error: %v", err)
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("Parse(StandardMIMBytes) reported %d diagnostics; want 0: %+v",
			len(res.Diagnostics), res.Diagnostics)
	}
	if res.FOM == nil {
		t.Fatal("Parse(StandardMIMBytes) returned nil FOM")
	}
}

// TestProperty_HLAStandardMIMBytes_ParsesCleanly mirrors the standard-MIM
// property test for the HLAstandardMIM module (1516.1-2010 §4.13). For cut-1
// the file is a thin wrapper that introduces no additional declarations; the
// invariant is still that it parses cleanly through the same diagnoser stack.
//
// Implements: FR-FOM-2, FR-FOM-3.
func TestProperty_HLAStandardMIMBytes_ParsesCleanly(t *testing.T) {
	t.Parallel()

	got := mim.HLAStandardMIMBytes()
	if len(got) == 0 {
		t.Fatal("HLAStandardMIMBytes returned 0 bytes; expected embedded XML")
	}
	res, err := parser.Parse([]parser.Module{{Path: "mim/hla-standard-mim.xml", XML: got}})
	if err != nil {
		t.Fatalf("Parse(HLAStandardMIMBytes) returned error: %v", err)
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("Parse(HLAStandardMIMBytes) reported %d diagnostics; want 0: %+v",
			len(res.Diagnostics), res.Diagnostics)
	}
	if res.FOM == nil {
		t.Fatal("Parse(HLAStandardMIMBytes) returned nil FOM")
	}
}

// TestProperty_StandardMIMBytes_DefensiveCopy asserts that callers cannot
// mutate the embedded MIM through the byte slice returned by
// StandardMIMBytes — repeated calls return independent copies so a corrupt
// caller cannot poison subsequent reads.
//
// Implements: package invariant (immutable embedded data).
func TestProperty_StandardMIMBytes_DefensiveCopy(t *testing.T) {
	t.Parallel()

	first := mim.StandardMIMBytes()
	if len(first) == 0 {
		t.Fatal("StandardMIMBytes returned 0 bytes")
	}
	original := first[0]
	first[0] = ^original

	second := mim.StandardMIMBytes()
	if second[0] != original {
		t.Fatalf("second call observed mutation: got byte 0 = %#x; want %#x",
			second[0], original)
	}
}

// TestSpec_StandardMIMHandle_ReturnsParsedFOM asserts that StandardMIMHandle
// returns a non-nil parsed FOM containing the canonical HLAobjectRoot class
// and at least one canonical primitive (HLAfloat64BE). This is the lookup
// surface the FOM-101 redefine detector and the FOM-001 dataType-resolution
// validator both consume.
//
// Implements: FR-FOM-2, FR-FOM-3.
func TestSpec_StandardMIMHandle_ReturnsParsedFOM(t *testing.T) {
	t.Parallel()

	fom, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("StandardMIMHandle returned error: %v", err)
	}
	if fom == nil {
		t.Fatal("StandardMIMHandle returned nil FOM")
	}

	wantClass := "HLAobjectRoot"
	foundClass := false
	for _, oc := range fom.ObjectClasses() {
		if oc.Name == wantClass {
			foundClass = true
			break
		}
	}
	if !foundClass {
		t.Errorf("StandardMIMHandle: missing object class %q", wantClass)
	}

	wantDT := "HLAfloat64BE"
	foundDT := false
	for _, dt := range fom.DataTypes() {
		if dt.Name() == wantDT {
			foundDT = true
			break
		}
	}
	if !foundDT {
		t.Errorf("StandardMIMHandle: missing dataType %q", wantDT)
	}
}

// TestSpec_StandardMIMHandle_Memoized asserts repeated calls return the same
// underlying *model.FOM pointer, so callers (FOM-001, FOM-101) can cheaply
// hit the lookup without re-parsing the embedded XML on every Parse call.
func TestSpec_StandardMIMHandle_Memoized(t *testing.T) {
	t.Parallel()

	a, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	b, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if a != b {
		t.Errorf("StandardMIMHandle is not memoized: %p != %p", a, b)
	}
}
