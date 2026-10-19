package object

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func (b TransportationCheckpointBinding) MarshalJSON() ([]byte, error) {
	type plain TransportationCheckpointBinding
	normalized := plain(b)
	if normalized.Attributes == nil {
		normalized.Attributes = make([]core.AttributeHandle, 0)
	}
	if normalized.ActualAttributes == nil {
		normalized.ActualAttributes = make([]core.AttributeHandle, 0)
	}
	return json.Marshal(normalized)
}

func (b *TransportationCheckpointBinding) UnmarshalJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return fmt.Errorf("transportation checkpoint: binding must be an object")
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok || fields[name] != nil {
			return fmt.Errorf("transportation checkpoint: duplicate binding field")
		}
		switch name {
		case "object", "interaction", "attributes", "actual_attributes", "transportation", "activated":
		default:
			return fmt.Errorf("transportation checkpoint: unknown binding field %q", name)
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("transportation checkpoint: null binding field %q", name)
		}
		fields[name] = raw
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("transportation checkpoint: trailing binding data")
	}
	if err := requireTransportationBindingFields(fields); err != nil {
		return err
	}
	type plain TransportationCheckpointBinding
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := ValidateTransportationCheckpointBinding(TransportationCheckpointBinding(decoded)); err != nil {
		return err
	}
	*b = TransportationCheckpointBinding(decoded)
	return nil
}

func requireTransportationBindingFields(fields map[string]json.RawMessage) error {
	for _, name := range []string{"attributes", "actual_attributes", "transportation", "activated"} {
		if fields[name] == nil {
			return fmt.Errorf("transportation checkpoint: missing binding field %q", name)
		}
	}
	return nil
}
