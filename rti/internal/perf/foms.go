package perf

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// permissiveFOMRepo is a hand-rolled core.FOMRepository that accepts any
// Load call and resolves every name to handle 1. Mirrors the same shim
// in rti/cmd/rtid/pingpong.go::pingpongFOMRepo (pulled in here so the
// perf package has no dependency on cmd/rtid).
type permissiveFOMRepo struct{}

func newPermissiveFOMRepo() *permissiveFOMRepo { return &permissiveFOMRepo{} }

func (r *permissiveFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}

func (r *permissiveFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}

type permissiveFOMHandle struct{}

func (permissiveFOMHandle) IsValid() bool { return true }
func (permissiveFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}
