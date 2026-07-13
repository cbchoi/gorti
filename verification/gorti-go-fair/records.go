package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

type semanticRecord struct {
	Kind    string         `json:"kind"`
	Seq     uint64         `json:"seq"`
	Service string         `json:"service"`
	Event   string         `json:"event"`
	Actor   string         `json:"actor"`
	Data    map[string]any `json:"data"`
}

type metricRecord struct {
	Kind    string  `json:"kind"`
	Service string  `json:"service"`
	Metric  string  `json:"metric"`
	Unit    string  `json:"unit"`
	Value   float64 `json:"value"`
}

type sampleRecord struct {
	Sequence   uint64           `json:"sequence"`
	Operation  string           `json:"operation"`
	DurationNS int64            `json:"duration_ns"`
	Dimensions sampleDimensions `json:"dimensions"`
}

type sampleDimensions struct {
	SampleKind string `json:"sample_kind"`
	Service    string `json:"service"`
}

type ndjsonLogger struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
	actor   string
	seq     uint64
}

func newNDJSONLogger(path, actor string) (*ndjsonLogger, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	return &ndjsonLogger{file: file, encoder: encoder, actor: actor}, nil
}

func (logger *ndjsonLogger) semantic(service, event string, data map[string]any) error {
	if err := validateService(service); err != nil {
		return err
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	record := semanticRecord{
		Kind: "semantic", Seq: logger.seq, Service: service,
		Event: event, Actor: logger.actor, Data: data,
	}
	if err := logger.encoder.Encode(record); err != nil {
		return err
	}
	logger.seq++
	return nil
}

func (logger *ndjsonLogger) metric(service, metric, unit string, value float64) error {
	if err := validateService(service); err != nil {
		return err
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return logger.encoder.Encode(metricRecord{
		Kind: "metric", Service: service, Metric: metric, Unit: unit, Value: value,
	})
}

func (logger *ndjsonLogger) close() error {
	if logger == nil || logger.file == nil {
		return nil
	}
	return logger.file.Close()
}

func validateService(service string) error {
	switch service {
	case "FM", "DM", "OM", "TM":
		return nil
	default:
		return fmt.Errorf("invalid service %q", service)
	}
}

type runLog struct {
	semantic *ndjsonLogger
	metrics  *ndjsonLogger
	samples  *ndjsonLogger
}

func (log *runLog) event(service, event string, data map[string]any) error {
	return log.semantic.semantic(service, event, data)
}

func (log *runLog) metric(service, metric, unit string, value float64) error {
	return log.metrics.metric(service, metric, unit, value)
}

func (log *runLog) sample(operation string, durationNS int64, service, sampleKind string) error {
	if durationNS < 0 {
		return errors.New("sample duration must be non-negative")
	}
	if err := validateService(service); err != nil {
		return err
	}
	if sampleKind != "call" && sampleKind != "delivery" {
		return fmt.Errorf("invalid sample kind %q", sampleKind)
	}
	log.samples.mu.Lock()
	defer log.samples.mu.Unlock()
	record := sampleRecord{
		Sequence: log.samples.seq, Operation: operation, DurationNS: durationNS,
		Dimensions: sampleDimensions{SampleKind: sampleKind, Service: service},
	}
	if err := log.samples.encoder.Encode(record); err != nil {
		return err
	}
	log.samples.seq++
	return nil
}

func (log *runLog) timed(service, metric string, call func() error) error {
	started := counterNow()
	err := call()
	metricErr := log.metric(service, "call_latency."+metric, "nanoseconds", float64(counterElapsed(started)))
	if err != nil {
		return err
	}
	return metricErr
}

func (log *runLog) benchmarkTimed(service, operation, metric string, call func() error) error {
	started := counterNow()
	err := call()
	duration := counterElapsed(started)
	metricErr := log.metric(service, "call_latency."+metric, "nanoseconds", float64(duration))
	sampleErr := log.sample(operation, duration, service, "call")
	return errors.Join(err, metricErr, sampleErr)
}

func (log *runLog) close() error {
	semanticErr := log.semantic.close()
	metricErr := log.metrics.close()
	sampleErr := log.samples.close()
	if semanticErr != nil {
		return semanticErr
	}
	return errors.Join(metricErr, sampleErr)
}
