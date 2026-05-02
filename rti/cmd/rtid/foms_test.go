package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// minimalFOMXML returns a small valid FOM with one Vehicle objectClass + one
// Honk interactionClass; this matches tests/conformance/foms/good/minimal.xml
// but is inlined here so tests don't depend on relative file paths.
func minimalFOMXML(t *testing.T) []byte {
	t.Helper()
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>fom-repo-test</name>
    <type>FOM</type>
    <version>1.0</version>
    <modificationDate>2026-04-29</modificationDate>
    <securityClassification>Unclassified</securityClassification>
    <description>Test FOM.</description>
    <useHistory>None</useHistory>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name>
        <sharing>PublishSubscribe</sharing>
        <semantics>A simulated vehicle.</semantics>
        <attribute>
          <name>position</name>
          <dataType>HLAfloat64BE</dataType>
          <updateType>Periodic</updateType>
          <updateCondition>NA</updateCondition>
          <ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation>
          <order>TimeStamp</order>
          <semantics>Position scalar.</semantics>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <interactionClass>
        <name>Honk</name>
        <sharing>PublishSubscribe</sharing>
        <transportation>HLAreliable</transportation>
        <order>TimeStamp</order>
        <semantics>Vehicle honks.</semantics>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>`
	return []byte(xml)
}

// TestFOMRepository_LoadValid: a well-formed FOM module loads successfully
// and the returned handle reports IsValid().
func TestFOMRepository_LoadValid(t *testing.T) {
	repo := newFOMRepository()
	h, err := repo.Load(context.Background(), []core.FOMModule{
		{Path: "fom-repo-test", XML: minimalFOMXML(t)},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !h.IsValid() {
		t.Errorf("Load: handle.IsValid() = false; want true")
	}
}

// TestFOMRepository_LoadEmpty: the federation manager calls Load with an
// empty modules slice when CreateFederation is invoked with FOMModules nil
// (e.g. spec tests using stub FOMs). The repo MUST accept this and return
// a valid empty handle, not an error.
func TestFOMRepository_LoadEmpty(t *testing.T) {
	repo := newFOMRepository()
	h, err := repo.Load(context.Background(), nil)
	if err != nil {
		t.Fatalf("Load nil modules: %v", err)
	}
	if !h.IsValid() {
		t.Errorf("Load nil: handle.IsValid() = false; want true (empty FOM is valid)")
	}
}

// TestFOMRepository_LoadInvalid: a malformed FOM module returns an error.
func TestFOMRepository_LoadInvalid(t *testing.T) {
	repo := newFOMRepository()
	_, err := repo.Load(context.Background(), []core.FOMModule{
		{Path: "bad", XML: []byte("<not-objectModel/>")},
	})
	if err == nil {
		t.Errorf("Load malformed module: got nil error")
	}
}

// TestFOMRepository_Lookup: the loaded handle resolves Vehicle and Honk by
// HLA-qualified path (or by leaf name; the adapter accepts both for cut-1).
func TestFOMRepository_Lookup(t *testing.T) {
	repo := newFOMRepository()
	h, err := repo.Load(context.Background(), []core.FOMModule{
		{Path: "fom-repo-test", XML: minimalFOMXML(t)},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := h.LookupObjectClass("Vehicle"); !ok {
		t.Errorf("LookupObjectClass(Vehicle): not found")
	}
	if _, ok := h.LookupInteractionClass("Honk"); !ok {
		t.Errorf("LookupInteractionClass(Honk): not found")
	}
}

// TestFOMRepository_LoadFromFile loads a FOM module from disk to confirm
// the typical rtid usage pattern (cmd/rtid resolves a --fom path and reads
// bytes off the filesystem).
func TestFOMRepository_LoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fom.xml")
	if err := os.WriteFile(path, minimalFOMXML(t), 0o644); err != nil {
		t.Fatalf("write fom: %v", err)
	}
	xmlBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fom: %v", err)
	}
	repo := newFOMRepository()
	if _, err := repo.Load(context.Background(), []core.FOMModule{
		{Path: path, XML: xmlBytes},
	}); err != nil {
		t.Errorf("Load from file: %v", err)
	}
}

// TestFOMRepository_GetUnknown: Get for an unrecorded federation returns
// ErrFederationNotFound.
func TestFOMRepository_GetUnknown(t *testing.T) {
	repo := newFOMRepository()
	_, err := repo.Get(context.Background(), "ghost")
	if !errors.Is(err, core.ErrFederationNotFound) {
		t.Errorf("Get(ghost): err = %v, want ErrFederationNotFound", err)
	}
}

// TestFOMRepository_LookupAttribute_AndParameter: with a loaded FOM,
// the (1, "position") and (1, "Honk-style param") lookups follow the
// indexed-from-1 contract.
func TestFOMRepository_LookupAttributeAndParameter(t *testing.T) {
	repo := newFOMRepository()
	h, err := repo.Load(context.Background(), []core.FOMModule{
		{Path: "lookup", XML: minimalFOMXML(t)},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cls, ok := h.LookupObjectClass("Vehicle")
	if !ok {
		t.Fatalf("no Vehicle class")
	}
	if _, ok := h.LookupAttribute(cls, "position"); !ok {
		t.Errorf("LookupAttribute(Vehicle, position) not found")
	}
	if _, ok := h.LookupAttribute(cls, "ghost"); ok {
		t.Errorf("LookupAttribute(Vehicle, ghost) returned ok=true")
	}
	if _, ok := h.LookupAttribute(99, "position"); ok {
		t.Errorf("LookupAttribute(99, position) returned ok=true for invalid class")
	}
	// LookupParameter on Honk has no params; should return false.
	ic, _ := h.LookupInteractionClass("Honk")
	if _, ok := h.LookupParameter(ic, "anything"); ok {
		t.Errorf("LookupParameter(Honk, anything) returned ok=true")
	}
	if _, ok := h.LookupParameter(99, "x"); ok {
		t.Errorf("LookupParameter(99, x) returned ok=true for invalid class")
	}
}

// TestFOMRepository_LeafNameQualified: the leafName helper handles both
// HLA-qualified ("HLAobjectRoot.Vehicle") and plain ("Vehicle") forms.
func TestFOMRepository_LeafNameQualified(t *testing.T) {
	repo := newFOMRepository()
	h, err := repo.Load(context.Background(), []core.FOMModule{
		{Path: "qual", XML: minimalFOMXML(t)},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := h.LookupObjectClass("HLAobjectRoot.Vehicle"); !ok {
		t.Errorf("LookupObjectClass(HLAobjectRoot.Vehicle) not found")
	}
	if _, ok := h.LookupInteractionClass("HLAinteractionRoot.Honk"); !ok {
		t.Errorf("LookupInteractionClass(HLAinteractionRoot.Honk) not found")
	}
}

// TestFOMRepository_LoadInvalidXML_DiagnosticsAggregated: when the
// parser surfaces FOM-* diagnostics, Load returns them in the error.
func TestFOMRepository_LoadDiagnosticsInError(t *testing.T) {
	// A FOM with a duplicate attribute should surface FOM-004.
	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>dup-attr</name><type>FOM</type><version>1</version>
    <modificationDate>2026-01-01</modificationDate>
    <securityClassification>U</securityClassification>
    <description>x</description><useHistory>none</useHistory>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name><sharing>PublishSubscribe</sharing><semantics>x</semantics>
        <attribute><name>pos</name><dataType>HLAfloat64BE</dataType><updateType>P</updateType>
          <updateCondition>NA</updateCondition><ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing><transportation>HLAreliable</transportation>
          <order>TimeStamp</order><semantics>x</semantics></attribute>
        <attribute><name>pos</name><dataType>HLAfloat64BE</dataType><updateType>P</updateType>
          <updateCondition>NA</updateCondition><ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing><transportation>HLAreliable</transportation>
          <order>TimeStamp</order><semantics>x</semantics></attribute>
      </objectClass>
    </objectClass>
  </objects>
</objectModel>`)
	repo := newFOMRepository()
	_, err := repo.Load(context.Background(), []core.FOMModule{
		{Path: "dup", XML: bad},
	})
	if err == nil {
		t.Errorf("Load with duplicate attribute returned nil error")
	}
}

// TestFOMRepository_RememberAndGet: after RememberFor records a federation's
// handle, Get returns the same handle.
func TestFOMRepository_RememberAndGet(t *testing.T) {
	repo := newFOMRepository()
	h, err := repo.Load(context.Background(), []core.FOMModule{
		{Path: "fom-repo-test", XML: minimalFOMXML(t)},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repo.RememberFor("alpha", h)
	got, err := repo.Get(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("Get(alpha): %v", err)
	}
	if got != h {
		t.Errorf("Get(alpha): got different handle; remembered handle wasn't returned")
	}
}
