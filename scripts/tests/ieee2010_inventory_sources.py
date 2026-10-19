"""Rebuild the mechanical, explicitly unverified inventory from pinned sources.

Python/Go/protobuf/XML use their structured parsers. C++ ambassador declarations
use a deliberately bounded extractor; utility headers remain explicit member-
audit obligations, not a claimed full C++ AST/ABI check. No runtime test is run.
"""

import argparse
import ast
import collections
import hashlib
import importlib.util
import json
import re
import subprocess
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

from google.protobuf import descriptor_pb2

SPEC = importlib.util.spec_from_file_location("inventory_gate", Path(__file__).resolve().with_name("ieee2010_feature_inventory.py"))
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)

REPORT = "engineering/verification/ieee1516-2010-remaining-services-20260920.md"
MIM = "rti/pkg/fom/mim/standard-mim.xml"
GROUPS = {
    "federation": (["rti/internal/hla/federation/manager.go", "rti/internal/application/server/run.go", "rti/internal/adapters/grpc/federation.go", "rti/pkg/federate/federation_services.go", "pysdk/rti1516e/connection.py", "cppsdk/src/dlc/RTIambassadorImpl.cpp"], "Observe legal connection/join/resign states, all six actions, identity/session fences and membership post-state through real callbacks."),
    "save": (["rti/internal/hla/save_restore/manager.go", "rti/internal/adapters/grpc/savepoint.go", "rti/internal/application/server/restore_rebinding.go", "rti/pkg/federate/savepoint.go", "pysdk/rti1516e/savepoint.py"], "Compare complete cross-manager state and continuation across immediate/timed save, failure, abort, retry, restart and restore; verify status vectors and identity epochs."),
    "object": (["rti/internal/hla/object/delete.go", "rti/internal/hla/object/local_delete.go", "rti/internal/adapters/grpc/object.go", "rti/pkg/federate/object.go", "pysdk/rti1516e/object.py", "cppsdk/src/dlc/FederateAmbassadorBridge.cpp"], "Assert caller knowledge, current ownership, actual recipients, tags, order/transport, supplemental metadata, retraction and callback partial order after successful and rejected mutations."),
    "ownership": (["rti/internal/hla/ownership/manager.go", "rti/internal/hla/ownership/release_denied.go", "rti/internal/adapters/grpc/ownership.go", "rti/pkg/federate/ownership.go", "pysdk/rti1516e/ownership.py"], "Competing acquire/divest/cancel/deny operations preserve atomic results, ownership authority, callback identity and binary tags; test invalid complete sets before mutation."),
    "ddm": (["rti/internal/hla/ddm/manager.go", "rti/internal/adapters/grpc/ddm.go", "rti/pkg/federate/ddm.go", "pysdk/rti1516e/ddm.py"], "Compare pending and committed ranges, dimension/context ownership, multi-region atomicity and actual distinct recipient sets including inherited/default/dimensionless cases."),
    "declaration": (["rti/internal/hla/declaration/options.go", "rti/internal/hla/declaration/update_rate.go", "rti/internal/adapters/grpc/declaration.go", "rti/pkg/federate/declaration.go", "pysdk/rti1516e/declaration.py"], "Use acknowledged phases to distinguish active/passive and inherited/regional demand; assert both absence and presence of advisory transitions and callback-admission rollback."),
    "time": (["rti/internal/hla/core/time.go", "rti/internal/hla/time/manager.go", "rti/internal/adapters/grpc/time.go", "rti/pkg/federate/time.go"], "Separate multi-federate TAR/TARA/NMR/NMRA/FQR oracles; cover pending enable, equal timestamp, lookahead changes, GALT/LITS, asynchronous delivery, special values and grant failure."),
    "support": (["rti/internal/adapters/grpc/support.go", "rti/pkg/federate/support.go", "pysdk/rti1516e/support.py", "cppsdk/src/dlc/RTIambassadorImpl.cpp"], "Check legal disconnected/connected/joined states, caller-scoped knowledge, normalization, invalid handles and exact operation-specific error identities."),
    "mom": (["rti/internal/hla/mom/metrics.go", "rti/internal/hla/mom/catalog.go", "rti/internal/adapters/grpc/mom_observer.go", "rti/internal/adapters/grpc/mom_receipts.go", "rti/internal/application/server/mom_metrics_sources.go", "rti/pkg/federate/mom.go", "pysdk/rti1516e/mom.py"], "Use official MIM encoding vectors and a stateful observer federation; test inherited targets, recipients, switches, periodic/requested publication, real callback invocation and unavailable histories without feedback loops."),
    "fom": (["rti/pkg/fom/parser/strict.go", "rti/internal/application/composition/fom_repository.go", "rti/pkg/fom/mim/merge.go", "rti/internal/application/server/run.go"], "Validate independently against official schema and semantic round trip; accepted extension updates one coherent model while rejected joins and failed restore leave it unchanged."),
    "binding": (["cppsdk/src/dlc/RTIambassadorImpl.cpp", "cppsdk/src/dlc/FederateAmbassadorBridge.cpp", "pysdk/rti1516e/standard.py", "rti/pkg/federate/federate.go"], "Compile/link/run independently authored official-header clients; verify every overload/input/exception/callback and independent encoding vectors on each advertised ABI and callback model."),
    "wire": (["proto/rti/v1/stream.proto", "rti/internal/adapters/grpc/server.go", "rti/internal/application/server/run.go"], "Verify additive descriptor compatibility and actual SDK-adapter-domain-callback effects; descriptor presence alone never proves implementation."),
    "quality": (["scripts/ieee2010_quality_gate.py", REPORT], "Execute exact expected cases on a frozen source/profile; reject missing/skipped/stale evidence and preserve external logs and source digests."),
    "normative": (["engineering/verification/ieee1516-2010-requirements-matrix.md", "engineering/verification/ieee1516-2010-quality-gates.md"], "Review authorized 2010 fulltexts and corrections by locator/responsible party; supplementary API/MIM artifacts cannot settle whole-standard applicability."),
}

