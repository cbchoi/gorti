// Package sdkstate validates SDK checkpoints before target identity allocation
// and rewrites explicit federate references, never arbitrary numeric handles.
package sdkstate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

const MaxEntries = 8192

type Envelope struct {
	Magic      string          `json:"magic"`
	Version    uint32          `json:"version"`
	Language   string          `json:"language"`
	Scalar     string          `json:"scalar"`
	Generation uint64          `json:"generation"`
	Self       uint64          `json:"self"`
	State      json.RawMessage `json:"state"`
	Checksum   string          `json:"checksum"`
}

func checksum(e Envelope) (string, error) {
	var state bytes.Buffer
	if err := json.Compact(&state, e.State); err != nil {
		return "", err
	}
	header := "GRS1\n1\n" + e.Language + "\n" + e.Scalar + "\n" + strconv.FormatUint(e.Generation, 10) + "\n" + strconv.FormatUint(e.Self, 10) + "\n"
	hash := sha256.New()
	_, _ = hash.Write([]byte(header))
	_, _ = hash.Write(state.Bytes())
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Encode preserves state member order and avoids HTML escaping checksum bytes.
func Encode(e Envelope) ([]byte, error) {
	e.Magic, e.Version = "GRS1", 1
	var err error
	e.Checksum, err = checksum(e)
	if err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	w := json.NewEncoder(&encoded)
	w.SetEscapeHTML(false)
	if err = w.Encode(e); err != nil {
		return nil, err
	}
	data := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
	if len(data) > savepoint.MaxClientStateBytes {
		return nil, savepoint.ErrClientStateTooLarge
	}
	return data, nil
}

func Decode(encoded []byte) (Envelope, error) {
	var e Envelope
	if len(encoded) == 0 || len(encoded) > savepoint.MaxClientStateBytes || !utf8.Valid(encoded) {
		return e, errors.New("invalid SDK checkpoint size")
	}
	if err := validateJSON(encoded); err != nil {
		return e, err
	}
	if err := strictDecode(encoded, &e); err != nil {
		return e, err
	}
	if !validEnvelopeIdentity(e) {
		return e, errors.New("unknown SDK checkpoint format or invalid identity")
	}
	expected, err := checksum(e)
	if err != nil || expected != e.Checksum {
		return e, errors.New("SDK checkpoint checksum mismatch")
	}
	if err = validateState(e); err != nil {
		return e, err
	}
	return e, nil
}

func strictDecode(data []byte, target any) error {
	if err := validateFieldShape(data, reflect.TypeOf(target).Elem()); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing SDK checkpoint JSON")
	}
	return nil
}

// The stock decoder matches struct names case-insensitively and treats null as
// the zero value of numeric fields. Neither is a portable schema agreement.
func validateFieldShape(data json.RawMessage, typ reflect.Type) error {
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	if typ.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return nil
		}
		return validateFieldShape(data, typ.Elem())
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("null SDK field has no declared nullable type")
	}
	switch typ.Kind() {
	case reflect.Struct:
		return validateStructShape(data, typ)
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for _, raw := range values {
			if err := validateFieldShape(raw, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

// Reject duplicates before ordinary decoding can silently select one value.
func validateJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()

	if err := validateJSONValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing SDK checkpoint JSON")
	}
	return nil
}

func Validate(ctx context.Context, in savepoint.ClientStateValidationRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e, err := Decode(in.State)
	if err != nil {
		return err
	}
	if e.Generation != in.SourceGeneration || e.Self != uint64(in.SourceHandle) || (in.LogicalTimeImplementationName != "" && e.Scalar != in.LogicalTimeImplementationName) {
		return errors.New("SDK checkpoint source identity or scalar mismatch")
	}
	if e.Language == checkpointJava {
		var s javaState
		_ = json.Unmarshal(e.State, &s)
		if s.Federation != string(in.Federation) || s.Label != in.Label {
			return errors.New("Java checkpoint federation/label mismatch")
		}
	}
	return nil
}

func validEnvelopeIdentity(e Envelope) bool {
	return e.Magic == "GRS1" && e.Version == 1 &&
		(e.Language == "go" || e.Language == checkpointPython || e.Language == checkpointJava) &&
		(e.Scalar == checkpointHLAfloat64Time || e.Scalar == checkpointHLAinteger64Time) && validFederate(e.Self, e.Generation)
}

func validateStructShape(data json.RawMessage, typ reflect.Type) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("SDK state field must be an object")
	}
	allowed := structFieldShapes(typ)
	for name, raw := range fields {
		field, ok := allowed[name]
		if !ok {
			return fmt.Errorf("unknown SDK state field %s", name)
		}
		if err := validateFieldShape(raw, field.typ); err != nil {
			return fmt.Errorf("SDK state field %s: %w", name, err)
		}
	}
	for name, field := range allowed {
		if _, present := fields[name]; !present && !field.optional {
			return fmt.Errorf("missing SDK state field %s", name)
		}
	}
	return nil
}

func structFieldShapes(typ reflect.Type) map[string]fieldShape {
	allowed := map[string]fieldShape{}
	var collect func(reflect.Type)
	collect = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.Anonymous {
				collect(field.Type)
				continue
			}
			tag := field.Tag.Get("json")
			parts := strings.Split(tag, ",")
			if parts[0] == "-" {
				continue
			}
			optional := len(parts) > 1 && parts[1] == "omitempty"
			allowed[parts[0]] = fieldShape{typ: field.Type, optional: optional}
		}
	}
	collect(typ)
	return allowed
}

type fieldShape struct {
	typ      reflect.Type
	optional bool
}

func validateJSONValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("SDK checkpoint nesting limit")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	count := 0
	switch delim {
	case '{':
		if err := validateJSONObject(d, depth); err != nil {
			return err
		}
	case '[':
		for d.More() {
			count++
			if count > MaxEntries {
				return errors.New("SDK checkpoint collection limit")
			}
			if err := validateJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid SDK checkpoint JSON delimiter")
	}
	_, err = d.Token()
	return err
}

func validateJSONObject(d *json.Decoder, depth int) error {
	count := 0
	keys := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok || keys[name] {
			return errors.New("duplicate SDK checkpoint key")
		}
		keys[name] = true
		count++
		if count > MaxEntries {
			return errors.New("SDK checkpoint collection limit")
		}
		if err := validateJSONValue(d, depth+1); err != nil {
			return err
		}
	}
	return nil
}

const checkpointHLAfloat64Time = "HLAfloat64Time"

const checkpointPython = "python"

const checkpointHLAinteger64Time = "HLAinteger64Time"

const checkpointJava = "java"
