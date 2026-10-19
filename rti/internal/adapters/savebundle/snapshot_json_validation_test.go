package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

type jsonValidationCodec interface {
	Marshal(core.FederationName) ([]byte, error)
	PrepareRestore(core.FederationName, []byte) (func(), error)
	Unmarshal(core.FederationName, []byte) error
}

func newJSONValidationCodec(t *testing.T, name string) jsonValidationCodec {
	t.Helper()
	switch name {
	case "object":
		codec := objectStateCodec(t)
		state := codecObjectState()
		state.Objects[0].Name = "model-\u017f-\u212a-\uc2dc\ubbac\ub808\uc774\uc158"
		commit, err := codec.Manager.PrepareImportState("fed", state)
		if err != nil {
			t.Fatal(err)
		}
		commit()
		return codec
	case "declaration":
		m := declaration.New()
		if err := m.PublishObjectClassAttributes(context.Background(), "fed", 1, 2, []core.AttributeHandle{3}); err != nil {
			t.Fatal(err)
		}
		return Declaration{Manager: m}
	case "time":
		codec, _, _ := newTimeStateCodec(t)
		if err := codec.Manager.EnableConstrained(context.Background(), "fed", 2); err != nil {
			t.Fatal(err)
		}
		event := timeStateCallbacks()[0]
		event.ObjectName = "model-\u017f-\u212a-\uc2dc\ubbac\ub808\uc774\uc158"
		if err := codec.Manager.BufferTSO(context.Background(), "fed", 2, *event.LogicalTime, event); err != nil {
			t.Fatal(err)
		}
		return codec
	case "mom":
		codec := completeMOMCodec(t)
		state := codec.Manager.ExportState("fed")
		state.Objects = mom.ObjectInstanceState{
			Enabled: true, FederationClass: 10, FederateClass: 11,
			FederationObject: 100, FederationObjectName: "federation-\u017f-\u212a-\uc2dc\ubbac\ub808\uc774\uc158",
		}
		state.Federates[0].ObjectHandle = 101
		state.Federates[0].ObjectName = "federate-\u017f-\u212a-\uc2dc\ubbac\ub808\uc774\uc158"
		codec.Manager.ImportState("fed", state)
		return codec
	default:
		t.Fatalf("unknown codec %q", name)
		return nil
	}
}

func assertJSONSnapshotRejected(t *testing.T, codec jsonValidationCodec, data []byte, errorText string) {
	t.Helper()
	before, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := codec.PrepareRestore("fed", data)
	if err == nil || commit != nil {
		t.Fatalf("invalid JSON snapshot accepted: commit nil=%v error=%v", commit == nil, err)
	}
	if errorText != "" && !strings.Contains(err.Error(), errorText) {
		t.Errorf("error=%v, want %q", err, errorText)
	}
	if err := codec.Unmarshal("fed", data); err == nil {
		t.Error("immediate unmarshal accepted invalid JSON")
	}
	after, err := codec.Marshal("fed")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("invalid JSON changed live state: %v", err)
	}
}

func TestSnapshotJSONAllAdaptersRejectInvalidFraming(t *testing.T) {
	for _, name := range []string{"object", "declaration", "time", "mom"} {
		t.Run(name, func(t *testing.T) {
			codec := newJSONValidationCodec(t, name)
			valid, err := codec.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			var header struct {
				Version json.RawMessage `json:"version"`
			}
			if err := json.Unmarshal(bytes.TrimPrefix(valid, []byte(momSnapshotMagic)), &header); err != nil {
				t.Fatal(err)
			}
			versionField := append([]byte(`"version":`), header.Version...)
			cases := []struct {
				name, replacement, suffix, errorText string
			}{
				{name: "duplicate ASCII case alias", replacement: `"version":2,"Version":1`, errorText: "duplicate"},
				{name: "duplicate escaped ASCII key", replacement: `"version":2,"\u0076ersion":1`, errorText: "duplicate"},
				{name: "single long-s version alias", replacement: `"ver\u017fion":1`, errorText: "ASCII"},
				{name: "duplicate long-s version alias", replacement: `"version":2,"ver\u017fion":1`, errorText: "ASCII"},
				{name: "unknown field", replacement: `"version":1,"unrecognized":0`, errorText: "unknown field"},
				{name: "invalid UTF-8", replacement: "\"version\":1,\"bad\xff\":0", errorText: "UTF-8"},
				{name: "excessive nesting", replacement: `"version":1,"unrecognized":` + strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40), errorText: "nesting"},
				{name: "trailing document", suffix: ` {}`, errorText: "trailing"},
				{name: "trailing garbage", suffix: ` garbage`},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					data := bytes.Clone(valid)
					if tc.replacement != "" {
						data = bytes.Replace(data, versionField, []byte(tc.replacement), 1)
					}
					data = append(data, tc.suffix...)
					if bytes.Equal(data, valid) {
						t.Fatal("fixture did not alter JSON")
					}
					assertJSONSnapshotRejected(t, codec, data, tc.errorText)
				})
			}
		})
	}
}

