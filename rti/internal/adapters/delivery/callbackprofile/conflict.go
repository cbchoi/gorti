//go:build callback_async && callback_sync

package callbackprofile

// Select one callback transport profile, never both.
var _ = callback_async_and_callback_sync_are_mutually_exclusive
