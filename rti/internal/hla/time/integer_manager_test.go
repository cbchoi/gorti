package time

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type integerTestOutbox struct {
	events []core.OutboundEvent
	fail   error
}

func (o *integerTestOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	if o.fail != nil {
		return o.fail
	}
	o.events = append(o.events, event)
	return nil
}

type integerTestMessage struct{ ID int }

func (*integerTestMessage) Seq() uint64 { return 0 }

func integerClone(event core.OutboundEvent) (core.OutboundEvent, error) {
	message, ok := event.(*integerTestMessage)
	if !ok {
		return nil, errors.New("unexpected queued callback")
	}
	eventCopy := *message
	return &eventCopy, nil
}

func integerManagerFixture(t *testing.T, states ...IntegerFederateState) (*IntegerManager, *integerTestOutbox) {
	t.Helper()
	out := &integerTestOutbox{}
	m, err := NewInteger(IntegerOptions{Clock: core.NewRealClock(), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { managerExt.Delete(m) })
	if len(states) != 0 {
		if err := m.ImportState("integer", &IntegerManagerState{StallTimeout: DefaultStallTimeout, Federates: states}, integerClone); err != nil {
			t.Fatal(err)
		}
	}
	return m, out
}

func integerState(h core.FederateHandle, at, lookahead int64, regulating, constrained bool) IntegerFederateState {
	return IntegerFederateState{Handle: h, HasRegulationState: true, HasAdvanceState: true, CurrentTime: at, Lookahead: lookahead, Regulating: regulating, Constrained: constrained}
}

func integerAdvance(ctx context.Context, m *IntegerManager, h core.FederateHandle, target int64, mode AdvanceMode) error {
	switch mode {
	case ModeTAR:
		return m.TimeAdvanceRequest(ctx, "integer", h, target)
	case ModeTARA:
		return m.TimeAdvanceRequestAvailable(ctx, "integer", h, target)
	case ModeNER:
		return m.NextMessageRequest(ctx, "integer", h, target)
	case ModeNMRA:
		return m.NextMessageRequestAvailable(ctx, "integer", h, target)
	case ModeFQR:
		return m.FlushQueueRequest(ctx, "integer", h, target)
	default:
		panic("unsupported test mode")
	}
}

func integerGrants(out *integerTestOutbox) []int64 {
	var grants []int64
	for _, event := range out.events {
		if grant, ok := event.(*IntegerTimeAdvanceGrant); ok {
			grants = append(grants, grant.Time)
		}
	}
	return grants
}

func TestIntegerManagerExactAdvance(t *testing.T) {
	out := &integerTestOutbox{}
	m, err := NewInteger(IntegerOptions{Clock: core.NewRealClock(), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const target int64 = 1<<53 + 1
	if err := m.TimeAdvanceRequest(ctx, "integer", 1, target); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot("integer").Federates[0].CurrentTime; got != target {
		t.Fatalf("time=%d want=%d", got, target)
	}
	grant, ok := out.events[len(out.events)-1].(*TimeAdvanceGrantOf[int64])
	if !ok || grant.Time != target {
		t.Fatalf("grant=%#v", out.events)
	}
}

func TestIntegerManagerAllModesUseExactQueueOrder(t *testing.T) {
	const base int64 = 1<<53 + 1
	for _, mode := range []AdvanceMode{ModeTAR, ModeTARA, ModeNER, ModeNMRA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, out := integerManagerFixture(t, integerState(1, base+10, 2, true, false), integerState(2, base, 0, false, true))
			ctx := context.Background()
			// These adjacent timestamps collapse to the same double. Reverse their
			// arrival order to distinguish exact ordering from a stable rounded sort.
			for _, row := range []struct {
				at int64
				id int
			}{{base + 3, 3}, {base + 2, 2}} {
				if err := m.BufferTSO(ctx, "integer", 2, row.at, &integerTestMessage{row.id}); err != nil {
					t.Fatal(err)
				}
			}
			want := base + 7
			if mode.usesNextMessageTarget() || mode == ModeFQR {
				want = base + 2
			}
			if err := integerAdvance(ctx, m, 2, base+7, mode); err != nil {
				t.Fatal(err)
			}
			if got := integerGrants(out); !reflect.DeepEqual(got, []int64{want}) {
				t.Fatalf("grants=%v want=%d", got, want)
			}
			if got := m.Metrics("integer", 2).CurrentTime; got != want {
				t.Fatalf("current=%d want=%d", got, want)
			}
			var ids []int
			for _, event := range out.events {
				if message, ok := event.(*integerTestMessage); ok {
					ids = append(ids, message.ID)
				}
			}
			wantIDs := []int{2, 3}
			if mode.usesNextMessageTarget() {
				wantIDs = []int{2}
			}
			if !reflect.DeepEqual(ids, wantIDs) {
				t.Fatalf("delivery order=%v want=%v", ids, wantIDs)
			}
		})
	}
}

func TestIntegerManagerStrictAndInclusiveBounds(t *testing.T) {
	const base int64 = 1<<53 + 1
	for _, mode := range []AdvanceMode{ModeTAR, ModeTARA, ModeNER, ModeNMRA} {
		t.Run(mode.String(), func(t *testing.T) {
			m, out := integerManagerFixture(t, integerState(1, base, 1, true, false), integerState(2, base, 0, false, true))
			ctx := context.Background()
			if err := integerAdvance(ctx, m, 2, base+1, mode); err != nil {
				t.Fatal(err)
			}
			if got := len(integerGrants(out)); got != boolCount(mode.inclusiveLBTS()) {
				t.Fatalf("equal bound grants=%d", got)
			}
			if err := m.TimeAdvanceRequestAvailable(ctx, "integer", 1, base+1); err != nil {
				t.Fatal(err)
			}
			if m.Metrics("integer", 2).Advancing || m.Metrics("integer", 2).CurrentTime != base+1 {
				t.Fatal("strict receiver did not resume at exact target")
			}
		})
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestIntegerManagerFinalTimeAndUnboundedRemainDistinct(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeTARA, ModeNER, ModeNMRA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, out := integerManagerFixture(t, integerState(1, math.MaxInt64-1, 1, true, false), integerState(2, math.MaxInt64-1, 0, false, true))
			ctx := context.Background()
			bound, defined := m.QueryGALTBound("integer", 2)
			if !defined || bound.Unbounded || bound.Limit != math.MaxInt64 {
				t.Fatalf("finite final GALT=%+v defined=%t", bound, defined)
			}
			if err := integerAdvance(ctx, m, 2, math.MaxInt64, mode); err != nil {
				t.Fatal(err)
			}
			if got := len(integerGrants(out)); got != boolCount(mode.inclusiveLBTS()) {
				t.Fatalf("finite final grants=%v", integerGrants(out))
			}
			if err := m.DisableRegulation(ctx, "integer", 1); err != nil {
				t.Fatal(err)
			}
			if got := integerGrants(out); !reflect.DeepEqual(got, []int64{math.MaxInt64}) {
				t.Fatalf("unbounded final grants=%v", got)
			}
			if !m.Snapshot("integer").LBTSUnbounded {
				t.Fatal("empty regulator set must be unbounded")
			}
		})
	}
	m, _ := integerManagerFixture(t, integerState(1, math.MaxInt64, 1, true, false), integerState(2, math.MaxInt64, 0, false, true))
	bound, defined := m.QueryGALTBound("integer", 2)
	if !defined || !bound.Unbounded {
		t.Fatalf("overflow horizon=%+v defined=%t", bound, defined)
	}
	if err := m.ValidateOutgoingTSO("integer", 1, math.MaxInt64); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("overflow send=%v", err)
	}
	if err := m.TimeAdvanceRequest(context.Background(), "integer", 2, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if m.Metrics("integer", 2).Advancing {
		t.Fatal("overflow horizon incorrectly blocks final strict advance")
	}
}

