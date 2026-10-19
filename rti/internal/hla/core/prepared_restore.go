package core

// PreparedRestore retains a preparation lease until commit or cancellation.
// Commit errors are observable; Release is idempotent and always called.
type PreparedRestore interface {
	Commit() error
	Release()
}
