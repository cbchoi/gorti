package savebundle

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func runtimeMetricCodec(t *testing.T) MOM {
	t.Helper()
	manager, err := mom.New(mom.Options{Outbox: momCompleteOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRuntimeMetricsEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := manager.FederateJoined(context.Background(), "metrics", 1, "member", "type"); err != nil {
		t.Fatal(err)
	}
	return MOM{Manager: manager}
}

func TestMOMRuntimeMetricsDurablePreparedRestoreAndLegacyUnknown(t *testing.T) {
	codec := runtimeMetricCodec(t)
	ctx := context.Background()
	if err := codec.Manager.RecordServiceInvocation("metrics", 1, mom.ServiceInvocation{Service: "updateAttributeValues", Success: true}); err != nil {
		t.Fatal(err)
	}
	if err := codec.Manager.RecordCallbackInvocation("metrics", 1, mom.CallbackInvocation{Service: "reflectAttributeValues", Object: 7, ObjectClass: 11}); err != nil {
		t.Fatal(err)
	}
	data, err := codec.Marshal("metrics")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := codec.PrepareRestore("metrics", data)
	if err != nil {
		t.Fatal(err)
	}
	clear(data)
	_ = codec.Manager.RecordServiceInvocation("metrics", 1, mom.ServiceInvocation{Service: "updateAttributeValues", Success: true})
	commit()
	commit()
	metrics, err := codec.Manager.FederateMetricValues(ctx, "metrics", 1)
	if err != nil || binary.BigEndian.Uint32(metrics.Values["HLAupdatesSent"]) != 1 || binary.BigEndian.Uint32(metrics.Values["HLAobjectInstancesReflected"]) != 1 {
		t.Fatal("runtime metric snapshot lost", metrics, err)
	}
	data, err = codec.Marshal("metrics")
	if err != nil {
		t.Fatal(err)
	}
	var envelope momSnapshotEnvelope
	if err := json.Unmarshal(data[len(momSnapshotMagic):], &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Runtime.Federates[0].RuntimeMetrics = nil
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	commit, err = codec.PrepareRestore("metrics", append([]byte(momSnapshotMagic), body...))
	if err != nil {
		t.Fatal(err)
	}
	commit()
	metrics, err = codec.Manager.FederateMetricValues(ctx, "metrics", 1)
	if err != nil || metrics.Unavailable["HLAupdatesSent"] == "" || metrics.Unavailable["HLAreflectionsReceived"] == "" {
		t.Fatal("legacy missing baseline fabricated0", metrics, err)
	}
}

func TestMOMRuntimeMetricsRejectMalformedBeforeMutation(t *testing.T) {
	for _, kind := range []string{"overflow", "unknownMetric", "historyMismatch", "invalidObject"} {
		t.Run(kind, func(t *testing.T) {
			codec := runtimeMetricCodec(t)
			data, err := codec.Marshal("metrics")
			if err != nil {
				t.Fatal(err)
			}
			before := codec.Manager.ExportState("metrics")
			var envelope momSnapshotEnvelope
			if err := json.Unmarshal(data[len(momSnapshotMagic):], &envelope); err != nil {
				t.Fatal(err)
			}
			runtime := envelope.Runtime.Federates[0].RuntimeMetrics
			switch kind {
			case "overflow":
				runtime.Counters["HLAupdatesSent"] = math.MaxInt32 + 1
			case "unknownMetric":
				runtime.Counters["madeUp"] = 0
			case "historyMismatch":
				runtime.ReflectedObjects[7] = 11
			case "invalidObject":
				runtime.ReflectedObjects[core.ObjectHandle(0)] = 11
				runtime.Counters["HLAobjectInstancesReflected"] = 1
			}
			body, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codec.PrepareRestore("metrics", append([]byte(momSnapshotMagic), body...)); err == nil {
				t.Fatal("malformed runtime accepted")
			}
			if !reflect.DeepEqual(before, codec.Manager.ExportState("metrics")) {
				t.Fatal("failed preparation mutated runtime")
			}
		})
	}
}

func TestMOMRuntimeMetricsRestoredOutstandingReceiptsStayUnavailable(t *testing.T) {
	codec := runtimeMetricCodec(t)
	codec.Manager.CallbackPending("metrics", 1, 1)
	data, err := codec.Marshal("metrics")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := codec.PrepareRestore("metrics", data)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	metrics, err := codec.Manager.FederateMetricValues(context.Background(), "metrics", 1)
	if err != nil || metrics.Unavailable["HLAreflectionsReceived"] == "" {
		t.Fatal("purged receipt history treated aszero", metrics, err)
	}
	if codec.Manager.ExportState("metrics").Federates[0].RuntimeMetrics.PendingCallbacks != 0 {
		t.Fatal("ephemeral tickets restored")
	}
}
