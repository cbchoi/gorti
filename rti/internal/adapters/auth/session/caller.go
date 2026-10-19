package session

import (
	"context"
	"crypto/subtle"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CallerIdentity identifies the session, not a current roster handle. The caller
// must retain lifecycle admission and revalidate this identity before use;
// restore can rebind its handle, while resign/rejoin replaces its incarnation.
func (m *Manager) CallerIdentity(ctx context.Context) (core.MemberIdentity, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return core.MemberIdentity{}, err
	}
	if err := ctx.Err(); err != nil {
		return core.MemberIdentity{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var found *entry
	if !m.closed {
		for _, candidate := range m.entries {
			if subtle.ConstantTimeCompare(candidate.token[:], token) == 1 {
				if found != nil {
					return core.MemberIdentity{}, status.Error(codes.PermissionDenied, "ambiguous session identity")
				}
				found = candidate
			}
		}
	}
	if found == nil {
		return core.MemberIdentity{}, status.Error(codes.PermissionDenied, "session is no longer active")
	}
	return found.identity, nil
}
