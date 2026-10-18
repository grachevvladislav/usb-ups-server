#!/usr/bin/env python3
"""Pin Alpine NUT and shared libraries; extract a private musl runtime."""
import argparse
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import tarfile
import urllib.request

BASE = "https://dl-cdn.alpinelinux.org/alpine/v3.22"
HERE = Path(__file__).resolve().parent


def download(url):
    with urllib.request.urlopen(url, timeout=90) as r:
        return r.read()


def index(arch):
    packages, provides = {}, {}
    for repo in ("main", "community"):
        raw = download(f"{BASE}/{repo}/{arch}/APKINDEX.tar.gz")
        with tarfile.open(fileobj=io.BytesIO(raw), mode="r:gz") as t:
            content = t.extractfile("APKINDEX").read().decode()
        for block in content.split("\n\n"):
            fields = dict(line.split(":", 1) for line in block.splitlines() if ":" in line)
            if "P" not in fields:
                continue
            fields["repo"] = repo
            packages[fields["P"]] = fields
            for capability in fields.get("p", "").split():
                provides[capability.split("=", 1)[0]] = fields["P"]
    return packages, provides


def lock(arch):
    packages, provides = index(arch)
    pending, found = ["nut"], {}
    while pending:
        name = pending.pop()
        if name in found:
            continue
        p = packages[name]
        url = f"{BASE}/{p['repo']}/{arch}/{name}-{p['V']}.apk"
        print(f"Pinning {arch}: {name}-{p['V']}", flush=True)
        raw = download(url)
        found[name] = {"name": name, "version": p["V"], "url": url,
                       "sha256": hashlib.sha256(raw).hexdigest(), "license": p.get("L", ""),
                       "source_commit": p.get("c", ""), "origin": p.get("o", name), "repo": p["repo"]}
        for dep in p.get("D", "").split():
            if dep.startswith("so:"):
                pending.append(provides[dep.split("=", 1)[0]])
    result = {"arch": arch, "alpine": "3.22", "packages": sorted(found.values(), key=lambda x: x["name"])}
    (HERE / f"runtime-{arch}.lock.json").write_text(json.dumps(result, indent=2) + "\n")
    return result


def extract(manifest, dest):
    dest.mkdir(parents=True, exist_ok=True)
    for p in manifest["packages"]:
        print(f"Extracting {manifest['arch']}: {p['name']}", flush=True)
        raw = download(p["url"])
        if hashlib.sha256(raw).hexdigest() != p["sha256"]:
            raise ValueError(f"SHA256 mismatch: {p['name']}")
        # APK v2 concatenates signature, control and data gzip/tar streams.
        with tarfile.open(fileobj=io.BytesIO(raw), mode="r:gz", ignore_zeros=True) as t:
            for member in t:
                path = PurePosixPath(member.name)
                if path.is_absolute() or ".." in path.parts:
                    raise ValueError("unsafe archive path")
                if not (member.name.startswith("lib/") or member.name.startswith("usr/lib/") or member.name == "usr/sbin/upsd"):
                    continue
                if not (member.isfile() or member.issym() or member.isdir()):
                    continue
                target = dest / str(path)
                if target.is_symlink():
                    target.unlink()
                if member.isdir():
                    target.mkdir(parents=True, exist_ok=True)
                    continue
                target.parent.mkdir(parents=True, exist_ok=True)
                if member.issym():
                    link = PurePosixPath(member.linkname)
                    if link.is_absolute() or ".." in link.parts:
                        if str(link).startswith("/lib/ld-musl-"):
                            target.symlink_to(link.name)
                        else:
                            raise ValueError(f"unsafe link: {member.name} -> {link}")
                    else:
                        target.symlink_to(member.linkname)
                else:
                    target.write_bytes(t.extractfile(member).read())
                    target.chmod(0o755 if member.mode & 0o111 else 0o644)
    if len(list((dest / "lib").glob("ld-musl-*.so.1"))) != 1:
        raise ValueError("missing musl loader")
    for binary in ["usr/sbin/upsd", "usr/lib/nut/usbhid-ups", "usr/lib/nut/nutdrv_qx"]:
        if not (dest / binary).is_file():
            raise ValueError(f"missing binary: {binary}")
    (dest / "runtime-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["lock", "extract"])
    parser.add_argument("--arch", choices=["x86_64", "aarch64"], required=True)
    parser.add_argument("--dest", type=Path)
    args = parser.parse_args()
    if args.command == "lock":
        lock(args.arch)
    else:
        if args.dest is None:
            parser.error("--dest is required")
        extract(json.loads((HERE / f"runtime-{args.arch}.lock.json").read_text()), args.dest)
