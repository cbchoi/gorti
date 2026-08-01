"""Cross-language FOM handle alignment (M6 W1A follow-up).

Verifies the Python FOM parser assigns the same numeric handle to a given
class name as the Go-side production ``*fomHandle`` in
``rti/cmd/rtid/foms.go``. The Go side derives handles by 1-based position
in the name-sorted slice produced by ``model.NewFOM`` after
``mim.Merge(StandardMIM, userFOM)``.

Why this matters: when a Python federate publishes ``ModesProbe`` over
the real-gRPC transport, the Python SDK encodes Python's handle K on the
wire. The Go RTI's ``OrderForInteraction(K)`` resolves against ITS handle
table. If the two tables disagree, Go either looks up a different class
or hits an out-of-range slot — both fall back to TSO, defeating the
per-class FOM-order contract that the M5 best-effort RO test exercises
(``pysdk/tests/spec/m5/test_spec_m5_modes.py``).

This file is the component-owned diagnostic + regression. It builds a tiny
Go program at test time that enumerates the Go-side handles for the same
FOM modules the Python parser consumes, then asserts every class name
appearing in EITHER table maps to the same handle on BOTH sides. The Go
toolchain is required; the test SKIPs gracefully if ``go`` is not on PATH
so the regression is enforced wherever the full toolchain is available
(CI, contributor laptops with the dev environment) and a missing-go
scenario doesn't cause spurious failures on minimal machines.
"""

from __future__ import annotations

import json
import shutil
import subprocess
import tempfile
from pathlib import Path
from typing import Any

import pytest

from rti1516e.fom.parser import parse

REPO_ROOT = Path(__file__).resolve().parents[2]
CONFORMANCE_FOMS = REPO_ROOT / "tests" / "conformance" / "foms" / "good"

# A tiny inline Go program that mirrors the production rtid handle-derivation
# path (``rti/cmd/rtid/foms.go::Load`` + ``fomHandle.Lookup*``): it parses
# the requested FOM modules, merges the standard MIM on top, and emits one
# JSON line per class with its 1-based handle. Keeping the program inline
# (rather than vendoring a separate Go file) makes the test self-contained
# and proves the alignment against the actual Go code paths via real
# ``go run`` rather than a Python-side reimplementation. Both
# ``rti/pkg/fom/parser`` and ``rti/pkg/fom/mim`` are imported, so this
# program will fail to compile if the Go API drifts — which is exactly
# the regression signal we want at that layer.
_GO_LIST_HANDLES_SRC = r"""
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

type entry struct {
	Kind   string `json:"kind"`
	Handle int    `json:"handle"`
	Name   string `json:"name"`
	Parent string `json:"parent"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: list_handles <fom.xml> [...]")
		os.Exit(2)
	}
	mods := make([]parser.Module, 0, len(os.Args)-1)
	for _, p := range os.Args[1:] {
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read:", err)
			os.Exit(2)
		}
		mods = append(mods, parser.Module{Path: p, XML: b})
	}
	res, err := parser.Parse(mods)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(2)
	}
	if len(res.Diagnostics) > 0 {
		for _, d := range res.Diagnostics {
			fmt.Fprintln(os.Stderr, "DIAG:", d.Code, d.Message)
		}
		os.Exit(2)
	}
	fm, ok := res.FOM.(*model.FOM)
	if !ok {
		fmt.Fprintln(os.Stderr, "unexpected FOM type")
		os.Exit(2)
	}
	mimFOM, mimErr := mim.StandardMIMHandle()
	if mimErr != nil {
		fmt.Fprintln(os.Stderr, "mim:", mimErr)
		os.Exit(2)
	}
	merged, mdiags := mim.Merge(mimFOM, fm)
	if len(mdiags) > 0 {
		for _, d := range mdiags {
			fmt.Fprintln(os.Stderr, "MIM-DIAG:", d.Code, d.Message)
		}
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	for i, ic := range merged.InteractionClasses() {
		_ = enc.Encode(entry{
			Kind: "interaction", Handle: i + 1, Name: ic.Name, Parent: ic.ParentName,
		})
	}
	for i, oc := range merged.ObjectClasses() {
		_ = enc.Encode(entry{
			Kind: "object", Handle: i + 1, Name: oc.Name, Parent: oc.ParentName,
		})
	}
}
"""


