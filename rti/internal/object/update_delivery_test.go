package object

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
)

// Make the package's general recording outbox model the production admission
// contract for update tests. Other object tests keep their existing observable
// Send/Records behavior while multi-recipient updates can reserve atomically.
func (o *recordingOutbox) Reserve(_ context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	return &recordingUpdateReservation{
		outbox: o, fed: fed, deliveries: append([]core.OutboxDelivery(nil), deliveries...),
	}, nil
}

type recordingUpdateReservation struct {
	outbox     *recordingOutbox
	fed        core.FederationName
	deliveries []core.OutboxDelivery
	done       bool
}

func (r *recordingUpdateReservation) Commit() error {
	if r.done {
		return nil
	}
	r.outbox.mu.Lock()
	for _, delivery := range r.deliveries {
		r.outbox.sent = append(r.outbox.sent, outboxRecord{
			Federation: r.fed,
			Federate:   delivery.Recipient,
			Event:      delivery.Event,
		})
	}
	r.outbox.mu.Unlock()
	r.done = true
	return nil
}

func (r *recordingUpdateReservation) Release() { r.done = true }

type atomicUpdateOutbox struct {
	reserveErr   error
	commitErr    error
	sendErr      error
	sendCalls    int
	attempts     [][]core.OutboxDelivery
	reservations []*atomicUpdateReservation
	records      []outboxRecord
}

func (o *atomicUpdateOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	o.sendCalls++
	if o.sendErr != nil {
		return o.sendErr
	}
	o.records = append(o.records, outboxRecord{Federation: fed, Federate: h, Event: event})
	return nil
}

func (o *atomicUpdateOutbox) Reserve(_ context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	owned := append([]core.OutboxDelivery(nil), deliveries...)
	o.attempts = append(o.attempts, owned)
	if o.reserveErr != nil {
		return nil, o.reserveErr
	}
	reservation := &atomicUpdateReservation{
		outbox: o, fed: fed, deliveries: owned, commitErr: o.commitErr,
	}
	o.commitErr = nil
	o.reservations = append(o.reservations, reservation)
	return reservation, nil
}

type atomicUpdateReservation struct {
	outbox     *atomicUpdateOutbox
	fed        core.FederationName
	deliveries []core.OutboxDelivery
	commitErr  error
	committed  bool
	released   bool
}

func (r *atomicUpdateReservation) Commit() error {
	if r.committed || r.released {
		return nil
	}
	if r.commitErr != nil {
		return r.commitErr
	}
	for _, delivery := range r.deliveries {
		r.outbox.records = append(r.outbox.records, outboxRecord{
			Federation: r.fed,
			Federate:   delivery.Recipient,
			Event:      delivery.Event,
		})
	}
	r.committed = true
	return nil
}

func (r *atomicUpdateReservation) Release() { r.released = true }

type updateTSOGate struct {
	deliverNow    bool
	deliverNowFor map[core.FederateHandle]bool
	bufferErr     error
	bufferCalls   int
	reservations  []*updateTSOReservation
}

func (g *updateTSOGate) ShouldDeliverNow(_ core.FederationName, h core.FederateHandle, _ core.LogicalTime) bool {
	return g.shouldDeliverNow(h)
}

func (g *updateTSOGate) shouldDeliverNow(h core.FederateHandle) bool {
	if g.deliverNowFor != nil {
		return g.deliverNowFor[h]
	}
	return g.deliverNow
}

func (g *updateTSOGate) BufferTSO(context.Context, core.FederationName, core.FederateHandle, core.LogicalTime, core.OutboundEvent) error {
	g.bufferCalls++
	return g.bufferErr
}

func (g *updateTSOGate) BufferTSOWithRetraction(context.Context, core.FederationName, core.FederateHandle, core.LogicalTime, core.OutboundEvent, core.FederateHandle, uint64) error {
	g.bufferCalls++
	return g.bufferErr
}

func (*updateTSOGate) RetractMessage(core.FederationName, core.FederateHandle, uint64) int {
	return 0
}

