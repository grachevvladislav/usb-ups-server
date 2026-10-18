#!/usr/bin/env python3
"""Validate an SPK's metadata, privilege boundary, checksum and ELF architecture."""
import argparse
import hashlib
import io
import json
from pathlib import Path
import struct
import tarfile


def check(path, extract=None):
    with tarfile.open(path) as outer:
        names = set(outer.getnames())
        required = {"INFO", "package.tgz", "conf/privilege", "conf/resource", "PACKAGE_ICON.PNG", "PACKAGE_ICON_256.PNG", "scripts/start-stop-status", "scripts/postinst"}
        if not required <= names:
            raise ValueError(f"missing SPK entries: {required - names}")
        info = dict(line.split("=", 1) for line in outer.extractfile("INFO").read().decode().splitlines() if "=" in line)
        info = {k: v.strip('"') for k, v in info.items()}
        if info["os_min_ver"] != "7.2.2-72803":
            raise ValueError("unexpected DSM minimum")
        if any(k in info for k in ("adminurl", "adminport", "adminprotocol")):
            raise ValueError("Package Center must launch the DSM window, not an external URL")
        privilege = json.load(outer.extractfile("conf/privilege"))
        if privilege["defaults"]["run-as"] != "package" or "ctrl-script" in privilege:
            raise ValueError("package must run without root")
        resources = json.load(outer.extractfile("conf/resource"))
        if resources["web-config"]["nginx-static-config"]["enable"] != [{"type": "dsm", "relpath": "nginx/dsm.conf"}]:
            raise ValueError("DSM same-origin proxy resource is missing")
        payload = outer.extractfile("package.tgz").read()
        if hashlib.md5(payload).hexdigest() != info["checksum"]:
            raise ValueError("payload checksum mismatch")
        with tarfile.open(fileobj=io.BytesIO(payload), mode="r:gz") as inner:
            if not inner.extractfile("LICENSE").read().startswith(b"MIT License"):
                raise ValueError("project license notice is missing")
            ui = json.load(inner.extractfile("ui/config"))
            entry = ui["USBUPSServer.js"][info["dsmappname"]]
            if entry["type"] != "app" or entry["appWindow"] != info["dsmappname"] or ".url" in ui:
                raise ValueError("DSM window launcher is missing")
            if not inner.extractfile("ui/USBUPSServer.js").read():
                raise ValueError("DSM application JavaScript is missing")
            if "proxy_pass http://127.0.0.1:8788/;" not in inner.extractfile("nginx/dsm.conf").read().decode():
                raise ValueError("DSM proxy must target the loopback listener")
            arch = {"x86_64": 62, "aarch64": 183}[info["arch"]]
            for binary in ["bin/upsdsm", "nut/usr/sbin/upsd", "nut/usr/lib/nut/usbhid-ups", "nut/usr/lib/nut/nutdrv_qx"]:
                header = inner.extractfile(binary).read(20)
                if header[:5] != b"\x7fELF\x02" or struct.unpack("<H", header[18:20])[0] != arch:
                    raise ValueError(f"wrong ELF architecture: {binary}")
            for member in inner:
                if member.name.startswith(("etc/", "var/")):
                    raise ValueError("runtime must not install global configuration")
            if extract:
                extract.mkdir(parents=True, exist_ok=True)
                inner.extractall(extract, filter="data")
    print(f"Validated {path.name}: {info['arch']}, DSM {info['os_min_ver']}+, non-root")


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("spk", type=Path)
    p.add_argument("--extract", type=Path)
    args = p.parse_args()
    check(args.spk, args.extract)
