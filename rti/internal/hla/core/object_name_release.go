package core

import "context"

// MultipleObjectNameReleaser atomically releases a caller-owned set of names.
// A validation failure leaves every reservation in the batch unchanged.
type MultipleObjectNameReleaser interface {
	ReleaseMultipleObjectInstanceNames(context.Context, FederationName, FederateHandle, []string) error
}