func (g *updateTSOGate) ReserveTSO(_ core.FederationName, deliveries []core.TSOBufferedDelivery) core.TSOBufferReservation {
	reservation := &updateTSOReservation{gate: g}
	for _, delivery := range deliveries {
		if g.shouldDeliverNow(delivery.Recipient) {
			reservation.immediate = append(reservation.immediate, delivery)
		} else {
			reservation.buffered = append(reservation.buffered, delivery)
		}
	}
	g.reservations = append(g.reservations, reservation)
	return reservation
}

type updateTSOReservation struct {
	gate      *updateTSOGate
	immediate []core.TSOBufferedDelivery
	buffered  []core.TSOBufferedDelivery
	committed bool
	released  bool
}

func (r *updateTSOReservation) Immediate() []core.TSOBufferedDelivery {
	return append([]core.TSOBufferedDelivery(nil), r.immediate...)
}

func (r *updateTSOReservation) Buffered() []core.TSOBufferedDelivery {
	return append([]core.TSOBufferedDelivery(nil), r.buffered...)
}

func (r *updateTSOReservation) Commit(context.Context) { r.committed = true }
func (r *updateTSOReservation) Release()               { r.released = true }

type legacyUpdateTSOGate struct {
	bufferErr   error
	bufferCalls int
}

func (*legacyUpdateTSOGate) ShouldDeliverNow(core.FederationName, core.FederateHandle, core.LogicalTime) bool {
	return false
}

func (g *legacyUpdateTSOGate) BufferTSO(context.Context, core.FederationName, core.FederateHandle, core.LogicalTime, core.OutboundEvent) error {
	g.bufferCalls++
	return g.bufferErr
}

func (g *legacyUpdateTSOGate) BufferTSOWithRetraction(context.Context, core.FederationName, core.FederateHandle, core.LogicalTime, core.OutboundEvent, core.FederateHandle, uint64) error {
	g.bufferCalls++
	return g.bufferErr
}

func (*legacyUpdateTSOGate) RetractMessage(core.FederationName, core.FederateHandle, uint64) int {
	return 0
}

type updateDDMFilter struct {
	subs map[core.AttributeHandle][]core.FederateHandle
}

func (*updateDDMFilter) HasObjectAssociations(core.FederationName, core.ObjectHandle) bool {
	return true
}

func (*updateDDMFilter) PublisherRegionsFor(core.FederationName, core.ObjectHandle, core.AttributeHandle) []DDMRegionHandle {
	return []DDMRegionHandle{1}
}

func (d *updateDDMFilter) SubscribersForUpdate(_ core.FederationName, _ core.ObjectClassHandle, attr core.AttributeHandle, _ []DDMRegionHandle) []core.FederateHandle {
	return append([]core.FederateHandle(nil), d.subs[attr]...)
}

func (*updateDDMFilter) RegionSubscribersFor(core.FederationName, core.ObjectClassHandle, core.AttributeHandle) []core.FederateHandle {
	return nil
}

type rejectingUpdateOutbox struct {
	err        error
	sendCalls  int
	deliveries []core.OutboxDelivery
}

func (o *rejectingUpdateOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	o.sendCalls++
	return nil
}

func (o *rejectingUpdateOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	o.deliveries = append([]core.OutboxDelivery(nil), deliveries...)
	return nil, o.err
}

type failingUpdateOutbox struct {
	err       error
	sendCalls int
}

func (o *failingUpdateOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	o.sendCalls++
	return o.err
}

