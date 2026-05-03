"""FOM XML parser. Agent C implements per TASK-061.

Same diagnostic codes as Agent B's Go parser
(rti/pkg/fom/parser/) — FOM-001 through FOM-101. The fixtures in
tests/conformance/foms/{good,bad}/ drive both implementations.

Spec test: pysdk/tests/spec/m4/test_spec_m4_fom_diagnostics.py
parametrizes over the bad-FOM fixtures and asserts the matching code is
emitted; pysdk/tests/spec/m4/test_spec_m4_fom_acceptance.py iterates
the good-FOM fixtures and asserts zero diagnostics.
"""

from __future__ import annotations

import re
import xml.etree.ElementTree as ET  # noqa: N817
from dataclasses import dataclass, field
from pathlib import Path

from rti1516e.fom.model import (
    FOM,
    ArrayData,
    Attribute,
    BasicData,
    DataType,
    EnumeratedData,
    FixedRecordData,
    InteractionClass,
    ObjectClass,
    Parameter,
    SimpleData,
    VariantRecordData,
)

# --- Public types -----------------------------------------------------------


@dataclass(frozen=True)
class Diagnostic:
    """A single parser diagnostic with the FOM-NNN code."""

    code: str  # e.g. "FOM-001"
    message: str  # human-readable
    source: str  # file path or module name
    line: int = 0  # 1-based; 0 = unknown


@dataclass
class ParseResult:
    """Result of parse(). When diagnostics is non-empty, fom is None."""

    fom: FOM | None = None
    diagnostics: list[Diagnostic] = field(default_factory=list)

    def has_code(self, code: str) -> bool:
        """True iff any diagnostic carries ``code``."""
        return any(d.code == code for d in self.diagnostics)


# --- Repo-relative MIM lookup ----------------------------------------------

# pysdk/rti1516e/fom/parser.py -> parents[3] is the repo root.
_REPO_ROOT = Path(__file__).resolve().parents[3]
_MIM_PATHS: tuple[Path, ...] = (
    _REPO_ROOT / "rti" / "pkg" / "fom" / "mim" / "standard-mim.xml",
    _REPO_ROOT / "rti" / "pkg" / "fom" / "mim" / "hla-standard-mim.xml",
)

# Universal class roots per IEEE 1516.2-2010 §6 / Annex B.
_OBJECT_ROOT = "HLAobjectRoot"
_INTERACTION_ROOT = "HLAinteractionRoot"

# IEEE 1516-2010 DIF Annex A whitelist of element local names. Mirrors
# rti/pkg/fom/parser/strict.go's annexAElements; adding to this set is a
# contract change shared with the Go side.
_ANNEX_A_ELEMENTS: frozenset[str] = frozenset(
    {
        # Root + top-level sections.
        "objectModel",
        "modelIdentification",
        "objects",
        "interactions",
        "dimensions",
        "timeRepresentation",
        "userSuppliedTags",
        "synchronizations",
        "transportations",
        "switches",
        "updateRates",
        "dataTypes",
        "notes",
        # Identification block.
        "name",
        "type",
        "version",
        "modificationDate",
        "securityClassification",
        "releaseRestriction",
        "purpose",
        "applicationDomain",
        "description",
        "useLimitation",
        "useHistory",
        "keyword",
        "taxonomy",
        "keywordValue",
        "poc",
        "pocType",
        "pocName",
        "pocOrg",
        "pocTelephone",
        "pocEmail",
        "reference",
        "referenceType",
        "identification",
        "other",
        "glyph",
        "note",
        # Objects / interactions.
        "objectClass",
        "interactionClass",
        "sharing",
        "semantics",
        "attribute",
        "parameter",
        # Per-attribute / per-interaction descriptors.
        "dataType",
        "updateType",
        "updateCondition",
        "ownership",
        "transportation",
        "order",
        "dimensionRefs",
        "dimensionRef",
        "dimension",
        "upperBound",
        "normalization",
        "value",
        # Data types: containers + items.
        "basicDataRepresentations",
        "basicData",
        "size",
        "interpretation",
        "endian",
        "encoding",
        "simpleDataTypes",
        "simpleData",
        "representation",
        "units",
        "resolution",
        "accuracy",
        "enumeratedDataTypes",
        "enumeratedData",
        "enumerator",
        "values",
        "arrayDataTypes",
        "arrayData",
        "cardinality",
        "fixedRecordDataTypes",
        "fixedRecordData",
        "field",
        "variantRecordDataTypes",
        "variantRecordData",
        "discriminant",
        "alternative",
        # Time representation.
        "timeStamp",
        "timeInterval",
        "epoch",
        # Synchronizations / transportations / switches.
        "synchronization",
        "capability",
        "label",
        "tag",
        "transportType",
        "reliable",
        "autoProvide",
        "conveyRegionDesignatorSets",
        "conveyProducingFederate",
        "serviceReporting",
        "exceptionReporting",
        "delaySubscriptionEvaluation",
        "automaticResignAction",
        # Update rate.
        "updateRate",
        "rate",
    }
)


