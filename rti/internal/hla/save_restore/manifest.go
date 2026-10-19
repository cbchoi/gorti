package savepoint

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Manifest is the JSON header at the front of every save bundle.
//
// It contains provenance, participant identities and adapter-owned snapshot
// bytes. JSON encodes the byte slices as base64. ReadBundle retains support for
// legacy framing and optional event-log bytes; the production restore path is
// snapshot-based and does not replay those bytes. Production requires every
// configured state key, so readable legacy partial bundles can still be rejected
// as insufficient for restoring the live runtime.
type ManifestOf[T core.LogicalTimeScalar] core.CheckpointManifestOf[T]

// Manager-snapshot key constants identify each service's persistence adapter.

const (
	ManagerSnapshotKeySync        = "sync"
	ManagerSnapshotKeyOwnership   = "ownership"
	ManagerSnapshotKeyMOM         = "mom"
	ManagerSnapshotKeyDDM         = "ddm"
	ManagerSnapshotKeyObject      = "object"
	ManagerSnapshotKeyDeclaration = "declaration"
	ManagerSnapshotKeyTime        = "time"
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

const (
	bundleFormatVersionV1 uint32 = 1
	bundleFormatVersionV2 uint32 = 2
	maxManifestBytes             = 16 * 1024 * 1024
	maxEventLogBytes             = 1024 * 1024 * 1024
)

func manifestVersion(generation *uint64) uint32 {
	if generation == nil {
		return bundleFormatVersionV1
	}
	return bundleFormatVersionV2
}

// ErrBundleCorrupt indicates the on-disk bundle could not be parsed.
// Wraps core.ErrSaveBundleCorrupt for callers that want sentinel
// matching across the package boundary.
var ErrBundleCorrupt = fmt.Errorf("%w: invalid bundle layout", core.ErrSaveBundleCorrupt)

// WriteBundle writes a manifest + event-log slice into w. eventLog may
// be nil/empty; in that case EventLogBytes is set to 0 in the manifest
// before serialization.
func WriteBundleOf[T core.LogicalTimeScalar](w io.Writer, manifest ManifestOf[T], eventLog []byte) error {
	if manifest.Version == 0 {
		manifest.Version = bundleFormatVersionV1
	}
	if manifest.Version != bundleFormatVersionV1 && manifest.Version != bundleFormatVersionV2 {
		return fmt.Errorf("savepoint: unsupported manifest version %d", manifest.Version)
	}
	if len(eventLog) > maxEventLogBytes {
		return fmt.Errorf("savepoint: event log exceeds bundle reader limit")
	}
	manifest.EventLogBytes = uint64(len(eventLog))

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("savepoint: marshal manifest: %w", err)
	}
	if len(manifestBytes) > maxManifestBytes {
		return fmt.Errorf("savepoint: manifest length %d exceeds bundle reader limit", len(manifestBytes))
	}

	var lenBuf [8]byte
	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(manifestBytes)))
	if err := writeBundlePart(w, lenBuf[:]); err != nil {
		return fmt.Errorf("savepoint: write manifest len: %w", err)
	}
	if err := writeBundlePart(w, manifestBytes); err != nil {
		return fmt.Errorf("savepoint: write manifest body: %w", err)
	}

	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(eventLog)))
	if err := writeBundlePart(w, lenBuf[:]); err != nil {
		return fmt.Errorf("savepoint: write event-log len: %w", err)
	}
	if len(eventLog) > 0 {
		if err := writeBundlePart(w, eventLog); err != nil {
			return fmt.Errorf("savepoint: write event-log slice: %w", err)
		}
	}
	return nil
}

func writeBundlePart(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

// ReadBundle parses a bundle from r and returns the manifest + event-log
// slice. Callers MUST close r themselves.
//
// Returns wrapped core.ErrSaveBundleCorrupt for any short read, JSON
// parse failure, or version mismatch.
func ReadBundleOf[T core.LogicalTimeScalar](r io.Reader) (ManifestOf[T], []byte, error) {
	manifest, err := readManifestOf[T](r)
	if err != nil {
		return ManifestOf[T]{}, nil, err
	}
	eventLog, err := readEventLog(r, manifest.EventLogBytes)
	if err != nil {
		return ManifestOf[T]{}, nil, err
	}
	return manifest, eventLog, nil
}

// readManifest reads the length-prefixed JSON manifest off r and
// validates its version. Returns wrapped ErrBundleCorrupt on any
// error.
func readManifestOf[T core.LogicalTimeScalar](r io.Reader) (ManifestOf[T], error) {
	manifestLen, err := readLenPrefix(r, "header")
	if err != nil {
		return ManifestOf[T]{}, err
	}
	if manifestLen == 0 || manifestLen > maxManifestBytes {
		return ManifestOf[T]{}, fmt.Errorf("%w: manifest length %d out of range", ErrBundleCorrupt, manifestLen)
	}
	manifestBytes := make([]byte, manifestLen)
	if _, err := io.ReadFull(r, manifestBytes); err != nil {
		return ManifestOf[T]{}, fmt.Errorf("%w: manifest body truncated: %v", ErrBundleCorrupt, err)
	}
	var manifest ManifestOf[T]
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return ManifestOf[T]{}, fmt.Errorf("%w: manifest unmarshal: %v", ErrBundleCorrupt, err)
	}
	if manifest.Version != bundleFormatVersionV1 && manifest.Version != bundleFormatVersionV2 {
		return ManifestOf[T]{}, fmt.Errorf("%w: unsupported manifest version %d",
			ErrBundleCorrupt, manifest.Version)
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
	if eventLogLen > maxEventLogBytes {
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
