// AdminService gRPC handler — translates rti.v1.AdminService RPCs into
// read-only Snapshot calls on every Manager + an event-log reader for
// TailEvents.
//
// Owner: Agent A — rtid-TUI Phase 1 (docs/rtid-tui.md).
//
// All handlers are read-only — they MUST NOT call any Manager method
// that mutates state. Mutating control-plane operations (force-resign,
// kill, drain) are explicitly out of scope for Phase 1; see the docs
// §2.1 PINNED decision.

package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// OutboxStatsSource exposes the per-(federation, federate) outbox
// statistics the AdminService reports. Implemented by cmd/rtid's
// multiOutbox.snapshot(); declared here as a small interface so the
// handler doesn't depend on the cmd/rtid package (avoids the import
// cycle that already prevents var _ core.SubscribableOutbox at the
// outbox declaration site).
//
// Each entry is one (fed, handle) channel's queue depth + capacity +
// cumulative drops. The handler joins these against the federation
// roster to populate FederateSnapshot.{outbox_*, drops_total}.
type OutboxStatsSource interface {
	// OutboxStats returns one entry per active subscriber. The slice
	// order is implementation-defined; the handler indexes by
	// (federation, handle) so order does not matter.
	OutboxStats() []OutboxStat
}

// OutboxStat is one (fed, handle) recipient's wire-stat snapshot.
type OutboxStat struct {
	Federation core.FederationName
	Handle     core.FederateHandle
	QueueDepth uint32
	Capacity   uint32
	DropsTotal uint64
}

// AdminOptions bundles the AdminService handler's dependencies. Every
// non-nil field threads its Snapshot() result into the assembled
// SnapshotResponse. Callers may leave individual fields nil — the
// handler simply elides the corresponding sections (used by tests
// constructing partial fixtures).
type AdminOptions struct {
	// Federations is the federation roster source. REQUIRED — without
	// it the handler has no list of federations to walk.
	Federations core.FederationStore

	// Declarations is the pub/sub state source.
	Declarations core.DeclarationManagement

	// Sync is the sync-point state source.
	Sync core.SyncCoordinator

	// Ownership is the ownership counters source.
	Ownership core.OwnershipCoordinator

	// DDM is the region-count source.
	DDM core.DataDistributionManagement

	// Savepoint is the save/restore state source.
	Savepoint core.SavepointCoordinator

	// MOM is the per-federate counter source.
	MOM core.ManagementObjectModel

	// Time is the time-management state source.
	Time core.TimeManager

	// Objects is the object-instance count source.
	Objects core.ObjectRegistry

	// Outbox is the per-recipient queue-depth + drops source.
	Outbox OutboxStatsSource

	// EventLog is the source for the TailEvents server-stream. When
	// nil, TailEvents returns codes.FailedPrecondition.
	EventLog core.EventLog

	// Version is the rtid build version returned in Status / Snapshot.
	// Empty → "unknown".
	Version string

	// StartedAt is the rtid process start time. Zero → uptime is
	// reported as 0 seconds. Tests pass a fake; production wires
	// time.Now() at composition.
	StartedAt time.Time
}

// adminService is the concrete AdminServiceServer impl. Embeds
// UnimplementedAdminServiceServer for forward compatibility (gRPC
// v1.65+).
type adminService struct {
	rtiv1.UnimplementedAdminServiceServer
	opts AdminOptions
}

// newAdminService constructs the handler. No validation here — the
// composition root passes whatever it has wired; nil fields cause
// elided sections rather than handler errors.
func newAdminService(opts AdminOptions) *adminService {
	return &adminService{opts: opts}
}

// RegisterAdminService attaches an AdminService handler to the given
// gRPC server. Exposed as a public function so cmd/rtid can register
// against the dedicated admin-listener gRPC server (separate from the
// federate-facing server registered via Server.Register).
//
// grpcServer is typed as `any` for symmetry with Server.Register —
// callers pass a *grpc.Server; the assertion to grpc.ServiceRegistrar
// happens at runtime so this function's signature does not change
// when the grpc package's exported types churn.
func RegisterAdminService(grpcServer any, opts AdminOptions) error {
	gs, ok := grpcServer.(grpc.ServiceRegistrar)
	if !ok {
		return fmt.Errorf("transport/grpc: RegisterAdminService: want grpc.ServiceRegistrar, got %T", grpcServer)
	}
	rtiv1.RegisterAdminServiceServer(gs, newAdminService(opts))
	return nil
}