# --- Internal: parsed-module shape ----------------------------------------


@dataclass
class _ParsedModule:
    """Flattened intermediate from one XML module — feeds the diagnostic passes."""

    object_classes: list[ObjectClass] = field(default_factory=list)
    interaction_classes: list[InteractionClass] = field(default_factory=list)
    data_types: list[DataType] = field(default_factory=list)
    variant_records_missing_disc: list[str] = field(default_factory=list)


# --- XML helpers -----------------------------------------------------------


def _local_name(tag: str) -> str:
    """Strip XML namespace from a tag name, returning just the local part."""
    if "}" in tag:
        return tag.split("}", 1)[1]
    return tag


def _child_text(elem: ET.Element, local_name: str) -> str:
    """Return the text of the first direct child whose local name matches."""
    for child in elem:
        if _local_name(child.tag) == local_name:
            return (child.text or "").strip()
    return ""


def _children(elem: ET.Element, local_name: str) -> list[ET.Element]:
    """Return all direct children whose local name matches."""
    return [c for c in elem if _local_name(c.tag) == local_name]


def _find_first(root: ET.Element, local_name: str) -> ET.Element | None:
    """First direct child with the given local name, or None."""
    for child in root:
        if _local_name(child.tag) == local_name:
            return child
    return None


# --- Strict-mode FOM-009 element scanner ----------------------------------

# Match an XML start-element tag's local name. Picks up '<localName' or
# '<ns:localName' (namespace prefixes are stripped). Skips comments,
# processing instructions, declarations, and end tags. Approximate but
# adequate: the source files we accept are well-formed XML, and the
# whitelist sees only Annex A names regardless of namespace prefix.
_START_TAG_RE = re.compile(rb"<(?!/|\?|!)\s*(?:[\w.-]+:)?([\w.-]+)")


def _strict_unknown_elements(xml_bytes: bytes) -> list[tuple[str, int]]:
    """Return [(elem_name, line)] for any element name not in the Annex A whitelist."""
    out: list[tuple[str, int]] = []
    for match in _START_TAG_RE.finditer(xml_bytes):
        name = match.group(1).decode("ascii", errors="replace")
        if name in _ANNEX_A_ELEMENTS:
            continue
        # 1-based line number
        line = xml_bytes.count(b"\n", 0, match.start()) + 1
        out.append((name, line))
    return out


# --- XML walk: build _ParsedModule -----------------------------------------


def _flatten_object_classes(
    node: ET.Element, parent: str | None, out: list[ObjectClass]
) -> None:
    """Walk recursive <objectClass> nodes; emit flat list with parent names."""
    name = _child_text(node, "name")
    attributes: list[Attribute] = []
    for a in _children(node, "attribute"):
        attributes.append(
            Attribute(
                name=_child_text(a, "name"),
                data_type=_child_text(a, "dataType"),
                order=_child_text(a, "order") or "TimeStamp",
                transportation=_child_text(a, "transportation") or "HLAreliable",
            )
        )
    out.append(
        ObjectClass(
            name=name,
            parent=parent,
            attributes=tuple(attributes),
        )
    )
    for child in _children(node, "objectClass"):
        _flatten_object_classes(child, name, out)


