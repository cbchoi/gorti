# DDM FOM Follow-Up Evidence

Date: 2026-09-19. This records bounded implementation checks, not a claim of
OMT or IEEE conformance.

## Evidence Move

Moved `rti/pkg/fom/parser/dimension_reference_evidence.md` to
`engineering/verification/ieee1516-2010-ddm-fom-evidence.md` using an
`apply_patch` move directive. Contents are byte-for-byte unchanged:
SHA256 `413a232b3f5146a627b1022140e80cfe167c6cd7ab697b8e49f3a62fd582b42f`
before and after the move. The original note describes the earlier checkpoint;
the new checks below supersede its duplicate-declaration limitation and its
previous composition/race-test blockers.

## Duplicate Declaration RED

New production regression test:
`rti/pkg/fom/parser/dimension_merge_production_test.go`,
`TestDuplicateDimensionsProductionRegionContext`.
It loads two identical modules through the production composition repository,
creates a region through the real DDM manager, and prepares an object binding.
No composition, core, or DDM source files were edited for this test.

```powershell
$env:GOCACHE = Join-Path $env:TEMP 'gorti-fom-dimensions-go-cache'
go test ./rti/pkg/fom/parser ./rti/pkg/fom/mim -run 'Test(DuplicateDimensionsProductionRegionContext|DimensionDeclarations)' -count=1 -v
```

Actual pre-fix output, exit 1:

```text
production dimension handles: attribute=[3] DDM=4
identical modular dimension declarations broke region context:
ddm: prepare region 1 for attribute 1 of class 5 in "duplicate-dimensions":
region dimensions do not match the attribute context
```

Parser tests also failed because identical/equivalent definitions were retained
twice, while conflicting upper bounds and normalization keys were accepted.
MIM merge tests failed on the same cases and on duplicate declarations when
either merge input was nil.

## Fix And GREEN

Only parser/MIM implementation code changed for the duplicate-definition fix:

- Parser merges dimensions by name after numeric conversion, retaining one
  compatible modeled definition. Conflicts report `FOM-015` with the incoming
  module path.
- MIM merge likewise deduplicates compatible declarations and reports
  `FOM-101` for conflicts, including nil-side merges.
- Compatibility compares the modeled name, upper bound, and normalization key.
  This is not general XML schema or complete dimension-metadata validation.

The same focused command passed, exit 0:

```text
production dimension handles: attribute=[3] DDM=3
--- PASS: TestDuplicateDimensionsProductionRegionContext (0.00s)
ok   github.com/cbchoi/gorti/rti/pkg/fom/parser 0.139s
ok   github.com/cbchoi/gorti/rti/pkg/fom/mim    0.450s
```

All identical, equivalent lexical, upper-bound conflict, normalization conflict,
and MIM nil-side regression cases passed.

Windows, Go 1.26.5, exit 0:

```text
go test ./rti/pkg/fom/... ./rti/internal/application/composition ./rti/tests/unit/ddm ./rti/tests/integration/ddm_registration ./tests/spec/M1/... -count=1
```

Results: MIM 0.840s, model 0.762s, parser 0.268s, composition 0.260s,
DDM unit 0.820s, DDM integration 9.014s, M1 0.725s.

## Ubuntu 24.04 Race Checks

Runtime: Go 1.22.2, cgo enabled. The first offline run used the existing Linux
module cache; all targets except integration passed, but integration setup
could not find cached `github.com/BurntSushi/toml v1.6.0`.

The successful rerun used the existing populated Windows module cache and the
Linux build cache, with network module lookup disabled:

```powershell
wsl.exe -d Ubuntu-24.04 --cd /mnt/d/workspace/gorti/gorti --exec env GOCACHE=/home/cbchoi/.cache/go-build GOMODCACHE=/mnt/c/Users/cbchoi/go/pkg/mod GOPROXY=off CGO_ENABLED=1 go test -race -count=1 ./rti/pkg/fom/... ./rti/internal/application/composition ./rti/internal/hla/ddm ./rti/tests/unit/ddm ./rti/tests/integration/ddm_registration
```

Exit 0, no race report:

```text
ok github.com/cbchoi/gorti/rti/pkg/fom/mim                      1.192s
ok github.com/cbchoi/gorti/rti/pkg/fom/model                    1.014s
ok github.com/cbchoi/gorti/rti/pkg/fom/parser                   1.280s
ok github.com/cbchoi/gorti/rti/internal/application/composition 1.075s
ok github.com/cbchoi/gorti/rti/internal/hla/ddm                 1.011s
ok github.com/cbchoi/gorti/rti/tests/unit/ddm                   1.109s
ok github.com/cbchoi/gorti/rti/tests/integration/ddm_registration 9.756s
```

`go vet` on FOM, composition, DDM, and DDM unit packages passed. Formatting and
the scoped whitespace check passed.

## Remaining Read-Only Review Finding

Final integration update: the finding below was subsequently corrected in
`ddm/state.go` by using the legacy effective-upper-bound policy consistently.
Omitted/model-sentinel zero maps to MaxUint64; explicit XML zero is still
rejected by the parser. Added regression cases and the final full RTI race run
pass. The original review record is retained below; see the
[integrated report](ieee1516-2010-ddm-registration.md) for final evidence.

P2: `rti/internal/hla/ddm/manager.go:359` compares against the stored zero upper
bound, while `CreateRegion` at lines 290-293 expands that same zero to MaxUint64.
`state.go:119` now retains the raw zero. This is observable with both an omitted
XML upperBound, which the parser accepts, and a direct zero-valued model literal.
Explicit XML upperBound zero is rejected by the parser and is a different case.

A temporary Go test overlay reproduced the inconsistency through production
composition and DDM without adding or editing core/DDM files:

```text
created bounds: {Lower:0 Upper:18446744073709551615}
newly created bounds fail roundtrip: ... range bounds are invalid for the dimension
narrowing newly created range to [0,1) fails: ... range bounds are invalid for the dimension
```

Both `parsed_omitted_bound` and `direct_zero_literal` subtests failed as expected
in the review probe. Suggested correction for the DDM owner: establish one
effective-bound policy shared by creation and validation; distinguish an
omitted bound from a literal zero if those inputs require different behavior.

Probe files remain outside the repository under the Windows temporary directory:
`gorti-fom-ddm-review-probe_test.go` and `gorti-fom-ddm-review-overlay.json`.
The probe is not included in normal package or race-test runs.

```powershell
go test -overlay C:/Users/cbchoi/AppData/Local/Temp/gorti-fom-ddm-review-overlay.json ./rti/pkg/fom/parser -run TestDDMReadOnlyReviewOmittedUpperBound -count=1 -v
```

No additional actionable aliasing or lock-release defect was found in the
reviewed preparation path. Bindings and dimension sets are copied before use,
validation failures unlock via defer, and successful preparations retain the
lock through Commit until serialized Release. Existing tests exercise copy
isolation and lock retention; the targeted race runs above passed. This is
bounded review coverage, not proof that all concurrency paths are correct.
