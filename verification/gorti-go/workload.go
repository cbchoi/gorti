package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

const (
	objectClass        = "VerifierEntity"
	interactionClass   = "VerifierMessage"
	objectName         = "gorti-go-verifier-entity-1"
	sequenceField      = "Sequence"
	payloadField       = "Payload"
	benchmarkLookahead = 1.0
)

var (
	callDimensions     = map[string]any{"service": "OM", "sample_kind": "call"}
	tarDimensions      = map[string]any{"service": "TM", "sample_kind": "call"}
	deliveryDimensions = map[string]any{"service": "OM", "sample_kind": "delivery"}
)

type workloadConfig struct {
	Address    string
	Federation string
	FOMPath    string
	FOMXML     []byte
	Count      int
	Seed       uint64
	Timeout    time.Duration
}

type encodedIteration struct {
	index                 int
	logicalTime           float64
	attributes            map[string][]byte
	interactionParameters map[string][]byte
}

type observedEvent struct {
	actor      string
	event      federate.Event
	receivedAt counterStamp
	err        error
}

type timedCall struct {
	operation string
	invoke    func(context.Context) error
}

type timedResult struct {
	operation string
	startedAt counterStamp
	endedAt   counterStamp
	err       error
}

type liveRunner struct {
	cfg      workloadConfig
	recorder *sampleRecorder
	payloads []encodedIteration

	producerConn *federate.Connection
	consumerConn *federate.Connection
	producer     *federate.Federate
	consumer     *federate.Federate

	events     chan observedEvent
	pumpCancel context.CancelFunc
	pumpWG     sync.WaitGroup

	objectHandle       uint64
	delivered          int
	explicitlyRejected int
}

func preencodeWorkload(seed uint64, count int) ([]encodedIteration, error) {
	if count < 1 {
		return nil, errors.New("count must be at least 1")
	}
	if uint64(count-1) > math.MaxInt32 {
		return nil, errors.New("count exceeds HLAinteger32BE sequence range")
	}

	payloads := make([]encodedIteration, count)
	for index := 0; index < count; index++ {
		sequence := make([]byte, 4)
		binary.BigEndian.PutUint32(sequence, uint32(index))
		attributePayload := encodeASCIIString(deterministicPayload(seed, "attribute", index))
		interactionPayload := encodeASCIIString(deterministicPayload(seed, "interaction", index))
		payloads[index] = encodedIteration{
			index:       index,
			logicalTime: float64(index + 1),
			attributes: map[string][]byte{
				sequenceField: sequence,
				payloadField:  attributePayload,
			},
			interactionParameters: map[string][]byte{
				sequenceField: sequence,
				payloadField:  interactionPayload,
			},
		}
	}
	return payloads, nil
}

func deterministicPayload(seed uint64, channel string, index int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%d", seed, channel, index)))
	return hex.EncodeToString(digest[:])[:16]
}

func encodeASCIIString(value string) []byte {
	encoded := make([]byte, 4+len(value))
	binary.BigEndian.PutUint32(encoded, uint32(len(value)))
	copy(encoded[4:], value)
	return encoded
}

func newLiveRunner(cfg workloadConfig, recorder *sampleRecorder, payloads []encodedIteration) *liveRunner {
	return &liveRunner{
		cfg:      cfg,
		recorder: recorder,
		payloads: payloads,
		events:   make(chan observedEvent, 128),
	}
}

func (r *liveRunner) run(ctx context.Context) error {
	if err := r.setup(ctx); err != nil {
		return err
	}
	defer r.close()

	for index := range r.payloads {
		if err := r.runIteration(ctx, r.payloads[index]); err != nil {
			return fmt.Errorf("iteration %d: %w", index, err)
		}
	}
	return nil
}

