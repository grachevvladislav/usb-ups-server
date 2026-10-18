#!/usr/bin/env python3
"""Build just the missing OHCI drivers against Synology's broadwellnk SDK."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
from fetch import fetch


def main():
    repo = HERE.parents[2]
    work = repo / "dist/ohci-build"
    cache = work / "cache"
    output = repo / "dist/ohci-broadwellnk-7.2-4.4.302"
    cache.mkdir(parents=True, exist_ok=True)
    output.mkdir(parents=True, exist_ok=True)
    manifest = json.loads((HERE / "sources.json").read_text())
    for entry in manifest["sources"]:
        if not entry.get("sha256"):
            raise ValueError("pin source hashes before building")
        fetch(entry, cache)
    for name in ["build-in-container.sh", "verify.py"]:
        shutil.copy(HERE / name, work / name)
    subprocess.run(["docker", "build", "--platform", "linux/amd64", "-f", str(HERE / "Dockerfile"), "-t", "usb-ups-ohci-build", str(work)], check=True)
    subprocess.run(["docker", "run", "--rm", "--platform", "linux/amd64", "--network", "none", "--cap-drop=ALL", "--security-opt=no-new-privileges", "-v", f"{output}:/output", "usb-ups-ohci-build"], check=True)
    for name in ["sources.json", "README.ru.md", "test-load.sh", "build-in-container.sh", "verify.py", "Dockerfile", "build.py", "fetch.py"]:
        shutil.copy(HERE / name, output / name)
    sums = []
    for path in sorted(output.rglob("*")):
        if path.is_file() and path.name != "SHA256SUMS":
            sums.append(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.relative_to(output)}")
    (output / "SHA256SUMS").write_text("\n".join(sums) + "\n")
    archive = output.parent / (output.name + ".tar.gz")
    with tarfile.open(archive, "w:gz") as t:
        t.add(output, arcname=output.name)
    print(archive)


if __name__ == "__main__":
    main()
