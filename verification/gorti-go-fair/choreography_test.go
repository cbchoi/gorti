package main

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

type choreographyFederate struct {
	state *eventState
	calls []string
}

func (fake *choreographyFederate) UpdateAttributeValuesByHandle(
	_ context.Context, _ uint64, attributes map[uint64][]byte, timestamp *float64,
) error {
	if len(attributes) != 2 || attributes[31] == nil || attributes[32] == nil {
		return fmt.Errorf("attribute handles = %v, want 31 and 32", attributes)
	}
	fake.calls = append(fake.calls, fmt.Sprintf("update:%g", *timestamp))
	return nil
}

func (fake *choreographyFederate) SendInteractionByHandle(
	_ context.Context, classHandle uint64, parameters map[uint64][]byte, timestamp *float64,
) error {
	if classHandle != 17 {
		return fmt.Errorf("interaction class handle = %d, want 17", classHandle)
	}
	if len(parameters) != 2 || parameters[21] == nil || parameters[22] == nil {
		return fmt.Errorf("parameter handles = %v, want 21 and 22", parameters)
	}
	fake.calls = append(fake.calls, fmt.Sprintf("interaction:%g", *timestamp))
	return nil
}

func (fake *choreographyFederate) TimeAdvanceRequest(_ context.Context, logicalTime float64) error {
	fake.calls = append(fake.calls, fmt.Sprintf("tar:%g", logicalTime))
	fake.state.accept(federate.TimeAdvanceGrant{Time: logicalTime}, counterNow())
	return nil
}

func (fake *choreographyFederate) DeleteObjectInstance(
	_ context.Context, _ uint64, _ []byte, timestamp *float64,
) error {
	fake.calls = append(fake.calls, fmt.Sprintf("delete:%g", *timestamp))
	return nil
}

func TestPublisherUsesPitchSequentialChoreography(t *testing.T) {
	cfg := config{Role: rolePublisher, Seed: "1516", Count: 2, Timeout: time.Second}
	payloads, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		t.Fatal(err)
	}
	for index := range payloads {
		payloads[index].wireAttrs = map[uint64][]byte{
			31: payloads[index].attributes[sequenceField],
			32: payloads[index].attributes[payloadField],
		}
		payloads[index].wireParams = map[uint64][]byte{
			21: payloads[index].parameters[sequenceField],
			22: payloads[index].parameters[payloadField],
		}
	}
	log := testRunLog(t)
	state := newEventState(cfg)
	fake := &choreographyFederate{state: state}
	p := participant{
		cfg: cfg, log: log, payloads: payloads, state: state, interactionClassHandle: 17,
	}
	if err := p.publishTraffic(context.Background(), fake, 9); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"update:1", "interaction:1", "tar:1",
		"update:2", "interaction:2", "tar:2",
		"delete:3", "tar:3",
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

func testRunLog(t *testing.T) *runLog {
	t.Helper()
	directory := t.TempDir()
	semantic, err := newNDJSONLogger(filepath.Join(directory, "semantic.ndjson"), "publisher")
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := newNDJSONLogger(filepath.Join(directory, "metrics.ndjson"), "publisher")
	if err != nil {
		t.Fatal(err)
	}
	samples, err := newNDJSONLogger(filepath.Join(directory, "samples.ndjson"), "publisher")
	if err != nil {
		t.Fatal(err)
	}
	log := &runLog{semantic: semantic, metrics: metrics, samples: samples}
	t.Cleanup(func() { _ = log.close() })
	return log
}