def _flatten_interaction_classes(
    node: ET.Element, parent: str | None, out: list[InteractionClass]
) -> None:
    """Walk recursive <interactionClass> nodes; emit flat list with parent names."""
    name = _child_text(node, "name")
    parameters: list[Parameter] = []
    for p in _children(node, "parameter"):
        parameters.append(
            Parameter(
                name=_child_text(p, "name"),
                data_type=_child_text(p, "dataType"),
            )
        )
    out.append(
        InteractionClass(
            name=name,
            parent=parent,
            parameters=tuple(parameters),
            order=_child_text(node, "order") or "TimeStamp",
            transportation=_child_text(node, "transportation") or "HLAreliable",
        )
    )
    for child in _children(node, "interactionClass"):
        _flatten_interaction_classes(child, name, out)


def _convert_data_types(
    dt_section: ET.Element | None,
) -> tuple[list[DataType], list[str]]:
    """Convert <dataTypes> into a flat DataType list and a list of variant
    record names that are missing a discriminant (for FOM-013)."""
    if dt_section is None:
        return [], []
    out: list[DataType] = []
    missing_disc: list[str] = []

    basic_section = _find_first(dt_section, "basicDataRepresentations")
    if basic_section is not None:
        for b in _children(basic_section, "basicData"):
            out.append(
                BasicData(
                    name=_child_text(b, "name"),
                    size=_safe_int(_child_text(b, "size")),
                    endianness=_child_text(b, "endian"),
                )
            )

    simple_section = _find_first(dt_section, "simpleDataTypes")
    if simple_section is not None:
        for s in _children(simple_section, "simpleData"):
            out.append(
                SimpleData(
                    name=_child_text(s, "name"),
                    representation=_child_text(s, "representation"),
                )
            )

    enum_section = _find_first(dt_section, "enumeratedDataTypes")
    if enum_section is not None:
        for e in _children(enum_section, "enumeratedData"):
            enums: dict[str, int] = {}
            for en in _children(e, "enumerator"):
                en_name = _child_text(en, "name")
                # MIM uses <value>, DIF Annex A also allows <values>; accept
                # either as the integer enumerator value.
                en_val = _child_text(en, "value") or _child_text(en, "values")
                if en_name:
                    enums[en_name] = _safe_int(en_val)
            out.append(
                EnumeratedData(
                    name=_child_text(e, "name"),
                    representation=_child_text(e, "representation"),
                    enumerators=enums,
                )
            )

    array_section = _find_first(dt_section, "arrayDataTypes")
    if array_section is not None:
        for a in _children(array_section, "arrayData"):
            card_text = _child_text(a, "cardinality")
            try:
                cardinality = int(card_text)
            except (TypeError, ValueError):
                cardinality = -1  # "Dynamic" or other variable cardinality
            out.append(
                ArrayData(
                    name=_child_text(a, "name"),
                    element_type=_child_text(a, "dataType"),
                    cardinality=cardinality,
                )
            )

    fixed_section = _find_first(dt_section, "fixedRecordDataTypes")
    if fixed_section is not None:
        for f in _children(fixed_section, "fixedRecordData"):
            fields: list[tuple[str, str]] = []
            for fld in _children(f, "field"):
                fields.append(
                    (_child_text(fld, "name"), _child_text(fld, "dataType"))
                )
            out.append(
                FixedRecordData(
                    name=_child_text(f, "name"),
                    fields=tuple(fields),
                )
            )

    variant_section = _find_first(dt_section, "variantRecordDataTypes")
    if variant_section is not None:
        for v in _children(variant_section, "variantRecordData"):
            v_name = _child_text(v, "name")
            disc_name = _child_text(v, "discriminant")
            disc_type = _child_text(v, "dataType")
            variants: list[tuple[str, str, str]] = []
            for alt in _children(v, "alternative"):
                variants.append(
                    (
                        _child_text(alt, "enumerator"),
                        _child_text(alt, "name"),
                        _child_text(alt, "dataType"),
                    )
                )
            if not disc_name:
                missing_disc.append(v_name)
            out.append(
                VariantRecordData(
                    name=v_name,
                    discriminant_name=disc_name,
                    discriminant_type=disc_type,
                    variants=tuple(variants),
                )
            )

    return out, missing_disc


def _safe_int(text: str) -> int:
    """Parse text as int; returns 0 for empty / non-numeric input."""
    try:
        return int(text)
    except (TypeError, ValueError):
        return 0


