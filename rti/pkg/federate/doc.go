// Package federate is gorti's exported Go federate SDK.
//
// It wraps the gRPC services rtid serves (FederationService,
// DeclarationService, ObjectService.SendInteraction, StreamService.Events)
// behind a small idiomatic Go API:
//
//	conn, err := federate.Connect(ctx, "127.0.0.1:8442")
//	defer conn.Close()
//
//	fed, err := conn.JoinFederation(ctx, federate.FederationSpec{
//	    Name:       "my-federation",
//	    FOMModules: []federate.FOMModule{ {Path: "fom.xml", XML: xmlBytes} },
//	}, "my-federate")
//	defer fed.Resign(ctx)
//
//	if err := fed.PublishInteractionClass(ctx, "Ping"); err != nil { ... }
//	if err := fed.SubscribeInteractionClass(ctx, "Pong"); err != nil { ... }
//
//	go func() {
//	    for ev := range fed.Events() {
//	        callbackErr, reportErr := fed.DispatchEvent(ctx, ev, func(admitted federate.Event) error {
//	            if rx, ok := admitted.(federate.ReceiveInteraction); ok { /* ... */ }
//	            return nil
//	        })
//	        if callbackErr != nil { /* handle application failure */ }
//	        if reportErr != nil && !errors.Is(reportErr, federate.ErrCallbackReceiptUnavailable) {
//	            /* handle admission deferral or retry the retained outcome report */
//	        }
//	    }
//	}()
//
//	for i := 1; i <= 10; i++ {
//	    payload := encoding.HLAinteger32BE(int32(i)).Encode()
//	    fed.SendInteraction(ctx, "Ping",
//	        map[string][]byte{"seq": payload}, nil)
//	}
//
// # Scope
//
// The SDK wraps interaction and object-class declaration, object instance
// registration and updates, typed interaction/object callbacks, time
// management, and selected DDM and lifecycle services. Other service groups
// continue to be added without exposing generated protobuf types.
//
// # Failed join recovery
//
// A FederationCreationResponseError means the server already created an
// execution but returned contradictory metadata. Its Creation field retains
// the generation, seed, and reported type; no automatic destruction is attempted.
// Inspect that evidence before explicitly calling Connection.DestroyFederation.
// This error wraps ErrRTIInternal, not ErrCouldNotCreateLogicalTimeFactory.
//
// A failed local join setup is compensated with the returned membership session.
// If that resignation fails, errors.As can retrieve a *JoinCleanupError. Retain
// it and call RetryResign with a bounded context before closing the Connection;
// the error privately retains the exact identity and capability for retries.
// No usable Federate is returned for the rejected join.
//
// # FOM-driven handles
//
// The wire surface refers to object and interaction classes, attributes, and
// parameters by integer handles, not strings. JoinFederation parses the FOM
// modules supplied in FederationSpec and builds local name→handle tables that
// mirror the Go-side rtid scheme. Public declaration and update methods take
// FOM names; handle resolution happens internally.
//
// # Time management
//
// FederationSpec.LogicalTimeImplementationName selects HLAfloat64Time or
// HLAinteger64Time; empty keeps Float64. Unsupported names fail locally.
// Creation/list results and Federate.LogicalTimeImplementationName expose the
// selected type. Absent legacy metadata means Float64 only. This selection API
// does not activate the server's Integer64 factory or establish full IEEE conformance.
// Integer64 services use the Value-suffixed methods and rti/pkg/logicaltime values,
// never float64. TimestampValue, TimeValue and SaveTimeValue are the exact callback
// fields; the older numeric fields remain Float64-only. A legacy Float64 +Inf
// callback has no finite TimeValue and retains +Inf in its original field.
// EnableOwnershipCallbackReceipts opts raw event consumers into terminal receipts.
// DispatchEvent acknowledges actual handler entry, including handler errors;
// manual consumers must call ReportOwnershipCallbackInvocation after invocation.
// Generic receipts require DispatchEvent for authenticated ENTRY before user code
// and outcome reporting afterward. Reading Events alone does not authorize
// SynchronizationPointAchieved on a strict server. ENTRY is sent only when join
// explicitly advertised support; legacy servers receive completion-only reports.
// TransportationChangeConfirmed also requires DispatchEvent to activate its
// scoped ticket before the application's confirmation handler.
// A synchronization registration confirmation can be deferred while this
// participant is saving or restoring. A manual dispatcher must retain the event,
// continue dispatching checkpoint controls, and retry the event after the terminal
// callback. A failed entry has not executed the handler; a failed outcome report
// must instead be retried with FlushCallbackInvocationReports.
//
// The SDK exposes time regulation, time-constrained delivery, lookahead
// changes, TAR/TARA/NER/NERA/NMRA/NMRAA/FQR requests, and typed
// TimeAdvanceGrant callbacks. Timestamped interactions are delivered before
// the grant that makes their logical time reachable. The go-tar-wait example
// demonstrates a request that remains pending until a peer federate advances.
package federate
