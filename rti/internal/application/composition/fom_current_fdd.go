package composition

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/exporter"
)

// CurrentFDD serializes one captured, committed immutable model. Create,
// additive join and restore all publish that model through byFedKey; candidate
// preparation never changes this result. Source modules and stable handle
// tables are not an export source, and serialization holds no repository lock.
func (r *Repository) CurrentFDD(ctx context.Context, fed core.FederationName) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	handle, err := r.Get(ctx, fed)
	if err != nil {
		return nil, err
	}
	h, ok := handle.(*fomHandle)
	if !ok || !h.IsValid() {
		return nil, fmt.Errorf("current FDD: %w: no authoritative model", exporter.ErrUnsupported)
	}
	data, err := exporter.Marshal(h.fom)
	if err != nil {
		return nil, fmt.Errorf("current FDD for %q: %w", fed, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}
