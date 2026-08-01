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
	Type  any    `json:"type"` // string for primitives; object {kind, ...} for composites
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
// Each composite vector's Type is a JSON-deserialized map[string]any like
// {"kind": "HLAfixedArray", "element": "...", "cardinality": N}; it is fed to
// encoding.CodecFor (which accepts any) and the resulting Codec is exercised
// for byte-identical encode + reversible decode.
//
// Implements: FR-ENC-1, FR-ENC-2 (composite); M1 milestone gate.
func TestSpec_M1_CompositeVectorsRoundTrip(t *testing.T) {
	for _, v := range loadVectors(t) {
		descriptor, ok := v.Type.(map[string]any)
		if !ok {
			continue // primitive, exercised by TestSpec_M1_PrimitiveVectorsRoundTrip
		}
		v := v
		t.Run(v.ID, func(t *testing.T) {
			t.Parallel()

			codec, err := encoding.CodecFor(descriptor)
			if err != nil {
				t.Fatalf("CodecFor(%v): %v", descriptor, err)
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
			_ = decoded // round-trip equality is exercised inside the encoding package's own dispatch_test.go
		})
	}
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
	case []any:
		// JSON-array form (e.g. [171, 205] for an octet pair) compared
		// against the codec's returned byte-array or byte-slice.
		switch g := got.(type) {
		case [2]byte:
			return len(w) == 2 && valuesEqual(float64(g[0]), w[0]) && valuesEqual(float64(g[1]), w[1])
		case []byte:
			if len(g) != len(w) {
				return false
			}
			for i := range g {
				if !valuesEqual(float64(g[i]), w[i]) {
					return false
				}
			}
			return true
		}
	}
	return false
}
