#!/usr/bin/env python3
"""Build a self-contained DSM 7.2.2+ SPK using Go and Python 3."""
import argparse
import hashlib
import io
import json
import os
import re
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tarfile
import tempfile
import zlib

ROOT = Path(__file__).resolve().parents[1]
# DSM package families differ from Go/Alpine CPU names.
PACKAGE_ARCH = {"x86_64": "x86_64", "aarch64": "armv8"}
VERSION = (ROOT / "internal/app/version.txt").read_text().strip()
if not re.fullmatch(r"\d+\.\d+\.\d+", VERSION):
    raise ValueError("invalid release version")
sys.path.insert(0, str(ROOT / "tools"))
import runtime
import check_spk


def icon(size):
    pixels = bytearray()
    for y in range(size):
        pixels.append(0)
        for x in range(size):
            body = size*.18 < x < size*.78 and size*.26 < y < size*.75
            tip = size*.78 <= x < size*.86 and size*.41 < y < size*.60
            fill = size*.25 < x < size*.65 and size*.34 < y < size*.67
            pixels.extend((230, 255, 247, 255) if fill else (17, 122, 109, 255) if body or tip else (0, 0, 0, 0))
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind+data))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(bytes(pixels))) + chunk(b"IEND", b"")


def archive(source, dest, mode):
    with tarfile.open(dest, mode, format=tarfile.USTAR_FORMAT) as t:
        for path in sorted(source.rglob("*")):
            info = t.gettarinfo(str(path), arcname=path.relative_to(source).as_posix())
            info.uid = info.gid = 0
            info.uname = info.gname = "root"
            info.mtime = 0
            if path.is_file() and not path.is_symlink():
                with path.open("rb") as f:
                    t.addfile(info, f)
            else:
                t.addfile(info)


def package_info(arch):
    return (ROOT / "packaging/INFO").read_text().replace("@ARCH@", PACKAGE_ARCH[arch]).replace("@VERSION@", VERSION)


def reuse_runtime(source, manifest, dest):
    """Reuse a validated, unchanged runtime from an earlier local build."""
    check_spk.check(source)
    checksum = source.with_suffix('.spk.sha256').read_text().split()[0]
    if hashlib.sha256(source.read_bytes()).hexdigest() != checksum:
        raise ValueError('source SPK SHA256 mismatch')
    with tarfile.open(source) as spk:
        payload = spk.extractfile('package.tgz').read()
    with tarfile.open(fileobj=io.BytesIO(payload), mode='r:gz') as package:
        old_manifest = json.load(package.extractfile('nut/runtime-manifest.json'))
        if old_manifest != manifest:
            raise ValueError('source SPK runtime does not match the selected architecture and lock file')
        package.extractall(dest.parent, members=[m for m in package.getmembers() if m.name == 'nut' or m.name.startswith('nut/')], filter='data')


def build(arch, out, runtime_from_spk=None):
    with tempfile.TemporaryDirectory(prefix="ups-spk-") as temp:
        temp = Path(temp)
        package, spk = temp / "target", temp / "spk"
        (package / "bin").mkdir(parents=True)
        shutil.copytree(ROOT / "packaging", spk)
        shutil.move(str(spk / "ui"), package / "ui")
        shutil.move(str(spk / "nginx"), package / "nginx")
        (package / "ui/images").mkdir()
        env = dict(os.environ, GOOS="linux", GOARCH={"x86_64": "amd64", "aarch64": "arm64"}[arch], GOAMD64="v1", CGO_ENABLED="0")
        subprocess.run(["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(package / "bin/upsdsm"), "./cmd/upsdsm"], cwd=ROOT, env=env, check=True)
        manifest = json.loads((ROOT / f"tools/runtime-{arch}.lock.json").read_text())
        if runtime_from_spk:
            reuse_runtime(runtime_from_spk, manifest, package / "nut")
        else:
            runtime.extract(manifest, package / "nut")
        shutil.copytree(ROOT / "tools/host", package / "tools")
        # MIT grants permission without a click-through EULA. Keep its notice
        # in the installed payload instead of asking DSM to display an EULA.
        shutil.copy(ROOT.parent / "LICENSE", package / "LICENSE")
        for p in (spk / "scripts").iterdir():
            p.chmod(0o755)
        for size, name in [(64, "PACKAGE_ICON.PNG"), (256, "PACKAGE_ICON_256.PNG")]:
            (spk / name).write_bytes(icon(size))
        for size in (16, 24, 32, 48, 64, 72, 256):
            (package / "ui/images" / f"icon_{size}.png").write_bytes(icon(size))
        info = package_info(arch)
        archive(package, spk / "package.tgz", "w:gz")
        digest = hashlib.md5((spk / "package.tgz").read_bytes()).hexdigest()
        (spk / "INFO").write_text(info + f'\nchecksum="{digest}"\n')
        out.mkdir(parents=True, exist_ok=True)
        path = out / f"usb-ups-server-{VERSION}-{arch}.spk"
        archive(spk, path, "w")
        checksum = hashlib.sha256(path.read_bytes()).hexdigest()
        path.with_suffix(".spk.sha256").write_text(f"{checksum}  {path.name}\n")
        print(path)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--arch", choices=["x86_64", "aarch64"], required=True)
    parser.add_argument("--out", type=Path, default=ROOT.parent / "dist")
    parser.add_argument("--runtime-from-spk", type=Path, help="offline: reuse the exact locked runtime from a validated local SPK with its SHA256 sidecar")
    args = parser.parse_args()
    build(args.arch, args.out.resolve(), args.runtime_from_spk)