func newUpdateDeliveryRegistry(t *testing.T, outbox core.Outbox) (*Registry, *declaration.Manager, *recordingEventLog) {
	t.Helper()
	decl := declaration.New()
	log := &recordingEventLog{}
	reg, err := New(Options{
		EventLog:     log,
		Declarations: decl,
		Outbox:       outbox,
		FOMs:         &stubFOMRepo{},
		Clock:        core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg, decl, log
}

func registerUpdateProducer(t *testing.T, reg *Registry, decl *declaration.Manager) core.ObjectHandle {
	t.Helper()
	ctx := context.Background()
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := reg.Register(ctx, "fed", 1, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func TestUpdateAttributesReservationRejectionHasNoPartialDelivery(t *testing.T) {
	outbox := &rejectingUpdateOutbox{err: core.ErrFederateOverflow}
	reg, decl, log := newUpdateDeliveryRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	for _, subscriber := range []core.FederateHandle{3, 2} {
		if err := decl.SubscribeObjectClassAttributes(ctx, "fed", subscriber, 7, []core.AttributeHandle{2}); err != nil {
			t.Fatal(err)
		}
	}
	delivered, sent := 0, 0
	reg.opts.OnReflectDelivered = func(core.FederationName, core.FederateHandle) { delivered++ }
	reg.opts.OnUpdateSent = func(core.FederationName, core.FederateHandle) { sent++ }
	logBefore := len(log.Records())

	err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil)
	if !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("UpdateAttributes = %v, want ErrFederateOverflow", err)
	}
	if outbox.sendCalls != 0 {
		t.Fatalf("Send calls = %d, want 0 after atomic reservation rejection", outbox.sendCalls)
	}
	if len(outbox.deliveries) != 2 {
		t.Fatalf("reserved deliveries = %d, want 2", len(outbox.deliveries))
	}
	for i, want := range []core.FederateHandle{2, 3} {
		if got := outbox.deliveries[i].Recipient; got != want {
			t.Fatalf("reserved recipient[%d] = %d, want %d", i, got, want)
		}
	}
	if delivered != 0 || sent != 0 {
		t.Fatalf("callbacks = delivered %d sent %d, want 0/0", delivered, sent)
	}
	if got := len(log.Records()); got != logBefore {
		t.Fatalf("event-log records = %d, want unchanged %d after reservation rejection", got, logBefore)
	}
}

func TestUpdateAttributesPropagatesOutboxSendFailure(t *testing.T) {
	outbox := &failingUpdateOutbox{err: core.ErrOutboxUnavailable}
	reg, decl, log := newUpdateDeliveryRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	delivered, sent := 0, 0
	reg.opts.OnReflectDelivered = func(core.FederationName, core.FederateHandle) { delivered++ }
	reg.opts.OnUpdateSent = func(core.FederationName, core.FederateHandle) { sent++ }

	err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil)
	if !errors.Is(err, core.ErrOutboxUnavailable) {
		t.Fatalf("UpdateAttributes = %v, want ErrOutboxUnavailable", err)
	}
	if outbox.sendCalls != 1 {
		t.Fatalf("Send calls = %d, want 1", outbox.sendCalls)
	}
	if got := len(log.Records()); got != 2 {
		t.Fatalf("event-log records = %d, want registration plus committed update", got)
	}
	if delivered != 0 || sent != 0 {
		t.Fatalf("callbacks = delivered %d sent %d, want 0/0", delivered, sent)
	}
}

func TestUpdateAttributesRejectsUnsafeNonReservableMultiRecipientFanout(t *testing.T) {
	outbox := &failingUpdateOutbox{}
	reg, decl, log := newUpdateDeliveryRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	for _, subscriber := range []core.FederateHandle{2, 3} {
		if err := decl.SubscribeObjectClassAttributes(ctx, "fed", subscriber, 7, []core.AttributeHandle{2}); err != nil {
			t.Fatal(err)
		}
	}
	logBefore := len(log.Records())

	err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil)
	if err == nil || !strings.Contains(err.Error(), "requires a reservable outbox") {
		t.Fatalf("UpdateAttributes = %v, want explicit reservable-outbox error", err)
	}
	if outbox.sendCalls != 0 {
		t.Fatalf("Send calls = %d, want 0", outbox.sendCalls)
	}
	if got := len(log.Records()); got != logBefore {
		t.Fatalf("event-log records = %d, want unchanged %d", got, logBefore)
	}
}

