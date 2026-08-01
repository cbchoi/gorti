package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

func TestCompactModeCreatesOnlyAcceptedRoleSummary(t *testing.T) {
	plan, payloads := compactTestPlan(t, 1)
	cfg := config{
		Role: roleSubscriber, Seed: "1516", Count: 1, ReceiveOrder: true,
		WorkloadPlan: "plan.bin", CompactSummary: true, OutputDir: t.TempDir(), Timeout: time.Second,
	}
	state, err := newEventStateWithPayloads(cfg, payloads)
	if err != nil {
		t.Fatal(err)
	}
	markCompactSynchronizations(state)
	state.discovered = true
	state.objectHandle = 17
	started := counterNow()
	if err := state.armReceiveOrderBatch(started); err != nil {
		t.Fatal(err)
	}
	state.accept(federate.ReflectAttributeValues{
		ObjectHandle: 17, ClassName: objectClass, Attributes: payloads[0].attributes,
	}, started+1)
	state.accept(federate.ReceiveInteraction{
		ClassName: interactionClass, Parameters: payloads[0].parameters,
	}, started+2)
	if err := state.failure(); err != nil {
		t.Fatal(err)
	}
	log := &runLog{compact: true}
	if err := log.sample("completedReceiveOrderBatch", 42, "OM", "delivery"); err != nil {
		t.Fatal(err)
	}
	if err := prepareOutputDirectory(cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeParticipantSummary(cfg, plan, state, log); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(cfg.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "subscriber-summary.json" {
		t.Fatalf("compact output entries = %v", entryNames(entries))
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutputDir, "subscriber-summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary participantSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Schema != participantSummarySchema || summary.Status != "accepted" ||
		summary.Seed != 1516 || summary.Count != 1 ||
		summary.PlanSHA256 != plan.digestHex() || summary.TopologySHA256 != plan.topologyDigestHex() ||
		!summary.Ready || !summary.Measure || !summary.Start || !summary.Done {
		t.Fatalf("summary identity/status = %+v", summary)
	}
	if summary.CallbackAccounting.Delivered != 2 ||
		summary.CallbackAccounting.AttributeDelivered != 1 ||
		summary.CallbackAccounting.InteractionDelivered != 1 ||
		summary.CompletedReceiveOrderBatchNS == nil || *summary.CompletedReceiveOrderBatchNS != 42 {
		t.Fatalf("summary evidence = %+v", summary)
	}
	if summary.UpdateAttributeValuesMedianNS != nil || summary.SendInteractionMedianNS != nil {
		t.Fatal("subscriber summary contains publisher measurements")
	}
}

func TestCompactFailureWritesNoAcceptedSummary(t *testing.T) {
	plan, payloads := compactTestPlan(t, 1)
	cfg := config{
		Role: roleSubscriber, Seed: "1516", Count: 1, ReceiveOrder: true,
		WorkloadPlan: "plan.bin", CompactSummary: true, OutputDir: t.TempDir(),
	}
	state, err := newEventStateWithPayloads(cfg, payloads)
	if err != nil {
		t.Fatal(err)
	}
	markCompactSynchronizations(state)
	log := &runLog{compact: true}
	if err := log.sample("completedReceiveOrderBatch", 1, "OM", "delivery"); err != nil {
		t.Fatal(err)
	}
	if err := prepareOutputDirectory(cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeParticipantSummary(cfg, plan, state, log); err == nil {
		t.Fatal("incomplete callback accounting produced an accepted summary")
	}
	entries, err := os.ReadDir(cfg.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed compact output entries = %v", entryNames(entries))
	}
}

func TestCompactPublisherReportsMeasuredCallMedians(t *testing.T) {
	plan, payloads := compactTestPlan(t, 3)
	cfg := config{
		Role: rolePublisher, Seed: "1516", Count: 3, ReceiveOrder: true,
		WorkloadPlan: "plan.bin", CompactSummary: true, OutputDir: t.TempDir(),
	}
	state, err := newEventStateWithPayloads(cfg, payloads)
	if err != nil {
		t.Fatal(err)
	}
	markCompactSynchronizations(state)
	log := &runLog{compact: true}
	for _, duration := range []int64{9, 1, 5} {
		log.memory.recordCall("updateAttributeValues", duration)
	}
	for _, duration := range []int64{12, 4, 8} {
		log.memory.recordCall("sendInteraction", duration)
	}
	if err := prepareOutputDirectory(cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeParticipantSummary(cfg, plan, state, log); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutputDir, "publisher-summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary participantSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.UpdateAttributeValuesMedianNS == nil || *summary.UpdateAttributeValuesMedianNS != 5 ||
		summary.SendInteractionMedianNS == nil || *summary.SendInteractionMedianNS != 8 {
		t.Fatalf("publisher medians = %+v", summary)
	}
}

func TestCompactPublisherMapsLocalLRCAdmissionsToDiagnosticMedians(t *testing.T) {
	log := &runLog{compact: true}
	for _, duration := range []int64{9, 1, 5} {
		log.memory.recordCall("queueAttributeValues", duration)
	}
	for _, duration := range []int64{12, 4, 8} {
		log.memory.recordCall("queueInteraction", duration)
	}
	snapshot := log.compactSnapshot()
	if snapshot.UpdateAttributeValuesMedianNS == nil ||
		*snapshot.UpdateAttributeValuesMedianNS != 5 ||
		snapshot.SendInteractionMedianNS == nil ||
		*snapshot.SendInteractionMedianNS != 8 {
		t.Fatalf("LocalLRC diagnostic medians = %+v", snapshot)
	}
}

func TestReceiveOrderCompletionUsesPreStartTimingArm(t *testing.T) {
	_, payloads := compactTestPlan(t, 1)
	cfg := config{
		Role: roleSubscriber, Seed: "1516", Count: 1, ReceiveOrder: true,
		WorkloadPlan: "plan.bin", CompactSummary: true, Timeout: time.Second,
	}
	unarmed, err := newEventStateWithPayloads(cfg, payloads)
	if err != nil {
		t.Fatal(err)
	}
	unarmed.discovered = true
	unarmed.objectHandle = 17
	unarmed.accept(federate.ReflectAttributeValues{
		ObjectHandle: 17, ClassName: objectClass, Attributes: payloads[0].attributes,
	}, counterNow())
	if unarmed.failure() == nil {
		t.Fatal("measured callback was accepted before the START timing arm")
	}

	state, err := newEventStateWithPayloads(cfg, payloads)
	if err != nil {
		t.Fatal(err)
	}
	state.discovered = true
	state.objectHandle = 17
	started := counterNow()
	attributeAt := started + counterStamp(performanceCounterFrequency()/1000)
	interactionAt := started + counterStamp(performanceCounterFrequency()/500)
	if err := state.armReceiveOrderBatch(started); err != nil {
		t.Fatal(err)
	}
	state.accept(federate.ReflectAttributeValues{
		ObjectHandle: 17, ClassName: objectClass, Attributes: payloads[0].attributes,
	}, attributeAt)
	state.accept(federate.ReceiveInteraction{
		ClassName: interactionClass, Parameters: payloads[0].parameters,
	}, interactionAt)
	log := &runLog{compact: true}
	p := participant{cfg: cfg, log: log, state: state, payloads: payloads}
	if err := p.subscribeReceiveOrder(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := log.compactSnapshot()
	want := counterBetween(started, interactionAt)
	if snapshot.CompletedReceiveOrderBatchNS == nil || *snapshot.CompletedReceiveOrderBatchNS != want {
		t.Fatalf("completed batch = %v, want armed duration %d", snapshot.CompletedReceiveOrderBatchNS, want)
	}
}

func TestCallbackDigestsUseActualArrivalOrder(t *testing.T) {
	cfg := config{Role: roleSubscriber, Seed: "1516", Count: 2, ReceiveOrder: true}
	payloads, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		t.Fatal(err)
	}
	inOrder := acceptPayloadOrder(t, cfg, payloads, []int{0, 1})
	reversed := acceptPayloadOrder(t, cfg, payloads, []int{1, 0})
	inAttribute, inInteraction, _ := inOrder.callbackDigests()
	reverseAttribute, reverseInteraction, _ := reversed.callbackDigests()
	if inAttribute == reverseAttribute || inInteraction == reverseInteraction {
		t.Fatal("arrival-order digest did not change when callbacks were reordered")
	}
	if inAttribute != expectedArrivalDigest(payloads, []int{0, 1}, true) ||
		inInteraction != expectedArrivalDigest(payloads, []int{0, 1}, false) {
		t.Fatal("arrival digest does not use uint32 BE sequence plus lowercase payload bytes")
	}
}

func TestCompactCallbackTraceRejectsCrossChannelReordering(t *testing.T) {
	_, payloads := compactTestPlan(t, 1)
	cfg := config{
		Role: roleSubscriber, Seed: "1516", Count: 1, ReceiveOrder: true,
		WorkloadPlan: "plan.dvshla", CompactSummary: true,
	}
	state, err := newEventStateWithPayloads(cfg, payloads)
	if err != nil {
		t.Fatal(err)
	}
	state.discovered = true
	state.objectHandle = 17
	if err := state.armReceiveOrderBatch(counterNow()); err != nil {
		t.Fatal(err)
	}
	state.accept(federate.ReceiveInteraction{
		ClassName: interactionClass, Parameters: payloads[0].parameters,
	}, counterNow())
	if err := state.failure(); err == nil || !strings.Contains(err.Error(), "callback trace") {
		t.Fatalf("cross-channel reorder error = %v", err)
	}
}

func TestLegacyRunLogStillCreatesThreeNDJSONStreams(t *testing.T) {
	cfg := config{Role: rolePublisher, OutputDir: t.TempDir()}
	if err := prepareOutputDirectory(cfg); err != nil {
		t.Fatal(err)
	}
	log, err := newRunLog(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if log.compact {
		t.Fatal("legacy logger unexpectedly selected compact mode")
	}
	if err := log.event("FM", "legacy", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := log.metric("OM", "legacy", "count", 1); err != nil {
		t.Fatal(err)
	}
	if err := log.sample("legacy", 1, "OM", "call"); err != nil {
		t.Fatal(err)
	}
	if err := log.close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(cfg.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	names := entryNames(entries)
	sort.Strings(names)
	want := []string{"publisher-metrics.ndjson", "publisher-samples.ndjson", "publisher-semantic.ndjson"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("legacy log files = %v, want %v", names, want)
	}
}

func TestCompactRunLogWritesNoPerEventFiles(t *testing.T) {
	cfg := config{Role: rolePublisher, CompactSummary: true, OutputDir: t.TempDir()}
	if err := prepareOutputDirectory(cfg); err != nil {
		t.Fatal(err)
	}
	log, err := newRunLog(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.event("FM", "ignored", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := log.metric("OM", "ignored", "count", 1); err != nil {
		t.Fatal(err)
	}
	if err := log.sample("ignored", 1, "OM", "delivery"); err != nil {
		t.Fatal(err)
	}
	if err := log.close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(cfg.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("compact run log created per-event files: %v", entryNames(entries))
	}
}

func TestCompactOutputDirectoryMustStartEmpty(t *testing.T) {
	for _, stale := range []string{
		participantSummaryFilename(rolePublisher),
		participantSummaryFilename(roleSubscriber),
		"other.txt",
	} {
		t.Run(stale, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, stale), []byte("stale\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := config{Role: roleSubscriber, CompactSummary: true, OutputDir: directory}
			if err := prepareOutputDirectory(cfg); err == nil {
				t.Fatalf("compact mode accepted stale output %q", stale)
			}
		})
	}
}

func compactTestPlan(t *testing.T, count int) (*workloadPlan, []encodedIteration) {
	t.Helper()
	records := make([]workloadPlanRecord, count)
	for index := range records {
		records[index] = workloadPlanRecord{
			Index: uint32(index), EventSequence: 1,
			TargetOrdinal: uint32(index + 1), OccurrenceOrdinal: 1,
			AttributePayload:   [8]byte{byte(index + 1)},
			InteractionPayload: [8]byte{byte(index + 101)},
		}
	}
	plan, err := parseWorkloadPlan(encodePlanForTest(t, 1516, records), count, 1516)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := preencodePlanWorkload(plan)
	if err != nil {
		t.Fatal(err)
	}
	return plan, payloads
}

func markCompactSynchronizations(state *eventState) {
	state.synchronized[readySync] = true
	state.synchronized[measureSync] = true
	state.synchronized[startSync] = true
	state.synchronized[doneSync] = true
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	return names
}

func acceptPayloadOrder(
	t *testing.T,
	cfg config,
	payloads []encodedIteration,
	order []int,
) *eventState {
	t.Helper()
	state, err := newEventStateWithPayloads(cfg, payloads)
	if err != nil {
		t.Fatal(err)
	}
	state.discovered = true
	state.objectHandle = 17
	for _, index := range order {
		state.accept(federate.ReflectAttributeValues{
			ObjectHandle: 17, ClassName: objectClass, Attributes: payloads[index].attributes,
		}, counterNow())
		state.accept(federate.ReceiveInteraction{
			ClassName: interactionClass, Parameters: payloads[index].parameters,
		}, counterNow())
	}
	if err := state.failure(); err != nil {
		t.Fatal(err)
	}
	return state
}

func expectedArrivalDigest(payloads []encodedIteration, order []int, attribute bool) string {
	digest := sha256.New()
	var sequence [4]byte
	for _, index := range order {
		binary.BigEndian.PutUint32(sequence[:], uint32(index))
		_, _ = digest.Write(sequence[:])
		payload := payloads[index].interaction
		if attribute {
			payload = payloads[index].attribute
		}
		_, _ = digest.Write([]byte(payload))
	}
	return hex.EncodeToString(digest.Sum(nil))
}
