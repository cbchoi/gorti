package m1spec

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/encoding"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
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

// TestSpec_M1_CompositeVectorsRoundTrip exercises encoding.CodecFor over the
// composite vectors. Each vector's Type is a {"kind": "...", ...} descriptor
// that we lift into a model.DataType, hand to CodecFor, and exercise:
//
//  1. encode(value) == bytes
//  2. decode(bytes) consumes all input
//  3. encode(decode(bytes)) == bytes  (round-trip)
//
// The third assertion is what catches padding bugs without needing a deep
// recursive value comparator.
//
// Implements: FR-ENC-1, FR-ENC-2 (composite).
func TestSpec_M1_CompositeVectorsRoundTrip(t *testing.T) {
	composites := 0
	for _, v := range loadVectors(t) {
		m, ok := v.Type.(map[string]any)
		if !ok {
			continue
		}
		composites++
		v := v
		typeMap := m
		t.Run(v.ID, func(t *testing.T) {
			t.Parallel()

			dt, err := compositeFromTypeMap(typeMap)
			if err != nil {
				t.Fatalf("build dataType from %v: %v", typeMap, err)
			}
			codec, err := encoding.CodecFor(dt)
			if err != nil {
				t.Fatalf("CodecFor(%T): %v", dt, err)
			}

			expected, err := hex.DecodeString(stripHexWhitespace(v.Bytes))
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
				t.Errorf("Decode consumed %d, want %d", n, len(expected))
			}

			re, err := codec.Encode(decoded)
			if err != nil {
				t.Fatalf("re-Encode(decoded=%v): %v", decoded, err)
			}
			if !bytes.Equal(re, expected) {
				t.Errorf("round-trip byte mismatch: got %x, want %x", re, expected)
			}
		})
	}
	if composites == 0 {
		t.Skip("no composite vectors in fixture set yet")
	}
}

// compositeFromTypeMap converts the JSON type descriptor used in vectors
// into a model.DataType that encoding.CodecFor can dispatch on.
func compositeFromTypeMap(m map[string]any) (model.DataType, error) {
	kind, _ := m["kind"].(string)
	switch kind {
	case "HLAfixedRecord":
		raw, ok := m["fields"].([]any)
		if !ok {
			return nil, fmt.Errorf("HLAfixedRecord: missing fields")
		}
		fields := make([]model.RecordField, len(raw))
		for i, fr := range raw {
			fm, _ := fr.(map[string]any)
			name, _ := fm["name"].(string)
			ft, _ := fm["type"].(string)
			fields[i] = model.RecordField{Name: name, DataType: ft}
		}
		return &model.FixedRecordData{TypeName: "anonymous", Field: fields}, nil
	case "HLAfixedArray":
		elem, _ := m["element"].(string)
		card, _ := m["cardinality"].(float64)
		return &model.ArrayData{
			TypeName:    "anonymous",
			DataType:    elem,
			Cardinality: strconv.Itoa(int(card)),
		}, nil
	case "HLAvariableArray":
		elem, _ := m["element"].(string)
		return &model.ArrayData{
			TypeName:    "anonymous",
			DataType:    elem,
			Cardinality: "Dynamic",
		}, nil
	case "HLAvariantRecord":
		disc, _ := m["discriminantType"].(string)
		raw, _ := m["alternatives"].([]any)
		alts := make([]model.VariantAlternative, len(raw))
		for i, ar := range raw {
			am, _ := ar.(map[string]any)
			enum, _ := am["enumerator"].(string)
			ty, _ := am["type"].(string)
			alts[i] = model.VariantAlternative{Enumerator: enum, DataType: ty}
		}
		return &model.VariantRecordData{
			TypeName:    "anonymous",
			DataType:    disc,
			Alternative: alts,
		}, nil
	}
	return nil, fmt.Errorf("unknown composite kind %q", kind)
}

func stripHexWhitespace(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		out = append(out, c)
	}
	return string(out)
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