func TestIntegerManagerExactLookaheadAndOutgoingAdmission(t *testing.T) {
	const base int64 = 1<<53 + 1
	m, _ := integerManagerFixture(t, integerState(1, base, 4, true, false))
	ctx := context.Background()
	if err := m.ModifyLookahead(ctx, "integer", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequestAvailable(ctx, "integer", 1, base+1); err != nil {
		t.Fatal(err)
	}
	if got := *m.Metrics("integer", 1).Lookahead; got != 3 {
		t.Fatalf("lookahead=%d", got)
	}
	for _, at := range []int64{base + 3, base + 4} {
		err := m.ValidateOutgoingTSO("integer", 1, at)
		if (err == nil) != (at == base+4) {
			t.Fatalf("at=%d error=%v", at, err)
		}
	}
	if err := m.TimeAdvanceRequest(ctx, "integer", 1, base+4); err != nil {
		t.Fatal(err)
	}
	if got := *m.Metrics("integer", 1).Lookahead; got != 1 {
		t.Fatalf("settled lookahead=%d", got)
	}
	if err := m.ModifyLookahead(ctx, "integer", 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "integer", 1, base+5); err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateOutgoingTSO("integer", 1, base+5); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("strict equality admitted: %v", err)
	}
	if err := m.ValidateOutgoingTSO("integer", 1, base+6); err != nil {
		t.Fatal(err)
	}
}

