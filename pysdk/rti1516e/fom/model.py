"""FOM dataclass mirror of Agent B's Go model. Agent C implements per TASK-060.

Strictly mirrors rti/pkg/fom/model/ — same field names, same hierarchy.
This is the cross-language shared mental model: a developer reading the Go
code and the Python code should see identical structure.

Mutability: dataclasses are frozen by default to enforce the post-parse
immutability invariant (FOMs are read-only after parsing).
"""

from __future__ import annotations

from dataclasses import dataclass, field

# --- DataType sum type ------------------------------------------------------
# Mirrors rti/pkg/fom/model/dataclass.go's DataType variants.


@dataclass(frozen=True)
class DataType:
    """Base class for the DataType sum. Concrete variants below."""

    name: str


@dataclass(frozen=True)
class BasicData(DataType):
    """Built-in primitive (HLAinteger32BE, HLAfloat64BE, etc.)."""

    size: int = 0  # bytes; agent fills from spec
    endianness: str = ""  # "BE" | "LE" | "" for endian-agnostic types


@dataclass(frozen=True)
class SimpleData(DataType):
    """Aliased basic type (e.g. ``Speed`` aliasing ``HLAfloat64BE``)."""

    representation: str = ""


@dataclass(frozen=True)
class EnumeratedData(DataType):
    """Discrete values keyed by name."""

    representation: str = ""
    enumerators: dict[str, int] = field(default_factory=dict)


@dataclass(frozen=True)
class ArrayData(DataType):
    """Fixed or variable array. ``cardinality`` = -1 ⇒ variable."""

    element_type: str = ""
    cardinality: int = -1


@dataclass(frozen=True)
class FixedRecordData(DataType):
    """Ordered named fields."""

    fields: tuple[tuple[str, str], ...] = ()  # (field_name, type_name)


@dataclass(frozen=True)
class VariantRecordData(DataType):
    """Discriminated union."""

    discriminant_name: str = ""
    discriminant_type: str = ""
    variants: tuple[tuple[str, str, str], ...] = ()  # (enum_value, name, type)


# --- Class hierarchy --------------------------------------------------------


@dataclass(frozen=True)
class Attribute:
    """Attribute on an ObjectClass."""

    name: str
    data_type: str  # name resolved against FOM.data_types
    order: str = "TimeStamp"  # "TimeStamp" or "Receive"
    transportation: str = "HLAreliable"  # or "HLAbestEffort"


@dataclass(frozen=True)
class ObjectClass:
    """ObjectClass node in the inheritance tree."""

    name: str
    parent: str | None = None  # None for HLAobjectRoot
    attributes: tuple[Attribute, ...] = ()


@dataclass(frozen=True)
class Parameter:
    """Parameter on an InteractionClass."""

    name: str
    data_type: str


@dataclass(frozen=True)
class InteractionClass:
    """InteractionClass node in the inheritance tree."""

    name: str
    parent: str | None = None  # None for HLAinteractionRoot
    parameters: tuple[Parameter, ...] = ()
    order: str = "TimeStamp"
    transportation: str = "HLAreliable"


# --- Root FOM ---------------------------------------------------------------


@dataclass(frozen=True)
class FOM:
    """Root node — the parsed FOM as an immutable value.

    Iteration over object_classes / interaction_classes / data_types is
    deterministic (sorted by name) per IEEE 1516.2 Annex A and Agent B's
    Go convention.
    """

    object_classes: tuple[ObjectClass, ...] = ()
    interaction_classes: tuple[InteractionClass, ...] = ()
    data_types: tuple[DataType, ...] = ()

    def find_object_class(self, name: str) -> ObjectClass | None:
        """Lookup helper. Linear scan over self.object_classes."""
        for oc in self.object_classes:
            if oc.name == name:
                return oc
        return None

    def find_interaction_class(self, name: str) -> InteractionClass | None:
        """Lookup helper. Linear scan over self.interaction_classes."""
        for ic in self.interaction_classes:
            if ic.name == name:
                return ic
        return None

    def find_data_type(self, name: str) -> DataType | None:
        """Lookup helper. Linear scan over self.data_types."""
        for dt in self.data_types:
            if dt.name == name:
                return dt
        return None
