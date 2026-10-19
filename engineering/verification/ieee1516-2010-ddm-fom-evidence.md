# Bounded Dimension Reference Preservation

Date: 2026-09-19. Runtime: `go version go1.26.5 windows/amd64`.
Scope: `rti/pkg/fom/model`, `rti/pkg/fom/parser`, `rti/pkg/fom/mim` only.
Existing unrelated worktree changes were retained. This is not evidence of
full OMT or IEEE conformance.

## Official Inputs Inspected

Directory:
`C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee1516-audit-20260918/interface/1516.1-2010_downloads`.

- `IEEE1516-FDD-2010.xsd`, SHA256
  `8846b46dc0ee8ee1299c0e854e65630cf4de18375f6ce1200abd904e37f5a3c1`.
- `HLAstandardMIM.xml`, SHA256
  `839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023`.

Schema locations: lines 7-13 define the global dimension key and attribute /
interaction reference selectors; 1705-1716 and 1788-1799 define repeated
`dimensions/dimension` references; 2485-2489 define their simple text type;
1871-1882 declare optional `upperBound` with `xs:positiveInteger` content.
Consequently, an explicit zero is invalid; an omitted bound is distinct.
The model's additional uint64 limit is an implementation constraint.

Official MIM examples: lines 74-82 associate `HLAfederateHandle` with
`HLAfederate`; 1503-1507 associate `HLAreportServiceInvocation` with
`HLAfederate` and `HLAserviceGroup`; 2338-2351 declare those dimensions.

## RED

The three owned package suites passed before adding tests. Adding the tests
before the fields produced the expected model compile failure:

```text
unknown field Dimensions in struct literal of type Attribute
unknown field Dimensions in struct literal of type InteractionClass
```

That first command also encountered default Go cache permissions for the other
packages. Subsequent runs used a dedicated temporary cache:

```powershell
$env:GOCACHE = Join-Path $env:TEMP 'gorti-fom-dimensions-go-cache'
go test ./rti/pkg/fom/model ./rti/pkg/fom/parser ./rti/pkg/fom/mim -run TestDimensionReferences -count=1
```

After adding only the requested public fields, that command failed in all
three packages (exit 1). Actual failure excerpts:

```text
attribute changed through an alias: ... Dimensions:[input mutation X]
dimensions = [], want [Y X]
inherited model reference lost: ... Dimensions:[]
conflicting dimension sets accepted
container extension lost dimensions: []
unresolved reference "Missing": error=<nil>
MIM references dropped: attribute=false interaction=false
MIM dimension definitions = [], want [{Name:HLAfederate ...} ...]
dimension redefinition accepted: []
```

The added numeric-bound tests were also run before the implementation change:

```powershell
go test ./rti/pkg/fom/parser -run TestDimensionUpperBounds -count=1
```

Exit 1. `positive_sign` returned 0 instead of 100. `zero`, `negative`,
`empty`, `blank`, `units`, `decimal`, `exponent`, `text`, and `overflow`
all failed with `invalid upperBound accepted`.

## GREEN

```powershell
$env:GOCACHE = Join-Path $env:TEMP 'gorti-fom-dimensions-go-cache'
$env:GORTI_IEEE2010_SUPPLEMENTAL = 'C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee1516-audit-20260918'
go test ./rti/pkg/fom/model ./rti/pkg/fom/parser ./rti/pkg/fom/mim -run TestDimension -count=1 -v
```

Exit 0: all dimension tests and subtests passed, including the external
official MIM, embedded MIM, inherited attribute lookup through ParentKey,
both module orders, repeated modular copies, unordered-set compatibility,
container extensions, unresolved references, constructor/accessor/clone
isolation, MIM merging, and valid/invalid numeric bounds.

Full package run, `go test ./rti/pkg/fom/... -count=1`, exit 0:

```text
?    github.com/cbchoi/gorti/rti/pkg/fom        [no test files]
ok   github.com/cbchoi/gorti/rti/pkg/fom/mim    0.507s
ok   github.com/cbchoi/gorti/rti/pkg/fom/model  0.470s
ok   github.com/cbchoi/gorti/rti/pkg/fom/parser 0.534s
```

Additional actual verification:

- `go test ./rti/pkg/fom/... -count=10 -shuffle=on`: exit 0; MIM 0.763s,
  model 0.554s, parser 0.962s.
- `go test ./tests/spec/M1/... -count=1`: exit 0, 0.448s.
- `go vet ./rti/pkg/fom/...`: exit 0, no findings.
- `gofmt -l` on all changed Go files: exit 0, no output.
- `git diff --check -- rti/pkg/fom/model rti/pkg/fom/parser rti/pkg/fom/mim`:
  exit 0, no output.

## Integration Notes And Limits

Both public fields are named exactly `Dimensions` and have type `[]string`.
`model.Attribute` is no longer comparable with `==`. Owned package builds
pass; duplicate compatibility now compares dimension sets without mutating
the preserved reference order. Empty inherited containers remain supported.

`FOM-015` reports undefined/empty dimension references and invalid numeric
upper bounds. References resolve against all supplied modules and the standard
MIM. Omitted upper bounds retain the existing zero sentinel; malformed,
overflowing, empty, and explicitly zero bounds do not produce a model.

The MIM handle now preserves `HLAfederate` and `HLAserviceGroup` declarations
alongside its references. Consumers must resolve names instead of assuming
fixed dimension positions. This does not implement complete modular dimension
declaration conflict policy or general XML schema validation.

An attempted composition suite run was blocked outside this ownership scope:

```text
rti/internal/hla/object/registry.go:482:7:
ev.RegionScoped undefined (type *eventRecord has no field or method RegionScoped)
```

No composition, gRPC, or core files were edited. The race run could not start:
`go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`.
Neither `gcc` nor `clang` was found on PATH.