def _parse_xml_bytes(xml_bytes: bytes) -> _ParsedModule:
    """Parse one module's XML bytes into the flat _ParsedModule shape.

    Raises ET.ParseError on malformed XML.
    """
    # FOM modules are local files supplied by the caller (developer-provided
    # IEEE 1516.2 DIF XML), not untrusted network input. The standard library
    # ElementTree decoder is sufficient; defusedxml would only add a runtime
    # dep for callers who already control the file source.
    root = ET.fromstring(xml_bytes)  # noqa: S314
    pm = _ParsedModule()

    objects = _find_first(root, "objects")
    if objects is not None:
        for oc in _children(objects, "objectClass"):
            _flatten_object_classes(oc, None, pm.object_classes)

    interactions = _find_first(root, "interactions")
    if interactions is not None:
        for ic in _children(interactions, "interactionClass"):
            _flatten_interaction_classes(ic, None, pm.interaction_classes)

    data_section = _find_first(root, "dataTypes")
    pm.data_types, pm.variant_records_missing_disc = _convert_data_types(data_section)
    return pm


# --- Diagnostic passes ----------------------------------------------------


def _diag_unknown_elements(
    xml_bytes: bytes, source: str
) -> list[Diagnostic]:
    """FOM-009: unknown XML element (strict-mode whitelist)."""
    return [
        Diagnostic(
            code="FOM-009",
            message=(
                f"unknown XML element {name!r} (strict mode: not in IEEE 1516-2010 "
                "DIF Annex A)"
            ),
            source=source,
            line=line,
        )
        for name, line in _strict_unknown_elements(xml_bytes)
    ]


def _diag_datatype_refs(
    pm: _ParsedModule, source: str, mim_data_type_names: set[str]
) -> list[Diagnostic]:
    """FOM-001: every attribute / parameter dataType must resolve."""
    declared = {dt.name for dt in pm.data_types}
    known = mim_data_type_names | declared

    def resolves(name: str) -> bool:
        if not name:
            return True
        return name in known

    diags: list[Diagnostic] = []
    for oc in pm.object_classes:
        for a in oc.attributes:
            if not resolves(a.data_type):
                diags.append(
                    Diagnostic(
                        code="FOM-001",
                        message=(
                            f"DataType {a.data_type!r} referenced by attribute "
                            f"{oc.name}.{a.name} but not defined"
                        ),
                        source=source,
                    )
                )
    for ic in pm.interaction_classes:
        for p in ic.parameters:
            if not resolves(p.data_type):
                diags.append(
                    Diagnostic(
                        code="FOM-001",
                        message=(
                            f"DataType {p.data_type!r} referenced by parameter "
                            f"{ic.name}.{p.name} but not defined"
                        ),
                        source=source,
                    )
                )
    return diags


def _build_object_parent_sets(
    classes: list[ObjectClass],
) -> dict[str, set[str]]:
    """Name -> set-of-parent-names (excluding root sentinel)."""
    out: dict[str, set[str]] = {}
    for oc in classes:
        out.setdefault(oc.name, set())
        if oc.parent:
            out[oc.name].add(oc.parent)
    return out


def _diag_cycle(pm: _ParsedModule, source: str) -> list[Diagnostic]:
    """FOM-002: object class hierarchy contains a cycle."""
    parents = _build_object_parent_sets(pm.object_classes)
    diags: list[Diagnostic] = []
    seen_cycle: set[str] = set()
    for name in sorted(parents):
        if name in seen_cycle:
            continue
        cycle_start = _find_cycle(name, parents)
        if cycle_start is not None:
            seen_cycle.add(cycle_start)
            diags.append(
                Diagnostic(
                    code="FOM-002",
                    message=(
                        f"object class hierarchy contains a cycle through "
                        f"{cycle_start!r}"
                    ),
                    source=source,
                )
            )
    return diags


def _find_cycle(start: str, parents: dict[str, set[str]]) -> str | None:
    """DFS through parent edges; return the offending name on a back-edge."""
    stack: set[str] = set()

    def visit(node: str) -> str | None:
        if node in stack:
            return node
        stack.add(node)
        try:
            for p in sorted(parents.get(node, set())):
                hit = visit(p)
                if hit is not None:
                    return hit
        finally:
            stack.discard(node)
        return None

    return visit(start)