# These are bounded historical dispositions, never current full-service closure.
AUDIT = {
    "C01": ("binding", "Transportation APIs and callback forwarding added; independent official-header/ABI coverage remains open."),
    "C02": ("object", "Order changes and caller-local knowledge are implemented in later live-service records; old no-op descriptions are superseded."),
    "C03": ("federation", "Six resign actions and authenticated membership cleanup replaced ignored-action behavior; normative action/error precedence stays Q02."),
    "C04": ("federation", "Name/type, float64 factory validation and dynamic FOM join implemented; custom-MIM semantics and factory applicability remain open."),
    "C05": ("object", "Common callback metadata, binary tags, order, transport, producer, regions and retraction identity now flow through live services; overload-complete proof remains open."),
    "C06": ("mom", "Four-byte switch decoding and validate-before-mutate are bounded historical fixes, not complete MOM closure."),
    "C07": ("save", "Progress/status/failure callbacks, timed saves, full-state restore and identity rebinding implemented; all legal-state obligations remain separately unverified."),
    "C08": ("declaration", "Active/passive, advisories, regional demand and rate queries implemented; old ignored-option findings superseded."),
    "C09": ("support", "Support knowledge and automatic resign are real services, including authenticated disconnect grace/cleanup; not blanket lookup closure."),
    "C10": ("time", "Float64 is the actual wire/domain profile; unsupported C++ factories reject explicitly. Integer/custom exact time and pending-enable semantics remain open."),
    "N01": ("federation", "Deleting resign enumerates real state through prepared lifecycle operations; old sparse-handle bound is superseded."),
    "N02": ("ownership", "Acquisition/query preconditions and complete-set validation have bounded regressions; missing-key-as-unowned finding is superseded."),
    "N03": ("ownership", "Current attribute ownership and deletion privilege now govern authority; original registrant alone is no longer the claimed authority."),
    "N04": ("ownership", "Tags and atomic divest results are retained; selection/cancellation regressions replace historical post-query inference."),
    "N05": ("save", "Operation reservation and prepare-before-publish restore replace apply-before-busy-check behavior."),
    "N06": ("save", "Operation identity, finalization and save/restore admission guards replace stale terminal cleanup behavior."),
    "N07": ("ownership", "Two-phase pending ownership state is persisted; verify restored continuation, not reduced-struct equality."),
    "N08": ("binding", "Callback disabling now gates synthesized and wire callbacks in C++; do not retain the old bypass as a current defect."),
    "N09": ("binding", "C++ immediate dispatch and Python strict evoked/immediate dispatch are implemented in later checkpoints; exact reentry/threading obligations remain Q09."),
    "N10": ("mom", "Inherited target and framed handles are handled; reporting feedback exclusions now have regressions. Complete lifecycle reporting is still open."),
    "N11": ("ddm", "Reversed/over-limit pending bounds reject atomically; endpoint/default-region semantics are still Q09."),
    "N12": ("fom", "Optional FDD metadata and 168 service vocabulary entries are accepted/validated in bounded parser fixes; full OMT responsibilities remain Q03."),
    "F01": ("save", "Full cross-manager checkpoint state, FOM binding image and restored continuation implemented; historical callback-only restore characterization superseded."),
    "F02": ("save", "Operation admission, source generation selection, state graph validation, identity rebinding and restart restore implemented."),
    "F03": ("save", "Scheduled save crossing, zero lookahead, participant status and failure reasons implemented; complete normative scheduling review remains Q04/Q07."),
    "F04": ("object", "Actual callback metadata and order/transport state persist across queues/checkpoints; old synthesized-default finding superseded."),
    "F05": ("object", "Legacy transport routes and authority share corrected paths; exact standard pending/confirmation behavior remains Q05."),
    "F06": ("object", "Local deletion changes caller knowledge and hydration uses authoritative knowledge; peers remain unaffected and rediscovery is covered."),
    "F07": ("declaration", "Active/passive, inherited/regional advisories and rate queries implemented with rollback on admission failure."),
    "F08": ("support", "Support lookups, actual resign directive, dimension/rate queries and float64 factory implemented; factory scope remains unresolved."),
    "F09": ("binding", "Immediate callbacks and lifecycle/reentry corrections implemented; accepted-model-with-no-dispatch finding superseded."),
    "F10": ("time", "NaN/invalid advance targets reject without poisoning pending state; separate time-primitive oracles/pending enables still need review."),
    "F11": ("ddm", "Atomic regional registration has domain, wire and SDK paths; method-name-only tests are not the sole evidence anymore."),
    "F12": ("ddm", "Per-call regional routing and atomic mixed-pair validation implemented; complete DDM state/exception combinations remain open."),
    "F13": ("object", "Update requests route to current attribute owners with caller knowledge and region filtering; old registration-owner route superseded."),
}
QUESTIONS = {
    "Q01": "Claim scope, mandatory bindings/platforms, callback models, time factories and corrections.",
    "Q02": "Ownership/resign preconditions, ordering, callbacks, tag sources, automatic cleanup and errors.",
    "Q03": "OMT/modular/custom-MIM constraints and RTI versus model-author responsibilities.",
    "Q04": "Save/restore permitted states, completeness, failure, scheduled time and identity obligations.",
    "Q05": "Object knowledge, transport/order changes, callback overloads and retraction eligibility.",
    "Q06": "Active/passive declarations, advisory edges, inheritance, DDM and rate selection.",
    "Q07": "Time representations, pending enables, five advance primitives, bounds and lookahead.",
    "Q08": "Complete MOM target/report/periodic/counting/encoding behavior and exclusions.",
    "Q09": "Support and DDM legal states, boundaries, callback budgets/threading and reentry.",
    "Q10": "Independent RTI assessment subject/scope and actual external certification evidence.",
}

