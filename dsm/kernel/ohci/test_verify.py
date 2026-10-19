import json
from pathlib import Path
import struct
import tempfile
import unittest
from unittest.mock import patch

import verify


class ModuleValidationTests(unittest.TestCase):
    def run_validation(self, *, vermagic="4.4.302+ SMP mod_unload", imports="usb_function", alias=True, machine=62):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            kernel, source, output = [root / x for x in ["kernel", "source", "output"]]
            for d in [kernel, source, output]:
                d.mkdir()
            (kernel / "Module.symvers").write_text("0x0 usb_function usbcore EXPORT_SYMBOL\n")
            (source / "Module.symvers").write_text("0x0 ohci_setup ohci_hcd EXPORT_SYMBOL_GPL\n")
            header = bytearray(20)
            header[:5] = b"\x7fELF\x02"
            header[18:20] = struct.pack("<H", machine)
            for name in ["ohci-hcd", "ohci-pci"]:
                (source / f"{name}.ko").write_bytes(header)
            def tool(args, **kwargs):
                if args[0] == "nm":
                    return f" U {imports}\n w optional_weak_symbol\n"
                if args[2] == "vermagic":
                    return vermagic + " \n"
                if args[2] == "depends":
                    return "usbcore\n"
                if args[2] == "alias":
                    return "pci:v*d*sv*sd*bc0Csc03i10*\n" if alias else ""
                raise AssertionError(args)
            with patch.object(verify.subprocess, "check_output", side_effect=tool):
                verify.verify(kernel, source, output)
            return json.loads((output / "modules.json").read_text())

    def test_matching_modules(self):
        result = self.run_validation()
        self.assertFalse(result["hardware_tested"])
        self.assertEqual(result["modules"][0]["checked_imports"], 1)

    def test_wrong_vermagic_rejected(self):
        with self.assertRaisesRegex(ValueError, "vermagic"):
            self.run_validation(vermagic="4.4.180+ SMP mod_unload")

    def test_unknown_import_rejected(self):
        with self.assertRaisesRegex(ValueError, "unresolved target symbols"):
            self.run_validation(imports="not_exported_by_target")

    def test_wrong_device_alias_rejected(self):
        with self.assertRaisesRegex(ValueError, "alias"):
            self.run_validation(alias=False)

    def test_wrong_architecture_rejected(self):
        with self.assertRaisesRegex(ValueError, "x86-64"):
            self.run_validation(machine=183)


if __name__ == "__main__":
    unittest.main()
