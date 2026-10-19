package core

import "errors"

// ErrDDMJournalUnsupported means the selected audit format cannot preserve the
// operation's DDM state for replay. The frozen protobuf object-registration
// record has no region bindings, so its adapter rejects scoped registrations.
// This restriction does not apply to the HLA core profile without an audit log
// or to DDM snapshots, which use a separate state representation.
var ErrDDMJournalUnsupported = errors.New("DDM operation is not supported by the event journal")