def _diag_multiple_parents(pm: _ParsedModule, source: str) -> list[Diagnostic]:
    """FOM-003: object class declared under more than one distinct parent."""
    parents = _build_object_parent_sets(pm.object_classes)
    diags: list[Diagnostic] = []
    for name in sorted(parents):
        ps = parents[name]
        if len(ps) < 2:
            continue
        plist = ", ".join(sorted(ps))
        diags.append(
            Diagnostic(
                code="FOM-003",
                message=f"object class {name!r} has multiple parents: {plist}",
                source=source,
            )
        )
    return diags


def _walk_object_ancestors_inclusive(
    start: ObjectClass, by_name: dict[str, ObjectClass]
) -> list[ObjectClass]:
    """Return [root, ..., start] following the parent chain. Stops on cycles."""
    chain: list[ObjectClass] = []
    visited: set[str] = set()
    cur: ObjectClass | None = start
    while cur is not None:
        if cur.name in visited:
            break
        visited.add(cur.name)
        chain.insert(0, cur)
        if not cur.parent:
            break
        cur = by_name.get(cur.parent)
    return chain


def _walk_interaction_ancestors_inclusive(
    start: InteractionClass, by_name: dict[str, InteractionClass]
) -> list[InteractionClass]:
    """Same as _walk_object_ancestors_inclusive for interactions."""
    chain: list[InteractionClass] = []
    visited: set[str] = set()
    cur: InteractionClass | None = start
    while cur is not None:
        if cur.name in visited:
            break
        visited.add(cur.name)
        chain.insert(0, cur)
        if not cur.parent:
            break
        cur = by_name.get(cur.parent)
    return chain


def _diag_duplicate_attributes(
    pm: _ParsedModule, source: str
) -> list[Diagnostic]:
    """FOM-004: attribute name duplicated within a class (incl. inherited)."""
    by_name: dict[str, ObjectClass] = {oc.name: oc for oc in pm.object_classes}
    reported: set[tuple[str, str]] = set()
    diags: list[Diagnostic] = []
    for oc in pm.object_classes:
        ancestors_attrs: dict[str, str] = {}  # attr_name -> declaring class
        for owner in _walk_object_ancestors_inclusive(oc, by_name):
            for a in owner.attributes:
                if a.name in ancestors_attrs:
                    key = (oc.name, a.name)
                    if key in reported:
                        continue
                    reported.add(key)
                    prev = ancestors_attrs[a.name]
                    if prev == owner.name:
                        msg = (
                            f"attribute {a.name!r} duplicated in object class "
                            f"{owner.name!r}"
                        )
                    else:
                        msg = (
                            f"attribute {a.name!r} duplicated in object class "
                            f"{oc.name!r} (also declared on {prev!r})"
                        )
                    diags.append(
                        Diagnostic(code="FOM-004", message=msg, source=source)
                    )
                    continue
                ancestors_attrs[a.name] = owner.name
    return diags


def _diag_duplicate_parameters(
    pm: _ParsedModule, source: str
) -> list[Diagnostic]:
    """FOM-005: parameter name duplicated within an interaction (incl. inherited)."""
    by_name: dict[str, InteractionClass] = {
        ic.name: ic for ic in pm.interaction_classes
    }
    reported: set[tuple[str, str]] = set()
    diags: list[Diagnostic] = []
    for ic in pm.interaction_classes:
        ancestors_params: dict[str, str] = {}
        for owner in _walk_interaction_ancestors_inclusive(ic, by_name):
            for p in owner.parameters:
                if p.name in ancestors_params:
                    key = (ic.name, p.name)
                    if key in reported:
                        continue
                    reported.add(key)
                    prev = ancestors_params[p.name]
                    if prev == owner.name:
                        msg = (
                            f"parameter {p.name!r} duplicated in interaction "
                            f"class {owner.name!r}"
                        )
                    else:
                        msg = (
                            f"parameter {p.name!r} duplicated in interaction "
                            f"class {ic.name!r} (also declared on {prev!r})"
                        )
                    diags.append(
                        Diagnostic(code="FOM-005", message=msg, source=source)
                    )
                    continue
                ancestors_params[p.name] = owner.name
    return diags


