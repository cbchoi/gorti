package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type FederationCreation struct {
	EffectiveSeed, FederationGeneration uint64
	LogicalTimeImplementationName       string
}
type FederationSummary struct {
	Name                          string
	Mode                          int32
	FederatesJoined               uint32
	FederationGeneration          uint64
	LogicalTimeImplementationName string
}

// CreateFederation creates one execution and does not swallow AlreadyExists.
// A FederationCreationResponseError also returns the committed creation metadata;
// callers must not treat that error as evidence that no execution was created.
func (c *Connection) CreateFederation(ctx context.Context, spec FederationSpec) (FederationCreation, error) {
	if c == nil || c.cc == nil || c.fed == nil {
		return FederationCreation{}, ErrNotJoined
	}
	if err := requireSupportedLogicalTime(spec.LogicalTimeImplementationName); err != nil {
		return FederationCreation{}, err
	}
	if len(spec.AdditionalFOMModules) != 0 {
		return FederationCreation{}, fmt.Errorf("federate: AdditionalFOMModules applies to joining; put creation modules in FOMModules")
	}
	modules := make([]*rtiv1.FOMModule, len(spec.FOMModules))
	for i, m := range spec.FOMModules {
		modules[i] = &rtiv1.FOMModule{Path: m.Path, Xml: append([]byte(nil), m.XML...)}
	}
	selectedMIM, err := prepareMIMModule(spec.MIMModule)
	if err != nil {
		return FederationCreation{}, err
	}
	r, err := c.fed.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: createWireVersion(selectedMIM), FederationName: spec.Name, FomModules: modules, MimModule: selectedMIM, Seed: spec.Seed, StallTimeoutSeconds: spec.StallTimeoutSeconds, LogicalTimeImplementationName: spec.LogicalTimeImplementationName})
	if err != nil {
		return FederationCreation{}, wrapStatusErr(err)
	}
	created, err := c.recordFederationCreation(spec.Name, r)
	if err != nil {
		return created, err
	}
	c.rememberMIM(spec.Name, selectedMIM)
	return created, nil
}

func (c *Connection) recordFederationCreation(name string, r *rtiv1.CreateFederationResponse) (FederationCreation, error) {
	created := FederationCreation{EffectiveSeed: r.GetEffectiveSeed(), FederationGeneration: r.GetFederationGeneration(), LogicalTimeImplementationName: logicalTimeImplementationName(r.GetLogicalTimeImplementationName())}
	c.generationMu.Lock()
	if c.generations == nil {
		c.generations = make(map[string]uint64)
	}
	c.generations[name] = created.FederationGeneration
	c.generationMu.Unlock()
	if err := requireSupportedLogicalTime(created.LogicalTimeImplementationName); err != nil {
		return created, &FederationCreationResponseError{FederationName: name, Creation: created}
	}
	return created, nil
}

func (c *Connection) ListFederations(ctx context.Context) ([]FederationSummary, error) {
	if c == nil || c.cc == nil || c.fed == nil {
		return nil, ErrNotJoined
	}
	r, err := c.fed.ListFederations(ctx, &rtiv1.ListFederationsRequest{WireVersion: 1})
	if err != nil {
		return nil, wrapStatusErr(err)
	}
	out := make([]FederationSummary, 0, len(r.GetFederations()))
	for _, f := range r.GetFederations() {
		out = append(out, FederationSummary{Name: f.GetName(), Mode: int32(f.GetMode()), FederatesJoined: f.GetFederatesJoined(), FederationGeneration: f.GetFederationGeneration(), LogicalTimeImplementationName: logicalTimeImplementationName(f.GetLogicalTimeImplementationName())})
	}
	return out, nil
}

func (f *Federate) SetAutomaticResignDirective(ctx context.Context, action ResignAction) error {
	if f == nil || f.conn == nil || f.conn.fed == nil {
		return ErrNotJoined
	}
	_, err := f.conn.fed.SetAutomaticResignDirective(f.sessionContext(ctx), &rtiv1.SetAutomaticResignDirectiveRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), Action: action.wire()})
	return wrapStatusErr(err)
}
func (f *Federate) GetAutomaticResignDirective(ctx context.Context) (ResignAction, error) {
	if f == nil || f.conn == nil || f.conn.fed == nil {
		return ResignActionUnspecified, ErrNotJoined
	}
	r, err := f.conn.fed.GetAutomaticResignDirective(f.sessionContext(ctx), &rtiv1.GetAutomaticResignDirectiveRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle()})
	if err != nil {
		return ResignActionUnspecified, wrapStatusErr(err)
	}
	for _, a := range []ResignAction{ResignActionUnconditionallyDivestAttributes, ResignActionDeleteThenDivest, ResignActionCancelThenDelete, ResignActionCancelPendingOwnership, ResignActionNoAction, ResignActionDeleteObjects} {
		if a.wire() == r.GetAction() {
			return a, nil
		}
	}
	return ResignActionUnspecified, fmt.Errorf("federate: unknown automatic resign directive %d", r.GetAction())
}
