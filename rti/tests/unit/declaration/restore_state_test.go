package declaration_test

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func populateDeclaration(t *testing.T, m *declaration.Manager, fed core.FederationName, members []core.FederateHandle) {
	t.Helper()
	ctx := context.Background()
	for _, h := range members {
		for _, cls := range []core.ObjectClassHandle{9, 3} {
			if err := m.PublishObjectClassAttributes(ctx, fed, h, cls, []core.AttributeHandle{5, 2}); err != nil {
				t.Fatal(err)
			}
			if err := m.SubscribeObjectClassAttributes(ctx, fed, h, cls, []core.AttributeHandle{5, 2}); err != nil {
				t.Fatal(err)
			}
		}
		for _, cls := range []core.InteractionClassHandle{11, 7} {
			if err := m.PublishInteractionClass(ctx, fed, h, cls); err != nil {
				t.Fatal(err)
			}
			if err := m.SubscribeInteractionClass(ctx, fed, h, cls); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestDeclarationStateExportDeterministicAndDetached(t *testing.T) {
	m := declaration.New()
	high := core.FederateHandle(1<<63 + 17)
	populateDeclaration(t, m, "fed", []core.FederateHandle{high, 2, 1})
	wantObjects := []declaration.ObjectDeclarationState{
		{Class: 3, Attribute: 2, Federates: []core.FederateHandle{1, 2, high}},
		{Class: 3, Attribute: 5, Federates: []core.FederateHandle{1, 2, high}},
		{Class: 9, Attribute: 2, Federates: []core.FederateHandle{1, 2, high}},
		{Class: 9, Attribute: 5, Federates: []core.FederateHandle{1, 2, high}},
	}
	wantInteractions := []declaration.InteractionDeclarationState{
		{Class: 7, Federates: []core.FederateHandle{1, 2, high}},
		{Class: 11, Federates: []core.FederateHandle{1, 2, high}},
	}
	want := &declaration.ManagerState{
		ObjectPublications: wantObjects, ObjectSubscriptions: wantObjects,
		InteractionPublications: wantInteractions, InteractionSubscriptions: wantInteractions,
		AdvisoryDefaults: &[4]bool{true, true, true, true},
	}
	for _, record := range wantObjects {
		for _, h := range record.Federates {
			want.ObjectOptions = append(want.ObjectOptions, declaration.ObjectSubscriptionOptionsState{Class: record.Class, Attribute: record.Attribute, Federate: h, Options: core.SubscriptionOptions{Active: true}})
		}
	}
	for _, record := range wantInteractions {
		for _, h := range record.Federates {
			want.InteractionOptions = append(want.InteractionOptions, declaration.InteractionSubscriptionOptionsState{Class: record.Class, Federate: h, Active: true})
		}
	}
	for i := 0; i < 10; i++ {
		if got := m.ExportState("fed"); !reflect.DeepEqual(got, want) {
			t.Fatalf("export = %#v, want %#v", got, want)
		}
	}
	other := declaration.New()
	populateDeclaration(t, other, "fed", []core.FederateHandle{1, high, 2})
	if !reflect.DeepEqual(other.ExportState("fed"), want) {
		t.Fatal("export depends on insertion order")
	}
	exported := m.ExportState("fed")
	exported.ObjectPublications[0].Federates[0] = 99
	exported.ObjectSubscriptions[0].Federates[0] = 98
	exported.InteractionPublications[0].Federates[0] = 97
	exported.InteractionSubscriptions[0].Federates[0] = 96
	exported.ObjectPublications[0].Class = 100
	exported.ObjectOptions[0].Options.Active = false
	exported.ObjectOptions[0].Options.UpdateRateDesignator = "mutated"
	exported.InteractionOptions[0].Active = false
	exported.AdvisoryDefaults[0] = false
	if !reflect.DeepEqual(m.ExportState("fed"), want) {
		t.Fatal("editing exported DTO changed live declarations")
	}
	m.OnFederateResign("fed", high)
	if !slices.Contains(exported.ObjectSubscriptions[0].Federates, high) {
		t.Fatal("later live mutation changed an earlier DTO")
	}
}

func TestDeclarationStatePreparedRestoreReplacesTablesAndCaches(t *testing.T) {
	m := declaration.New()
	ctx := context.Background()
	populateDeclaration(t, m, "fed", []core.FederateHandle{1, 2})
	populateDeclaration(t, m, "other", []core.FederateHandle{8})
	saved, expected := m.ExportState("fed"), m.ExportState("fed")
	other := m.ExportState("other")
	m.OnFederateResign("fed", 1)
	m.OnFederateResign("fed", 2)
	populateDeclaration(t, m, "fed", []core.FederateHandle{99})
	if err := m.PublishInteractionClass(ctx, "fed", 99, 100); err != nil {
		t.Fatal(err)
	}
	oldObjectCache := m.ObjectSubscribersSnapshot(ctx, "fed", 3, []core.AttributeHandle{2})
	oldInteractionCache := m.InteractionSubscribersSnapshot(ctx, "fed", 7)
	before := m.ExportState("fed")
	commit, err := m.PrepareRestore("fed", saved)
	if err != nil || commit == nil {
		t.Fatalf("prepare: commit nil = %v, err = %v", commit == nil, err)
	}
	if !reflect.DeepEqual(m.ExportState("fed"), before) {
		t.Fatal("prepare mutated live declarations")
	}
	// Preparation must detach all input before the caller reuses its DTO.
	saved.ObjectPublications[0].Federates[0] = 201
	saved.ObjectSubscriptions[0].Federates[0] = 202
	saved.InteractionPublications[0].Federates[0] = 203
	saved.InteractionSubscriptions[0].Federates[0] = 204
	saved.ObjectSubscriptions[0].Attribute = 999
	saved.ObjectOptions[0].Options.Active = false
	saved.InteractionOptions[0].Active = false
	saved.AdvisoryDefaults[0] = false
	commit()
	if !reflect.DeepEqual(m.ExportState("fed"), expected) {
		t.Fatal("restore did not replace all four declaration tables")
	}
	if !reflect.DeepEqual(m.ExportState("other"), other) {
		t.Fatal("restore changed another federation")
	}
	queries := map[string][]core.FederateHandle{
		"object publishers":       m.PublishersFor(ctx, "fed", 3, 2),
		"object subscribers":      m.SubscribersFor(ctx, "fed", 3, []core.AttributeHandle{2}),
		"object cache":            m.ObjectSubscribersSnapshot(ctx, "fed", 3, []core.AttributeHandle{2}),
		"interaction publishers":  m.InteractionPublishersFor(ctx, "fed", 7),
		"interaction subscribers": m.InteractionSubscribersFor(ctx, "fed", 7),
		"interaction cache":       m.InteractionSubscribersSnapshot(ctx, "fed", 7),
	}
	for name, got := range queries {
		if !slices.Equal(got, []core.FederateHandle{1, 2}) {
			t.Errorf("%s = %v, want [1 2]", name, got)
		}
	}
	if !slices.Equal(oldObjectCache, []core.FederateHandle{99}) || !slices.Equal(oldInteractionCache, []core.FederateHandle{99}) {
		t.Fatal("restore mutated snapshots held by existing readers")
	}
	if m.PublishesInteraction(ctx, "fed", 100, 99) {
		t.Fatal("post-save publication survived restore")
	}
	if err := m.ValidateObjectPublication(ctx, "fed", 1, 3, []core.AttributeHandle{2, 5}); err != nil {
		t.Fatalf("restored publication validation failed: %v", err)
	}
}

func TestDeclarationStateEmptyRestoreRemovesPostSaveState(t *testing.T) {
	for name, state := range map[string]*declaration.ManagerState{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			m := declaration.New()
			populateDeclaration(t, m, "fed", []core.FederateHandle{1})
			populateDeclaration(t, m, "other", []core.FederateHandle{2})
			other := m.ExportState("other")
			ctx := context.Background()
			m.ObjectSubscribersSnapshot(ctx, "fed", 3, []core.AttributeHandle{2})
			m.InteractionSubscribersSnapshot(ctx, "fed", 7)
			if err := m.ImportState("fed", state); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(m.ExportState("fed"), m.ExportState("absent")) {
				t.Fatal("empty restore retained declarations")
			}
			if len(m.ObjectSubscribersSnapshot(ctx, "fed", 3, []core.AttributeHandle{2})) != 0 ||
				len(m.InteractionSubscribersSnapshot(ctx, "fed", 7)) != 0 {
				t.Fatal("empty restore retained cached subscribers")
			}
			if !reflect.DeepEqual(m.ExportState("other"), other) {
				t.Fatal("empty restore cleared another federation")
			}
		})
	}
}

func TestDeclarationStateInvalidRestoreIsAtomic(t *testing.T) {
	objectCases := map[string][]declaration.ObjectDeclarationState{
		"zero class":         {{Attribute: 2, Federates: []core.FederateHandle{1}}},
		"zero attribute":     {{Class: 3, Federates: []core.FederateHandle{1}}},
		"zero federate":      {{Class: 3, Attribute: 2, Federates: []core.FederateHandle{0}}},
		"empty federates":    {{Class: 3, Attribute: 2}},
		"duplicate federate": {{Class: 3, Attribute: 2, Federates: []core.FederateHandle{1, 1}}},
		"duplicate key":      {{Class: 3, Attribute: 2, Federates: []core.FederateHandle{1}}, {Class: 3, Attribute: 2, Federates: []core.FederateHandle{2}}},
	}
	interactionCases := map[string][]declaration.InteractionDeclarationState{
		"zero class":         {{Federates: []core.FederateHandle{1}}},
		"zero federate":      {{Class: 7, Federates: []core.FederateHandle{0}}},
		"empty federates":    {{Class: 7}},
		"duplicate federate": {{Class: 7, Federates: []core.FederateHandle{1, 1}}},
		"duplicate key":      {{Class: 7, Federates: []core.FederateHandle{1}}, {Class: 7, Federates: []core.FederateHandle{2}}},
	}
	check := func(t *testing.T, state *declaration.ManagerState) {
		t.Helper()
		m := declaration.New()
		populateDeclaration(t, m, "fed", []core.FederateHandle{99})
		before := m.ExportState("fed")
		commit, err := m.PrepareRestore("fed", state)
		if err == nil || commit != nil {
			t.Fatalf("invalid state accepted: commit nil = %v, err = %v", commit == nil, err)
		}
		if err := m.ImportState("fed", state); err == nil {
			t.Fatal("direct import accepted invalid state")
		}
		if !reflect.DeepEqual(m.ExportState("fed"), before) {
			t.Fatal("invalid restore changed live declarations")
		}
	}
	for _, table := range []string{"object publications", "object subscriptions"} {
		for name, records := range objectCases {
			t.Run(table+"/"+name, func(t *testing.T) {
				state := &declaration.ManagerState{InteractionPublications: []declaration.InteractionDeclarationState{{Class: 100, Federates: []core.FederateHandle{10}}}}
				if table == "object publications" {
					state.ObjectPublications = records
				} else {
					state.ObjectSubscriptions = records
				}
				check(t, state)
			})
		}
	}
	for _, table := range []string{"interaction publications", "interaction subscriptions"} {
		for name, records := range interactionCases {
			t.Run(table+"/"+name, func(t *testing.T) {
				state := &declaration.ManagerState{ObjectPublications: []declaration.ObjectDeclarationState{{Class: 100, Attribute: 1, Federates: []core.FederateHandle{10}}}}
				if table == "interaction publications" {
					state.InteractionPublications = records
				} else {
					state.InteractionSubscriptions = records
				}
				check(t, state)
			})
		}
	}
}

type declarationOutbox struct{ sends int }

func (o *declarationOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	o.sends++
	return nil
}

func TestDeclarationStateRestorePreservesHooksWithoutEmittingCallbacks(t *testing.T) {
	m := declaration.New()
	outbox := &declarationOutbox{}
	m.SetAdvisoryOutbox(outbox)
	hooks := 0
	m.SetOnSubscribeObjectClass(func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) {
		hooks++
	})
	populateDeclaration(t, m, "fed", []core.FederateHandle{1})
	saved := m.ExportState("fed")
	m.OnFederationDestroyed("fed")
	beforeSends, beforeHooks := outbox.sends, hooks
	if err := m.ImportState("fed", saved); err != nil {
		t.Fatal(err)
	}
	if outbox.sends != beforeSends || hooks != beforeHooks {
		t.Fatal("restore replayed subscribe hooks or advisories")
	}
	ctx := context.Background()
	if err := m.UnsubscribeObjectClassAttributes(ctx, "fed", 1, 3, []core.AttributeHandle{2, 5}); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeObjectClassAttributes(ctx, "fed", 2, 3, []core.AttributeHandle{2, 5}); err != nil {
		t.Fatal(err)
	}
	// Removing one's own subscription does not change remote relevance; only
	// the new remote subscriber produces an advisory after restore.
	if outbox.sends != beforeSends+1 || hooks != beforeHooks+1 {
		t.Fatalf("composition hooks lost: sends=%d hooks=%d", outbox.sends-beforeSends, hooks-beforeHooks)
	}
}

