package core

// MemberIdentity follows one live membership across restore handle rebinding.
// Incarnation is never loaded from a saved execution or reused on rejoin.
type MemberIdentity struct {
	Federation  FederationName
	Generation  uint64
	Name        string
	Handle      FederateHandle
	Incarnation uint64
}

type AutomaticResignDefault interface {
	AutomaticResignAction() ResignAction
}