def _go_handles_for(fom_paths: list[Path]) -> list[dict[str, Any]]:
    """Compile + run the inline Go enumerator; return parsed JSON entries.

    Each entry is ``{"kind": "interaction"|"object", "handle": int,
    "name": str, "parent": str}``. The list is in handle order within
    each kind (Go's ``model.NewFOM`` sort order; matches Python's
    ``sorted(...)`` stable order on the same names).
    """
    with tempfile.TemporaryDirectory(prefix="m6-handle-align-") as tmpdir:
        prog = Path(tmpdir) / "list_handles.go"
        prog.write_text(_GO_LIST_HANDLES_SRC, encoding="utf-8")
        argv = ["go", "run", str(prog), *(str(p) for p in fom_paths)]  # noqa: S607 — PATH-resolved by design
        proc = subprocess.run(  # noqa: S603 — argv built from literals
            argv,
            cwd=REPO_ROOT,
            capture_output=True,
            check=False,
        )
        if proc.returncode != 0:
            raise RuntimeError(
                f"go enumerator failed (rc={proc.returncode}):\n"
                f"stderr={proc.stderr.decode('utf-8', 'replace')}\n"
                f"stdout={proc.stdout.decode('utf-8', 'replace')}"
            )
        out: list[dict[str, Any]] = []
        for line in proc.stdout.decode("utf-8").splitlines():
            line = line.strip()
            if not line:
                continue
            out.append(json.loads(line))
        return out


def _python_handles_for(fom_paths: list[Path]) -> list[dict[str, Any]]:
    """Mirror of ``_go_handles_for`` driven by the Python FOM parser.

    Uses the same name-sorted, 1-based scheme the Python SDK transport
    layer uses in ``rti1516e/_transport.py::_populate_handle_tables``. If
    that scheme drifts from the parser's natural sort order this test
    will surface the regression alongside the Go-disagreement cases.
    """
    # Widen to ``list[str | Path]`` to match parse's signature: list[T]
    # is invariant in Python's type system, so a ``list[Path]`` cannot
    # be passed directly without re-typing the local.
    parser_input: list[str | Path] = list(fom_paths)
    result = parse(parser_input)
    if result.diagnostics or result.fom is None:
        raise RuntimeError(
            f"Python parse produced diagnostics: "
            f"{[(d.code, d.message) for d in result.diagnostics]}"
        )
    fom = result.fom
    out: list[dict[str, Any]] = []
    for idx, ic in enumerate(fom.interaction_classes):
        out.append(
            {
                "kind": "interaction",
                "handle": idx + 1,
                "name": ic.name,
                "parent": ic.parent or "",
            }
        )
    for idx, oc in enumerate(fom.object_classes):
        out.append(
            {
                "kind": "object",
                "handle": idx + 1,
                "name": oc.name,
                "parent": oc.parent or "",
            }
        )
    return out


@pytest.fixture(scope="module")
def go_available() -> bool:
    """True when the ``go`` toolchain is on PATH."""
    return shutil.which("go") is not None


