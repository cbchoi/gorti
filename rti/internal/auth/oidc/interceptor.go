// Package oidc — gRPC interceptor (M14 W4).
//
// UnaryServerInterceptor + StreamServerInterceptor that extract
// `authorization: Bearer <jwt>` from the gRPC metadata and validate
// against a *Verifier. On success, the verified subject is injected
// into the request context as auth.SubjectFromContext(ctx).

package oidc

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// subjectKey is the request-context key the interceptor uses to
// publish the verified subject. Service handlers may read it via
// SubjectFromContext.
type subjectKey struct{}

// SubjectFromContext returns the verified subject ("sub" claim) from
// a request context, or "" when no subject is attached.
func SubjectFromContext(ctx context.Context) string {
	v, _ := ctx.Value(subjectKey{}).(string)
	return v
}

// UnaryServerInterceptor returns a gRPC unary interceptor that
// validates the bearer token. Returns codes.Unauthenticated on any
// missing/invalid token.
func UnaryServerInterceptor(v *Verifier) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, err := authenticate(ctx, v)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns a gRPC stream interceptor for the
// same validation path.
func StreamServerInterceptor(v *Verifier) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := authenticate(ss.Context(), v)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedServerStream{ServerStream: ss, ctx: ctx})
	}
}

func authenticate(ctx context.Context, v *Verifier) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx, status.Error(codes.Unauthenticated, "oidc: no metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return ctx, status.Error(codes.Unauthenticated, "oidc: missing authorization metadata")
	}
	auth := values[0]
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ctx, status.Error(codes.Unauthenticated, "oidc: authorization header missing 'Bearer ' prefix")
	}
	token := strings.TrimPrefix(auth, prefix)
	claims, err := v.Verify(token)
	if err != nil {
		return ctx, status.Errorf(codes.Unauthenticated, "oidc: %v", err)
	}
	return context.WithValue(ctx, subjectKey{}, claims.Subject), nil
}

// wrappedServerStream lets us swap the request context after
// authentication so handlers see the subject-augmented ctx.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedServerStream) Context() context.Context { return w.ctx }
