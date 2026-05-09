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
//	        if rx, ok := ev.(federate.ReceiveInteraction); ok { /* ... */ }
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
// Cut-3 scope: interactions only — Publish/Subscribe interaction class,
// SendInteraction, ReceiveInteraction event delivery via the Events
// channel. Object-class operations (RegisterObjectInstance,
// UpdateAttributeValues), time management (NER/TAR), sync points,
// ownership, save/restore, DDM, and MOM are all reachable from the
// underlying genproto stubs but not yet wrapped here. Add as needed.
//
// # FOM-driven handles
//
// The wire surface refers to interaction classes and parameters by
// integer handles, not strings. JoinFederation parses the FOM modules
// supplied in FederationSpec and builds local name→handle tables that
// mirror the Go-side rtid scheme: 1-based handles, sorted by name.
// All public methods take string class / parameter names; handle
// resolution happens internally.
//
// # Time management
//
// Cut-3 rtid does not yet wire TimeService (timeService=nil in the
// gRPC server). Cross-process Time RPCs return Unimplemented. This
// SDK therefore exposes no NER/TAR/grant API yet; the SendInteraction
// call accepts an optional timestamp that flows through to the wire
// but federates can't currently coordinate via LBTS cross-process.
package federate
