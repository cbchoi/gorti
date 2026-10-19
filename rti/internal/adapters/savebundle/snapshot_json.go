package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const snapshotJSONMaxDepth = 32

// decodeSnapshotJSON validates structure before decoding a snapshot DTO. All
// snapshot member names are ASCII schema fields or numeric payload-map keys;
// string values may contain arbitrary valid UTF-8. Restricting member names
// also prevents Unicode aliases accepted by encoding/json's case folding.
func decodeSnapshotJSON(data []byte, target any) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8 in snapshot JSON")
	}
	scanner := json.NewDecoder(bytes.NewReader(data))
	scanner.UseNumber()
	if err := checkSnapshotJSONValue(scanner, 0); err != nil {
		return err
	}
	if _, err := scanner.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("trailing snapshot JSON data")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func checkSnapshotJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	container, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth >= snapshotJSONMaxDepth {
		return fmt.Errorf("snapshot JSON nesting exceeds %d levels", snapshotJSONMaxDepth)
	}
	switch container {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return fmt.Errorf("invalid snapshot JSON member name")
			}
			for i := 0; i < len(key); i++ {
				if key[i] >= utf8.RuneSelf {
					return fmt.Errorf("non-ASCII snapshot JSON member name %q", key)
				}
			}
			folded := strings.ToLower(key)
			if _, duplicate := seen[folded]; duplicate {
				return fmt.Errorf("duplicate snapshot JSON member name %q", key)
			}
			seen[folded] = struct{}{}
			if err := checkSnapshotJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := checkSnapshotJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected snapshot JSON delimiter %q", container)
	}
	_, err = decoder.Token()
	return err
}