func TestDeclarationStateConcurrentExportAndRestore(t *testing.T) {
	m := declaration.New()
	populateDeclaration(t, m, "fed", []core.FederateHandle{1, 2})
	first := m.ExportState("fed")
	other := declaration.New()
	populateDeclaration(t, other, "fed", []core.FederateHandle{8, 9})
	second := other.ExportState("fed")
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		state := first
		if worker%2 != 0 {
			state = second
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if err := m.ImportState("fed", state); err != nil {
					t.Error(err)
					return
				}
				exported := m.ExportState("fed")
				if !reflect.DeepEqual(exported, first) && !reflect.DeepEqual(exported, second) {
					t.Error("concurrent reader observed partial declaration state")
					return
				}
				m.ObjectSubscribersSnapshot(context.Background(), "fed", 3, []core.AttributeHandle{2})
				m.InteractionSubscribersSnapshot(context.Background(), "fed", 7)
			}
		}()
	}
	wg.Wait()
}

func TestDeclarationStateRestoreAcceptsUnsortedRecords(t *testing.T) {
	m := declaration.New()
	populateDeclaration(t, m, "fed", []core.FederateHandle{1, 2})
	want := m.ExportState("fed")
	state := m.ExportState("fed")
	slices.Reverse(state.ObjectPublications)
	slices.Reverse(state.ObjectSubscriptions)
	slices.Reverse(state.InteractionPublications)
	slices.Reverse(state.InteractionSubscriptions)
	for _, entries := range [][]declaration.ObjectDeclarationState{state.ObjectPublications, state.ObjectSubscriptions} {
		for _, entry := range entries {
			slices.Reverse(entry.Federates)
		}
	}
	for _, entries := range [][]declaration.InteractionDeclarationState{state.InteractionPublications, state.InteractionSubscriptions} {
		for _, entry := range entries {
			slices.Reverse(entry.Federates)
		}
	}
	m.OnFederationDestroyed("fed")
	commit, err := m.PrepareRestore("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	if !reflect.DeepEqual(m.ExportState("fed"), want) {
		t.Fatal("valid unsorted records were not restored canonically")
	}
	// A retry of the same commit must not resurrect state after teardown.
	m.OnFederationDestroyed("fed")
	commit()
	if !reflect.DeepEqual(m.ExportState("fed"), m.ExportState("absent")) {
		t.Fatal("prepared commit applied more than once")
	}
}