func TestUpdateAttributesTSOReservationFailureReleasesGateAndSkipsEventLog(t *testing.T) {
	outbox := &atomicUpdateOutbox{reserveErr: core.ErrFederateOverflow}
	gate := &updateTSOGate{deliverNow: true}
	reg, decl, log := newUpdateDeliveryRegistry(t, outbox)
	reg.opts.TSOGate = gate
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	for _, subscriber := range []core.FederateHandle{3, 2} {
		if err := decl.SubscribeObjectClassAttributes(ctx, "fed", subscriber, 7, []core.AttributeHandle{2}); err != nil {
			t.Fatal(err)
		}
	}
	logBefore := len(log.Records())
	ts := core.LogicalTime(5)

	err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, &ts)
	if !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("UpdateAttributes = %v, want ErrFederateOverflow", err)
	}
	if len(gate.reservations) != 1 || !gate.reservations[0].released || gate.reservations[0].committed {
		t.Fatalf("TSO reservation after rejection = %+v, want released and uncommitted", gate.reservations)
	}
	if got := len(log.Records()); got != logBefore {
		t.Fatalf("event-log records = %d, want unchanged %d", got, logBefore)
	}
	if len(outbox.records) != 0 || outbox.sendCalls != 0 {
		t.Fatalf("outbox side effects = records %d Send %d, want 0/0", len(outbox.records), outbox.sendCalls)
	}

	outbox.reserveErr = nil
	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, &ts); err != nil {
		t.Fatalf("retry UpdateAttributes: %v", err)
	}
	if len(gate.reservations) != 2 || !gate.reservations[1].committed || gate.reservations[1].released {
		t.Fatalf("retry TSO reservation = %+v, want committed", gate.reservations)
	}
	if len(outbox.records) != 2 {
		t.Fatalf("retry deliveries = %d, want 2", len(outbox.records))
	}
	for i, want := range []core.FederateHandle{2, 3} {
		if got := outbox.records[i].Federate; got != want {
			t.Fatalf("retry recipient[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestUpdateAttributesMixedTSOReservationFailureRollsBackGateWALAndScope(t *testing.T) {
	outbox := &atomicUpdateOutbox{reserveErr: core.ErrFederateOverflow}
	gate := &updateTSOGate{deliverNowFor: map[core.FederateHandle]bool{
		2: true,
		3: false,
	}}
	ddm := &updateDDMFilter{subs: map[core.AttributeHandle][]core.FederateHandle{
		2: {2, 3},
	}}
	reg, decl, log := newUpdateDeliveryRegistry(t, outbox)
	reg.opts.TSOGate = gate
	obj := registerUpdateProducer(t, reg, decl)
	reg.opts.DDM = ddm
	ctx := context.Background()
	logBefore := len(log.Records())
	ts := core.LogicalTime(5)
	payload := map[core.AttributeHandle][]byte{2: {0xAB}}

	err := reg.UpdateAttributes(ctx, "fed", 1, obj, payload, &ts)
	if !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("UpdateAttributes = %v, want ErrFederateOverflow", err)
	}
	if len(gate.reservations) != 1 {
		t.Fatalf("TSO reservations = %d, want 1", len(gate.reservations))
	}
	reservation := gate.reservations[0]
	if len(reservation.immediate) != 1 || reservation.immediate[0].Recipient != 2 {
		t.Fatalf("immediate TSO deliveries = %+v, want recipient 2", reservation.immediate)
	}
	if len(reservation.buffered) != 1 || reservation.buffered[0].Recipient != 3 {
		t.Fatalf("buffered TSO deliveries = %+v, want recipient 3", reservation.buffered)
	}
	if !reservation.released || reservation.committed {
		t.Fatalf("failed TSO reservation = %+v, want released and uncommitted", reservation)
	}
	if len(outbox.attempts) != 1 || len(outbox.attempts[0]) != 3 {
		t.Fatalf("outbox admission attempts = %+v, want 2 advisories plus immediate reflect", outbox.attempts)
	}
	if len(outbox.records) != 0 || outbox.sendCalls != 0 {
		t.Fatalf("outbox side effects = records %d Send %d, want 0/0", len(outbox.records), outbox.sendCalls)
	}
	if got := len(log.Records()); got != logBefore {
		t.Fatalf("event-log records = %d, want unchanged %d", got, logBefore)
	}
	st := reg.stateFor("fed")
	st.mu.Lock()
	for _, subscriber := range []core.FederateHandle{2, 3} {
		if _, applied := st.scope[obj][subscriber][2]; applied {
			st.mu.Unlock()
			t.Fatalf("scope cache applied recipient %d after failed reservation", subscriber)
		}
	}
	st.mu.Unlock()

	outbox.reserveErr = nil
	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, payload, &ts); err != nil {
		t.Fatalf("retry UpdateAttributes: %v", err)
	}
	if len(gate.reservations) != 2 || !gate.reservations[1].committed || gate.reservations[1].released {
		t.Fatalf("retry TSO reservation = %+v, want committed", gate.reservations)
	}
	if len(outbox.records) != 3 {
		t.Fatalf("retry immediate deliveries = %d, want 3", len(outbox.records))
	}
	if got := len(log.Records()); got != logBefore+1 {
		t.Fatalf("event-log records after retry = %d, want %d", got, logBefore+1)
	}
	st.mu.Lock()
	for _, subscriber := range []core.FederateHandle{2, 3} {
		if _, applied := st.scope[obj][subscriber][2]; !applied {
			st.mu.Unlock()
			t.Fatalf("scope cache missing recipient %d after successful retry", subscriber)
		}
	}
	st.mu.Unlock()
}

