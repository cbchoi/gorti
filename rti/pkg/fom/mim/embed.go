// Package mim embeds the standard MIM and HLAstandardMIM XML modules and
// exposes them as parsed *model.FOM handles for downstream validators.
//
// Provenance: the two XML files in this directory (standard-mim.xml and
// hla-standard-mim.xml) are interim, hand-derived approximations of the
// IEEE-published artifacts (1516.2-2010 Annex B and 1516.1-2010 §4.13). They
// are orchestrator-vendored per docs/ORTHOGONALITY.md §2 (row added
// 2026-05-02); Agent B reads them via //go:embed but never modifies them.
// Each XML file carries its own <!-- PROVENANCE NOTICE --> comment block;
// canonical sourcing is tracked in https://github.com/cbchoi/gorti/issues/1
// and is scheduled for post-M1 work.
//
// Determinism: this package is pure data + parsing. StandardMIMBytes and
// HLAStandardMIMBytes return defensive copies of the embedded byte slices so
// no caller can mutate the singleton view across goroutines.
// StandardMIMHandle memoizes the parsed model so the lookup is O(1) after
// first call; the underlying FOM is immutable by construction (see
// rti/pkg/fom/model — exported fields, no setters).
package mim

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

//go:embed standard-mim.xml
var standardMIMXML []byte

//go:embed hla-standard-mim.xml
var hlaStandardMIMXML []byte

// StandardMIMBytes returns the embedded standard MIM XML (1516.2-2010 Annex
// B, interim per the package provenance notice) as a fresh byte slice on
// every call. Callers may mutate the returned slice without affecting the
// embedded source.
func StandardMIMBytes() []byte {
	return append([]byte(nil), standardMIMXML...)
}

// HLAStandardMIMBytes returns the embedded HLAstandardMIM XML
// (1516.1-2010 §4.13, interim) as a fresh byte slice on every call.
func HLAStandardMIMBytes() []byte {
	return append([]byte(nil), hlaStandardMIMXML...)
}

// standardMIM holds the lazily parsed standard MIM. Populated once via
// standardMIMOnce; subsequent reads observe the same *model.FOM pointer.
var (
	standardMIMOnce sync.Once
	standardMIMFOM  *model.FOM
	standardMIMErr  error
)

// StandardMIMHandle parses the embedded standard MIM XML once and returns
// the resulting *model.FOM. Subsequent calls return the same pointer.
//
// Returns a non-nil error only if the embedded XML cannot be parsed cleanly;
// in normal operation that indicates a corrupted vendored artifact and is
// treated as a build-time bug. Callers that wish to surface the failure
// without panicking should pass the error through their own diagnostic path
// (see rti/pkg/fom/parser/mim_merge.go for the parser-side handler).
func StandardMIMHandle() (*model.FOM, error) {
	standardMIMOnce.Do(func() {
		res, err := parser.Parse([]parser.Module{{
			Path: "rti/pkg/fom/mim/standard-mim.xml",
			XML:  standardMIMXML,
		}})
		if err != nil {
			standardMIMErr = fmt.Errorf("mim: parse embedded standard MIM: %w", err)
			return
		}
		if len(res.Diagnostics) != 0 {
			standardMIMErr = fmt.Errorf(
				"mim: embedded standard MIM produced %d diagnostics: %+v",
				len(res.Diagnostics), res.Diagnostics)
			return
		}
		fm, ok := res.FOM.(*model.FOM)
		if !ok || fm == nil {
			standardMIMErr = fmt.Errorf("mim: embedded standard MIM did not produce a *model.FOM (got %T)", res.FOM)
			return
		}
		standardMIMFOM = fm
	})
	return standardMIMFOM, standardMIMErr
}