def _diag_object_parents(pm: _ParsedModule, source: str) -> list[Diagnostic]:
    """FOM-011: object class references non-existent parent."""
    known: set[str] = {_OBJECT_ROOT}
    for oc in pm.object_classes:
        known.add(oc.name)
    reported: set[tuple[str, str]] = set()
    diags: list[Diagnostic] = []
    for oc in pm.object_classes:
        if oc.name == _OBJECT_ROOT:
            continue
        if not oc.parent:
            key = (oc.name, "<root>")
            if key in reported:
                continue
            reported.add(key)
            diags.append(
                Diagnostic(
                    code="FOM-011",
                    message=(
                        f"object class {oc.name!r} is not nested under "
                        f"{_OBJECT_ROOT}; missing parent class"
                    ),
                    source=source,
                )
            )
            continue
        if oc.parent not in known:
            key = (oc.name, oc.parent)
            if key in reported:
                continue
            reported.add(key)
            diags.append(
                Diagnostic(
                    code="FOM-011",
                    message=(
                        f"object class {oc.name!r} references non-existent "
                        f"parent {oc.parent!r}"
                    ),
                    source=source,
                )
            )
    return diags


def _diag_interaction_parents(
    pm: _ParsedModule, source: str
) -> list[Diagnostic]:
    """FOM-012: interaction class references non-existent parent."""
    known: set[str] = {_INTERACTION_ROOT}
    for ic in pm.interaction_classes:
        known.add(ic.name)
    reported: set[tuple[str, str]] = set()
    diags: list[Diagnostic] = []
    for ic in pm.interaction_classes:
        if ic.name == _INTERACTION_ROOT:
            continue
        if not ic.parent:
            key = (ic.name, "<root>")
            if key in reported:
                continue
            reported.add(key)
            diags.append(
                Diagnostic(
                    code="FOM-012",
                    message=(
                        f"interaction class {ic.name!r} is not nested under "
                        f"{_INTERACTION_ROOT}; missing parent class"
                    ),
                    source=source,
                )
            )
            continue
        if ic.parent not in known:
            key = (ic.name, ic.parent)
            if key in reported:
                continue
            reported.add(key)
            diags.append(
                Diagnostic(
                    code="FOM-012",
                    message=(
                        f"interaction class {ic.name!r} references non-existent "
                        f"parent {ic.parent!r}"
                    ),
                    source=source,
                )
            )
    return diags


def _diag_variant_discriminator(
    pm: _ParsedModule, source: str
) -> list[Diagnostic]:
    """FOM-013: variantRecordData declared without a discriminant."""
    return [
        Diagnostic(
            code="FOM-013",
            message=f"variantRecordData {name!r} is missing a discriminant field",
            source=source,
        )
        for name in pm.variant_records_missing_disc
    ]


# --- MIM loading and merge --------------------------------------------------

# Cache the MIM after first load — read-only by construction, safe to share.
_MIM_CACHE: _ParsedModule | None = None
_MIM_LOAD_FAILED = False


def _load_mim() -> _ParsedModule | None:
    """Implicitly load the IEEE/SISO standard MIM as the base FOM.

    Returns None if the vendored MIM XML cannot be read or parsed; callers
    treat that as an empty MIM (FOM-001 then catches every MIM-only name as
    an undefined reference, surfacing the build-config issue loudly via the
    existing diagnostic channel rather than masking it with a silent error).

    Cross-language handle alignment (M6 follow-up): the IEEE 1516.1-2010
    standard MIM declares several interaction class names twice under
    different parents (for example ``HLAadjust`` appears once under
    ``HLAmanager.HLAfederate`` and again under ``HLAmanager.HLAfederation``;
    same for ``HLArequest``, ``HLAreport``, ``HLAreportFOMmoduleData``,
    ``HLArequestFOMmoduleData`` and ``HLAsetSwitches``). It also declares a
    few classes that, when flattened, end up sharing the SAME (name,
    parent_name_string) key because their parents themselves share a name
    (e.g. two distinct ``HLAreport`` containers — one nested under
    ``HLAfederate``, one under ``HLAfederation`` — each carry a child
    ``HLAreportFOMmoduleData``, and after flattening both children list
    parent="HLAreport"). Go's ``model.NewFOM`` keeps every one of these
    flat entries verbatim (no dedup, just a stable name sort) and the
    production ``*fomHandle.LookupInteractionClass`` resolves a leaf name
    by 1-based position in that slice. If the Python side dedupes the MIM
    by name (or even by (name, parent_name)), the user FOM's class lands
    at a SMALLER handle than the Go side assigns, and Go's
    ``OrderForInteraction`` lookup against its own table either returns a
    different class or hits an out-of-range slot — both fall back to TSO,
    defeating the per-class FOM order contract. So we mirror Go: append
    every MIM interaction/object class verbatim. Data types ARE name-unique
    by IEEE schema and stay deduped across MIM files (Go's MIM corpus is
    a single XML in cut-1 so the cross-file dedup is a no-op for it; we
    keep it on the Python side as defense-in-depth for the second wrapper
    XML the Python loader still consumes for forward compatibility).
    """
    global _MIM_CACHE, _MIM_LOAD_FAILED
    if _MIM_CACHE is not None:
        return _MIM_CACHE
    if _MIM_LOAD_FAILED:
        return None
    merged = _ParsedModule()
    seen_dt: set[str] = set()
    for path in _MIM_PATHS:
        try:
            xml_bytes = path.read_bytes()
            pm = _parse_xml_bytes(xml_bytes)
        except (OSError, ET.ParseError):
            _MIM_LOAD_FAILED = True
            return None
        # Append all MIM object / interaction classes verbatim — no name
        # or (name, parent) dedup. Mirrors Go's flat-list semantics; see
        # the docstring above for the cross-language alignment rationale.
        merged.object_classes.extend(pm.object_classes)
        merged.interaction_classes.extend(pm.interaction_classes)
        for dt in pm.data_types:
            if dt.name in seen_dt:
                continue
            seen_dt.add(dt.name)
            merged.data_types.append(dt)
    _MIM_CACHE = merged
    return merged


