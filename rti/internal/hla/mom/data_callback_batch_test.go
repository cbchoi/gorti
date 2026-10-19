package mom

import (
	"math"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestMOMDataCallbackBatchMatchesSequentialAccounting(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		a, at := newEntryCounterManager(t)
		b, bt := newEntryCounterManager(t)
		if overflow {
			at.federations["f"][7].counts[InteractionsReceived] = map[uint64]uint32{9: math.MaxInt32}
			bt.federations["f"][7].counts[InteractionsReceived] = map[uint64]uint32{9: math.MaxInt32}
		}
		entries := []DataCallbackEntry{
			{Service: "receiveInteraction", InteractionClass: 9, Transportation: core.TransportTypeReliable},
			{Service: "reflectAttributeValues", Object: 5, ObjectClass: 3, Transportation: core.TransportTypeBestEffort},
			{Service: "reflectAttributeValues", Object: 5, ObjectClass: 3, Transportation: core.TransportTypeReliable},
		}
		var reports []DataCallbackReport
		for i := range entries {
			a.CallbackPending("f", 7, 1)
			b.CallbackPending("f", 7, 1)
			reports = append(reports, DataCallbackReport{Entry: &entries[i]}, DataCallbackReport{})
		}
		for _, report := range reports {
			if report.Entry == nil {
				a.CallbackPending("f", 7, -1)
			} else if err := a.RecordDataCallbackEntry("f", 7, *report.Entry); err != nil {
				a.DataCallbackEntryUnavailable("f", 7, *report.Entry, err)
			}
		}
		applied, failures := b.TryRecordDataCallbackReports("f", 7, reports)
		if !applied || (len(failures) != 0) != overflow {
			t.Fatalf("applied=%v failures=%v overflow=%v", applied, failures, overflow)
		}
		if !reflect.DeepEqual(a.ExportState("f"), b.ExportState("f")) {
			t.Fatal("batch changed counter, pending, failure, or reflection-history semantics")
		}
	}
}

func TestMOMDataCallbackBatchDeclinesReportingWithoutMutation(t *testing.T) {
	for _, service := range []bool{false, true} {
		m, _ := newEntryCounterManager(t)
		fs := m.fed["f"].federates[7]
		fs.serviceReportingEnabled, fs.exceptionReportingEnabled = service, !service
		before := m.ExportState("f")
		entry := DataCallbackEntry{Service: "receiveInteraction", InteractionClass: 9, Transportation: core.TransportTypeReliable}
		applied, failures := m.TryRecordDataCallbackReports("f", 7, []DataCallbackReport{{Entry: &entry}})
		if applied || len(failures) != 0 || !reflect.DeepEqual(before, m.ExportState("f")) {
			t.Fatal("reporting-enabled batch must use the detailed observer unchanged")
		}
	}
}
