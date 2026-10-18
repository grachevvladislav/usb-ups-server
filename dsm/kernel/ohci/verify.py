#!/usr/bin/env python3
"""Reject wrong vermagic/architecture or imports absent from target exports."""
import hashlib
import json
from pathlib import Path
import struct
import subprocess
import sys


def verify(kernel, source, output):
    exports = set()
    for path in [kernel / "Module.symvers", source / "Module.symvers"]:
        for line in path.read_text().splitlines():
            fields = line.split()
            if len(fields) >= 2:
                exports.add(fields[1])
    modules = []
    for name in ["ohci-hcd", "ohci-pci"]:
        path = source / f"{name}.ko"
        raw = path.read_bytes()
        if raw[:5] != b"\x7fELF\x02" or struct.unpack("<H", raw[18:20])[0] != 62:
            raise ValueError(f"not x86-64 ELF: {name}")
        vermagic = subprocess.check_output(["modinfo", "-F", "vermagic", str(path)], text=True).strip()
        if vermagic != "4.4.302+ SMP mod_unload":
            raise ValueError(f"unexpected vermagic: {vermagic!r}")
        symbols = subprocess.check_output(["nm", "-u", str(path)], text=True)
        imports = {line.split()[-1] for line in symbols.splitlines() if line.split() and "U" in line.split()[:-1]}
        missing = imports - exports
        if missing:
            raise ValueError(f"{name}: unresolved target symbols: {sorted(missing)}")
        depends = subprocess.check_output(["modinfo", "-F", "depends", str(path)], text=True).strip()
        aliases = subprocess.check_output(["modinfo", "-F", "alias", str(path)], text=True).splitlines()
        if name == "ohci-pci" and not any("bc0csc03i10" in alias.lower() for alias in aliases):
            raise ValueError("OHCI PCI class alias is missing")
        modules.append({"name": name, "vermagic": vermagic, "depends": depends,
                        "sha256": hashlib.sha256(raw).hexdigest(), "checked_imports": len(imports), "aliases": aliases})
    result = {"platform": "broadwellnk", "dsm": "7.2-72806", "hardware_tested": False, "modules": modules}
    (output / "modules.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    verify(*(Path(arg) for arg in sys.argv[1:]))
