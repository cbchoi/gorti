package savebundle

import (
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func retractionGraphFixture(t *testing.T) (core.CheckpointManifest, core.FOMHandle, *timepkg.RetractionHistory) {
	t.Helper()
	m, model, _ := roGraphFixture(t)
	m.ManagerSnapshots["time"] = nil
	history := &timepkg.RetractionHistory{Complete: true, Producers: []timepkg.RetractionProducerHistory{{
		Producer: 1<<32 | 1, Accepted: []timepkg.RetractionIDRange{{First: 7, Last: 7}},
		Messages: []timepkg.RetractionMessageState{{ID: 7, Timestamp: 5, Delivered: []core.FederateHandle{1<<32 | 2}}},
	}}}
	var err error
	m.ManagerSnapshots["retractions"], err = marshalRetractionSnapshot(history)
	if err != nil {
		t.Fatal(err)
	}
	return m, model, history
}

func TestRetractionV2GraphReferencesAndRebind(t *testing.T) {
	m, model, _ := retractionGraphFixture(t)
	if err := (RestoreGraph{}).Validate("fed", m, model); err != nil {
		t.Fatal(err)
	}
	refs, err := (RestoreGraph{}).References(m)
	if err != nil || !reflect.DeepEqual(refs, []core.FederateHandle{1<<32 | 1, 1<<32 | 2}) {
		t.Fatalf("missing ledger references: %v %v", refs, err)
	}
	mapping := map[core.FederateHandle]core.FederateHandle{1<<32 | 1: 9<<32 | 20, 1<<32 | 2: 9<<32 | 10}
	rebound, err := (RestoreGraph{}).Rebind(m, 9, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if err := (RestoreGraph{}).Validate("fed", rebound, model); err != nil {
		t.Fatal(err)
	}
	history, err := decodeRetractionSnapshot(rebound.ManagerSnapshots["retractions"])
	if err != nil {
		t.Fatal(err)
	}
	p := history.Producers[0]
	if p.Producer != mapping[1<<32|1] || p.Messages[0].Delivered[0] != mapping[1<<32|2] || p.Messages[0].ID != 7 {
		t.Fatalf("incomplete ledger rebind: %+v", p)
	}
	if _, err := (RestoreGraph{}).Rebind(m, 9, map[core.FederateHandle]core.FederateHandle{1<<32 | 2: 9<<32 | 10}); err == nil {
		t.Fatal("missing historical producer identity accepted")
	}
}

func TestRetractionV2GraphRecipientLiveness(t *testing.T) {
	for _, tc := range []struct {
		name             string
		recipient        core.FederateHandle
		retracted, valid bool
	}{
		{"live", 1<<32 | 2, false, true},
		{"departed unretracted", 1<<32 | 3, false, false},
		{"departed retracted", 1<<32 | 3, true, true},
		{"other execution", 2<<32 | 2, true, false},
		{"internal", ^core.FederateHandle(0), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, model, history := retractionGraphFixture(t)
			history.Producers[0].Messages[0].Delivered = []core.FederateHandle{tc.recipient}
			history.Producers[0].Messages[0].Retracted = tc.retracted
			data, err := marshalRetractionSnapshot(history)
			if err != nil {
				if !tc.valid {
					return
				}
				t.Fatal(err)
			}
			m.ManagerSnapshots["retractions"] = data
			err = (RestoreGraph{}).Validate("fed", m, model)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestRetractionV2GraphRejectsQueueLedgerConflicts(t *testing.T) {
	for _, tc := range []struct {
		name             string
		timestamp        core.LogicalTime
		retracted, valid bool
	}{
		{"matching", 5, false, true}, {"wrong timestamp", 6, false, false}, {"already retracted", 5, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, model, doc := roGraphFixture(t)
			q := roDocumentQueue(doc)[1].(map[string]any)
			q["receive_order"], q["timestamp"], q["retraction_handle"] = false, 5, 7
			e := q["event"].(map[string]any)
			e["logical_time"] = 5
			metadata := e["metadata"].(*core.ObjectCallbackMetadata)
			metadata.SentOrder, metadata.ReceivedOrder, metadata.RetractionHandle = core.OrderTimestamp, core.OrderTimestamp, 7
			f := roDocumentFederate(doc)
			delete(f, "ro_queue")
			f["queue"] = []any{q}
			doc["version"] = 1
			m.ManagerSnapshots["time"] = graphJSON(t, doc)
			history := &timepkg.RetractionHistory{Complete: true, Producers: []timepkg.RetractionProducerHistory{{Producer: 1<<32 | 1, Accepted: []timepkg.RetractionIDRange{{First: 7, Last: 7}}, Messages: []timepkg.RetractionMessageState{{ID: 7, Timestamp: tc.timestamp, Retracted: tc.retracted}}}}}
			var err error
			m.ManagerSnapshots["retractions"], err = marshalRetractionSnapshot(history)
			if err != nil {
				t.Fatal(err)
			}
			err = (RestoreGraph{}).Validate("fed", m, model)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestRetractionV2SessionMergeKeepsCheckpointMessagesOnly(t *testing.T) {
	saved := retractionV2Fixture(t, 5)
	live := &timepkg.RetractionHistory{Complete: true, Producers: []timepkg.RetractionProducerHistory{{Producer: 10, Accepted: []timepkg.RetractionIDRange{{First: 20, Last: 20}}, Messages: []timepkg.RetractionMessageState{{ID: 20, Timestamp: 30, Delivered: []core.FederateHandle{99}}}}}}
	current, err := marshalRetractionSnapshot(live)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := MergeSessionRetractions(saved, current, map[core.FederateHandle]core.FederateHandle{10: 1})
	if err != nil {
		t.Fatal(err)
	}
	history, err := decodeRetractionSnapshot(merged)
	if err != nil {
		t.Fatal(err)
	}
	p := history.Producers[0]
	if len(history.Producers) != 1 || p.Producer != 1 || !reflect.DeepEqual(p.Accepted, []timepkg.RetractionIDRange{{First: 7, Last: 8}, {First: 20, Last: 20}}) {
		t.Fatalf("accepted session IDs lost: %+v", history)
	}
	if len(p.Messages) != 1 || p.Messages[0].ID != 7 || !reflect.DeepEqual(p.Messages[0].Delivered, []core.FederateHandle{2, 3}) {
		t.Fatalf("post-save message ledger leaked into restored execution: %+v", p.Messages)
	}
}
