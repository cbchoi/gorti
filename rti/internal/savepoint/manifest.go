package savepoint

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// Manifest is the JSON header at the front of every save bundle.
//
// Cut-2 scope (FR-SR-4): the manifest carries the federation identity,
// the requested label + save-time, the deterministic federate list,
// and the per-manager state snapshots. M13 thread C
// (docs/srs.md §10.4) extends the cut-1 design with
// ManagerSnapshots — proto-encoded byte slices keyed by manager
// name ("sync", "ownership", "mom", "ddm"). Snapshots are ADDITIVE
// to the event-log slice: restore Unmarshals each snapshot on the
// matching manager, then replays the event-log slice for full
// replay-determinism on top.
//
// On-disk layout: ManagerSnapshots is JSON-serialized with each
// value as a base64-encoded string. JSON encoding chosen so manifests
// stay human-readable — the values are short proto bytes, base64 is
// trivial, and we avoid a second framing layer for cut-2.
//
// Wire-version compatibility: old bundles that pre-date M13 omit
// ManagerSnapshots entirely. ReadBundle silently treats absent /
// nil maps as "no per-manager state to restore"; the restore flow
// then falls back to the event-log replay path (the only path
// available pre-M13). New bundles can therefore restore on a
// pre-M13 reader without crashing on the new field.
type Manifest struct {
	// Version is the bundle format version. Bumped when the on-disk
	// layout changes incompatibly. Cut-1 = 1.
	Version uint32 `json:"version"`

	// Federation is the federation name the bundle was saved against.
	Federation core.FederationName `json:"federation"`

	// Label is the user-supplied save label (FR-SR-1).
	Label string `json:"label"`

	// SaveTime is the optional logical save-point time (FR-SR-1). nil
	// means "save at current synchronization point".
	SaveTime *core.LogicalTime `json:"save_time,omitempty"`

	// Federates is the deterministic federate-handle list captured at
	// save-request time. Restore uses this to drive
	// initiateFederateRestore broadcast in the same order.
	Federates []core.FederateHandle `json:"federates"`

	// EventLogBytes is the byte length of the event-log slice that
	// follows the manifest in the bundle. Zero when no slice is
	// captured (cut-1 default — see writeBundle in manager.go).
	EventLogBytes uint64 `json:"event_log_bytes"`

	// ManagerSnapshots holds proto-encoded per-manager state slices
	// keyed by manager name. M13 thread C — restoring an old (pre-M13)
	// bundle leaves this nil, which is the documented "no
	// per-manager state to restore; fall back to event-log replay"
	// signal.
	ManagerSnapshots map[string][]byte `json:"manager_snapshots,omitempty"`
}

// Manager-snapshot key constants for the ManagerSnapshots map. M13
// thread C (docs/srs.md §10.4): each Marshalable Manager is
// identified by its package name.
const (
	ManagerSnapshotKeySync      = "sync"
	ManagerSnapshotKeyOwnership = "ownership"
	ManagerSnapshotKeyMOM       = "mom"
	ManagerSnapshotKeyDDM       = "ddm"
)

// Bundle layout (cut-1):
//
//	[ 8 bytes ] uint64 manifestLen (little-endian)
//	[ N bytes ] JSON manifest (length = manifestLen)
//	[ 8 bytes ] uint64 eventLogLen (little-endian; matches manifest.EventLogBytes)
//	[ M bytes ] raw event-log slice (length = eventLogLen)
//
// The 8-byte length prefixes are the framing that lets ReadBundle
// stream the two regions without tar/gz overhead. The format is
// intentionally trivial: we trade compression/checksumming for
// maximum debuggability at this milestone. A future cut may wrap the
// concatenation in tar.gz per FR-SR-4's "sealed bundle" wording.
//
// The cut-1 default writes manifestLen + manifest + 0 + (no slice),
// because OpenReader on a record-oriented EventLog can't recover raw
// bytes without backdoor access to the underlying *os.File. The W1
// manager nevertheless emits the framing so a future W2 patch can fill
// in the slice without changing the bundle layout.

const bundleFormatVersion uint32 = 1

// ErrBundleCorrupt indicates the on-disk bundle could not be parsed.
// Wraps core.ErrSaveBundleCorrupt for callers that want sentinel
// matching across the package boundary.
var ErrBundleCorrupt = fmt.Errorf("%w: invalid bundle layout", core.ErrSaveBundleCorrupt)

