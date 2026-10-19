import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import runtime
import build
import check_spk


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

    def test_dsm_architecture_names_match_arm64_elf(self):
        # DS423 / rtd1619b belongs to Synology's armv8 package family.
        info = build.package_info("aarch64")
        self.assertIn('arch="armv8"', info)
        self.assertNotIn('arch="aarch64"', info)
        self.assertEqual(check_spk.elf_machine("armv8"), 183)
        self.assertIn('arch="x86_64"', build.package_info("x86_64"))
        self.assertEqual(check_spk.elf_machine("x86_64"), 62)
        with self.assertRaisesRegex(ValueError, "must declare armv8"):
            check_spk.elf_machine("aarch64")

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
