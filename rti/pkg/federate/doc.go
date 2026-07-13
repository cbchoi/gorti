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
// The SDK wraps interaction and object-class declaration, object instance
// registration and updates, typed interaction/object callbacks, time
// management, and selected DDM and lifecycle services. Other service groups
// continue to be added without exposing generated protobuf types.
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
// The SDK exposes time regulation, time-constrained delivery, lookahead
// changes, TAR/TARA/NER/NERA/NMRA/NMRAA/FQR requests, and typed
// TimeAdvanceGrant callbacks. Timestamped interactions are delivered before
// the grant that makes their logical time reachable. The go-tar-wait example
// demonstrates a request that remains pending until a peer federate advances.
package federate