func TestSnapshotJSONReviewObjectLongSAlias(t *testing.T) {
	codec := newJSONValidationCodec(t, "object")
	data := []byte(`{"version":1,"state":{"Reservations":[{"Name":"x","Holder":0}],"Re\u017fervations":[]}}`)
	assertJSONSnapshotRejected(t, codec, data, "ASCII")
}

func TestSnapshotJSONReviewMOMDuplicateObjectCache(t *testing.T) {
	codec := newJSONValidationCodec(t, "mom")
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"objects":`), []byte(`"objects":{"Enabled":true},"objects":`), 1)
	assertJSONSnapshotRejected(t, codec, data, "duplicate")
}

func TestSnapshotJSONTimeKelvinAliases(t *testing.T) {
	codec := newJSONValidationCodec(t, "time")
	valid, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{`"\u212aind":"reflect"`, `"kind":"invalid","\u212aind":"reflect"`} {
		data := bytes.Replace(valid, []byte(`"kind":"reflect"`), []byte(alias), 1)
		if bytes.Equal(data, valid) {
			t.Fatal("fixture did not alter a valid kind field")
		}
		assertJSONSnapshotRejected(t, codec, data, "ASCII")
	}
}

func TestSnapshotJSONUnicodeValuesAndDataRoundTripUnchanged(t *testing.T) {
	for _, name := range []string{"object", "declaration", "time", "mom"} {
		t.Run(name, func(t *testing.T) {
			codec := newJSONValidationCodec(t, name)
			before, err := codec.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			// An escaped ASCII field is the same valid member name, not a duplicate.
			data := bytes.Replace(before, []byte(`"version":1`), []byte(`"\u0076ersion":1`), 1)
			data = append(data, ' ', '\r', '\n', '\t')
			commit, err := codec.PrepareRestore("fed", data)
			if err != nil {
				t.Fatal(err)
			}
			clear(data)
			commit()
			after, err := codec.Marshal("fed")
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("JSON validation changed serialized version/data: %v", err)
			}
		})
	}
}

func TestSnapshotJSONDepthBound(t *testing.T) {
	for _, depth := range []int{32, 33} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			data := []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
			var value any
			err := decodeSnapshotJSON(data, &value)
			if depth == 32 && err != nil {
				t.Fatalf("bounded JSON rejected: %v", err)
			}
			if depth == 33 && (err == nil || !strings.Contains(err.Error(), "nesting")) {
				t.Fatalf("excessive nesting accepted: %v", err)
			}
		})
	}
}

type snapshotJSONDecodeProbe struct{ called bool }

func (p *snapshotJSONDecodeProbe) UnmarshalJSON([]byte) error {
	p.called = true
	return nil
}

func TestSnapshotJSONValidationPrecedesTypedDecode(t *testing.T) {
	for name, data := range map[string][]byte{
		"duplicate": []byte(`{"a":1,"A":2}`),
		"non-ASCII": []byte(`{"\u017f":1}`),
		"UTF-8":     []byte("{\"name\":\"\xff\"}"),
		"nesting":   []byte(strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33)),
		"trailing":  []byte(`{} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			var probe snapshotJSONDecodeProbe
			if err := decodeSnapshotJSON(data, &probe); err == nil {
				t.Fatal("invalid JSON accepted")
			}
			if probe.called {
				t.Fatal("typed decoder invoked before structural validation completed")
			}
		})
	}
}

func TestSnapshotJSONNumericPayloadMapKeysAndUnicodeValues(t *testing.T) {
	values := make(map[uint64]string, 10001)
	for i := uint64(0); i < 10000; i++ {
		values[i] = "\u017f \u212a \uc2dc\ubbac\ub808\uc774\uc158"
	}
	values[^uint64(0)] = "max"
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var got map[uint64]string
	if err := decodeSnapshotJSON(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(values) || got[^uint64(0)] != "max" || got[9999] != values[9999] {
		t.Fatal("numeric map keys or Unicode string values changed")
	}
}
