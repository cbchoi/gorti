package federate

import (
	"context"

	"google.golang.org/grpc/metadata"
)

func (f *Federate) sessionContext(ctx context.Context) context.Context {
	if f == nil || len(f.sessionToken) == 0 {
		return ctx
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set("gorti-session-token-bin", string(f.sessionToken))
	return metadata.NewOutgoingContext(ctx, md)
}