func TestUpdateAttributesEventLogFailureAbortsOutboxAndTSOReservations(t *testing.T) {
	appendErr := errors.New("append failed")
	outbox := &atomicUpdateOutbox{}
	gate := &updateTSOGate{deliverNow: true}
	reg, decl, log := newUpdateDeliveryRegistry(t, outbox)
	reg.opts.TSOGate = gate
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	logBefore := len(log.Records())
	log.failNext = appendErr
	ts := core.LogicalTime(5)

	err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, &ts)
	if !errors.Is(err, appendErr) {
		t.Fatalf("UpdateAttributes = %v, want append failure", err)
	}
	if len(outbox.reservations) != 1 || !outbox.reservations[0].released || outbox.reservations[0].committed {
		t.Fatalf("outbox reservation = %+v, want released and uncommitted", outbox.reservations)
	}
	if len(gate.reservations) != 1 || !gate.reservations[0].released || gate.reservations[0].committed {
		t.Fatalf("TSO reservation = %+v, want released and uncommitted", gate.reservations)
	}
	if len(outbox.records) != 0 || outbox.sendCalls != 0 {
		t.Fatalf("outbox side effects = records %d Send %d, want 0/0", len(outbox.records), outbox.sendCalls)
	}
	if got := len(log.Records()); got != logBefore {
		t.Fatalf("event-log records = %d, want unchanged %d", got, logBefore)
	}
}

func TestUpdateAttributesTSOBufferedRecipientsCommitAsOneGateReservation(t *testing.T) {
	outbox := &atomicUpdateOutbox{}
	gate := &updateTSOGate{deliverNow: false}
	reg, decl, log := newUpdateDeliveryRegistry(t, outbox)
	reg.opts.TSOGate = gate
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	for _, subscriber := range []core.FederateHandle{3, 2} {
		if err := decl.SubscribeObjectClassAttributes(ctx, "fed", subscriber, 7, []core.AttributeHandle{2}); err != nil {
			t.Fatal(err)
		}
	}
	delivered := 0
	reg.opts.OnReflectDelivered = func(core.FederationName, core.FederateHandle) { delivered++ }
	logBefore := len(log.Records())
	ts := core.LogicalTime(5)

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, &ts); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}
	if len(gate.reservations) != 1 {
		t.Fatalf("TSO reservations = %d, want 1", len(gate.reservations))
	}
	reservation := gate.reservations[0]
	if !reservation.committed || reservation.released || len(reservation.buffered) != 2 {
		t.Fatalf("TSO reservation = %+v, want committed with 2 buffered recipients", reservation)
	}
	if len(outbox.attempts) != 0 || len(outbox.records) != 0 || outbox.sendCalls != 0 {
		t.Fatalf("immediate outbox side effects = attempts %d records %d Send %d, want 0/0/0", len(outbox.attempts), len(outbox.records), outbox.sendCalls)
	}
	if got := len(log.Records()); got != logBefore+1 {
		t.Fatalf("event-log records = %d, want %d", got, logBefore+1)
	}
	if delivered != 2 {
		t.Fatalf("OnReflectDelivered calls = %d, want 2", delivered)
	}
}