CURRENT_BATCH = {
    "source": "Main handoff in this inventory task, 2026-09-20; no new source freeze supplied",
    "verification": "reported-not-independently-verified; does not change baseline closure status",
    "M1": "Host, durable last-save history and realized currentFDD implementation underway with user approval.",
    "M2_elapsed": "Elapsed-time subset ready for independent integration review; not promoted by this inventory.",
    "M2_queues": "RO/TSO end-to-end delivery ledger remains unassigned.",
    "cpp_abi": "Official C++ probe reproduced float64 constructor ABI/named-factory defects; Parfit reports fixes. Strict official-header ownership/exception incompatibilities remain open; integer64 execution remains unsupported.",
    "python_full_campaign": {"inventoried_cases": 1069, "passed": 1062, "failed": 2, "skipped": 5,
                             "disposition": "not a passing full campaign; fixes underway, including four placeholder cross-language tests"},
}

CURRENT_BATCH = {
    "source": "Main handoff in this inventory task, 2026-09-20; no new source freeze supplied",
    "verification": "reported-not-independently-verified; does not change baseline closure status",
    "M1": "Host, durable last-save history and realized currentFDD implementation underway with user approval.",
    "M2_elapsed": "Elapsed-time subset ready for independent integration review; not promoted by this inventory.",
    "M2_queues": "RO/TSO end-to-end delivery ledger remains unassigned.",
    "cpp_abi": "Official C++ probe reproduced float64 constructor ABI/named-factory defects; Parfit reports fixes. Strict official-header ownership/exception incompatibilities remain open; integer64 execution remains unsupported.",
    "python_full_campaign": {"inventoried_cases": 1069, "passed": 1062, "failed": 2, "skipped": 5,
                             "disposition": "not a passing full campaign; fixes underway, including four placeholder cross-language tests"},
}


def strip_cpp_comments(text):
    return re.sub(r"/\*.*?\*/|//[^\n]*", lambda m: "\n" * m[0].count("\n"), text, flags=re.S)


def cpp_methods(text):
    clean = strip_cpp_comments(text)
    for match in re.finditer(r"\bvirtual\s+([^;]+);", clean):
        declaration = " ".join(match[1].split())
        if declaration.startswith("~"):
            continue
        method = re.match(r"(.+?)\s+(\w+)\s*\(", declaration)
        if not method:
            raise ValueError(f"unparsed virtual declaration: {declaration}")
        result, name = method.groups()
        start, depth, end = method.end(), 1, method.end()
        quoted, escaped = None, False
        for end in range(start, len(declaration)):
            char = declaration[end]
            if quoted:
                if char == quoted and not escaped:
                    quoted = None
                escaped = char == "\\" and not escaped
                continue
            if char in {'"', "'"}:
                quoted = char
            elif char == "(":
                depth += 1
            elif char == ")":
                depth -= 1
                if depth == 0:
                    break
        if depth:
            raise ValueError(f"unbalanced virtual declaration: {declaration}")
        parameters, tail = declaration[start:end], declaration[end + 1:].strip()
        exceptions = re.search(r"throw\s*\((.*?)\)", tail)
        yield {"name": name, "returns": result, "parameters": parameters,
               "exceptions": [s.strip() for s in exceptions[1].split(",") if s.strip()] if exceptions else [],
               "qualifiers": "const" if re.match(r"const\b", tail) else "",
               "line": clean[:match.start()].count("\n") + 1}


