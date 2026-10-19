"""Build the nearest preserved release without changing the active checkout."""

import argparse
import json
import os
import shutil
import subprocess
import tarfile
from pathlib import Path

from compare import REPO, digest, save

REFERENCE = "5cf718a8f282d6be36cd1eed8d0efb06d388a80c"
TREE = "5017a49882d048990507d100e4250597b888f638"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    output = args.output.resolve()
    if output == REPO or REPO in output.parents:
        parser.error("reference source and builds must be outside the repository")
    go = shutil.which(args.go)
    git = shutil.which("git")
    if not go:
        parser.error("Go executable not found; provide --go")
    if not git:
        parser.error("Git executable not found")
    # Only resolved local tools and the pinned repository revision are executed.
    actual = subprocess.check_output(  # noqa: S603
        [git, "rev-parse", REFERENCE + "^{tree}"], cwd=REPO, text=True).strip()
    if actual != TREE:
        raise RuntimeError("reference tree does not match the preserved release")
    output.mkdir(parents=True, exist_ok=False)
    archive = output / "source.tar"
    subprocess.run([git, "archive", "--format=tar", "--output=" + str(archive), REFERENCE],  # noqa: S603
                   cwd=REPO, check=True)
    source = output / "source"
    source.mkdir()
    with tarfile.open(archive) as bundle:
        bundle.extractall(source, filter="data")
    suffix = ".exe" if os.name == "nt" else ""
    binaries = {}
    packages = (("rtid", "./rti/cmd/rtid"), ("gorti-go-fair", "./verification/gorti-go-fair"))
    for name, package in packages:
        binary = output / (name + suffix)
        subprocess.run([go, "build", "-o", str(binary), package], cwd=source, check=True)  # noqa: S603
        binaries[name] = {"path": str(binary), "sha256": digest(binary)}
    manifest = {
        "commit": REFERENCE, "tree": TREE, "source_sha256": digest(archive),
        "go": subprocess.check_output([go, "version"], text=True).strip(),  # noqa: S603
        "binaries": binaries,
        "scope": ("nearest preserved release, not the bit-identical July 27 benchmark build; "
                  "performance reference only"),
    }
    save(output / "reference.json", manifest)
    print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    main()