def _is_object_class_passthrough(oc: ObjectClass) -> bool:
    """Pass-through: re-declares MIM root only to anchor inheritance."""
    return len(oc.attributes) == 0


def _is_interaction_class_passthrough(ic: InteractionClass) -> bool:
    """Pass-through: re-declares MIM interaction root only to anchor inheritance."""
    return len(ic.parameters) == 0


def _diag_mim_redefinition(
    pm: _ParsedModule, source: str, mim: _ParsedModule
) -> list[Diagnostic]:
    """FOM-101: user FOM redefines a MIM type / class."""
    mim_obj = {oc.name for oc in mim.object_classes}
    mim_int = {ic.name for ic in mim.interaction_classes}
    mim_dt = {dt.name for dt in mim.data_types}
    diags: list[Diagnostic] = []
    for oc in pm.object_classes:
        if oc.name not in mim_obj:
            continue
        if _is_object_class_passthrough(oc):
            continue
        diags.append(
            Diagnostic(
                code="FOM-101",
                message=f"user module redefines MIM object class {oc.name!r}",
                source=source,
            )
        )
    for ic in pm.interaction_classes:
        if ic.name not in mim_int:
            continue
        if _is_interaction_class_passthrough(ic):
            continue
        diags.append(
            Diagnostic(
                code="FOM-101",
                message=(
                    f"user module redefines MIM interaction class {ic.name!r}"
                ),
                source=source,
            )
        )
    for dt in pm.data_types:
        if dt.name not in mim_dt:
            continue
        diags.append(
            Diagnostic(
                code="FOM-101",
                message=f"user module redefines MIM dataType {dt.name!r}",
                source=source,
            )
        )
    return diags


def _merge_user_onto_mim(
    user: _ParsedModule, mim: _ParsedModule
) -> _ParsedModule:
    """Union MIM + user, MIM names winning on collision (callers reject those
    via FOM-101 before reaching this step).

    The MIM side is appended verbatim — including duplicate (name, parent)
    pairs from the standard MIM (see ``_load_mim`` docstring on cross-language
    handle alignment). The user side is filtered by NAME against the MIM
    name set, mirroring Go's ``mim.mergeNoCollision``: a user re-mention of
    an MIM root (HLAobjectRoot / HLAinteractionRoot) is the canonical
    pass-through pattern and must be skipped. Beyond that, user-vs-user
    dedup is also by NAME — matching Go's ``mergeNoCollision`` which uses
    the same name-keyed map (so a user FOM with two ``Foo`` siblings would
    keep both on Go's side because Go's map check is against the MIM-only
    set, but Go's ``model.NewFOM`` would still flatten and append both).
    To preserve cross-language handle parity even for that edge case we
    mirror Go's appending behavior for user classes whose name is unique
    relative to the MIM but may repeat among user siblings.
    """
    out = _ParsedModule()
    mim_oc_names: set[str] = {oc.name for oc in mim.object_classes}
    mim_ic_names: set[str] = {ic.name for ic in mim.interaction_classes}
    seen_dt: set[str] = set()
    for oc in mim.object_classes:
        out.object_classes.append(oc)
    for oc in user.object_classes:
        if oc.name in mim_oc_names:
            continue
        out.object_classes.append(oc)
    for ic in mim.interaction_classes:
        out.interaction_classes.append(ic)
    for ic in user.interaction_classes:
        if ic.name in mim_ic_names:
            continue
        out.interaction_classes.append(ic)
    for dt in mim.data_types:
        seen_dt.add(dt.name)
        out.data_types.append(dt)
    for dt in user.data_types:
        if dt.name in seen_dt:
            continue
        seen_dt.add(dt.name)
        out.data_types.append(dt)
    return out