def service_group(name):
    name = name.lower()
    for group, words in [
        ("save", ("save", "restore")), ("ownership", ("ownership", "divest", "acquisition")),
        ("ddm", ("region", "dimension", "rangebounds")),
        ("time", ("time", "lookahead", "galt", "lits", "asynchronous", "nextmessage", "flushqueue", "retract")),
        ("declaration", ("publish", "subscribe", "advisory", "turnupdates", "turninteractions", "registrationfor", "scope")),
        ("federation", ("federation", "resign", "connect", "synchronization", "synchronized")),
        ("support", ("get", "decode", "normalize")),
        ("object", ("object", "attribute", "interaction")),
    ]:
        if any(word in name for word in words):
            return group
    return "binding"


def definition(element):
    """Retain structural encoding declarations without copying explanatory prose."""
    if not list(element):
        return (element.text or "").strip()
    result = {}
    for child in element:
        if child.tag in {"semantics", "notes", "description"}:
            continue
        result.setdefault(child.tag, []).append(definition(child))
    return result


class Inventory:
    def __init__(self, root, official):
        self.root, self.official = Path(root), Path(official)
        self.sources, self.rows = {}, []

    def source(self, path, official=False):
        source_id = ("official:" if official else "repo:") + path
        if source_id not in self.sources:
            base = self.official if official else self.root
            self.sources[source_id] = {"id": source_id, "path": path, "root": "official" if official else "repository", "sha256": gate.file_hash(base / path)}
        return source_id

    def add(self, name, kind, path, locator, group="binding", status="unverified", rationale=None, official=False, **extra):
        row = {"id": name, "kind": kind, "status": status,
               "applicability": "unresolved" if status == "normativeunknown" else "product-required",
               "anchors": [{"source": self.source(path, official), "locator": locator}],
               "trace": group, "profiles": ["cpp-linux", "cpp-windows"] if official else ["go-windows", "go-linux", "python-windows", "python-linux", "cpp-linux", "cpp-windows"],
               "facets": {key: "normativeunknown" if status == "normativeunknown" else "unverified" for key in sorted(gate.FACETS)},
               "rationale": rationale or "Declaration inventoried; exact implementation trace and stateful per-overload/profile oracle remain unverified. Presence is not completion.",
               "blocking_dependencies": ["authorized-text-review"] if status == "normativeunknown" else ["per-row-trace-and-oracle-review", "frozen-profile-execution"],
               "required_tests": [f"planned:{group}:stateful-boundaries"], "evidence": [], "reviewer": None, **extra}
        self.rows.append(row)
        return row

    def scan_cpp(self):
        for base, prefix, official in [(self.official, "official-api", True), (self.root / "cppsdk/include", "cpp-sdk", False)]:
            for filename in ["RTI/RTIambassador.h", "RTI/FederateAmbassador.h"]:
                path = filename if official else "cppsdk/include/" + filename
                for contract in cpp_methods((base / filename).read_text(encoding="utf-8-sig")):
                    identity = {k: v for k, v in contract.items() if k != "line"}
                    name = f"{prefix}:{Path(filename).stem}.{contract['name']}:{gate.digest(identity)[:16]}"
                    row = self.add(name, "cpp-callback" if "FederateAmbassador" in filename else "cpp-service-overload", path, f"{Path(filename).stem}::{contract['name']}@{contract['line']}", service_group(contract["name"]), official=official, contract=contract)
                    if "RTIambassador" in filename:
                        implementation = "cppsdk/src/dlc/RTIambassadorImpl.cpp"
                        marker = "DLCRTIambassadorImpl::" + contract["name"]
                        if marker in (self.root / implementation).read_text(encoding="utf-8-sig"):
                            row["anchors"].append({"source": self.source(implementation), "locator": marker})
        for path in sorted(self.official.rglob("*.h")):
            relative = path.relative_to(self.official).as_posix()
            if relative in {"RTI/RTIambassador.h", "RTI/FederateAmbassador.h"}:
                continue
            self.add("official-support:" + relative, "cpp-support-header", relative, "whole header: member/ABI audit pending", official=True,
                     rationale="Pinned official factory/encoding/handle/type support header; member-by-member semantic and ABI audit remains open, not silently excluded.")
        for path in sorted((self.root / "cppsdk/include").rglob("*.h")):
            relative = path.relative_to(self.root).as_posix()
            if relative.endswith(("RTI/RTIambassador.h", "RTI/FederateAmbassador.h")):
                continue
            self.add("cpp-support:" + relative, "cpp-support-header", relative, "whole header: product surface audit pending")

    def scan_python(self):
        for path in sorted((self.root / "pysdk/rti1516e").rglob("*.py")):
            relative = path.relative_to(self.root).as_posix()
            if path.name == "__init__.py" and "_generated" not in path.parts:
                tree = ast.parse(path.read_text(encoding="utf-8-sig"))
                exports = []
                origins = {}
                for node in tree.body:
                    if isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "__all__" for t in node.targets):
                        exports = ast.literal_eval(node.value)
                    if isinstance(node, ast.ImportFrom):
                        origins.update({a.asname or a.name: {"module": node.module, "level": node.level, "name": a.name} for a in node.names})
                self.add("python-exports:" + relative, "python-export-manifest", relative, "__all__; re-export imports", contract={"exports": exports, "origins": origins}, profiles=["python-windows", "python-linux"], rationale="Package exports are explicit, including symbols sourced from private modules. Their implementation/member semantics still require per-export tracing.")
                for origin in origins.values():
                    module = origin["module"] or ""
                    if module.startswith("rti1516e._"):
                        candidate = "pysdk/" + module.replace(".", "/") + ".py"
                        if (self.root / candidate).is_file():
                            self.source(candidate)
                continue
            if "_generated" in path.parts or path.name.startswith("_"):
                continue
            tree = ast.parse(path.read_text(encoding="utf-8-sig"))
            def visit(nodes, owner=""):
                for node in nodes:
                    if isinstance(node, ast.ClassDef) and not node.name.startswith("_"):
                        qualified = owner + node.name
                        fields = [{"name": ast.unparse(n.target), "type": ast.unparse(n.annotation), "default": ast.unparse(n.value) if n.value else None} for n in node.body if isinstance(n, ast.AnnAssign)]
                        self.add(f"python:{relative}:{qualified}", "python-type", relative, f"{qualified}@{node.lineno}", service_group(qualified), contract={"bases": [ast.unparse(b) for b in node.bases], "fields": fields}, profiles=["python-windows", "python-linux"])
                        visit(node.body, qualified + ".")
                    if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and not node.name.startswith("_"):
                        qualified = owner + node.name
                        self.add(f"python:{relative}:{qualified}", "python-callable", relative, f"{qualified}@{node.lineno}", service_group(node.name), contract={"parameters": ast.unparse(node.args), "returns": ast.unparse(node.returns) if node.returns else None, "async": isinstance(node, ast.AsyncFunctionDef)}, profiles=["python-windows", "python-linux"])
            visit(tree.body)

    def scan_go(self):
        command = ["go", "run", str(Path(__file__).with_name("ieee2010_inventory_go.go")), str(self.root / "rti/pkg/federate")]
        result = subprocess.run(command, capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError(f"Go AST scanner failed: {result.stderr}")
        for item in json.loads(result.stdout):
            path = "rti/pkg/federate/" + item["file"]
            self.add(f"go:{path}:{item['name']}", item["kind"], path, f"{item['name']}@{item['line']}", service_group(item["name"].rsplit(".", 1)[-1]), contract={"declaration": item["contract"]}, profiles=["go-windows", "go-linux"])

    def scan_wire(self):
        for path in sorted((self.root / "proto/rti/v1").glob("*.proto")):
            relative = path.relative_to(self.root).as_posix()
            generated = "pysdk/rti1516e/_generated/rti/v1/" + path.stem + "_pb2.py"
            tree = ast.parse((self.root / generated).read_text(encoding="utf-8-sig"))
            payloads = [ast.literal_eval(n.args[0]) for n in ast.walk(tree) if isinstance(n, ast.Call) and isinstance(n.func, ast.Attribute) and n.func.attr == "AddSerializedFile"]
            if len(payloads) != 1:
                raise ValueError(f"expected one protobuf descriptor in {generated}")
            descriptor = descriptor_pb2.FileDescriptorProto.FromString(payloads[0])
            self.source(generated)
            for service in descriptor.service:
                for method in service.method:
                    name = descriptor.package + "." + service.name + "/" + method.name
                    self.add("rpc:" + name, "wire-rpc", relative, name, service_group(method.name), contract={"request": method.input_type, "response": method.output_type, "client_stream": method.client_streaming, "server_stream": method.server_streaming})
            def enums(values, owner):
                for enum in values:
                    name = owner + "." + enum.name
                    self.add("wire-enum:" + name, "wire-enum", relative, name, "wire", contract={"values": {v.name: v.number for v in enum.value}})
            def messages(values, owner):
                for message in values:
                    name = owner + "." + message.name
                    fields = [{"name": f.name, "number": f.number, "type": f.type_name or descriptor_pb2.FieldDescriptorProto.Type.Name(f.type), "label": descriptor_pb2.FieldDescriptorProto.Label.Name(f.label), "optional_presence": f.proto3_optional, "oneof": message.oneof_decl[f.oneof_index].name if f.HasField("oneof_index") else None} for f in message.field]
                    self.add("message:" + name, "wire-message", relative, name, "wire", contract={"fields": fields, "map_entry": message.options.map_entry})
                    messages(message.nested_type, name)
                    enums(message.enum_type, name)
            messages(descriptor.message_type, descriptor.package)
            enums(descriptor.enum_type, descriptor.package)

    def scan_mim(self):
        tree = ET.parse(self.root / MIM)
        for element in tree.iter():
            element.tag = element.tag.rsplit("}", 1)[-1]
        def text(element, tag):
            return (element.findtext(tag) or "").strip()
        def walk(element, owner="", inherited=()):
            name = text(element, "name")
            qualified = (owner + "." if owner else "") + name
            fields = "attribute" if element.tag == "objectClass" else "parameter"
            own = [text(child, "name") for child in element.findall(fields)]
            self.add(f"mim:{element.tag}:{qualified}", "mim-" + element.tag, MIM, qualified, "mom", contract={"declared_members": own, "inherited_members": list(inherited)})
            for child in element.findall(fields):
                field = qualified + "." + text(child, "name")
                self.add(f"mim:{fields}:{field}", "mim-" + fields, MIM, field, "mom", contract={"data_type": text(child, "dataType"), "order": text(child, "order"), "transportation": text(child, "transportation"), "sharing": text(child, "sharing")})
            for child in element.findall(element.tag):
                walk(child, qualified, (*inherited, *own))
        for group, tag in [("objects", "objectClass"), ("interactions", "interactionClass")]:
            for element in tree.getroot().findall(f"{group}/{tag}"):
                walk(element)
        for group in tree.getroot().findall("dataTypes/*"):
            for element in group:
                name = text(element, "name")
                if not name:
                    continue
                self.add("mim:datatype:" + name, "mim-datatype", MIM, "dataTypes/" + group.tag + "/" + name, "mom", contract={"kind": element.tag, "definition": definition(element)})
        for container in ["dimensions", "transportations"]:
            for element in tree.getroot().findall(container + "/*"):
                name = text(element, "name")
                self.add(f"mim:{element.tag}:{name}", "mim-" + element.tag, MIM, container + "/" + name, "mom", contract={"definition": definition(element)})

    def reconcile(self):
        matrix = "engineering/verification/ieee1516-2010-requirements-matrix.md"
        assessment = "engineering/verification/ieee1516-2010-conformance-assessment.md"
        audit = "engineering/verification/ieee1516-2010-full-rti-audit.md"
        for name, (group, reason) in AUDIT.items():
            original = assessment if name.startswith("C") else matrix if name.startswith("N") else audit
            row = self.add(name, "audit", original, name, group, rationale=reason,
                           historical_disposition="bounded-fix-recorded" if name != "C10" else "partial-fix-and-profile-question",
                           priority="P1", evidence=["historical-remaining-services"])
            row["anchors"].append({"source": self.source(REPORT), "locator": "Implementation; Verification Status; Evidence Boundary"})
        for name, reason in QUESTIONS.items():
            self.add(name, "audit", matrix, name, "normative", "normativeunknown", reason, priority="P0-claim")
        profiles = {
            "go-windows": "Historical 71-package normal and vet campaigns; fresh frozen-candidate run required.",
            "go-linux": "Historical 71-package Go 1.22.2 race campaign; fresh frozen-candidate run required.",
            "python-windows": "Historical selected SDK campaign 676 passes is not all pysdk/tests; collect exact applicable full test inventory.",
            "python-linux": "Independent Linux Python product-profile execution and full test selection remain unverified.",
            "cpp-linux": "Historical 200 runtime cases and two production scenarios are bounded, not all official API services/overloads.",
            "cpp-windows": "No native Windows C++ compiler/ABI/runtime campaign established by the Linux results.",
            "python-tls": "Three TLS dependency skips in latest historical report; cryptography install and actual rerun required, not skip-to-pass relabeling.",
            "cpp-tsan": "Pre-fix race reproduced; post-fix process could not start (unexpected memory mapping), dependencies not fully instrumented. No clean TSAN result.",
            "cpp-official-abi": "Official-header independent executable include/link provenance and advertised toolchain/ABI coverage remain open.",
            "java-wsdl": "Official distribution includes Java/WSDL; mandatory-versus-conditional binding obligation unresolved without fulltexts. Neither automatic duty nor exemption.",
        }
        for name, reason in profiles.items():
            self.add(name, "profile", REPORT, "Verification Status; Evidence Boundary", "normative" if name == "java-wsdl" else "quality", "normativeunknown" if name == "java-wsdl" else "unverified", reason, profiles=[name], priority="P1")
        for name, reason in {
            "HLAfederateHost": "Missing authoritative opt-in client host source; peer IP is not an established substitute.",
            "HLAROlength": "No complete ledger across server outbox and SDK callback queues; optional ROQueue is not wired in baseline production composition.",
            "HLATSOlength": "Time buffer alone excludes deliveries already moved to outbox/client and cannot supply the total.",
            "HLAtimeGrantedTime": "No complete granted elapsed-duration source and per-attribute successful-publication baseline.",
            "HLAtimeAdvancingTime": "No complete advancing elapsed-duration source and per-attribute successful-publication baseline.",
            "HLAcurrentFDD": "No realized committed merged FDD XML exporter in baseline; source-module concatenation is not the value.",
            "HLAlastSaveName": "No authoritative durable completed-save history source in baseline metrics composition.",
            "HLAlastSaveTime": "No authoritative durable completed-save history source distinguishing no-save-yet from unknown old history.",
        }.items():
            row = self.add("gap:mom:" + name, "implementation-gap", "rti/internal/application/server/mom_metrics_sources.go", "configureMOMMetrics", "mom", rationale=reason, priority="P1", finding_basis="source-inspection-and-latest-report; not a fresh behavioral RED")
            row["anchors"].append({"source": self.source(MIM), "locator": name})
        for name, group, path, locator, reason in [
            ("mom-caller-attribution", "mom", "rti/internal/application/server/run.go", "newMOMRuntimeObservers", "Production composition does not supply ResolveCaller; requests lacking a federate handle can remain unattributed. Determine legitimate identity and applicability rather than assign an arbitrary member."),
            ("mom-post-resign-report", "mom", "rti/internal/adapters/grpc/mom_observer.go", "UnaryInterceptor; Observe", "Observation follows handler completion while resign destroys MOM membership. Capture/report lifecycle boundary requires dedicated stateful proof; service-name mapping is insufficient."),
            ("custom-mim-creation", "fom", "cppsdk/src/dlc/RTIambassadorImpl.cpp", "createFederationExecutionWithMIM", "Actual method prepends custom MIM to ordinary module list, and CreateFederationRequest has no separate MIM role. Validate custom-MIM constraints, composition and reported MIM designator; do not infer failure from a stale comment alone."),
            ("python-standard-parity", "binding", "pysdk/rti1516e/standard.py", "Rti1516eAmbassador", "No same-name createFederationExecutionWithMIM/getTimeFactory/nine decodeHandle methods in baseline standard adapter. Creation omits time designator and join signature omits federate type. Product parity/application applicability requires explicit decision; no automatic Python normative binding claim."),
            ("time-enable-pending", "time", "rti/internal/hla/time/manager.go", "EnableTimeRegulation; EnableTimeConstrained", "Independent pending-enable and competing five-primitive scheduling oracles remain open after numeric-target fix; no fresh missing-service defect inferred."),
            ("full-python-selection", "quality", REPORT, "Verification Status", "Selected 676-case campaign is not all SDK tests; inventory historical launchers and explicitly triage unrelated RL dependencies/exclusions."),
            ("immutable-release-performance", "quality", "engineering/design/ieee1516-2010-feature-completion-plan.md", "M6; M7", "Fresh same-source integrated profiles, independent reference scenarios and paired N=2/4/8/16 performance budgets remain open."),
            ("baseline-exclusions", "quality", "engineering/design/ieee1516-2010-feature-completion-plan.md", "M0", "Baseline manifest warnings about inaccessible legacy .qa/pytest directories are scope exclusions, not proof about implementation or source completeness. Review unavailable paths individually."),
        ]:
            self.add("gap:" + name, "review-gap", path, locator, group, rationale=reason, priority="P1", finding_basis="bounded-source-inspection; behavioral reproduction not yet recorded")

    def build(self):
        self.scan_cpp()
        self.scan_python()
        self.scan_go()
        self.scan_wire()
        self.scan_mim()
        self.reconcile()
        evidence_path = "engineering/verification/ieee1516-2010-evidence.json"
        evidence_manifest = gate.load_json(self.root / evidence_path)
        self.source(evidence_path)
        aliases = {"createfederation": "createfederationexecution", "destroyfederation": "destroyfederationexecution", "joinfederation": "joinfederationexecution", "resignfederation": "resignfederationexecution", "listfederations": "listfederationexecutions", "querysavestate": "queryfederationsavestatus", "queryrestorestate": "queryfederationrestorestatus"}
        symbols = collections.defaultdict(list)
        for row in self.rows:
            if row["kind"] in {"cpp-service-overload", "python-callable", "go-callable", "wire-rpc"}:
                name = row.get("contract", {}).get("name")
                if not name:
                    name = row["id"].rsplit("/", 1)[-1] if row["kind"] == "wire-rpc" else row["id"].rsplit(":", 1)[-1].rsplit(".", 1)[-1]
                key = re.sub("[^a-z0-9]", "", name.lower())
                symbols[aliases.get(key, key)].append(row["id"])
        for row in self.rows:
            if row["id"].startswith("official-api:") and row["kind"] == "cpp-service-overload":
                key = row["contract"]["name"].lower()
                row["surface_links"] = {"basis": "name-and-explicit-alias candidates only; semantic mapping unverified", "ids": sorted(i for i in symbols[key] if i != row["id"])}
        traces = {}
        for name, (paths, oracle) in GROUPS.items():
            actual = [p for p in paths if (self.root / p).is_file()]
            for path in actual:
                self.source(path)
            traces[name] = {"implementation_paths": actual, "oracle": oracle, "mapping_status": "unverified",
                            "boundary": "candidate component trace, not verified per-overload end-to-end coverage",
                            "historical_test_group_candidates": [t["id"] for t in evidence_manifest["tests"] if name in (t["id"] + " " + t["package"]) or (name == "save" and "checkpoint" in t["id"]) or (name == "declaration" and "update-rate" in t["id"])],
                            "facet_oracles": {
                                "inputs": "Each declared parameter/overload, omitted optional, invalid enum, empty set and mixed valid/invalid batch must have an explicit disposition.",
                                "exceptions": "Review each declared C++ exception and wire/SDK translation against actual legal state, including empty messages; precedence remains normativeunknown.",
                                "lifecycle": "Disconnected, connected, joined, save/restore active, resign/rejoin, stale session and callback epoch are separate cases, or require reviewed NA.",
                                "post_state": oracle,
                                "callbacks": "Assert actual callback identity/payload, recipient and required partial order through real SDK dispatch; distinguish evoked/immediate and disable/re-enable.",
                                "failure_concurrency": "Exercise admission failure, full queue, competing mutation, disconnect, retry and no partial effects; independent race execution required.",
                                "persistence": "Check source selection, round trip, restart/restore, migration and missing-history states or obtain a reviewed per-row NA.",
                                "binding_encoding": "Independent vectors and exact parameter/callback preservation on declared language/platform/ABI; names do not imply parity.",
                            }}
        self.rows.sort(key=lambda r: r["id"])
        ids = [r["id"] for r in self.rows]
        return {"schema_version": 1, "claim": "inventory-only", "as_of": "2026-09-20",
                "source_snapshot": "feature-completion-baseline-20260920-195349",
                "current_batch_reported": CURRENT_BATCH,
                "current_batch_reported": CURRENT_BATCH,
                "baseline": {"path": "D:/workspace/gorti/feature-completion-baseline-20260920-195349", "manifest_sha256": "eb82812afeb9dd6fb9f9193e30bc436a5d4844d2d898d0e79ba5281cd49d1c2a", "source_zip_sha256": "a4949ce0cd2d4785072c6716f76b511596c6637d1c4ea75d7fabe7b8d28cdd08", "exclusion_policy": "Inaccessible legacy QA directories and unavailable tracked paths are scope exclusions to review, never positive source/implementation proof."},
                "official_provenance": {"archive_url": "https://standards.ieee.org/wp-content/uploads/import/download/1516.1-2010_downloads.zip", "archive_sha256": "e6343f40b066d51b7f435c9aae6788be24f5d585e370d972c032ab0b503e27c1", "mim_sha256": "839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023", "fulltexts_available": False},
                "claims": {"engineering_complete": False, "conformance_established": False, "certification": False},
                "attribution": "Reprinted with permission from IEEE 1516.1(TM)-2010",
                "scope": "Declared C++ ambassador overloads/callbacks and support-header obligations; MIM class/attribute/interaction/parameter/datatype rows; generated wire descriptors; exported Go and public Python SDK declarations. This is an engineering discovery inventory, not a complete normative or per-state obligation matrix.",
                "limitations": ["Supplemental official artifacts do not replace the unavailable full normative texts.", "C++ support headers require member-level audit; inherited Python mixin behavior and dynamic exports need independent product review.", "Wire descriptors are read structurally from generated Python stubs and pinned together with proto source; independent regeneration parity remains the existing descriptor gate's responsibility.", "All historical fixes remain unverified for the new candidate until profile-specific stateful evidence is attached; missing current evidence does not mean missing implementation.", "Source drift must be reconciled after parallel workers finish. This baseline inventory is not a release freeze.", "The scanner excludes SDK private implementation names and Go test files from public API discovery, not from implementation/test evidence.", "Row counts are inventory counts, never conformance percentages."],
                "sources": sorted(self.sources.values(), key=lambda s: s["id"]), "traces": traces,
                "historical_test_groups": evidence_manifest["tests"],
                "expected_ids": ids, "surface_digest": gate.digest(ids), "rows": self.rows,
                "evidence": [{"id": "historical-remaining-services", "kind": "historical-report", "path": REPORT, "sha256": self.sources["repo:" + REPORT]["sha256"], "snapshot": "historical-unattested-working-tree", "result": "bounded-pass-and-explicit-gaps", "fresh": False}]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--official-root", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--compare", type=Path)
    args = parser.parse_args()
    data = Inventory(args.root, args.official_root).build()
    if args.output:
        # One object per row keeps the mechanical catalog reviewable in diffs.
        sections = []
        for key, value in data.items():
            if key == "rows":
                encoded = "[\n" + ",\n".join("    " + json.dumps(row, separators=(",", ":")) for row in value) + "\n  ]"
            else:
                encoded = json.dumps(value, indent=2).replace("\n", "\n  ")
            sections.append("  " + json.dumps(key) + ": " + encoded)
        args.output.write_text("{\n" + ",\n".join(sections) + "\n}\n", encoding="utf-8")
    counts = dict(sorted(collections.Counter(r["kind"] for r in data["rows"]).items()))
    result = {"rows": len(data["rows"]), "sources": len(data["sources"]), "kinds": counts, "surface_digest": data["surface_digest"]}
    if args.compare:
        old = gate.load_json(args.compare)
        before, after = set(old["expected_ids"]), set(data["expected_ids"])
        result.update(removed=sorted(before - after), added=sorted(after - before))
        if before != after:
            print(json.dumps(result, indent=2))
            return 1
    print(json.dumps(result, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