// WriteBundle writes a manifest + event-log slice into w. eventLog may
// be nil/empty; in that case EventLogBytes is set to 0 in the manifest
// before serialization.
func WriteBundle(w io.Writer, manifest Manifest, eventLog []byte) error {
	if manifest.Version == 0 {
		manifest.Version = bundleFormatVersion
	}
	manifest.EventLogBytes = uint64(len(eventLog))

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("savepoint: marshal manifest: %w", err)
	}

	var lenBuf [8]byte
	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(manifestBytes)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("savepoint: write manifest len: %w", err)
	}
	if _, err := w.Write(manifestBytes); err != nil {
		return fmt.Errorf("savepoint: write manifest body: %w", err)
	}

	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(eventLog)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("savepoint: write event-log len: %w", err)
	}
	if len(eventLog) > 0 {
		if _, err := w.Write(eventLog); err != nil {
			return fmt.Errorf("savepoint: write event-log slice: %w", err)
		}
	}
	return nil
}

// ReadBundle parses a bundle from r and returns the manifest + event-log
// slice. Callers MUST close r themselves.
//
// Returns wrapped core.ErrSaveBundleCorrupt for any short read, JSON
// parse failure, or version mismatch.
func ReadBundle(r io.Reader) (Manifest, []byte, error) {
	manifest, err := readManifest(r)
	if err != nil {
		return Manifest{}, nil, err
	}
	eventLog, err := readEventLog(r, manifest.EventLogBytes)
	if err != nil {
		return Manifest{}, nil, err
	}
	return manifest, eventLog, nil
}

// readManifest reads the length-prefixed JSON manifest off r and
// validates its version. Returns wrapped ErrBundleCorrupt on any
// error.
func readManifest(r io.Reader) (Manifest, error) {
	manifestLen, err := readLenPrefix(r, "header")
	if err != nil {
		return Manifest{}, err
	}
	if manifestLen == 0 || manifestLen > 16*1024*1024 {
		return Manifest{}, fmt.Errorf("%w: manifest length %d out of range", ErrBundleCorrupt, manifestLen)
	}
	manifestBytes := make([]byte, manifestLen)
	if _, err := io.ReadFull(r, manifestBytes); err != nil {
		return Manifest{}, fmt.Errorf("%w: manifest body truncated: %v", ErrBundleCorrupt, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: manifest unmarshal: %v", ErrBundleCorrupt, err)
	}
	if manifest.Version != bundleFormatVersion {
		return Manifest{}, fmt.Errorf("%w: manifest version %d != supported %d",
			ErrBundleCorrupt, manifest.Version, bundleFormatVersion)
	}
	return manifest, nil
}

// readEventLog reads the length-prefixed event-log slice off r and
// confirms it matches the manifest's recorded byte count.
func readEventLog(r io.Reader, manifestBytes uint64) ([]byte, error) {
	eventLogLen, err := readLenPrefix(r, "event-log header")
	if err != nil {
		return nil, err
	}
	if eventLogLen != manifestBytes {
		return nil, fmt.Errorf("%w: event-log length mismatch (header=%d manifest=%d)",
			ErrBundleCorrupt, eventLogLen, manifestBytes)
	}
	if eventLogLen > 1024*1024*1024 {
		return nil, fmt.Errorf("%w: event-log length %d exceeds 1 GiB cap",
			ErrBundleCorrupt, eventLogLen)
	}
	if eventLogLen == 0 {
		return nil, nil
	}
	out := make([]byte, eventLogLen)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, fmt.Errorf("%w: event-log slice truncated: %v", ErrBundleCorrupt, err)
	}
	return out, nil
}

// readLenPrefix reads the next 8-byte little-endian length prefix from
// r. context is used in the error message to help bisect bundle corruption.
func readLenPrefix(r io.Reader, context string) (uint64, error) {
	var lenBuf [8]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, fmt.Errorf("%w: %s truncated", ErrBundleCorrupt, context)
		}
		return 0, fmt.Errorf("savepoint: read %s len: %w", context, err)
	}
	return binary.LittleEndian.Uint64(lenBuf[:]), nil
}