func TestUpdateAttributesPropagatesLegacyTSOBufferFailure(t *testing.T) {
	bufferErr := errors.New("buffer failed")
	outbox := &failingUpdateOutbox{}
	gate := &legacyUpdateTSOGate{bufferErr: bufferErr}
	reg, decl, log := newUpdateDeliveryRegistry(t, outbox)
	reg.opts.TSOGate = gate
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	delivered := 0
	reg.opts.OnReflectDelivered = func(core.FederationName, core.FederateHandle) { delivered++ }
	logBefore := len(log.Records())
	ts := core.LogicalTime(5)

	err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, &ts)
	if !errors.Is(err, bufferErr) {
		t.Fatalf("UpdateAttributes = %v, want buffer failure", err)
	}
	if gate.bufferCalls != 1 || outbox.sendCalls != 0 {
		t.Fatalf("delivery calls = Buffer %d Send %d, want 1/0", gate.bufferCalls, outbox.sendCalls)
	}
	if got := len(log.Records()); got != logBefore+1 {
		t.Fatalf("event-log records = %d, want committed update %d", got, logBefore+1)
	}
	if delivered != 0 {
		t.Fatalf("OnReflectDelivered calls = %d, want 0", delivered)
	}
}

func TestUpdateAttributesDDMAdvisoryFailureLeavesScopeForRetry(t *testing.T) {
	advisoryErr := errors.New("advisory commit failed")
	outbox := &atomicUpdateOutbox{commitErr: advisoryErr}
	ddm := &updateDDMFilter{subs: map[core.AttributeHandle][]core.FederateHandle{
		2: {2},
	}}
	reg, decl, _ := newUpdateDeliveryRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	reg.opts.DDM = ddm
	ctx := context.Background()
	payload := map[core.AttributeHandle][]byte{2: {0xAB}}

	err := reg.UpdateAttributes(ctx, "fed", 1, obj, payload, nil)
	if !errors.Is(err, advisoryErr) {
		t.Fatalf("UpdateAttributes = %v, want advisory commit failure", err)
	}
	if len(outbox.reservations) != 1 || !outbox.reservations[0].released || outbox.reservations[0].committed {
		t.Fatalf("failed advisory reservation = %+v, want released and uncommitted", outbox.reservations)
	}
	if len(outbox.records) != 0 {
		t.Fatalf("failed advisory deliveries = %d, want 0", len(outbox.records))
	}
	st := reg.stateFor("fed")
	st.mu.Lock()
	scopeApplied := false
	if st.scope != nil {
		_, scopeApplied = st.scope[obj][2][2]
	}
	st.mu.Unlock()
	if scopeApplied {
		t.Fatal("scope cache advanced after failed advisory delivery")
	}

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, payload, nil); err != nil {
		t.Fatalf("retry UpdateAttributes: %v", err)
	}
	if len(outbox.records) != 2 {
		t.Fatalf("retry deliveries = %d, want in-scope plus reflect", len(outbox.records))
	}
	if got := outbox.records[0].Event.(*outboundEvent).Inner().GetAttributesInScope(); got == nil {
		t.Fatalf("retry event[0] = %T, want AttributesInScope", outbox.records[0].Event)
	}
	if got := outbox.records[1].Event.(*outboundEvent).Inner().GetReflect(); got == nil {
		t.Fatalf("retry event[1] = %T, want Reflect", outbox.records[1].Event)
	}

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, payload, nil); err != nil {
		t.Fatalf("steady-state UpdateAttributes: %v", err)
	}
	if len(outbox.records) != 3 || outbox.records[2].Event.(*outboundEvent).Inner().GetReflect() == nil {
		t.Fatalf("steady-state deliveries = %+v, want one additional Reflect", outbox.records)
	}

	outOfScopeErr := errors.New("out-of-scope advisory commit failed")
	outbox.commitErr = outOfScopeErr
	ddm.subs[2] = nil
	err = reg.UpdateAttributes(ctx, "fed", 1, obj, payload, nil)
	if !errors.Is(err, outOfScopeErr) {
		t.Fatalf("out-of-scope UpdateAttributes = %v, want advisory commit failure", err)
	}
	if len(outbox.records) != 3 {
		t.Fatalf("failed out-of-scope deliveries = %d, want unchanged 3", len(outbox.records))
	}
	st.mu.Lock()
	_, stillInScope := st.scope[obj][2][2]
	st.mu.Unlock()
	if !stillInScope {
		t.Fatal("scope cache removed attribute after failed out-of-scope advisory")
	}

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, payload, nil); err != nil {
		t.Fatalf("out-of-scope retry UpdateAttributes: %v", err)
	}
	if len(outbox.records) != 4 || outbox.records[3].Event.(*outboundEvent).Inner().GetAttributesOutOfScope() == nil {
		t.Fatalf("out-of-scope retry deliveries = %+v, want one AttributesOutOfScope", outbox.records)
	}
}

