package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	compact  bool
	memory   compactMeasurements
}

type compactMeasurements struct {
	mu                         sync.Mutex
	updateAttributeValuesNS    []int64
	sendInteractionNS          []int64
	completedReceiveOrderBatch *int64
}

type compactMeasurementSnapshot struct {
	UpdateAttributeValuesMedianNS *int64
	SendInteractionMedianNS       *int64
	CompletedReceiveOrderBatchNS  *int64
	UpdateAttributeValuesCount    int
	SendInteractionCount          int
}

func newRunLog(cfg config) (*runLog, error) {
	if cfg.CompactSummary {
		return &runLog{compact: true}, nil
	}
	semantic, err := newNDJSONLogger(
		filepath.Join(cfg.OutputDir, string(cfg.Role)+"-semantic.ndjson"),
		string(cfg.Role),
	)
	if err != nil {
		return nil, err
	}
	metrics, err := newNDJSONLogger(
		filepath.Join(cfg.OutputDir, string(cfg.Role)+"-metrics.ndjson"),
		string(cfg.Role),
	)
	if err != nil {
		_ = semantic.close()
		return nil, err
	}
	samples, err := newNDJSONLogger(
		filepath.Join(cfg.OutputDir, string(cfg.Role)+"-samples.ndjson"),
		string(cfg.Role),
	)
	if err != nil {
		_ = semantic.close()
		_ = metrics.close()
		return nil, err
	}
	return &runLog{semantic: semantic, metrics: metrics, samples: samples}, nil
}

func (log *runLog) event(service, event string, data map[string]any) error {
	if log.compact {
		return validateService(service)
	}
	return log.semantic.semantic(service, event, data)
}

func (log *runLog) metric(service, metric, unit string, value float64) error {
	if log.compact {
		return validateService(service)
	}
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
	if log.compact {
		if operation == "completedReceiveOrderBatch" {
			return log.memory.recordCompletedReceiveOrderBatch(durationNS)
		}
		return nil
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
	if log.compact {
		return call()
	}
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
	if log.compact {
		log.memory.recordCall(operation, duration)
		return err
	}
	metricErr := log.metric(service, "call_latency."+metric, "nanoseconds", float64(duration))
	sampleErr := log.sample(operation, duration, service, "call")
	return errors.Join(err, metricErr, sampleErr)
}

func (log *runLog) close() error {
	if log == nil || log.compact {
		return nil
	}
	semanticErr := log.semantic.close()
	metricErr := log.metrics.close()
	sampleErr := log.samples.close()
	if semanticErr != nil {
		return semanticErr
	}
	return errors.Join(metricErr, sampleErr)
}

func (measurements *compactMeasurements) recordCall(operation string, durationNS int64) {
	measurements.mu.Lock()
	defer measurements.mu.Unlock()
	switch operation {
	case "updateAttributeValues", "queueAttributeValues":
		measurements.updateAttributeValuesNS = append(measurements.updateAttributeValuesNS, durationNS)
	case "sendInteraction", "queueInteraction":
		measurements.sendInteractionNS = append(measurements.sendInteractionNS, durationNS)
	}
}

func (measurements *compactMeasurements) recordCompletedReceiveOrderBatch(durationNS int64) error {
	measurements.mu.Lock()
	defer measurements.mu.Unlock()
	if measurements.completedReceiveOrderBatch != nil {
		return errors.New("completed receive-order batch was recorded more than once")
	}
	value := durationNS
	measurements.completedReceiveOrderBatch = &value
	return nil
}

func (log *runLog) compactSnapshot() compactMeasurementSnapshot {
	log.memory.mu.Lock()
	defer log.memory.mu.Unlock()
	return compactMeasurementSnapshot{
		UpdateAttributeValuesMedianNS: medianDuration(log.memory.updateAttributeValuesNS),
		SendInteractionMedianNS:       medianDuration(log.memory.sendInteractionNS),
		CompletedReceiveOrderBatchNS:  cloneInt64(log.memory.completedReceiveOrderBatch),
		UpdateAttributeValuesCount:    len(log.memory.updateAttributeValuesNS),
		SendInteractionCount:          len(log.memory.sendInteractionNS),
	}
}

func medianDuration(values []int64) *int64 {
	if len(values) == 0 {
		return nil
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	middle := len(ordered) / 2
	median := ordered[middle]
	if len(ordered)%2 == 0 {
		median = ordered[middle-1] + (ordered[middle]-ordered[middle-1])/2
	}
	return &median
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
