package grpc

import (
	"context"
	"unicode"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func joinHost(ctx context.Context) (string, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("gorti-federate-host-bin")
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 1024 || !utf8.ValidString(values[0]) {
		return "", status.Error(codes.InvalidArgument, "federate host must be one nonempty UTF-8 value of at most 1024 bytes")
	}
	for _, r := range values[0] {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", status.Error(codes.InvalidArgument, "federate host contains whitespace or control characters")
		}
	}
	return values[0], nil
}