func TestUpdateAttributesConcurrentDDMUpdatesSerializeScopeAdvisory(t *testing.T) {
	outbox := &recordingOutbox{}
	ddm := &updateDDMFilter{subs: map[core.AttributeHandle][]core.FederateHandle{
		2: {2},
	}}
	reg, decl, _ := newUpdateDeliveryRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	reg.opts.DDM = ddm
	ctx := context.Background()
	before := len(outbox.Records())
	start := make(chan struct{})
	done := make(chan error, 2)

	for i := 0; i < 2; i++ {
		value := byte(i + 1)
		go func() {
			<-start
			done <- reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {value}}, nil)
		}()
	}
	close(start)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("concurrent UpdateAttributes: %v", err)
			}
		case <-timer.C:
			t.Fatal("concurrent DDM updates deadlocked")
		}
	}

	records := outbox.Records()[before:]
	if len(records) != 3 {
		t.Fatalf("concurrent deliveries = %d, want one InScope plus two Reflect", len(records))
	}
	inScope, outOfScope, reflects := 0, 0, 0
	var previousSeq uint64
	for i, record := range records {
		event := record.Event.(*outboundEvent).Inner()
		if event.GetAttributesInScope() != nil {
			inScope++
		}
		if event.GetAttributesOutOfScope() != nil {
			outOfScope++
		}
		if event.GetReflect() != nil {
			reflects++
		}
		if i > 0 && event.GetSeq() <= previousSeq {
			t.Fatalf("delivery seq[%d] = %d after %d", i, event.GetSeq(), previousSeq)
		}
		previousSeq = event.GetSeq()
	}
	if inScope != 1 || outOfScope != 0 || reflects != 2 {
		t.Fatalf("concurrent event counts = InScope %d OutOfScope %d Reflect %d, want 1/0/2", inScope, outOfScope, reflects)
	}
	if records[0].Event.(*outboundEvent).Inner().GetAttributesInScope() == nil ||
		records[1].Event.(*outboundEvent).Inner().GetReflect() == nil ||
		records[2].Event.(*outboundEvent).Inner().GetReflect() == nil {
		t.Fatalf("concurrent delivery order = %+v, want InScope, Reflect, Reflect", records)
	}
}
