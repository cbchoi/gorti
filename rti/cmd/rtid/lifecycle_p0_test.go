package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/ddm"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestP0DestroyCleansComposedFederationState(t *testing.T) {
	logDir := t.TempDir()
	srv, err := newRTID(rtidConfig{
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		PluginFactories: pluginFactories(auditReplayPluginEventJournal, logDir, false),
		SaveDir:         t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.plugins.Close() }()
	auditLog := srv.plugins.AdminEventLog()
	ctx := context.Background()
	const fed = core.FederationName("lifecycle-p0")
	if err := srv.fedMgr.CreateFederation(ctx, core.CreateFederationRequest{Name: fed, Mode: core.ModeVerbose}); err != nil {
		t.Fatal(err)
	}
	fom := &fomHandle{fom: model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{{Name: "X", UpperBound: 100}})}
	srv.foms.RememberFor(fed, fom)
	h, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "member"})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.declMgr.PublishObjectClassAttributes(ctx, fed, h, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	objectHandle, _, err := srv.objReg.Register(ctx, fed, h, 1, "retained-object")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.syncMgr.Register(ctx, fed, "retained-sync", nil, []core.FederateHandle{h}); err != nil {
		t.Fatal(err)
	}
	space, ok := srv.ddmMgr.LookupRoutingSpace(fed, ddm.DefaultRoutingSpace)
	if !ok {
		t.Fatal("default routing space not found")
	}
	dimension, ok := srv.ddmMgr.LookupDimension(fed, space, "X")
	if !ok {
		t.Fatal("X dimension not found")
	}
	if _, err := srv.ddmMgr.CreateRegion(ctx, fed, h, space, []core.DDMDimensionHandle{dimension}); err != nil {
		t.Fatal(err)
	}
	if err := srv.saveMgr.RequestFederationSave(ctx, fed, "retained-save", nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.timeMgr.EnableRegulation(ctx, fed, h, 1); err != nil {
		t.Fatal(err)
	}
	if err := srv.fedMgr.ResignFederation(ctx, fed, h, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	// Recreate representative stale state after resign so the destroy hook,
	// rather than only the resign hooks, is responsible for removing it.
	if err := srv.declMgr.PublishInteractionClass(ctx, fed, h, 1); err != nil {
		t.Fatal(err)
	}
	srv.ownMgr.RegisterInitialOwnership(fed, h, objectHandle, []core.AttributeHandle{1})
	srv.outbox.Bind(fed, h)

	if err := srv.fedMgr.DestroyFederation(ctx, fed); err != nil {
		t.Fatal(err)
	}
	if got := len(srv.declMgr.Snapshot(fed).PerFederate); got != 0 {
		t.Fatalf("declaration cardinality = %d", got)
	}
	if got := srv.objReg.Snapshot(fed).InstanceCount; got != 0 {
		t.Fatalf("object cardinality = %d", got)
	}
	if _, err := srv.foms.Get(ctx, fed); !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatalf("FOM state remains: %v", err)
	}
	if err := auditLog.Sync(ctx, fed); !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatalf("event-log writer remains: %v", err)
	}
	for key := range *srv.outbox.subs.Load() {
		if key.fed == fed {
			t.Fatalf("outbox binding remains for %+v", key)
		}
	}
	if snapshot := srv.timeMgr.Snapshot(fed); len(snapshot.Federates) != 0 {
		t.Fatalf("time state remains: %+v", snapshot)
	}
	if snapshot := srv.syncMgr.Snapshot(fed); len(snapshot) != 0 {
		t.Fatalf("sync state remains: %+v", snapshot)
	}
	if snapshot := srv.ownMgr.Snapshot(fed); snapshot != (core.OwnershipSnapshot{}) {
		t.Fatalf("ownership state remains: %+v", snapshot)
	}
	if snapshot := srv.ddmMgr.Snapshot(fed); snapshot.RegionCount != 0 {
		t.Fatalf("DDM state remains: %+v", snapshot)
	}
	if snapshot := srv.saveMgr.Snapshot(fed); snapshot.SaveState != core.SaveStateIdle || snapshot.RestoreState != core.SaveRestoreIdle {
		t.Fatalf("save/restore state remains: %+v", snapshot)
	}

	if err := srv.fedMgr.CreateFederation(ctx, core.CreateFederationRequest{Name: fed, Mode: core.ModeVerbose}); err != nil {
		t.Fatalf("same-name recreation: %v", err)
	}
	newHandle, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "next-member"})
	if err != nil {
		t.Fatal(err)
	}
	if newHandle == h {
		t.Fatalf("recreated generation reused stale handle %d", h)
	}
	if err := srv.fedMgr.ValidateMember(fed, h); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("stale generation validation = %v, want ErrFederateNotJoined", err)
	}
	if err := srv.fedMgr.ResignFederation(ctx, fed, newHandle, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if err := srv.fedMgr.DestroyFederation(ctx, fed); err != nil {
		t.Fatal(err)
	}
}

func TestP0FederationGenerationEpochPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := nextFederationGenerationEpoch(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := nextFederationGenerationEpoch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first == 0 || second != first+generationReservationSpan {
		t.Fatalf("generation epochs = %d then %d, want non-overlapping blocks", first, second)
	}
}

func TestP0EventLogUsesLiveFederationGenerationMetadata(t *testing.T) {
	logDir := t.TempDir()
	srv, err := newRTID(rtidConfig{
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		PluginFactories: pluginFactories(auditReplayPluginEventJournal, logDir, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.plugins.Close() }()
	auditLog := srv.plugins.AdminEventLog()

	ctx := context.Background()
	const fed = core.FederationName("generation-log")
	run := func(seed uint64, mode core.Mode, member string) core.FederateHandle {
		t.Helper()
		if err := srv.fedMgr.CreateFederation(ctx, core.CreateFederationRequest{
			Name: fed, Seed: seed, Mode: mode,
		}); err != nil {
			t.Fatal(err)
		}
		h, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{
			Federation: fed, FederateName: member,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := auditLog.Sync(ctx, fed); err != nil {
			t.Fatal(err)
		}
		return h
	}

	first := run(1516, core.ModeBestEffort, "first")
	reader, err := auditLog.OpenReader(ctx, string(fed))
	if err != nil {
		t.Fatal(err)
	}
	firstHeader := reader.Header()
	_ = reader.Close()
	if firstHeader.Generation != 0 || firstHeader.Seed != 1516 || firstHeader.Mode != core.ModeBestEffort {
		t.Fatalf("first header = %+v", firstHeader)
	}
	if err := srv.fedMgr.ResignFederation(ctx, fed, first, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if err := srv.fedMgr.DestroyFederation(ctx, fed); err != nil {
		t.Fatal(err)
	}

	second := run(1517, core.ModeVerbose, "second")
	reader, err = auditLog.OpenReader(ctx, string(fed))
	if err != nil {
		t.Fatal(err)
	}
	secondHeader := reader.Header()
	_ = reader.Close()
	if secondHeader.Generation != 1 || secondHeader.Seed != 1517 || secondHeader.Mode != core.ModeVerbose {
		t.Fatalf("second header = %+v", secondHeader)
	}
	files, err := filepath.Glob(filepath.Join(logDir, "*", "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("generation log files = %v, want two distinct files", files)
	}
	if err := srv.fedMgr.ResignFederation(ctx, fed, second, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if err := srv.fedMgr.DestroyFederation(ctx, fed); err != nil {
		t.Fatal(err)
	}
}