func (r *liveRunner) setup(ctx context.Context) error {
	var err error
	r.producerConn, err = federate.Connect(ctx, r.cfg.Address)
	if err != nil {
		return fmt.Errorf("connect producer: %w", err)
	}
	r.consumerConn, err = federate.Connect(ctx, r.cfg.Address)
	if err != nil {
		r.close()
		return fmt.Errorf("connect consumer: %w", err)
	}

	spec := federate.FederationSpec{
		Name: r.cfg.Federation,
		FOMModules: []federate.FOMModule{{
			Path: r.cfg.FOMPath,
			XML:  r.cfg.FOMXML,
		}},
	}
	r.producer, err = r.join(ctx, r.producerConn, spec, "gorti-go-producer")
	if err != nil {
		r.close()
		return fmt.Errorf("join producer: %w", err)
	}
	r.consumer, err = r.join(ctx, r.consumerConn, spec, "gorti-go-consumer")
	if err != nil {
		r.close()
		return fmt.Errorf("join consumer: %w", err)
	}
	r.startEventPumps(ctx)

	setupCalls := []struct {
		name string
		call func(context.Context) error
	}{
		{"publish object attributes", func(callCtx context.Context) error {
			return r.producer.PublishObjectClassAttributes(callCtx, objectClass, []string{sequenceField, payloadField})
		}},
		{"publish interaction", func(callCtx context.Context) error {
			return r.producer.PublishInteractionClass(callCtx, interactionClass)
		}},
		{"subscribe object attributes", func(callCtx context.Context) error {
			return r.consumer.SubscribeObjectClassAttributes(callCtx, objectClass, []string{sequenceField, payloadField})
		}},
		{"subscribe interaction", func(callCtx context.Context) error {
			return r.consumer.SubscribeInteractionClass(callCtx, interactionClass)
		}},
		{"enable producer regulation", func(callCtx context.Context) error {
			return r.producer.EnableTimeRegulation(callCtx, benchmarkLookahead)
		}},
		{"enable consumer regulation", func(callCtx context.Context) error {
			return r.consumer.EnableTimeRegulation(callCtx, benchmarkLookahead)
		}},
		{"enable producer constrained", r.producer.EnableTimeConstrained},
		{"enable consumer constrained", r.consumer.EnableTimeConstrained},
	}
	for _, item := range setupCalls {
		if err := callWithTimeout(ctx, r.cfg.Timeout, item.call); err != nil {
			r.close()
			return fmt.Errorf("%s: %w", item.name, err)
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	r.objectHandle, err = r.producer.RegisterObjectInstance(callCtx, objectClass, objectName)
	cancel()
	if err != nil {
		r.close()
		return fmt.Errorf("register object: %w", err)
	}
	if err := r.waitForDiscovery(ctx); err != nil {
		r.close()
		return err
	}
	return nil
}

func (r *liveRunner) join(
	ctx context.Context, conn *federate.Connection, spec federate.FederationSpec, name string,
) (*federate.Federate, error) {
	callCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	return conn.JoinFederation(callCtx, spec, name)
}

func (r *liveRunner) startEventPumps(parent context.Context) {
	pumpCtx, cancel := context.WithCancel(parent)
	r.pumpCancel = cancel
	for _, source := range []struct {
		actor string
		fed   *federate.Federate
	}{{"producer", r.producer}, {"consumer", r.consumer}} {
		r.pumpWG.Add(1)
		go func(actor string, fed *federate.Federate) {
			defer r.pumpWG.Done()
			for {
				select {
				case event, ok := <-fed.Events():
					if !ok {
						select {
						case r.events <- observedEvent{actor: actor, err: errors.New("events channel closed")}:
						case <-pumpCtx.Done():
						}
						return
					}
					observation := observedEvent{actor: actor, event: event, receivedAt: counterNow()}
					select {
					case r.events <- observation:
					case <-pumpCtx.Done():
						return
					}
				case <-pumpCtx.Done():
					return
				}
			}
		}(source.actor, source.fed)
	}
}

func (r *liveRunner) waitForDiscovery(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	for {
		select {
		case observation := <-r.events:
			if observation.err != nil {
				return fmt.Errorf("%s event pump: %w", observation.actor, observation.err)
			}
			switch event := observation.event.(type) {
			case federate.DiscoverObjectInstance:
				if observation.actor != "consumer" {
					continue
				}
				if event.ObjectHandle != r.objectHandle || event.ClassName != objectClass || event.ObjectName != objectName {
					return fmt.Errorf("unexpected discovery: %+v", event)
				}
				return nil
			case federate.FederationHalted:
				return fmt.Errorf("federation halted: %s", event.Reason)
			}
		case <-waitCtx.Done():
			return fmt.Errorf("wait for object discovery: %w", waitCtx.Err())
		}
	}
}

func (r *liveRunner) runIteration(ctx context.Context, expected encodedIteration) error {
	timestamp := expected.logicalTime
	omResults := runConcurrentCalls(ctx, r.cfg.Timeout, []timedCall{
		{
			operation: "updateAttributeValues",
			invoke: func(callCtx context.Context) error {
				return r.producer.UpdateAttributeValues(callCtx, r.objectHandle, expected.attributes, &timestamp)
			},
		},
		{
			operation: "sendInteraction",
			invoke: func(callCtx context.Context) error {
				return r.producer.SendInteraction(callCtx, interactionClass, expected.interactionParameters, &timestamp)
			},
		},
	})
	for _, result := range omResults {
		r.recorder.record(result.operation, elapsedCounterNS(result.startedAt, result.endedAt), callDimensions)
	}
	if err := timedResultsError(omResults); err != nil {
		return err
	}
	batchBoundary := counterNow()

	tarResults := runConcurrentCalls(ctx, r.cfg.Timeout, []timedCall{
		{
			operation: "timeAdvanceRequest",
			invoke: func(callCtx context.Context) error {
				return r.producer.TimeAdvanceRequest(callCtx, expected.logicalTime)
			},
		},
		{
			operation: "timeAdvanceRequest",
			invoke: func(callCtx context.Context) error {
				return r.consumer.TimeAdvanceRequest(callCtx, expected.logicalTime)
			},
		},
	})
	for _, result := range tarResults {
		r.recorder.record(result.operation, elapsedCounterNS(result.startedAt, result.endedAt), tarDimensions)
	}
	if err := timedResultsError(tarResults); err != nil {
		return err
	}

	return r.waitForIterationEvents(ctx, expected, omResults, batchBoundary)
}

func runConcurrentCalls(ctx context.Context, timeout time.Duration, calls []timedCall) []timedResult {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := make(chan struct{})
	results := make([]chan timedResult, len(calls))
	for index, call := range calls {
		results[index] = make(chan timedResult, 1)
		go func(call timedCall, result chan<- timedResult) {
			<-start
			startedAt := counterNow()
			err := call.invoke(callCtx)
			result <- timedResult{
				operation: call.operation,
				startedAt: startedAt,
				endedAt:   counterNow(),
				err:       err,
			}
		}(call, results[index])
	}
	close(start)

	completed := make([]timedResult, len(calls))
	for index := range results {
		completed[index] = <-results[index]
	}
	return completed
}

func timedResultsError(results []timedResult) error {
	var failures []error
	for _, result := range results {
		if result.err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", result.operation, result.err))
		}
	}
	return errors.Join(failures...)
}

func (r *liveRunner) waitForIterationEvents(
	ctx context.Context,
	expected encodedIteration,
	omResults []timedResult,
	batchBoundary counterStamp,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	var reflectAt, interactionAt counterStamp
	reflectReceived := false
	interactionReceived := false
	producerGrant := false
	consumerGrant := false
	for !reflectReceived || !interactionReceived || !producerGrant || !consumerGrant {
		select {
		case observation := <-r.events:
			if observation.err != nil {
				return fmt.Errorf("%s event pump: %w", observation.actor, observation.err)
			}
			switch event := observation.event.(type) {
			case federate.ReflectAttributeValues:
				if observation.actor != "consumer" {
					continue
				}
				if err := validateReflect(event, r.objectHandle, expected); err != nil {
					r.explicitlyRejected++
					return err
				}
				if reflectReceived {
					return errors.New("duplicate ReflectAttributeValues")
				}
				reflectAt = observation.receivedAt
				reflectReceived = true
				r.delivered++
			case federate.ReceiveInteraction:
				if observation.actor != "consumer" {
					continue
				}
				if err := validateInteraction(event, expected); err != nil {
					r.explicitlyRejected++
					return err
				}
				if interactionReceived {
					return errors.New("duplicate ReceiveInteraction")
				}
				interactionAt = observation.receivedAt
				interactionReceived = true
				r.delivered++
			case federate.TimeAdvanceGrant:
				if event.Time != expected.logicalTime {
					return fmt.Errorf("%s TimeAdvanceGrant time = %g, want %g", observation.actor, event.Time, expected.logicalTime)
				}
				if observation.actor == "producer" {
					producerGrant = true
				} else if observation.actor == "consumer" {
					consumerGrant = true
				}
			case federate.FederationHalted:
				return fmt.Errorf("federation halted: %s", event.Reason)
			}
		case <-waitCtx.Done():
			return fmt.Errorf(
				"wait for reflect=%t interaction=%t producer_grant=%t consumer_grant=%t: %w",
				reflectReceived, interactionReceived, producerGrant, consumerGrant, waitCtx.Err(),
			)
		}
	}

	updateStarted := omResults[0].startedAt
	interactionStarted := omResults[1].startedAt
	r.recorder.record("reflectAttributeValues.latency", elapsedCounterNS(updateStarted, reflectAt), deliveryDimensions)
	r.recorder.record("receiveInteraction.latency", elapsedCounterNS(interactionStarted, interactionAt), deliveryDimensions)
	batchCompleted := reflectAt
	if interactionAt > batchCompleted {
		batchCompleted = interactionAt
	}
	sendStarted := updateStarted
	if interactionStarted < sendStarted {
		sendStarted = interactionStarted
	}
	r.recorder.record("completed_delivery_batch_latency", elapsedCounterNS(batchBoundary, batchCompleted), deliveryDimensions)
	r.recorder.record("send_to_delivery_batch_latency", elapsedCounterNS(sendStarted, batchCompleted), deliveryDimensions)
	return nil
}

func validateReflect(event federate.ReflectAttributeValues, objectHandle uint64, expected encodedIteration) error {
	if event.ObjectHandle != objectHandle || event.ClassName != objectClass {
		return fmt.Errorf("ReflectAttributeValues identity = (%d, %q), want (%d, %q)", event.ObjectHandle, event.ClassName, objectHandle, objectClass)
	}
	if event.Timestamp == nil || *event.Timestamp != expected.logicalTime {
		return fmt.Errorf("ReflectAttributeValues timestamp = %v, want %g", event.Timestamp, expected.logicalTime)
	}
	if !bytes.Equal(event.Attributes[sequenceField], expected.attributes[sequenceField]) {
		return fmt.Errorf("ReflectAttributeValues %s payload mismatch", sequenceField)
	}
	if !bytes.Equal(event.Attributes[payloadField], expected.attributes[payloadField]) {
		return fmt.Errorf("ReflectAttributeValues %s payload mismatch", payloadField)
	}
	return nil
}

func validateInteraction(event federate.ReceiveInteraction, expected encodedIteration) error {
	if event.ClassName != interactionClass {
		return fmt.Errorf("ReceiveInteraction class = %q, want %q", event.ClassName, interactionClass)
	}
	if event.Timestamp == nil || *event.Timestamp != expected.logicalTime {
		return fmt.Errorf("ReceiveInteraction timestamp = %v, want %g", event.Timestamp, expected.logicalTime)
	}
	if !bytes.Equal(event.Parameters[sequenceField], expected.interactionParameters[sequenceField]) {
		return fmt.Errorf("ReceiveInteraction %s payload mismatch", sequenceField)
	}
	if !bytes.Equal(event.Parameters[payloadField], expected.interactionParameters[payloadField]) {
		return fmt.Errorf("ReceiveInteraction %s payload mismatch", payloadField)
	}
	return nil
}

func callWithTimeout(ctx context.Context, timeout time.Duration, call func(context.Context) error) error {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return call(callCtx)
}

func (r *liveRunner) close() {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var resignWG sync.WaitGroup
	for _, fed := range []*federate.Federate{r.producer, r.consumer} {
		if fed == nil {
			continue
		}
		resignWG.Add(1)
		go func(fed *federate.Federate) {
			defer resignWG.Done()
			_ = fed.ResignWithAction(cleanupCtx, federate.ResignActionCancelThenDelete)
		}(fed)
	}
	resignWG.Wait()
	if r.pumpCancel != nil {
		r.pumpCancel()
	}
	r.pumpWG.Wait()
	if r.producerConn != nil {
		_ = r.producerConn.Close()
	}
	if r.consumerConn != nil {
		_ = r.consumerConn.Close()
	}
	r.producer = nil
	r.consumer = nil
	r.producerConn = nil
	r.consumerConn = nil
}
