package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

type tmPublisherFederate struct {
	state *eventState
}

func (*tmPublisherFederate) UpdateAttributeValuesByHandle(
	context.Context, uint64, map[uint64][]byte, *float64,
) error {
	return nil
}

func (*tmPublisherFederate) SendInteractionByHandle(
	context.Context, uint64, map[uint64][]byte, *float64,
) error {
	return nil
}

func (fake *tmPublisherFederate) TimeAdvanceRequest(_ context.Context, logicalTime float64) error {
	fake.state.accept(federate.TimeAdvanceGrant{Time: logicalTime}, counterNow())
	return nil
}

func (*tmPublisherFederate) DeleteObjectInstance(
	context.Context, uint64, []byte, *float64,
) error {
	return nil
}

type tmSubscriberFederate struct {
	state        *eventState
	payloads     []encodedIteration
	objectHandle uint64
	discovered   bool
}

func (*tmSubscriberFederate) UpdateAttributeValuesByHandle(
	context.Context, uint64, map[uint64][]byte, *float64,
) error {
	return nil
}

func (*tmSubscriberFederate) SendInteractionByHandle(
	context.Context, uint64, map[uint64][]byte, *float64,
) error {
	return nil
}

func (fake *tmSubscriberFederate) TimeAdvanceRequest(
	_ context.Context,
	logicalTime float64,
) error {
	if logicalTime <= float64(len(fake.payloads)) {
		if !fake.discovered {
			fake.state.accept(federate.DiscoverObjectInstance{
				ObjectHandle: fake.objectHandle,
				ClassName:    objectClass,
				ObjectName:   objectName,
			}, counterNow())
			fake.discovered = true
		}
		item := fake.payloads[int(logicalTime)-1]
		timestamp := logicalTime
		fake.state.accept(federate.ReflectAttributeValues{
			ObjectHandle: fake.objectHandle,
			ClassName:    objectClass,
			Attributes:   item.attributes,
			Timestamp:    &timestamp,
		}, counterNow())
		fake.state.accept(federate.ReceiveInteraction{
			ClassName:  interactionClass,
			Parameters: item.parameters,
			Timestamp:  &timestamp,
		}, counterNow())
		fake.state.accept(federate.TimeAdvanceGrant{Time: logicalTime}, counterNow())
		return nil
	}
	timestamp := logicalTime
	fake.state.accept(federate.RemoveObjectInstance{
		ObjectHandle: fake.objectHandle,
		Timestamp:    &timestamp,
	}, counterNow())
	fake.state.accept(federate.TimeAdvanceGrant{Time: logicalTime}, counterNow())
	return nil
}

func (*tmSubscriberFederate) DeleteObjectInstance(
	context.Context, uint64, []byte, *float64,
) error {
	return nil
}

func TestPublisherRecordsOneGrantBoundarySamplePerMeasuredIteration(t *testing.T) {
	cfg := config{Role: rolePublisher, Seed: "1516", Count: 3, Timeout: time.Second}
	payloads, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		t.Fatal(err)
	}
	log, samplesPath := newTMTestLog(t, cfg.Role)
	state := newEventState(cfg)
	p := participant{cfg: cfg, log: log, payloads: payloads, state: state}

	if err := p.publishTraffic(
		context.Background(),
		&tmPublisherFederate{state: state},
		17,
	); err != nil {
		t.Fatal(err)
	}
	if err := p.advancePublishedTimestampOrderTraffic(
		context.Background(),
		&tmPublisherFederate{state: state},
		17,
	); err != nil {
		t.Fatal(err)
	}

	assertMeasuredGrantSamples(t, samplesPath, cfg.Count)
}

func TestSubscriberRecordsOneGrantBoundarySamplePerMeasuredIteration(t *testing.T) {
	cfg := config{Role: roleSubscriber, Seed: "1516", Count: 3, Timeout: time.Second}
	payloads, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		t.Fatal(err)
	}
	log, samplesPath := newTMTestLog(t, cfg.Role)
	state := newEventState(cfg)
	p := participant{cfg: cfg, log: log, payloads: payloads, state: state}
	fake := &tmSubscriberFederate{state: state, payloads: payloads, objectHandle: 17}

	if err := p.subscribeTimestampOrder(context.Background(), fake); err != nil {
		t.Fatal(err)
	}

	assertMeasuredGrantSamples(t, samplesPath, cfg.Count)
}

func newTMTestLog(t *testing.T, actor role) (*runLog, string) {
	t.Helper()
	directory := t.TempDir()
	semantic, err := newNDJSONLogger(filepath.Join(directory, "semantic.ndjson"), string(actor))
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := newNDJSONLogger(filepath.Join(directory, "metrics.ndjson"), string(actor))
	if err != nil {
		_ = semantic.close()
		t.Fatal(err)
	}
	samplesPath := filepath.Join(directory, "samples.ndjson")
	samples, err := newNDJSONLogger(samplesPath, string(actor))
	if err != nil {
		_ = semantic.close()
		_ = metrics.close()
		t.Fatal(err)
	}
	log := &runLog{semantic: semantic, metrics: metrics, samples: samples}
	t.Cleanup(func() { _ = log.close() })
	return log, samplesPath
}

func assertMeasuredGrantSamples(t *testing.T, path string, count int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var grantDurations []int64
	var requestDurations []int64
	for _, line := range splitNonemptyLines(data) {
		var record struct {
			Operation  string            `json:"operation"`
			DurationNS int64             `json:"duration_ns"`
			Dimensions map[string]string `json:"dimensions"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode sample: %v", err)
		}
		switch record.Operation {
		case "timeAdvanceRequest":
			requestDurations = append(requestDurations, record.DurationNS)
			if want := map[string]string{"sample_kind": "call", "service": "TM"}; !reflect.DeepEqual(record.Dimensions, want) {
				t.Fatalf("timeAdvanceRequest dimensions = %v, want %v", record.Dimensions, want)
			}
		case "timeAdvanceGrantLatency":
			grantDurations = append(grantDurations, record.DurationNS)
			if want := map[string]string{"boundary": "grant", "service": "TM"}; !reflect.DeepEqual(record.Dimensions, want) {
				t.Fatalf("timeAdvanceGrantLatency dimensions = %v, want %v", record.Dimensions, want)
			}
		}
	}
	if len(requestDurations) != count {
		t.Fatalf("timeAdvanceRequest samples = %d, want %d", len(requestDurations), count)
	}
	if len(grantDurations) != count {
		t.Fatalf("timeAdvanceGrantLatency samples = %d, want %d", len(grantDurations), count)
	}
	for index := range grantDurations {
		if grantDurations[index] < requestDurations[index] {
			t.Fatalf(
				"grant latency %d = %d ns, shorter than TAR call %d ns",
				index,
				grantDurations[index],
				requestDurations[index],
			)
		}
	}
}

func splitNonemptyLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for index, value := range data {
		if value != '\n' {
			continue
		}
		if index > start {
			lines = append(lines, data[start:index])
		}
		start = index + 1
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
