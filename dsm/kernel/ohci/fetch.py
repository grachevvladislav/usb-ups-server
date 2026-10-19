#!/usr/bin/env python3
"""Fetch only official Synology sources, headers and compiler with pinned hashes."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import sys
import urllib.request

HERE = Path(__file__).resolve().parent


def fetch(entry, cache):
    path = cache / entry["name"]
    size = entry["size"]
    if path.exists() and path.stat().st_size == size:
        if hashlib.sha256(path.read_bytes()).hexdigest() == entry.get("sha256"):
            print(f"Verified {path.name}", flush=True)
            return
    prefix = path.stat().st_size if path.exists() and path.stat().st_size < size else 0
    chunk = 4 * 1024 * 1024
    ranges = [(n, min(n + chunk, size) - 1) for n in range(prefix, size, chunk)]
    def part(bounds):
        start, end = bounds
        request = urllib.request.Request(entry["url"], headers={"Range": f"bytes={start}-{end}", "Accept-Encoding": "identity"})
        with urllib.request.urlopen(request, timeout=120) as r:
            expected = f"bytes {start}-{end}/{size}"
            if r.status != 206 or r.headers.get("Content-Range") != expected:
                raise ValueError(f"unexpected ranged response for {path.name}: {r.status}")
            body = r.read()
        if len(body) != end - start + 1:
            raise ValueError("truncated download")
        return start, body
    with path.open("ab" if prefix else "wb") as f, ThreadPoolExecutor(max_workers=4) as pool:
        # Ordered writes preserve a valid prefix for resuming interrupted downloads.
        for start, body in pool.map(part, ranges):
            if f.tell() != start:
                raise ValueError("download offset mismatch")
            f.write(body)
            f.flush()
            print(f"{path.name}: {f.tell()}/{size}", flush=True)
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if entry.get("sha256") and digest != entry["sha256"]:
        raise ValueError(f"SHA256 mismatch: {path.name}")
    print(f"SHA256 {path.name}: {digest}", flush=True)


if __name__ == "__main__":
    cache = Path(sys.argv[1])
    cache.mkdir(parents=True, exist_ok=True)
    manifest = json.loads((HERE / "sources.json").read_text())
    for entry in manifest["sources"]:
        fetch(entry, cache)
