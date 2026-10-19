package savebundle

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func retractionV2Fixture(t *testing.T, timestamp any) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"version": 2, "history": map[string]any{"complete": true, "producers": []any{
		map[string]any{"producer": 1, "accepted": []any{map[string]any{"first": 7, "last": 8}}, "messages": []any{
			map[string]any{"id": 7, "timestamp": timestamp, "delivered": []any{2, 3}},
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRetractionV2RoundTripAndLegacy(t *testing.T) {
	for _, timestamp := range []any{5, "+Inf"} {
		t.Run(stringMustJSON(t, timestamp), func(t *testing.T) {
			s, _, _ := newTimeStateCodec(t)
			codec := Retractions{Manager: s.Manager}
			if err := codec.Unmarshal("fed", retractionV2Fixture(t, timestamp)); err != nil {
				t.Fatal(err)
			}
			round, err := codec.Marshal("fed")
			if err != nil || !bytes.Contains(round, []byte(`"version":2`)) {
				t.Fatalf("lost v2 ledger: %s %v", round, err)
			}
			history, err := decodeRetractionSnapshot(round)
			if err != nil || len(history.Producers) != 1 || len(history.Producers[0].Messages) != 1 {
				t.Fatalf("lost message record: %+v %v", history, err)
			}
			if !reflect.DeepEqual(history.Producers[0].Messages[0].Delivered, []core.FederateHandle{2, 3}) {
				t.Fatal("lost delivered recipients")
			}
			if err := codec.Unmarshal("fed", round); err != nil {
				t.Fatal(err)
			}
			again, err := codec.Marshal("fed")
			if err != nil || !bytes.Equal(round, again) {
				t.Fatalf("unstable ledger: %s %v", again, err)
			}
		})
	}
	s, _, _ := newTimeStateCodec(t)
	codec := Retractions{Manager: s.Manager}
	legacy := []byte(`{"version":1,"history":{"complete":true,"producers":[{"producer":1,"accepted":[{"first":7,"last":8}]}]}}`)
	if err := codec.Unmarshal("fed", legacy); err != nil {
		t.Fatal(err)
	}
	got, err := codec.Marshal("fed")
	if err != nil || !bytes.Equal(got, legacy) {
		t.Fatalf("legacy format changed: %s %v", got, err)
	}
}

func stringMustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRetractionV2RejectsMalformedWithoutMutation(t *testing.T) {
	valid := retractionV2Fixture(t, 5)
	cases := map[string][]byte{
		"old version":             bytes.Replace(valid, []byte(`"version":2`), []byte(`"version":1`), 1),
		"missing timestamp":       bytes.Replace(valid, []byte(`,"timestamp":5`), nil, 1),
		"null timestamp":          bytes.Replace(valid, []byte(`"timestamp":5`), []byte(`"timestamp":null`), 1),
		"negative timestamp":      bytes.Replace(valid, []byte(`"timestamp":5`), []byte(`"timestamp":-1`), 1),
		"not accepted":            bytes.Replace(valid, []byte(`"id":7`), []byte(`"id":9`), 1),
		"duplicate recipient":     bytes.Replace(valid, []byte(`"delivered":[2,3]`), []byte(`"delivered":[2,2]`), 1),
		"zero recipient":          bytes.Replace(valid, []byte(`"delivered":[2,3]`), []byte(`"delivered":[0,3]`), 1),
		"legacy null extension":   []byte(`{"version":1,"history":{"complete":true,"producers":[{"producer":1,"accepted":[],"messages":null}]}}`),
		"legacy empty extension":  []byte(`{"version":1,"history":{"complete":true,"producers":[{"producer":1,"accepted":[],"messages":[]}]}}`),
		"new version no messages": []byte(`{"version":2,"history":{"complete":true,"producers":[]}}`),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(valid, data) {
				t.Fatal("invalid corruption fixture")
			}
			s, _, _ := newTimeStateCodec(t)
			codec := Retractions{Manager: s.Manager}
			before, err := codec.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := codec.PrepareRestoreState("fed", data)
			if err == nil || prepared != nil {
				t.Fatal("corrupt ledger accepted")
			}
			after, err := codec.Marshal("fed")
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed restore mutated ledger")
			}
		})
	}
}

func TestRetractionV2RebindsSenderAndRecipientsNotMessageID(t *testing.T) {
	source := retractionV2Fixture(t, 5)
	rewritten, err := (Retractions{}).RewriteFederates(source, func(h core.FederateHandle) (core.FederateHandle, error) {
		return map[core.FederateHandle]core.FederateHandle{1: 103, 2: 102, 3: 101}[h], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	history, err := decodeRetractionSnapshot(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	p := history.Producers[0]
	if p.Producer != 103 || p.Messages[0].ID != 7 || !reflect.DeepEqual(p.Messages[0].Delivered, []core.FederateHandle{101, 102}) {
		t.Fatalf("wrong identity rewrite: %+v", p)
	}
	if !bytes.Equal(source, retractionV2Fixture(t, 5)) {
		t.Fatal("rewriting modified saved input")
	}
	_, err = (Retractions{}).RewriteFederates(source, func(core.FederateHandle) (core.FederateHandle, error) { return 99, nil })
	if err == nil {
		t.Fatal("colliding recipient mapping accepted")
	}
}

func TestRetractionV2RestoreReplacesMessagesButRetainsAcceptedIDs(t *testing.T) {
	s, _, _ := newTimeStateCodec(t)
	codec := Retractions{Manager: s.Manager}
	if err := codec.Unmarshal("fed", retractionV2Fixture(t, 5)); err != nil {
		t.Fatal(err)
	}
	history := &timepkg.RetractionHistory{Complete: true, Producers: []timepkg.RetractionProducerHistory{{Producer: 1, Accepted: []timepkg.RetractionIDRange{{First: 20, Last: 20}}, Messages: []timepkg.RetractionMessageState{{ID: 20, Timestamp: 30, Delivered: []core.FederateHandle{4}}}}}}
	prepared, err := s.Manager.PrepareRetractionHistory("fed", history)
	if err != nil {
		t.Fatal(err)
	}
	history.Producers[0].Messages[0].Delivered[0] = 99
	defer prepared.Release()
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Manager.ExportRetractionHistory("fed")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Producers[0].Messages) != 1 || got.Producers[0].Messages[0].ID != 20 || got.Producers[0].Messages[0].Delivered[0] != 4 {
		t.Fatalf("restore records not replaced/detached: %+v", got)
	}
	if !reflect.DeepEqual(got.Producers[0].Accepted, []timepkg.RetractionIDRange{{First: 7, Last: 8}, {First: 20, Last: 20}}) {
		t.Fatal("accepted ID history rewound")
	}
	got.Producers[0].Messages[0].Delivered[0] = 88
	again, err := s.Manager.ExportRetractionHistory("fed")
	if err != nil || again.Producers[0].Messages[0].Delivered[0] != 4 {
		t.Fatal("export recipient slice aliases live state")
	}
}