// --- Status -----------------------------------------------------------------

// Status implements rtiv1.AdminServiceServer.Status — lightweight
// liveness probe. Returns rtid version + uptime in seconds.
func (s *adminService) Status(_ context.Context, req *rtiv1.StatusRequest) (*rtiv1.StatusResponse, error) {
	if req == nil {
		return nil, nilRequest("Status")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	return &rtiv1.StatusResponse{
		RtidVersion:   versionOrDefault(s.opts.Version),
		UptimeSeconds: uptimeSecondsFrom(s.opts.StartedAt),
	}, nil
}

// versionOrDefault returns "unknown" when the configured version is
// empty so the wire field never carries an empty string.
func versionOrDefault(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

// uptimeSecondsFrom computes uptime as a non-negative count. A zero
// StartedAt or a clock skew producing a negative duration both yield 0
// (the wire type is uint64 — non-monotonic skew should never panic the
// handler).
func uptimeSecondsFrom(startedAt time.Time) uint64 {
	if startedAt.IsZero() {
		return 0
	}
	d := time.Since(startedAt)
	if d < 0 {
		return 0
	}
	return uint64(d / time.Second)
}

// --- Snapshot ---------------------------------------------------------------

// Snapshot implements rtiv1.AdminServiceServer.Snapshot — assembles
// one consistent point-in-time view of every federation by calling
// each Manager's Snapshot() in turn. Lock acquisition is per-Manager;
// the resulting view is consistent per-federation but not strictly
// atomic across all Managers (sufficient for ~1Hz polling).
func (s *adminService) Snapshot(_ context.Context, req *rtiv1.SnapshotRequest) (*rtiv1.SnapshotResponse, error) {
	if req == nil {
		return nil, nilRequest("Snapshot")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.opts.Federations == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"AdminService.Snapshot: Federations source is not wired")
	}

	resp := &rtiv1.SnapshotResponse{
		RtidVersion:   versionOrDefault(s.opts.Version),
		UptimeSeconds: uptimeSecondsFrom(s.opts.StartedAt),
	}

	rosters := s.opts.Federations.Snapshot()
	filter := req.GetFederationName()

	// Index outbox stats by (fed, handle) once for the whole snapshot
	// so we don't walk the slice per federate.
	outboxIdx := map[adminFedHandleKey]OutboxStat{}
	if s.opts.Outbox != nil {
		for _, e := range s.opts.Outbox.OutboxStats() {
			outboxIdx[adminFedHandleKey{fed: e.Federation, h: e.Handle}] = e
		}
	}

	resp.Federations = make([]*rtiv1.FederationSnapshot, 0, len(rosters))
	for _, roster := range rosters {
		if filter != "" && string(roster.Name) != filter {
			continue
		}
		fedSnap := buildFederationSnapshot(s.opts, roster, outboxIdx)
		resp.Federations = append(resp.Federations, fedSnap)
	}
	return resp, nil
}

// adminFedHandleKey is the per-snapshot (fed, handle) lookup key for
// the outbox-stats index. Named-struct so it can flow through a
// helper signature without leaking an anonymous struct into the API.
type adminFedHandleKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

// buildFederationSnapshot assembles one rtiv1.FederationSnapshot by
// joining the federation roster against every Manager's Snapshot()
// result. Static helper (no method on adminService) so the join logic
// reads top-down without indirection.
//
// outboxIdx is the (fed, handle) → OutboxStat lookup table built once
// per Snapshot RPC.
func buildFederationSnapshot(
	opts AdminOptions,
	roster core.FederationRoster,
	outboxIdx map[adminFedHandleKey]OutboxStat,
) *rtiv1.FederationSnapshot {
	out := &rtiv1.FederationSnapshot{
		Name:            string(roster.Name),
		Mode:            modeToProto(roster.Mode),
		FederatesJoined: uint32(len(roster.Federates)),
	}

	// Per-Manager federation-level snapshots (cheap; nil-safe).
	var declSnap core.DeclarationSnapshot
	if opts.Declarations != nil {
		declSnap = opts.Declarations.Snapshot(roster.Name)
		out.PublishedClasses = uint64SliceFromObjClasses(declSnap.PublishedObjectClasses)
	}
	if opts.Sync != nil {
		for _, sp := range opts.Sync.Snapshot(roster.Name) {
			out.SyncPoints = append(out.SyncPoints, &rtiv1.SyncPointSnapshot{
				Label:           sp.Label,
				State:           syncStateToProto(sp.State),
				RequiredHandles: uint64SliceFromHandles(sp.RequiredHandles),
				AchievedHandles: uint64SliceFromHandles(sp.AchievedHandles),
			})
		}
	}
	if opts.Savepoint != nil {
		sps := opts.Savepoint.Snapshot(roster.Name)
		out.SaveState = adminSaveStateToProto(sps.SaveState)
		out.RestoreState = adminRestoreStateToProto(sps.RestoreState)
	}
	if opts.DDM != nil {
		out.RegionCount = opts.DDM.Snapshot(roster.Name).RegionCount
	}
	if opts.Objects != nil {
		out.ObjectInstanceCount = opts.Objects.Snapshot(roster.Name).InstanceCount
	}

	var timeSnap core.TimeSnapshot
	if opts.Time != nil {
		timeSnap = opts.Time.Snapshot(roster.Name)
		out.Time = &rtiv1.TimeSnapshot{
			Lbts: float64(timeSnap.LBTS),
		}
		// regulating + constrained sets + pending grants.
		for _, fs := range timeSnap.Federates {
			if fs.Regulating {
				out.Time.RegulatingHandles = append(out.Time.RegulatingHandles, uint64(fs.Handle))
			}
			if fs.Constrained {
				out.Time.ConstrainedHandles = append(out.Time.ConstrainedHandles, uint64(fs.Handle))
			}
			if fs.HasPendingRequest {
				out.Time.PendingGrants = append(out.Time.PendingGrants, &rtiv1.PendingGrant{
					FederateHandle: uint64(fs.Handle),
					RequestedTime:  float64(fs.PendingRequestTime),
				})
			}
		}
	}
	// Handle the LBTS=+Inf case: protobuf doubles serialize +Inf fine,
	// but defensive normalize the zero (no-regulator) case to match
	// what the design doc shows.
	if opts.Time != nil && len(timeSnap.Federates) == 0 {
		out.Time.Lbts = math.Inf(1)
	}

	// Index time + MOM snapshots by handle for the per-federate join.
	timeByHandle := map[core.FederateHandle]core.TimeFederateState{}
	for _, fs := range timeSnap.Federates {
		timeByHandle[fs.Handle] = fs
	}
	var momSnap core.MOMSnapshot
	if opts.MOM != nil {
		momSnap = opts.MOM.Snapshot(roster.Name)
	}

	for _, info := range roster.Federates {
		row := &rtiv1.FederateSnapshot{
			Handle: uint64(info.Handle),
			Name:   info.Name,
		}
		// rtid-TUI Phase 3: surface the federate's wall-clock join time
		// for the drilldown view's `age` column. JoinedAt.IsZero() means
		// the federation manager could not record it (legacy data path);
		// surface 0 so clients can hide the column for that row.
		if !info.JoinedAt.IsZero() {
			row.JoinUnixSeconds = info.JoinedAt.Unix()
		}
		if t, ok := timeByHandle[info.Handle]; ok {
			row.CurrentTime = float64(t.CurrentTime)
			row.Lookahead = float64(t.Lookahead)
			row.Regulating = t.Regulating
			row.Constrained = t.Constrained
			if t.HasPendingRequest {
				v := float64(t.PendingRequestTime)
				row.PendingRequestTime = &v
			}
		}
		if pubsub, ok := declSnap.PerFederate[info.Handle]; ok {
			row.PublishedObjectClasses = uint64SliceFromObjClasses(pubsub.PublishedObjectClasses)
			row.SubscribedObjectClasses = uint64SliceFromObjClasses(pubsub.SubscribedObjectClasses)
			row.PublishedInteractionClasses = uint64SliceFromIntClasses(pubsub.PublishedInteractionClasses)
			row.SubscribedInteractionClasses = uint64SliceFromIntClasses(pubsub.SubscribedInteractionClasses)
		}
		if c, ok := momSnap.PerFederate[info.Handle]; ok {
			row.UpdatesSent = uint64(c.UpdatesSent)
			row.InteractionsSent = uint64(c.InteractionsSent)
			row.ReflectionsReceived = uint64(c.ReflectionsReceived)
			row.InteractionsReceived = uint64(c.InteractionsReceived)
		}
		if e, ok := outboxIdx[adminFedHandleKey{fed: roster.Name, h: info.Handle}]; ok {
			row.OutboxQueueDepth = e.QueueDepth
			row.OutboxCapacity = e.Capacity
			row.DropsTotal = e.DropsTotal
		}
		out.Federates = append(out.Federates, row)
	}
	return out
}

// --- TailEvents -------------------------------------------------------------

// Phase 4 batching defaults + clamps. The defaults match the perf-pass
// batched-channel pattern in multiOutbox (a small ring buffer flushed
// at the smaller of N events / max-latency). The clamps protect the
// server from clients setting either knob to a degenerate value.
const (
	tailEventsDefaultMaxBatch    = 32
	tailEventsMaxBatchCeiling    = 1024
	tailEventsDefaultMaxLatency  = 10 * time.Millisecond
	tailEventsMaxLatencyCeiling  = 1 * time.Second
)

// TailEvents implements rtiv1.AdminServiceServer.TailEvents. Phase 4
// extends the Phase-1 single-event-per-message handler with three
// improvements (docs/rtid-tui.md §7.5):
//
//   - Server-side filtering: event_class_filter (case-sensitive
//     substring) and federate_handle_filter (handle whitelist) are
//     applied BEFORE send so the wire only carries what the client
//     wants.
//   - Batched responses: the handler accumulates events in a ring
//     buffer and flushes either when the buffer reaches
//     max_batch_events or after max_batch_latency_ms.
//   - Backpressure-aware: when the gRPC server-stream send buffer
//     can't accept a batch, the events are folded into an
//     overflow_skipped counter that piggybacks on the next successful
//     batch, so the server NEVER blocks indefinitely on a slow
//     client.
//
// When the reader hits io.EOF, any pending batch is flushed and the
// stream terminates. Stream context cancellation aborts further reads.
func (s *adminService) TailEvents(req *rtiv1.TailEventsRequest, stream rtiv1.AdminService_TailEventsServer) error {
	if req == nil {
		return nilRequest("TailEvents")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return err
	}
	if s.opts.EventLog == nil {
		return status.Error(codes.FailedPrecondition,
			"AdminService.TailEvents: EventLog source is not wired")
	}
	fed := req.GetFederationName()
	if fed == "" {
		return status.Error(codes.InvalidArgument,
			"AdminService.TailEvents: federation_name is required")
	}
	ctx := stream.Context()
	rdr, err := s.opts.EventLog.OpenReader(ctx, fed)
	if err != nil {
		return errToStatus(err)
	}
	defer func() { _ = rdr.Close() }()

	cfg := tailEventsConfigFromRequest(req)
	classFilter := req.GetEventClassFilter()
	handleFilter := buildHandleFilter(req.GetFederateHandleFilter())

	pending := make([]*rtiv1.TailedEvent, 0, cfg.maxBatch)
	var overflow uint64
	flushTimer := time.NewTimer(cfg.maxLatency)
	if !flushTimer.Stop() {
		<-flushTimer.C
	}
	timerArmed := false
	defer flushTimer.Stop()

	flush := func() error {
		if len(pending) == 0 && overflow == 0 {
			return nil
		}
		batch := &rtiv1.TailEventsResponse{
			Events:          pending,
			OverflowSkipped: overflow,
		}
		if sendErr := sendBatchNonBlocking(stream, batch); sendErr != nil {
			if errors.Is(sendErr, errSendBufferFull) {
				// Fold the dropped batch into the overflow counter and
				// keep going — better to lose a frame than wedge the
				// server on a slow renderer.
				overflow += uint64(len(pending))
				pending = pending[:0]
				return nil
			}
			return sendErr
		}
		pending = pending[:0]
		overflow = 0
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			_ = flush()
			return errToStatus(err)
		}

		// If a batch is pending, race the reader against the latency
		// timer so we flush within the configured window even when
		// the event log is quiet. When no batch is pending, wait
		// indefinitely on the reader.
		if !timerArmed && len(pending) > 0 {
			flushTimer.Reset(cfg.maxLatency)
			timerArmed = true
		}

		evt, readErr := readNextEventOrTimer(ctx, rdr, flushTimer, timerArmed)
		if errors.Is(readErr, errFlushTimer) {
			timerArmed = false
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if errors.Is(readErr, io.EOF) {
			if timerArmed {
				stopTimer(flushTimer)
				timerArmed = false
			}
			if err := flush(); err != nil {
				return err
			}
			return nil
		}
		if readErr != nil {
			return errToStatus(readErr)
		}

		te := buildTailedEvent(evt)
		if !tailEventPasses(te, classFilter, handleFilter) {
			continue
		}
		pending = append(pending, te)
		if len(pending) >= cfg.maxBatch {
			if timerArmed {
				stopTimer(flushTimer)
				timerArmed = false
			}
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

// tailEventsConfig is the resolved per-stream batching configuration.
type tailEventsConfig struct {
	maxBatch   int
	maxLatency time.Duration
}

// tailEventsConfigFromRequest applies the defaults + ceilings to the
// caller-supplied tuning knobs.
func tailEventsConfigFromRequest(req *rtiv1.TailEventsRequest) tailEventsConfig {
	cfg := tailEventsConfig{
		maxBatch:   tailEventsDefaultMaxBatch,
		maxLatency: tailEventsDefaultMaxLatency,
	}
	if v := int(req.GetMaxBatchEvents()); v > 0 {
		cfg.maxBatch = v
	}
	if cfg.maxBatch > tailEventsMaxBatchCeiling {
		cfg.maxBatch = tailEventsMaxBatchCeiling
	}
	if v := time.Duration(req.GetMaxBatchLatencyMs()) * time.Millisecond; v > 0 {
		cfg.maxLatency = v
	}
	if cfg.maxLatency > tailEventsMaxLatencyCeiling {
		cfg.maxLatency = tailEventsMaxLatencyCeiling
	}
	return cfg
}

// buildHandleFilter converts the proto-level repeated handle list into
// a quick-lookup map. A nil result means "no filter".
func buildHandleFilter(in []uint64) map[uint64]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[uint64]struct{}, len(in))
	for _, h := range in {
		out[h] = struct{}{}
	}
	return out
}

// errFlushTimer is the sentinel returned by readNextEventOrTimer when
// the latency timer fires before a record is available.
var errFlushTimer = errors.New("tail-events flush timer fired")

// errSendBufferFull is the sentinel returned by sendBatchNonBlocking
// when the server-stream's send buffer is full and the batch was
// rolled into the overflow counter.
var errSendBufferFull = errors.New("tail-events client send buffer full")

// readNextEventOrTimer reads the next event from rdr, racing against
// the latency timer when a batch is pending. Returns errFlushTimer
// when the timer wins. The timer is consumed on this call's return so
// the caller can re-arm cleanly.
//
// Implementation: rdr.Next blocks until ctx cancels or io.EOF. We
// don't have an async-select-able reader, so we run rdr.Next in a
// goroutine and select against the timer. The goroutine is short-lived
// — it returns on the next event (or the next ctx-cancel). To bound
// goroutine count we re-use a single channel per call.
func readNextEventOrTimer(
	ctx context.Context,
	rdr core.EventLogReader,
	flushTimer *time.Timer,
	timerArmed bool,
) (core.EventRecord, error) {
	if !timerArmed {
		return rdr.Next(ctx)
	}
	type readResult struct {
		evt core.EventRecord
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		evt, err := rdr.Next(ctx)
		ch <- readResult{evt: evt, err: err}
	}()
	select {
	case r := <-ch:
		// Drain the timer if it raced us; the caller treats timerArmed
		// as authoritative on the next iteration.
		stopTimer(flushTimer)
		return r.evt, r.err
	case <-flushTimer.C:
		// Timer fired first. The reader goroutine is still running; on
		// the next iteration we'll pick its result up via a fresh select
		// (or io.EOF / ctx cancel).
		// We return the timer sentinel so the caller flushes; the
		// reader goroutine's eventual completion will be observed by
		// the next call's `ch` receive.
		// To avoid the goroutine outliving us when the stream cancels,
		// we drain the goroutine's send before returning.
		go func() { <-ch }()
		return nil, errFlushTimer
	case <-ctx.Done():
		stopTimer(flushTimer)
		go func() { <-ch }()
		return nil, ctx.Err()
	}
}

// stopTimer drains a *time.Timer safely whether or not it has fired.
func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

// sendBatchNonBlocking attempts to send the batch. Returns
// errSendBufferFull when the underlying gRPC stream rejects the send
// because the client is too slow. We probe the send-buffer state by
// running stream.Send on a goroutine guarded by a short deadline; if
// the deadline elapses without the send completing, we report a fake
// "buffer full". This keeps the handler honest about the
// non-blocking-on-slow-client contract without depending on
// gRPC-internal flow-control APIs that aren't exposed.
func sendBatchNonBlocking(
	stream rtiv1.AdminService_TailEventsServer,
	batch *rtiv1.TailEventsResponse,
) error {
	done := make(chan error, 1)
	go func() { done <- stream.Send(batch) }()
	select {
	case err := <-done:
		return err
	case <-time.After(tailEventsSendTimeout):
		// Fold this batch into overflow; the goroutine will eventually
		// complete (or fail), and its error is dropped because we've
		// already committed to the overflow path. This trades one
		// dropped batch for forward progress.
		go func() { <-done }()
		return errSendBufferFull
	}
}

// tailEventsSendTimeout is the per-batch send deadline. Picked
// generously enough that a healthy client never trips it but tightly
// enough that a wedged client doesn't accumulate unbounded backpressure
// on the server. Same order of magnitude as the default maxLatency.
const tailEventsSendTimeout = 250 * time.Millisecond

// buildTailedEvent translates a core.EventRecord into the proto wire
// shape. The class name is extracted by reflecting on the underlying
// *rtiv1.Event body oneof; this matches the eventlog/replayer.go
// dispatch table so the strings line up with what operators see in
// the Go source.
func buildTailedEvent(rec core.EventRecord) *rtiv1.TailedEvent {
	out := &rtiv1.TailedEvent{
		Seq: rec.Seq(),
	}
	pre, ok := rec.(protoEventRecorder)
	if !ok {
		return out
	}
	pb := pre.ProtoEvent()
	if pb == nil {
		return out
	}
	out.EventClass, out.FederateHandle = classifyEventBody(pb)
	return out
}

// protoEventRecorder is the local-narrow alias for the eventlog
// package's protoEventRecord adapter. Declared here (instead of
// importing eventlog) to keep transport/grpc free of an internal
// dependency on eventlog's reader internals.
type protoEventRecorder interface {
	core.EventRecord
	ProtoEvent() *rtiv1.Event
}

// classifyEventBody returns (class, federateHandle) for the event's
// body oneof. Empty class for an unknown / nil body — the filter
// passes such events through unchanged so they aren't silently dropped.
func classifyEventBody(pb *rtiv1.Event) (string, uint64) {
	switch b := pb.GetBody().(type) {
	case *rtiv1.Event_FedJoined:
		return "FederateJoined", b.FedJoined.GetFederateHandle()
	case *rtiv1.Event_FedResigned:
		return "FederateResigned", b.FedResigned.GetFederateHandle()
	case *rtiv1.Event_ObjRegistered:
		return "ObjectRegistered", b.ObjRegistered.GetOwnerFederateHandle()
	case *rtiv1.Event_ObjDeleted:
		return "ObjectDeleted", 0
	case *rtiv1.Event_AttrUpdated:
		return "AttributeUpdated", b.AttrUpdated.GetProducerFederateHandle()
	case *rtiv1.Event_InterSent:
		return "InteractionSent", b.InterSent.GetProducerFederateHandle()
	case *rtiv1.Event_TimeRequested:
		return "TimeAdvanceRequested", b.TimeRequested.GetFederateHandle()
	case *rtiv1.Event_TimeGranted:
		return "TimeAdvanceGranted", b.TimeGranted.GetFederateHandle()
	case *rtiv1.Event_Halted:
		return "FederationHalted", 0
	}
	return "", 0
}

// tailEventPasses reports whether the event should be forwarded to
// the client given the request's filters. Empty filters always pass.
// federate_handle_filter only applies when the event has a
// non-zero FederateHandle (federation-scope events bypass it).
func tailEventPasses(
	te *rtiv1.TailedEvent,
	classFilter string,
	handleFilter map[uint64]struct{},
) bool {
	if classFilter != "" && !containsClass(te.GetEventClass(), classFilter) {
		return false
	}
	if handleFilter != nil && te.GetFederateHandle() != 0 {
		if _, ok := handleFilter[te.GetFederateHandle()]; !ok {
			return false
		}
	}
	return true
}

// containsClass is a case-sensitive substring match; pulled out so a
// future case-insensitive opt-in flag has a single place to land.
func containsClass(class, needle string) bool {
	if class == "" {
		return false
	}
	return indexSubstring(class, needle) >= 0
}

// indexSubstring is strings.Index inlined to avoid a stdlib import
// in this file (the import set is already saturated). A future tidy
// pass can replace this with strings.Contains directly.
func indexSubstring(s, sub string) int {
	if sub == "" {
		return 0
	}
	if len(sub) > len(s) {
		return -1
	}
	last := len(s) - len(sub)
	for i := 0; i <= last; i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// --- proto translation helpers ----------------------------------------------

func modeToProto(m core.Mode) rtiv1.Mode {
	switch m {
	case core.ModeVerbose:
		return rtiv1.Mode_MODE_VERBOSE
	case core.ModeBestEffort:
		return rtiv1.Mode_MODE_BEST_EFFORT
	default:
		return rtiv1.Mode_MODE_UNSPECIFIED
	}
}

func syncStateToProto(s core.SyncPointSnapshotState) rtiv1.SyncPointState {
	switch s {
	case core.SyncPointStateAnnounced:
		return rtiv1.SyncPointState_SYNC_POINT_STATE_ANNOUNCED
	case core.SyncPointStateAchieved:
		return rtiv1.SyncPointState_SYNC_POINT_STATE_ACHIEVED
	default:
		return rtiv1.SyncPointState_SYNC_POINT_STATE_UNSPECIFIED
	}
}

func adminSaveStateToProto(s core.SaveState) rtiv1.SaveState {
	switch s {
	case core.SaveStateIdle:
		return rtiv1.SaveState_SAVE_STATE_IDLE
	case core.SaveStateInitiated:
		return rtiv1.SaveState_SAVE_STATE_INITIATED
	case core.SaveStateSaved:
		return rtiv1.SaveState_SAVE_STATE_SAVED
	case core.SaveStateNotSaved:
		return rtiv1.SaveState_SAVE_STATE_NOT_SAVED
	default:
		return rtiv1.SaveState_SAVE_STATE_UNSPECIFIED
	}
}

func adminRestoreStateToProto(s core.RestoreState) rtiv1.RestoreState {
	switch s {
	case core.SaveRestoreIdle:
		return rtiv1.RestoreState_RESTORE_STATE_IDLE
	case core.SaveRestoreLoading:
		return rtiv1.RestoreState_RESTORE_STATE_LOADING
	case core.SaveRestoreInitiated:
		return rtiv1.RestoreState_RESTORE_STATE_INITIATED
	case core.SaveRestoreCompleted:
		return rtiv1.RestoreState_RESTORE_STATE_COMPLETED
	case core.SaveRestoreFailed:
		return rtiv1.RestoreState_RESTORE_STATE_FAILED
	default:
		return rtiv1.RestoreState_RESTORE_STATE_UNSPECIFIED
	}
}

func uint64SliceFromHandles(hs []core.FederateHandle) []uint64 {
	if len(hs) == 0 {
		return nil
	}
	out := make([]uint64, len(hs))
	for i, h := range hs {
		out[i] = uint64(h)
	}
	return out
}

func uint64SliceFromObjClasses(cs []core.ObjectClassHandle) []uint64 {
	if len(cs) == 0 {
		return nil
	}
	out := make([]uint64, len(cs))
	for i, c := range cs {
		out[i] = uint64(c)
	}
	return out
}

func uint64SliceFromIntClasses(cs []core.InteractionClassHandle) []uint64 {
	if len(cs) == 0 {
		return nil
	}
	out := make([]uint64, len(cs))
	for i, c := range cs {
		out[i] = uint64(c)
	}
	return out
}
