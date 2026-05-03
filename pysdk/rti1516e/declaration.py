"""Helpers backing Federate.publish_*/subscribe_*. Agent C implements per TASK-064.

The public surface is on Federate (in connection.py); this module is a
private extension point. Agent C may keep helpers here or inline them
into Federate — the orchestrator pre-work doesn't constrain that
choice. The file exists so TASK-064's "Scope (in)" has a target.
"""

from __future__ import annotations
