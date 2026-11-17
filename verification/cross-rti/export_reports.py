"""Copy selected experiment reports for publication, with explicit metadata redaction."""

import argparse
import hashlib
import json
from pathlib import Path
import xml.etree.ElementTree as ET


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input-dir", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--redactions-json", required=True, type=Path,
                        help="Private JSON list of [literal value, public placeholder] pairs")
    args = parser.parse_args()
    replacements = json.loads(args.redactions_json.read_text(encoding="utf-8-sig"))
    pairs = []
    for before, after in replacements:
        for value in {before, before.replace("\\", "/"), before.replace("/", "\\")}:
            pairs.append((value, after))
            pairs.append((json.dumps(value)[1:-1], after))
    pairs.sort(key=lambda pair: len(pair[0]), reverse=True)
    args.output_dir.mkdir(parents=True, exist_ok=False)
    manifest = []
    for source in sorted(args.input_dir.rglob("*")):
        if not source.is_file() or source.suffix not in {".xml", ".json", ".log", ".java", ".patch"}:
            continue
        relative = source.relative_to(args.input_dir)
        if "save-state" in relative.parts:
            continue
        original = source.read_bytes()
        text = original.decode("utf-8")
        for before, after in pairs:
            text = text.replace(before, after)
        if source.suffix == ".xml":
            root = ET.fromstring(text)
            if root.tag == "testsuite":
                for prop in root.findall("./properties/property"):
                    if prop.get("name") in {"user.name", "java.library.path"}:
                        prop.set("value", "REDACTED_LOCAL_METADATA")
                text = ET.tostring(root, encoding="unicode", xml_declaration=True)
        if source.suffix == ".json":
            json.loads(text)
        payload = text.encode("utf-8")
        target = args.output_dir / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(payload)
        manifest.append({"path": relative.as_posix(), "original_sha256": sha(original),
                         "public_sha256": sha(payload), "changed": payload != original})
    (args.output_dir / "publication-manifest.json").write_text(json.dumps({
        "policy": "Local paths and host identifiers replaced; user name and system library path removed. "
                  "JUnit XML reserialized. Outcomes, payload assertions, timings, and runtime settings retained. "
                  "JARs, executables, class files, and saved federation state are not distributed.",
        "files": manifest}, indent=2) + "\n", encoding="utf-8")
    print(f"Exported {len(manifest)} report/source files")


if __name__ == "__main__":
    main()
