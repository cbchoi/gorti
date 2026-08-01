package encoding

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// compositeVector is a minimal projection of the encoding_vectors.json
// schema sufficient to exercise the composite-codec build path. Loaded
// once per test by loadCompositeVectors and consumed by the round-trip
// test in dispatch_test.go.
type compositeVector struct {
	id         string
	descriptor map[string]any
	value      any
	bytesHex   string
}

// loadCompositeVectors finds the repo root by walking up from the test
// working directory until it sees go.mod, then reads
// tests/conformance/encoding_vectors.json and selects every vector whose
// "type" field is a JSON object (composite). Returns them in file order.
func loadCompositeVectors(t *testing.T) []compositeVector {
	t.Helper()

	d, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			path := filepath.Join(d, "tests", "conformance", "encoding_vectors.json")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var f struct {
				Version int `json:"version"`
				Vectors []struct {
					ID    string `json:"id"`
					Type  any    `json:"type"`
					Value any    `json:"value"`
					Bytes string `json:"bytes"`
				} `json:"vectors"`
			}
			if err := json.Unmarshal(b, &f); err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			out := make([]compositeVector, 0)
			for _, v := range f.Vectors {
				desc, ok := v.Type.(map[string]any)
				if !ok {
					continue
				}
				out = append(out, compositeVector{
					id:         v.ID,
					descriptor: desc,
					value:      v.Value,
					bytesHex:   v.Bytes,
				})
			}
			return out
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatalf("could not find repo root from %s", d)
		}
		d = parent
	}
}

// normalizeValue converts a JSON-decoded value to the typed Go value the
// codec built from descriptor expects. JSON gives us float64 for every
// number and string for every string; the codecs want int32 for int32
// fields, byte for octets, []byte for opaque, etc.
func normalizeValue(descriptor map[string]any, v any) any {
	kind, _ := descriptor["kind"].(string)
	switch kind {
	case "HLAfixedArray", "HLAvariableArray":
		elemName, _ := descriptor["element"].(string)
		arr, _ := v.([]any)
		out := make([]any, len(arr))
		for i, x := range arr {
			out[i] = normalizePrimitive(elemName, x)
		}
		return out
	case "HLAfixedRecord":
		fields, _ := descriptor["fields"].([]any)
		m, _ := v.(map[string]any)
		out := make(map[string]any, len(fields))
		for _, f := range fields {
			fm, _ := f.(map[string]any)
			name, _ := fm["name"].(string)
			ftype := fm["type"]
			val := m[name]
			switch ft := ftype.(type) {
			case string:
				out[name] = normalizePrimitive(ft, val)
			case map[string]any:
				out[name] = normalizeValue(ft, val)
			}
		}
		return out
	case "HLAvariantRecord":
		discName, _ := descriptor["discriminator"].(string)
		alts, _ := descriptor["alternatives"].([]any)
		m, _ := v.(map[string]any)
		discRaw := m["discriminator"]
		valRaw := m["value"]
		discNorm := normalizePrimitive(discName, discRaw)

		// Find alt whose discriminant matches discRaw.
		var altType any
		for _, a := range alts {
			am, _ := a.(map[string]any)
			if dEq(am["discriminant"], discRaw) {
				altType = am["type"]
				break
			}
		}
		var valNorm any
		switch at := altType.(type) {
		case string:
			valNorm = normalizePrimitive(at, valRaw)
		case map[string]any:
			valNorm = normalizeValue(at, valRaw)
		default:
			valNorm = valRaw
		}
		return map[string]any{
			"discriminator": discNorm,
			"value":         valNorm,
		}
	case "HLAopaqueData":
		// Vector encodes payload as a hex string ("" for empty).
		s, _ := v.(string)
		if s == "" {
			return []byte{}
		}
		raw, err := hex.DecodeString(s)
		if err != nil {
			return []byte(s)
		}
		return raw
	}
	return v
}

// normalizePrimitive converts a JSON-decoded value to the Go type the
// named primitive codec accepts on Encode and emits on Decode.
func normalizePrimitive(name string, v any) any {
	switch name {
	case "HLAinteger16BE", "HLAinteger16LE":
		if f, ok := v.(float64); ok {
			return int32(int16(f))
		}
	case "HLAinteger32BE", "HLAinteger32LE":
		if f, ok := v.(float64); ok {
			return int32(f)
		}
	case "HLAinteger64BE", "HLAinteger64LE":
		if f, ok := v.(float64); ok {
			return int64(f)
		}
	case "HLAfloat32BE", "HLAfloat32LE":
		if f, ok := v.(float64); ok {
			return float32(f)
		}
	case "HLAfloat64BE", "HLAfloat64LE":
		if f, ok := v.(float64); ok {
			return f
		}
	case "HLAoctet":
		if f, ok := v.(float64); ok {
			return byte(f)
		}
	case "HLAboolean":
		if f, ok := v.(float64); ok {
			return f != 0
		}
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return v
}

// dEq compares two JSON-decoded numbers loosely (any numeric type vs any
// numeric type) as well as strings. Used to match a vector's
// "discriminator" against an alternative's "discriminant" without caring
// which numeric Go type holds them.
func dEq(a, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	return reflect.DeepEqual(a, b)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case byte:
		return float64(x), true
	}
	return 0, false
}

// compositeValuesEqual compares a decoded value (coming back from
// Codec.Decode) to the expected value (post-normalization) for the
// purposes of the composite vector round-trip test. It accommodates the
// inevitable boxing in maps/slices.
func compositeValuesEqual(got, want any) bool {
	switch w := want.(type) {
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !compositeValuesEqual(g[i], w[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for k, wv := range w {
			gv, present := g[k]
			if !present {
				return false
			}
			if !compositeValuesEqual(gv, wv) {
				return false
			}
		}
		return true
	case []byte:
		g, ok := got.([]byte)
		if !ok {
			return false
		}
		if len(g) != len(w) {
			return false
		}
		for i := range w {
			if g[i] != w[i] {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}