@pytest.mark.parametrize(
    "fom_relpath",
    [
        "minimal.xml",
        "pyjevsim-bridge.xml",
    ],
)
def test_python_and_go_agree_on_handles(
    fom_relpath: str, go_available: bool
) -> None:
    """Every class name's handle on the Python side equals the Go side.

    Asserts pairwise: for each ``(kind, name, parent)`` Go entry, the
    Python entry at the same handle has the same identity. Catches both
    name-sort drift and MIM-corpus differences in one assertion shape.
    """
    if not go_available:
        pytest.skip("go toolchain not on PATH; cannot enumerate Go-side handles")

    fom_path = CONFORMANCE_FOMS / fom_relpath
    assert fom_path.exists(), f"fixture missing: {fom_path}"

    go_entries = _go_handles_for([fom_path])
    py_entries = _python_handles_for([fom_path])

    # Group by kind so duplicate-name interaction classes (e.g. HLAadjust
    # under both HLAfederate and HLAfederation) are matched by handle
    # position, not by name lookup. The handle-position match is the
    # only thing the wire protocol cares about.
    go_by_handle = {(e["kind"], e["handle"]): e for e in go_entries}
    py_by_handle = {(e["kind"], e["handle"]): e for e in py_entries}

    # 1) Same total count per kind.
    for kind in ("interaction", "object"):
        go_count = sum(1 for e in go_entries if e["kind"] == kind)
        py_count = sum(1 for e in py_entries if e["kind"] == kind)
        assert go_count == py_count, (
            f"{kind}-class count differs: Go={go_count} Python={py_count}. "
            f"Likely MIM-merge dedup drift in pysdk/rti1516e/fom/parser.py "
            f"(_load_mim) or _merge_user_onto_mim."
        )

    # 2) Pairwise match by (kind, handle).
    mismatches: list[str] = []
    for key, go_entry in go_by_handle.items():
        py_entry = py_by_handle.get(key)
        if py_entry is None:
            mismatches.append(f"missing on Python: {key} -> {go_entry}")
            continue
        if (go_entry["name"], go_entry["parent"]) != (
            py_entry["name"],
            py_entry["parent"],
        ):
            mismatches.append(
                f"handle {key}: Go={go_entry['name']!r}/parent={go_entry['parent']!r} "
                f"Python={py_entry['name']!r}/parent={py_entry['parent']!r}"
            )
    assert not mismatches, "\n".join(mismatches)


def test_python_modes_probe_lands_at_same_handle_as_go(go_available: bool) -> None:
    """Drives the exact inline FOM the M5 modes test uses (ModesProbe with
    ``<order>Receive</order>``) and asserts Python's handle for ModesProbe
    matches Go's. This is the specific case that gates
    ``test_spec_m5_best_effort_attribute_delivers_ro``: if the two sides
    disagree, the wire RPC carries a handle Go cannot resolve and the
    RO/TSO decision falls back to TSO.
    """
    if not go_available:
        pytest.skip("go toolchain not on PATH; cannot enumerate Go-side handles")

    # Mirrors pysdk/tests/spec/m5/_helpers.py::_write_modes_fom("Receive").
    # Kept inline here so this test stays self-contained — _helpers is owned
    # by the M5 spec test and importing it here would couple the diagnostic
    # to the spec-test scaffolding lifecycle.
    inline_fom = """<?xml version="1.0" encoding="UTF-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"
             xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
             xsi:schemaLocation="http://standards.ieee.org/IEEE1516-2010 IEEE1516-DIF-2010.xsd">
  <modelIdentification>
    <name>m5-modes-probe</name>
    <type>FOM</type>
    <version>1.0</version>
    <modificationDate>2026-05-03</modificationDate>
    <securityClassification>Unclassified</securityClassification>
    <description>Inline FOM mirroring pysdk/tests/spec/m5/_helpers.py.</description>
    <useHistory>None</useHistory>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <interactionClass>
        <name>ModesProbe</name>
        <sharing>PublishSubscribe</sharing>
        <transportation>HLAreliable</transportation>
        <order>Receive</order>
        <semantics>Probe interaction for handle alignment.</semantics>
        <parameter>
          <name>payload</name>
          <dataType>HLAinteger32BE</dataType>
          <semantics>Probe sequence number.</semantics>
        </parameter>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>
"""
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=".xml", prefix="modes-handle-align-",
        delete=False, encoding="utf-8",
    ) as tmp:
        tmp.write(inline_fom)
        fom_path = Path(tmp.name)

    try:
        go_entries = _go_handles_for([fom_path])
        py_entries = _python_handles_for([fom_path])
    finally:
        fom_path.unlink(missing_ok=True)

    def find_handle(entries: list[dict[str, Any]], name: str) -> int:
        for e in entries:
            if e["kind"] == "interaction" and e["name"] == name:
                return int(e["handle"])
        raise AssertionError(f"name {name!r} not found in entries")

    go_handle = find_handle(go_entries, "ModesProbe")
    py_handle = find_handle(py_entries, "ModesProbe")
    assert go_handle == py_handle, (
        f"ModesProbe handle disagreement: Go={go_handle} Python={py_handle}. "
        "This is the M5 best-effort RO blocker; if it fires, the Python "
        "MIM-merge in pysdk/rti1516e/fom/parser.py drifted from "
        "rti/pkg/fom/mim/standard-mim.xml."
    )
