package session

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CurrentCallerIdentity resolves a stable session to its current restored handle.
// The caller must retain checkpoint/lifecycle admission for the complete use.
func (m *Manager) CurrentCallerIdentity(ctx context.Context) (core.MemberIdentity, error) {
	saved, err := m.CallerIdentity(ctx)
	if err != nil {
		return core.MemberIdentity{}, err
	}
	handle := saved.Handle
	if roster, ok := m.opts.Membership.(interface {
		ListMembers(core.FederationName) []core.FederationMember
	}); ok {
		handle = 0
		for _, member := range roster.ListMembers(saved.Federation) {
			if member.Name == saved.Name {
				handle = member.Handle
				break
			}
		}
	}
	current, err := m.opts.Membership.MemberIdentity(saved.Federation, handle)
	if err != nil || current.Federation != saved.Federation || current.Name != saved.Name || current.Generation != saved.Generation || current.Incarnation != saved.Incarnation {
		return core.MemberIdentity{}, status.Error(codes.FailedPrecondition, "session membership is no longer active")
	}
	if err := m.Authenticate(ctx, current.Federation, current.Handle); err != nil {
		return core.MemberIdentity{}, err
	}
	return current, nil
}