func TestIntegerManagerRetractionAndPersistentDTOExact(t *testing.T) {
	const base int64 = 1<<53 + 1
	m, out := integerManagerFixture(t, integerState(1, base, 1, true, false), integerState(2, base, 0, false, true))
	ctx := context.Background()
	for _, row := range []struct {
		id uint64
		at int64
	}{{1, base + 1}, {2, base + 2}} {
		r, err := m.ReserveRetractionMessage(ctx, "integer", 1, row.id, row.at)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := m.BufferTSOWithRetraction(ctx, "integer", 2, row.at, &integerTestMessage{int(row.id)}, 1, row.id); err != nil {
			t.Fatal(err)
		}
	}
	history, err := m.ExportRetractionHistory("integer")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	var decoded IntegerRetractionHistory
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(history, &decoded) {
		t.Fatalf("history rounded: %s", encoded)
	}
	state, err := m.ExportState("integer", integerClone)
	if err != nil {
		t.Fatal(err)
	}
	if state.Federates[1].Queue[0].Timestamp != base+1 || state.Federates[1].Queue[1].Timestamp != base+2 {
		t.Fatal("queue snapshot rounded")
	}
	if err := m.RetractWithError(ctx, "integer", 1, 1); !errors.Is(err, core.ErrRetractionTooLate) {
		t.Fatalf("equal floor retraction=%v", err)
	}
	if err := m.RetractWithError(ctx, "integer", 1, 2); err != nil {
		t.Fatal(err)
	}
	if got := m.Metrics("integer", 2).TSOQueued; got != 1 {
		t.Fatalf("queue after retract=%d", got)
	}
	if len(out.events) != 0 {
		t.Fatal("buffered-only retraction fabricated callback")
	}
	commit, err := m.PrepareImportState("integer", state, integerClone)
	if err != nil {
		t.Fatal(err)
	}
	state.Federates[1].Queue[0].Timestamp = 0
	commit()
	r, err := m.PrepareRetractionHistory("integer", &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := m.Metrics("integer", 2).TSOQueued; got != 2 {
		t.Fatalf("restored queue=%d", got)
	}
	if err := m.RetractWithError(ctx, "integer", 1, 2); err != nil {
		t.Fatal(err)
	}
}

func TestIntegerManagerScheduledSaveHoldsExactCrossingRequest(t *testing.T) {
	const base int64 = 1<<53 + 1
	m, out := integerManagerFixture(t, integerState(1, base+10, 1, true, false), integerState(2, base, 0, false, true))
	ctx := context.Background()
	ready, release, err := m.ReserveScheduledSave("integer", base+2, []core.FederateHandle{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := m.BufferTSO(ctx, "integer", 2, base+2, &integerTestMessage{2}); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "integer", 2, base+2); err != nil {
		t.Fatal(err)
	}
	if len(integerGrants(out)) != 0 || !m.Metrics("integer", 2).Advancing {
		t.Fatal("save fabricated intermediate grant")
	}
	if ok, err := ready(); err != nil || !ok {
		t.Fatalf("save readiness=%t error=%v", ok, err)
	}
	if len(out.events) != 1 {
		t.Fatalf("save data callbacks=%#v", out.events)
	}
	state, err := m.ExportState("integer", integerClone)
	if err != nil {
		t.Fatal(err)
	}
	if state.Federates[1].CurrentTime != base || state.Federates[1].RequestedTime != base+2 {
		t.Fatal("save crossing state rounded")
	}
	release()
	if err := m.ResumeScheduledSave(ctx, "integer"); err != nil {
		t.Fatal(err)
	}
	if got := integerGrants(out); !reflect.DeepEqual(got, []int64{base + 2}) {
		t.Fatalf("resumed grants=%v", got)
	}
}

func TestIntegerManagerEnableMinimumAndFailedGrantRollback(t *testing.T) {
	const base int64 = 1<<53 + 1
	for _, strict := range []bool{false, true} {
		peer := integerState(2, base+10, 0, false, true)
		if strict {
			at := peer.CurrentTime
			peer.PreservedReceiveTime = &at
		}
		m, out := integerManagerFixture(t, integerState(1, base, 0, false, false), peer)
		if err := m.EnableRegulation(context.Background(), "integer", 1, 2); err != nil {
			t.Fatal(err)
		}
		want := base + 8 + int64(boolCount(strict))
		if got := out.events[0].(*IntegerTimeRegulationEnabled).Time; got != want {
			t.Fatalf("enable time=%d want=%d", got, want)
		}
	}
	for _, row := range []struct {
		current, lookahead, peer int64
		strict, possible         bool
		want                     int64
	}{
		{0, 0, math.MaxInt64, true, false, 0},
		{0, 1, math.MaxInt64, true, true, math.MaxInt64},
		{0, math.MaxInt64, math.MaxInt64, true, true, 1},
		{base, 1, base + 2, false, true, base + 1},
	} {
		got, possible := minimumEnableTimeOf(row.current, row.lookahead, row.peer, row.strict)
		if possible != row.possible || possible && got != row.want {
			t.Fatalf("minimum=%d,%t case=%+v", got, possible, row)
		}
	}
	m, out := integerManagerFixture(t)
	out.fail = core.ErrFederateOverflow
	if err := m.TimeAdvanceRequest(context.Background(), "integer", 1, base); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("failed grant=%v", err)
	}
	state := m.Metrics("integer", 1)
	if state.CurrentTime != 0 || !state.Advancing {
		t.Fatalf("failure committed state=%+v", state)
	}
	out.fail = nil
	if err := m.RetryPendingDeliveries(context.Background(), "integer"); err != nil {
		t.Fatal(err)
	}
	if got := integerGrants(out); !reflect.DeepEqual(got, []int64{base}) {
		t.Fatalf("retry grants=%v", got)
	}
	if err := m.TimeAdvanceRequest(context.Background(), "integer", 1, base-1); !errors.Is(err, core.ErrTimeRequestInPast) {
		t.Fatalf("past=%v", err)
	}
	if _, _, err := m.ReserveScheduledSave("integer", -1, nil); err == nil {
		t.Fatal("negative save target accepted")
	}
	if err := m.EnableRegulation(context.Background(), "integer", 1, -1); !errors.Is(err, core.ErrTimeInvalidLookahead) {
		t.Fatalf("negative lookahead=%v", err)
	}
}

func TestIntegerManagerBoundAndEnableOracle(t *testing.T) {
	random := rand.New(rand.NewSource(15162010))
	for index := 0; index < 512; index++ {
		base := random.Int63()
		lookahead := random.Int63()
		target := random.Int63()
		if index < 8 {
			base = math.MaxInt64 - int64(index)
			lookahead = int64(index % 3)
			target = math.MaxInt64 - int64(index%2)
		}
		m, out := integerManagerFixture(t, integerState(1, base, lookahead, true, false), integerState(2, 0, 0, false, true))
		sum := new(big.Int).Add(big.NewInt(base), big.NewInt(lookahead))
		bound, defined := m.QueryGALTBound("integer", 2)
		if !defined || bound.Unbounded != (!sum.IsInt64()) || sum.IsInt64() && bound.Limit != sum.Int64() {
			t.Fatalf("%d+%d horizon=%+v exact=%v", base, lookahead, bound, sum)
		}
		for _, strict := range []bool{false, true} {
			minimum, possible := minimumEnableTimeOf[int64](0, lookahead, target, strict)
			want := new(big.Int).Sub(big.NewInt(target), big.NewInt(lookahead))
			if strict {
				want.Add(want, big.NewInt(1))
			}
			if want.Sign() < 0 {
				want.SetInt64(0)
			}
			if possible != want.IsInt64() || possible && minimum != want.Int64() {
				t.Fatalf("enable LA=%d peer=%d strict=%t got=%d/%t want=%v", lookahead, target, strict, minimum, possible, want)
			}
		}
		if err := m.TimeAdvanceRequest(context.Background(), "integer", 2, target); err != nil {
			t.Fatal(err)
		}
		wantGrant := sum.Cmp(big.NewInt(target)) > 0
		if got := len(integerGrants(out)) != 0; got != wantGrant {
			t.Fatalf("TAR base=%d LA=%d target=%d grants=%v exact bound=%v", base, lookahead, target, integerGrants(out), sum)
		}
	}
}

func TestIntegerManagerScheduledFinalHorizon(t *testing.T) {
	for _, lookahead := range []int64{0, 1} {
		m, out := integerManagerFixture(t, integerState(1, math.MaxInt64, lookahead, true, false), integerState(2, math.MaxInt64-1, 0, false, true))
		ready, release, err := m.ReserveScheduledSave("integer", math.MaxInt64, []core.FederateHandle{1, 2})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.BufferTSO(context.Background(), "integer", 2, math.MaxInt64, &integerTestMessage{1}); err != nil {
			t.Fatal(err)
		}
		lits, defined := m.QueryLITSBound("integer", 2)
		if !defined || lits.Unbounded || lits.Limit != math.MaxInt64 {
			t.Fatalf("finite queued LITS=%+v", lits)
		}
		if err := m.TimeAdvanceRequest(context.Background(), "integer", 2, math.MaxInt64); err != nil {
			t.Fatal(err)
		}
		if ok, err := ready(); err != nil || ok != (lookahead == 1) {
			t.Fatalf("final save LA=%d ready=%t err=%v", lookahead, ok, err)
		}
		if len(integerGrants(out)) != 0 {
			t.Fatal("grant escaped final save barrier")
		}
		release()
		if err := m.ResumeScheduledSave(context.Background(), "integer"); err != nil {
			t.Fatal(err)
		}
		if got := len(integerGrants(out)); got != boolCount(lookahead == 1) {
			t.Fatalf("resumed final grants=%v LA=%d", integerGrants(out), lookahead)
		}
	}
}

type integerTestJournal struct{ records []core.EventRecord }

func (l *integerTestJournal) Append(_ context.Context, _ core.FederationName, event core.EventRecord) error {
	l.records = append(l.records, event)
	return nil
}
func (*integerTestJournal) Sync(context.Context, core.FederationName) error { return nil }
func (*integerTestJournal) OpenReader(context.Context, string) (core.EventLogReader, error) {
	return nil, nil
}

func TestIntegerManagerJournalRetainsExactGrant(t *testing.T) {
	out := &integerTestOutbox{}
	log := &integerTestJournal{}
	m, err := NewInteger(IntegerOptions{Clock: core.NewRealClock(), Outbox: out, EventLog: log})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { managerExt.Delete(m) })
	for _, target := range []int64{1<<53 + 1, 1<<53 + 2, math.MaxInt64} {
		if err := m.TimeAdvanceRequestAvailable(context.Background(), "integer", 1, target); err != nil {
			t.Fatal(err)
		}
		record, ok := log.records[len(log.records)-1].(*IntegerTimeAdvanceGrantedRecord)
		if !ok || record.Time != target || record.Federate != 1 {
			t.Fatalf("grant record=%#v", log.records)
		}
		record.SetSeq(uint64(len(log.records)))
		if record.Seq() != uint64(len(log.records)) {
			t.Fatal("journal sequence assignment failed")
		}
		if got := out.events[len(out.events)-1].(*IntegerTimeAdvanceGrant).Time; got != record.Time {
			t.Fatalf("callback=%d journal=%d", got, record.Time)
		}
	}
	if err := m.TimeAdvanceRequest(context.Background(), "integer", 1, -1); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("negative time=%v", err)
	}
	if err := m.ValidateOutgoingTSO("integer", 2, -1); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("negative send=%v", err)
	}
}
