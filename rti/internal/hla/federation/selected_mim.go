package federation

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Called with exclusive lifecycle/operation admission, before FOM preparation,
// journal append, handle allocation or membership changes. Read the repository
// to include a restored selection rather than a pre-restore creation handle.
func (m *Manager) validateExpectedMIM(ctx context.Context, federation core.FederationName, expected *core.FOMModule) error {
	if strings.TrimSpace(expected.Path) == "" || len(expected.XML) == 0 {
		return fmt.Errorf("federation: expected MIM requires designator and XML")
	}
	handle, err := m.opts.FOMs.Get(ctx, federation)
	if err != nil {
		return fmt.Errorf("federation: expected MIM lookup: %w", err)
	}
	selection, ok := handle.(core.FOMMIMSelection)
	if !ok {
		return fmt.Errorf("federation: expected MIM selection is unavailable")
	}
	actual := selection.SelectedMIM()
	if actual == nil || actual.Path != expected.Path || !bytes.Equal(actual.XML, expected.XML) {
		return fmt.Errorf("federation: selected MIM does not match join expectation")
	}
	return nil
}
