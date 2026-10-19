package savepoint

import "github.com/cbchoi/gorti/rti/internal/hla/core"

func (m *ManagerOf[T]) federateTypesFor(fed core.FederationName, handles []core.FederateHandle) []string {
	if m.opts.Roster == nil {
		return nil
	}
	byHandle := make(map[core.FederateHandle]string, len(handles))
	for _, member := range m.opts.Roster(fed) {
		byHandle[member.Handle] = member.FederateType
	}
	types := make([]string, len(handles))
	for i, handle := range handles {
		types[i] = byHandle[handle]
	}
	return types
}
