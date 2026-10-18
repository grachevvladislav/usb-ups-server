import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import runtime
import build


class RuntimeTests(unittest.TestCase):
    def test_lock_manifests(self):
        for arch in ["x86_64", "aarch64"]:
            manifest = json.loads((runtime.HERE / f"runtime-{arch}.lock.json").read_text())
            self.assertEqual(manifest["arch"], arch)
            names = {p["name"] for p in manifest["packages"]}
            self.assertTrue({"nut", "musl", "libusb"} <= names)
            for p in manifest["packages"]:
                self.assertEqual(len(p["sha256"]), 64)
                self.assertTrue(p["url"].startswith(runtime.BASE + "/"))

    def test_corrupt_download_rejected(self):
        with tempfile.TemporaryDirectory() as d, patch.object(runtime, "download", return_value=b"tampered"):
            with self.assertRaisesRegex(ValueError, "SHA256 mismatch"):
                runtime.extract({"arch": "x86_64", "packages": [{"url": "test", "name": "nut", "sha256": "0"*64}]}, Path(d))

    def test_icon_is_png(self):
        self.assertEqual(build.icon(64)[:8], b"\x89PNG\r\n\x1a\n")

    def test_archive_normalizes_ownership(self):
        with tempfile.TemporaryDirectory() as d:
            source = Path(d) / "source"
            source.mkdir()
            (source / "file").write_text("content")
            output = Path(d) / "out.tar"
            build.archive(source, output, "w")
            with tarfile.open(output) as t:
                entry = t.getmember("file")
                self.assertEqual((entry.uid, entry.gid, entry.mtime), (0, 0, 0))
                self.assertEqual(t.extractfile(entry).read(), b"content")


if __name__ == "__main__":
    unittest.main()
