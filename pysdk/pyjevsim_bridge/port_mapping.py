"""PortMapping: explicit pyjevsim port -> FOM class binding.

Agent C implements per TASK-069. Auto-generation from FOM XML can come
later; cut-1 is explicit.
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class PortMapping:
    """Bidirectional mapping between pyjevsim port names and FOM classes.

    out_ports maps {pyjevsim_output_port_name: FOM_interaction_class_name}
    in_ports  maps {FOM_interaction_class_name: pyjevsim_input_port_name}

    Construction takes a single dict and the implementation splits it into
    out/in based on the coupled model's port directions.
    """

    out_ports: dict[str, str]
    in_ports: dict[str, str]

    @classmethod
    def from_dict(cls, mapping: dict[str, str]) -> PortMapping:
        """Build a PortMapping from a flat {port: class} dict.

        Raises NotImplementedError until TASK-069. Agent C decides the
        direction-inference strategy (consult coupled_model.get_ports()
        or take direction-prefixed names like "out_/in_").
        """
        raise NotImplementedError("TASK-069")

    def fom_class_for_out_port(self, port: str) -> str | None:
        """Lookup helper — None if port is not mapped."""
        raise NotImplementedError("TASK-069")

    def in_port_for_fom_class(self, class_name: str) -> str | None:
        """Lookup helper — None if class is not subscribed via this mapping."""
        raise NotImplementedError("TASK-069")
