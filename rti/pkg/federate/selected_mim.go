package federate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
)

// LoadMIMModule reads an explicitly selected local file for upload. The server
// receives the bytes and does not open Path. Callers authorize this file read.
func LoadMIMModule(path string) (*FOMModule, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("federate: read selected MIM: %w", err)
	}
	module := &FOMModule{Path: path, XML: source}
	if _, err := prepareMIMModule(module); err != nil {
		return nil, err
	}
	return module, nil
}

func prepareMIMModule(module *FOMModule) (*rtiv1.FOMModule, error) {
	if module == nil {
		return nil, nil
	}
	if strings.TrimSpace(module.Path) == "" {
		return nil, fmt.Errorf("federate: explicit MIM requires a nonempty designator")
	}
	if module.Path == "HLAstandardMIM" {
		return nil, fmt.Errorf("federate: explicit MIM designator must not be HLAstandardMIM")
	}
	source := append([]byte(nil), module.XML...)
	if _, err := mim.ParseSelected(source); err != nil {
		return nil, fmt.Errorf("federate: selected MIM: %w", err)
	}
	return &rtiv1.FOMModule{Path: module.Path, Xml: source}, nil
}

func createWireVersion(module *rtiv1.FOMModule) rtiv1.WireVersion {
	if module != nil {
		return rtiv1.WireVersion_WIRE_VERSION_V2
	}
	return rtiv1.WireVersion_WIRE_VERSION_V1
}

func (c *Connection) cachedMIM(name string) *FOMModule {
	c.generationMu.RLock()
	defer c.generationMu.RUnlock()
	module := c.selectedMIMs[name]
	if module == nil {
		return nil
	}
	return &FOMModule{Path: module.Path, XML: append([]byte(nil), module.XML...)}
}

func (c *Connection) rememberMIM(name string, module *rtiv1.FOMModule) {
	c.generationMu.Lock()
	defer c.generationMu.Unlock()
	if module == nil {
		delete(c.selectedMIMs, name)
		return
	}
	if c.selectedMIMs == nil {
		c.selectedMIMs = make(map[string]*FOMModule)
	}
	c.selectedMIMs[name] = &FOMModule{Path: module.GetPath(), XML: append([]byte(nil), module.GetXml()...)}
}

func confirmSelectedMIM(selected *rtiv1.FOMModule, view *rtiv1.FOMView) error {
	actual := view.GetMimModule()
	if actual == nil || actual.GetPath() != selected.GetPath() || !bytes.Equal(actual.GetXml(), selected.GetXml()) {
		return fmt.Errorf("federate: server did not confirm the selected MIM")
	}
	return nil
}

// SelectedMIM returns the authoritative explicit selection; nil means the
// embedded compatibility path. Source bytes are independent of SDK state.
func (f *Federate) SelectedMIM(ctx context.Context) (*FOMModule, error) {
	if f == nil || f.conn == nil || f.conn.fed == nil {
		return nil, ErrNotJoined
	}
	view, err := f.conn.fed.GetFOMView(f.sessionContext(ctx), &rtiv1.GetFOMViewRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle()})
	if err != nil {
		return nil, wrapStatusErr(err)
	}
	module := view.GetMimModule()
	if module == nil {
		return nil, nil
	}
	return &FOMModule{Path: module.GetPath(), XML: append([]byte(nil), module.GetXml()...)}, nil
}
