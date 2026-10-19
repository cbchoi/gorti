package mom

import (
	"context"
	"errors"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type delegatedResignKey struct{}
type delegatedResignOutcome struct {
	mu          sync.Mutex
	dispatcher  *Dispatcher
	dctx        DispatchContext
	command     MOMServiceCommand
	recorded    bool
	reportError error
}

// ObserveMOMResignOutcome is called by composition under the actual lifecycle
// writer, before MOM state and declaration subscriptions are retired. An
// unrelated ordinary resign has no delegated state and is not counted here.
func (d *Dispatcher) ObserveMOMResignOutcome(ctx context.Context, fed core.FederationName, target core.FederateHandle, outcome error, exception string) error {
	state, ok := ctx.Value(delegatedResignKey{}).(*delegatedResignOutcome)
	if !ok || state.dispatcher != d || state.dctx.Federation != fed || state.command.Target != target {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.recorded {
		return state.reportError
	}
	state.recorded = true
	inv := ServiceInvocation{Service: "resignFederationExecution", Success: outcome == nil, SuppliedArguments: state.command.SuppliedArguments, Exception: exception}
	if outcome != nil && exception == "" {
		inv.Exception = outcome.Error()
	}
	state.reportError = errors.Join(state.dctx.MOM.RecordServiceInvocation(fed, target, inv), d.ReportServiceInvocation(ctx, fed, target, inv, state.dctx.FOM, state.dctx.FOMNames))
	return state.reportError
}

func (s *delegatedResignOutcome) result() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recorded, s.reportError
}
