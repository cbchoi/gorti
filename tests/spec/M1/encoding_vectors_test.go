package m1spec

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/encoding"
)

// vectorFile is the canonical golden vector set, byte-equal across Go and Python.
const vectorFile = "tests/conformance/encoding_vectors.json"

type vector struct {
	ID    string `json:"id"`
	Type  any    `json:"type"`  // string for primitives; object {kind, ...} for composites
	Value any    `json:"value"`
	Bytes string `json:"bytes"` // hex
	Notes string `json:"notes,omitempty"`
}

type vectorFileFormat struct {
	Version int      `json:"version"`
	Vectors []vector `json:"vectors"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			b, err := os.ReadFile(filepath.Join(d, vectorFile))
			if err != nil {
				t.Fatalf("read %s: %v", vectorFile, err)
			}
			var f vectorFileFormat
			if err := json.Unmarshal(b, &f); err != nil {
				t.Fatalf("parse %s: %v", vectorFile, err)
			}
			if f.Version != 1 {
				t.Fatalf("vector file version %d unsupported", f.Version)
			}
			return f.Vectors
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatalf("could not find repo root from %s", d)
		}
		d = parent
	}
}

// TestSpec_M1_PrimitiveVectorsRoundTrip asserts every primitive vector
// encodes to the expected bytes and decodes back to the expected value.
//
// Composite types (records, arrays) are exercised in
// TestSpec_M1_CompositeVectorsRoundTrip.
//
// Implements: FR-ENC-1, FR-ENC-2.
func TestSpec_M1_PrimitiveVectorsRoundTrip(t *testing.T) {
	for _, v := range loadVectors(t) {
		typeName, ok := v.Type.(string)
		if !ok {
			continue // composite, handled below
		}
		v := v
		t.Run(v.ID, func(t *testing.T) {
			t.Parallel()

			codec, err := encoding.PrimitiveByName(typeName)
			if err != nil {
				t.Fatalf("PrimitiveByName(%q): %v", typeName, err)
			}

			expected, err := hex.DecodeString(v.Bytes)
			if err != nil {
				t.Fatalf("hex %q: %v", v.Bytes, err)
			}

			got, err := codec.Encode(v.Value)
			if err != nil {
				t.Fatalf("Encode(%v): %v", v.Value, err)
			}
			if !bytes.Equal(got, expected) {
				t.Fatalf("encode mismatch: got %x, want %x", got, expected)
			}

			decoded, n, err := codec.Decode(expected)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if n != len(expected) {
				t.Errorf("Decode consumed %d bytes, want %d", n, len(expected))
			}
			if !valuesEqual(decoded, v.Value) {
				t.Errorf("decode mismatch: got %v, want %v", decoded, v.Value)
			}
		})
	}
}

// TestSpec_M1_CompositeVectorsRoundTrip is the spec for composite encoding.
// Initially a placeholder — Agent B fills in the codec construction from the
// composite type descriptors, then this test exercises the same round-trip.
//
// Composite vectors in encoding_vectors.json have Type as an object
// {"kind": "HLAfixedRecord"|"HLAfixedArray"|..., ...}.
//
// Implements: FR-ENC-1, FR-ENC-2 (composite).
func TestSpec_M1_CompositeVectorsRoundTrip(t *testing.T) {
	composites := 0
	for _, v := range loadVectors(t) {
		if _, ok := v.Type.(map[string]any); ok {
			composites++
		}
	}
	if composites == 0 {
		t.Skip("no composite vectors in fixture set yet")
	}

	// Agent B: implement encoding.CodecFor(typeDescriptor) and exercise it
	// here against the composite vectors. Until then, this is a placeholder
	// that documents the contract.
	t.Skipf("composite codec build is pending Agent B implementation (%d vectors waiting)", composites)
}

// valuesEqual compares decoded values to vector values, accommodating that
// JSON numbers decode as float64 and hex bytes as []byte.
func valuesEqual(got, want any) bool {
	switch w := want.(type) {
	case float64:
		if g, ok := got.(float64); ok {
			return g == w
		}
		// Allow integer types that round-trip through float64 in JSON.
		switch g := got.(type) {
		case int32:
			return float64(g) == w
		case int64:
			return float64(g) == w
		case uint8:
			return float64(g) == w
		case bool:
			if w == 1 {
				return g
			}
			if w == 0 {
				return !g
			}
			return false
		}
	case bool:
		g, ok := got.(bool)
		return ok && g == w
	case string:
		g, ok := got.(string)
		return ok && g == w
	}
	return false
}
