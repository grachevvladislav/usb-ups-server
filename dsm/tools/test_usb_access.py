"""Exercise the administrative helper in an isolated fake DSM filesystem."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class USBAccessTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.rules = self.root / "etc/udev/rules.d"
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.log = self.root / "udev.log"
        self.script = self.root / "usb-access.sh"
        source = (Path(__file__).parent / "host/usb-access.sh").read_text()
        self.script.write_text(source.replace("/etc/udev/rules.d", str(self.rules)))
        self.fake("id", 'if [ "$1" = -u ]; then echo 0; fi\n')
        self.fake("udevadm", 'echo "$*" >> "$UDEV_LOG"\nexit "${UDEV_RESULT:-0}"\n')
        self.env = dict(os.environ, PATH=f"{self.bin}:/usr/bin:/bin", UDEV_LOG=str(self.log))

    def fake(self, name, body):
        path = self.bin / name
        path.write_text("#!/bin/sh\n" + body)
        path.chmod(0o755)

    def run_helper(self, action, vendor="0925", product="1234"):
        return subprocess.run(["sh", str(self.script), action, vendor, product],
                              env=self.env, capture_output=True, text=True)

    def test_install_creates_missing_directory_and_scoped_rule(self):
        result = self.run_helper("install-rule", "0925", "12AB")
        self.assertEqual(result.returncode, 0, result.stderr)
        rule = self.rules / "99-usb-ups-server-0925-12ab.rules"
        self.assertEqual(rule.read_text(), 'SUBSYSTEM=="usb", ENV{DEVTYPE}=="usb_device", ATTR{idVendor}=="0925", ATTR{idProduct}=="12ab", GROUP="usb-ups-server", MODE="0660"\n')
        self.assertEqual(rule.stat().st_mode & 0o777, 0o644)
        self.assertEqual(self.log.read_text().splitlines(), ["control --reload-rules"] * 2)

    def test_daemon_failure_does_not_change_rules(self):
        self.env["UDEV_RESULT"] = "1"
        self.assertNotEqual(self.run_helper("install-rule").returncode, 0)
        self.assertFalse(self.rules.exists())

    def test_remove_preserves_other_rules(self):
        self.assertEqual(self.run_helper("install-rule").returncode, 0)
        other = self.rules / "other.rules"
        other.write_text("keep")
        self.assertEqual(self.run_helper("remove-rule").returncode, 0)
        self.assertEqual(list(self.rules.iterdir()), [other])

    def test_invalid_id_rejected_before_udev(self):
        self.assertNotEqual(self.run_helper("install-rule", "../bad").returncode, 0)
        self.assertFalse(self.log.exists())
        self.assertFalse(self.rules.exists())


if __name__ == "__main__":
    unittest.main()