def _to_fom(pm: _ParsedModule) -> FOM:
    """Materialize the immutable FOM dataclass with sorted-by-name members."""
    return FOM(
        object_classes=tuple(sorted(pm.object_classes, key=lambda oc: oc.name)),
        interaction_classes=tuple(
            sorted(pm.interaction_classes, key=lambda ic: ic.name)
        ),
        data_types=tuple(sorted(pm.data_types, key=lambda dt: dt.name)),
    )


# --- Public entry point ----------------------------------------------------


def parse(modules: list[str | Path]) -> ParseResult:
    """Parse a list of FOM module file paths and return a ParseResult.

    On success: ParseResult.fom is the merged FOM (MIM + user modules);
    diagnostics is empty.

    On failure: ParseResult.fom is None; diagnostics carries one entry per
    problem found, each with a FOM-NNN code matching the Go side.
    """
    diagnostics: list[Diagnostic] = []
    parsed_modules: list[_ParsedModule] = []

    mim = _load_mim()
    mim_data_type_names: set[str] = (
        {dt.name for dt in mim.data_types} if mim is not None else set()
    )

    for module in modules:
        path = Path(module)
        source = str(path)
        try:
            xml_bytes = path.read_bytes()
        except OSError as exc:
            diagnostics.append(
                Diagnostic(
                    code="FOM-INTERNAL",
                    message=f"cannot read module: {exc}",
                    source=source,
                )
            )
            continue

        # FOM-009 strict whitelist runs against the raw bytes — independent
        # of the structural decode so unknown elements get flagged even when
        # the encoding/xml-style walk silently discards them.
        diagnostics.extend(_diag_unknown_elements(xml_bytes, source))

        try:
            pm = _parse_xml_bytes(xml_bytes)
        except ET.ParseError as exc:
            diagnostics.append(
                Diagnostic(
                    code="FOM-INTERNAL",
                    message=f"malformed XML: {exc}",
                    source=source,
                    line=getattr(exc, "position", (0, 0))[0] or 0,
                )
            )
            continue

        # Run the structural diagnostic passes against the user module.
        module_diags: list[Diagnostic] = []
        module_diags.extend(_diag_datatype_refs(pm, source, mim_data_type_names))
        module_diags.extend(_diag_cycle(pm, source))
        module_diags.extend(_diag_multiple_parents(pm, source))
        module_diags.extend(_diag_duplicate_attributes(pm, source))
        module_diags.extend(_diag_duplicate_parameters(pm, source))
        module_diags.extend(_diag_object_parents(pm, source))
        module_diags.extend(_diag_interaction_parents(pm, source))
        module_diags.extend(_diag_variant_discriminator(pm, source))

        # FOM-101 (MIM redefinition) only runs when the structural passes
        # are clean — Agent B's Go side gates the merge the same way.
        if not module_diags and mim is not None:
            module_diags.extend(_diag_mim_redefinition(pm, source, mim))

        diagnostics.extend(module_diags)
        parsed_modules.append(pm)

    if diagnostics:
        return ParseResult(fom=None, diagnostics=diagnostics)

    # Success: union user modules onto the MIM and build the immutable FOM.
    merged = mim if mim is not None else _ParsedModule()
    for pm in parsed_modules:
        merged = _merge_user_onto_mim(pm, merged)
    return ParseResult(fom=_to_fom(merged), diagnostics=[])
