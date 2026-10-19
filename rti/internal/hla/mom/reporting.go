package mom

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const (
	ClassReportServiceInvocation = "HLAmanager.HLAfederate.HLAreport.HLAreportServiceInvocation"
	ClassReportException         = "HLAmanager.HLAfederate.HLAreport.HLAreportException"
	ClassReportMOMException      = "HLAmanager.HLAfederate.HLAreport.HLAreportMOMexception"
)

// ServiceInvocation must describe an actual service outcome, not a MOM request
// inferred to be a service call. Empty argument lists mean genuinely no args.
type ServiceInvocation struct {
	Service           string
	Success           bool
	SuppliedArguments []string
	ReturnedArguments []string
	Exception         string
}

type ServiceReportState struct{ NextSerial uint64 }

func (m *Manager) ReportingEnabled(fed core.FederationName, h core.FederateHandle) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil || st.federates[h] == nil {
		return false
	}
	fs := st.federates[h]
	return fs.serviceReportingEnabled || fs.exceptionReportingEnabled
}

func (d *Dispatcher) ReportServiceInvocation(ctx context.Context, fed core.FederationName, target core.FederateHandle, invocation ServiceInvocation, fom core.FOMHandle, names core.FOMHandleNameLookup) error {
	if !validServiceInvocation(invocation) {
		return fmt.Errorf("mom: inconsistent service invocation outcome")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d.reportMu.Lock()
	defer d.reportMu.Unlock()
	if d.mom == nil {
		return fmt.Errorf("mom: manager unavailable")
	}
	d.mom.mu.RLock()
	st := d.mom.fed[fed]
	if st == nil || st.federates[target] == nil {
		d.mom.mu.RUnlock()
		return core.ErrFederateNotJoined
	}
	fs := st.federates[target]
	service, exception := fs.serviceReportingEnabled, fs.exceptionReportingEnabled
	serial, known := fs.serviceSerial, fs.serviceSerialKnown
	d.mom.mu.RUnlock()
	var reports []ResponseInteraction
	if service {
		if !known {
			return fmt.Errorf("mom: service report serial baseline unavailable")
		}
		report, err := serviceInvocationReport(target, invocation, serial)
		if err != nil {
			return err
		}
		reports = append(reports, report)
	}
	if exception && !invocation.Success {
		reports = append(reports, ResponseInteraction{ClassName: ClassReportException, Params: map[string][]byte{"HLAfederate": encodeHLAhandle(target), "HLAservice": encodeHLAunicodeString(invocation.Service), "HLAexception": encodeHLAunicodeString(invocation.Exception)}})
	}
	if err := d.emitResponses(ctx, fed, 0, reports, fom, names, invocation.Service); err != nil {
		// A multi-recipient emission can fail after some deliveries. Do not
		// reuse a possibly observed serial or claim a reconstructed baseline.
		if service {
			d.mom.finishServiceReport(fed, target, fs, false)
		}
		return err
	}
	if service {
		d.mom.finishServiceReport(fed, target, fs, true)
	}
	return nil
}

func (d *Dispatcher) emitResponses(ctx context.Context, fed core.FederationName, requester core.FederateHandle, responses []ResponseInteraction, fom core.FOMHandle, names core.FOMHandleNameLookup, service string) error {
	d.mu.RLock()
	emit, recipients := d.emitter, d.catalog.ReportRecipients
	d.mu.RUnlock()
	if len(responses) == 0 {
		return nil
	}
	if emit == nil {
		return fmt.Errorf("mom: response emitter unavailable")
	}
	routes := make([][]core.FederateHandle, len(responses))
	// Resolve every route before sending any part of a multi-report response.
	for i, r := range responses {
		var subject core.FederateHandle
		if b, ok := r.Params["HLAfederate"]; ok {
			if len(b) != 12 || binary.BigEndian.Uint32(b) != 8 {
				return fmt.Errorf("mom: malformed report subject")
			}
			subject = core.FederateHandle(binary.BigEndian.Uint64(b[4:]))
		}
		if recipients != nil {
			var err error
			routes[i], err = recipients(ctx, fed, subject, r.ClassName, service)
			if err != nil {
				return err
			}
		} else {
			if requester == 0 {
				return fmt.Errorf("mom: service report recipients unavailable")
			}
			routes[i] = []core.FederateHandle{requester}
		}
		routes[i] = slices.Clone(routes[i])
		slices.Sort(routes[i])
		routes[i] = slices.Compact(routes[i])
		if err := d.validateReportRecipients(fed, routes[i]); err != nil {
			return err
		}
	}
	for i, r := range responses {
		for _, recipient := range routes[i] {
			if err := emit(ctx, fed, recipient, r, fom, names); err != nil {
				return fmt.Errorf("mom emit %s: %w", r.ClassName, err)
			}
		}
	}
	return nil
}

func (d *Dispatcher) reportMOMFailure(ctx context.Context, dctx DispatchContext, class string, sender core.FederateHandle, params map[core.ParameterHandle][]byte, cause error) error {
	// Legacy callers without subscriber routing retain their former error-only
	// behavior. An unparseable subject cannot truthfully select a federate report.
	if dctx.Catalog.ReportRecipients == nil {
		return nil
	}
	var pe *parameterError
	parameter := errors.As(cause, &pe)
	class = strings.TrimPrefix(class, "HLAinteractionRoot.")
	if !parameter && class != ClassModifyAttributeState && class != ClassSetServiceReporting {
		return nil
	}
	target, err := decodeSwitchTarget(dctx, class, params)
	if err != nil {
		return nil
	}
	if _, ok := dctx.MOM.QueryFederateAttributes(dctx.Federation, target); !ok {
		return nil
	}
	r := ResponseInteraction{ClassName: ClassReportMOMException, Params: map[string][]byte{
		"HLAfederate": encodeHLAhandle(target), "HLAservice": encodeHLAunicodeString("HLAinteractionRoot." + class), "HLAexception": encodeHLAunicodeString(cause.Error()), "HLAparameterError": boolEncoding(parameter),
	}}
	return d.emitResponses(ctx, dctx.Federation, sender, []ResponseInteraction{r}, dctx.FOM, dctx.FOMNames, "")
}
func validServiceInvocation(invocation ServiceInvocation) bool {
	return !(invocation.Service == "" || invocation.Success && invocation.Exception != "" || !invocation.Success && (invocation.Exception == "" || len(invocation.ReturnedArguments) != 0))
}

func serviceInvocationReport(target core.FederateHandle, invocation ServiceInvocation, serial uint64) (ResponseInteraction, error) {
	if serial > math.MaxInt32 {
		return ResponseInteraction{}, ErrCounterOverflow
	}
	supplied, err := encodeStringList(invocation.SuppliedArguments)
	if err != nil {
		return ResponseInteraction{}, err
	}
	returned, err := encodeStringList(invocation.ReturnedArguments)
	if err != nil {
		return ResponseInteraction{}, err
	}
	return ResponseInteraction{ClassName: ClassReportServiceInvocation, Params: map[string][]byte{
		"HLAfederate": encodeHLAhandle(target), "HLAservice": encodeHLAunicodeString(invocation.Service), "HLAsuccessIndicator": boolEncoding(invocation.Success),
		"HLAsuppliedArguments": supplied, "HLAreturnedArguments": returned,
		"HLAexception": encodeHLAunicodeString(invocation.Exception), "HLAserialNumber": encodeHLAcount(uint32(serial)),
	}}, nil
}

func (m *Manager) finishServiceReport(fed core.FederationName, target core.FederateHandle, fs *federateSnapshot, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.fed[fed]; current != nil && current.federates[target] == fs {
		if success {
			fs.serviceSerial++
		} else {
			fs.serviceSerialKnown = false
		}
	}
}

func (d *Dispatcher) validateReportRecipients(fed core.FederationName, handles []core.FederateHandle) error {
	for _, h := range handles {
		if h == 0 {
			return fmt.Errorf("mom: invalid report recipient")
		}
		if d.mom != nil {
			if _, ok := d.mom.QueryFederateAttributes(fed, h); !ok {
				return core.ErrFederateNotJoined
			}
		}
	}
	return nil
}
