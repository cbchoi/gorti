package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const participantSummarySchema = "gorti.devstone.participant-summary/v1"

type participantSummary struct {
	Schema                        string                    `json:"schema"`
	Role                          role                      `json:"role"`
	Seed                          uint64                    `json:"seed"`
	Count                         int                       `json:"count"`
	PlanSHA256                    string                    `json:"plan_sha256"`
	TopologySHA256                string                    `json:"topology_sha256"`
	Status                        string                    `json:"status"`
	Ready                         bool                      `json:"ready"`
	Measure                       bool                      `json:"measure"`
	Start                         bool                      `json:"start"`
	Done                          bool                      `json:"done"`
	CallbackAccounting            callbackAccountingSummary `json:"callback_accounting"`
	AttributeArrivalOrderSHA256   string                    `json:"attribute_arrival_order_sha256"`
	InteractionArrivalOrderSHA256 string                    `json:"interaction_arrival_order_sha256"`
	CallbackTraceSHA256           string                    `json:"callback_trace_sha256"`
	UpdateAttributeValuesMedianNS *int64                    `json:"update_attribute_values_median_ns,omitempty"`
	SendInteractionMedianNS       *int64                    `json:"send_interaction_median_ns,omitempty"`
	CompletedReceiveOrderBatchNS  *int64                    `json:"completed_receive_order_batch_ns,omitempty"`
}

type callbackAccountingSummary struct {
	Expected             int `json:"expected"`
	Delivered            int `json:"delivered"`
	AttributeDelivered   int `json:"attribute_delivered"`
	InteractionDelivered int `json:"interaction_delivered"`
	Rejected             int `json:"rejected"`
	Dropped              int `json:"dropped"`
	Unexpected           int `json:"unexpected"`
	Duplicates           int `json:"duplicates"`
	Invalid              int `json:"invalid"`
}

func prepareOutputDirectory(cfg config) error {
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if !cfg.CompactSummary {
		return nil
	}
	entries, err := os.ReadDir(cfg.OutputDir)
	if err != nil {
		return fmt.Errorf("inspect compact output directory: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf(
			"compact output directory must be empty; found %q",
			entries[0].Name(),
		)
	}
	return nil
}

func participantSummaryFilename(actor role) string {
	return string(actor) + "-summary.json"
}

func writeParticipantSummary(
	cfg config,
	plan *workloadPlan,
	state *eventState,
	log *runLog,
) error {
	if !cfg.CompactSummary || plan == nil || state == nil || log == nil || !log.compact {
		return errors.New("compact participant summary is not initialized")
	}
	if err := state.failure(); err != nil {
		return fmt.Errorf("refuse summary for failed participant: %w", err)
	}
	ready, measure, start, done := state.synchronizationStatus()
	if !ready || !measure || !start || !done {
		return fmt.Errorf(
			"refuse summary before all synchronization points: ready=%t measure=%t start=%t done=%t",
			ready, measure, start, done,
		)
	}
	accounting := state.accounting()
	attributeDelivered, interactionDelivered := state.callbackCounts()
	attributeDigest, interactionDigest, callbackTraceDigest := state.callbackDigests()
	measurements := log.compactSnapshot()
	if err := validateCompactEvidence(
		cfg,
		accounting,
		attributeDelivered,
		interactionDelivered,
		measurements,
	); err != nil {
		return err
	}

	summary := participantSummary{
		Schema: participantSummarySchema, Role: cfg.Role, Seed: plan.Seed, Count: cfg.Count,
		PlanSHA256: plan.digestHex(), TopologySHA256: plan.topologyDigestHex(), Status: "accepted",
		Ready: ready, Measure: measure, Start: start, Done: done,
		CallbackAccounting: callbackAccountingSummary{
			Expected: accounting.Expected, Delivered: accounting.Delivered,
			AttributeDelivered: attributeDelivered, InteractionDelivered: interactionDelivered,
			Rejected: accounting.Rejected, Dropped: accounting.Dropped,
			Unexpected: accounting.Unexpected, Duplicates: accounting.Duplicates, Invalid: accounting.Invalid,
		},
		AttributeArrivalOrderSHA256:   attributeDigest,
		InteractionArrivalOrderSHA256: interactionDigest,
		CallbackTraceSHA256:           callbackTraceDigest,
		UpdateAttributeValuesMedianNS: measurements.UpdateAttributeValuesMedianNS,
		SendInteractionMedianNS:       measurements.SendInteractionMedianNS,
		CompletedReceiveOrderBatchNS:  measurements.CompletedReceiveOrderBatchNS,
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("encode participant summary: %w", err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join(cfg.OutputDir, participantSummaryFilename(cfg.Role))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create participant summary: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write participant summary: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close participant summary: %w", err)
	}
	return nil
}

func validateCompactEvidence(
	cfg config,
	accounting deliveryAccounting,
	attributeDelivered int,
	interactionDelivered int,
	measurements compactMeasurementSnapshot,
) error {
	if accounting.Rejected != 0 || accounting.Dropped != 0 || accounting.Unexpected != 0 ||
		accounting.Duplicates != 0 || accounting.Invalid != 0 {
		return fmt.Errorf("refuse summary with callback errors: %+v", accounting)
	}
	if cfg.Role == rolePublisher {
		if accounting.Expected != 0 || accounting.Delivered != 0 ||
			attributeDelivered != 0 || interactionDelivered != 0 {
			return errors.New("publisher unexpectedly accepted delivery callbacks")
		}
		if measurements.UpdateAttributeValuesMedianNS == nil || measurements.SendInteractionMedianNS == nil ||
			measurements.CompletedReceiveOrderBatchNS != nil ||
			measurements.UpdateAttributeValuesCount != cfg.Count || measurements.SendInteractionCount != cfg.Count {
			return errors.New("publisher compact measurements are incomplete")
		}
		return nil
	}
	if accounting.Expected != 2*cfg.Count || accounting.Delivered != 2*cfg.Count ||
		attributeDelivered != cfg.Count || interactionDelivered != cfg.Count {
		return fmt.Errorf("subscriber callback accounting is incomplete: %+v", accounting)
	}
	if measurements.CompletedReceiveOrderBatchNS == nil ||
		measurements.UpdateAttributeValuesMedianNS != nil || measurements.SendInteractionMedianNS != nil ||
		measurements.UpdateAttributeValuesCount != 0 || measurements.SendInteractionCount != 0 {
		return errors.New("subscriber compact measurements are incomplete")
	}
	return nil
}
