// Package buildinfo holds release identifiers injected at link time.
//
// The release pipeline (.goreleaser.yaml) overrides Version, Commit,
// and Date via -ldflags '-X github.com/cbchoi/gorti/rti/internal/buildinfo.Version=...'.
// Source builds (go build, go run) keep the "dev" defaults so the
// binary is still self-identifying without a tagged release.
package buildinfo

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns "vX.Y.Z (commit abcdef0, built 2026-05-08)" for
// --version output and AdminService Status responses.
func String() string {
	return Version + " (commit " + Commit + ", built " + Date + ")"
}
