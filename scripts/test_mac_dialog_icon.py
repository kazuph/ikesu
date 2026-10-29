"""Exercise actual PNG/iconutil output and the bundle staged before signing."""
import importlib.util
from pathlib import Path
import plistlib
import subprocess
import tempfile
import unittest

from scripts.test_package_mac_dialog import packager

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("generate_mac_dialog_icon", ROOT / "scripts/generate_mac_dialog_icon.py")
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)


def rgba(path):
    return subprocess.check_output(["magick", str(path), "-depth", "8", "rgba:-"])


def visible_rgba(data):
    # ICNS's 16/32px ARGB encoding discards RGB under fully transparent pixels.
    # Compare alpha everywhere, and the exact nearest-neighbor RGB everywhere visible.
    result = bytearray(data)
    for offset in range(0, len(result), 4):
        if result[offset + 3] == 0:
            result[offset:offset + 3] = b"\0\0\0"
    return result


class MacDialogIconTest(unittest.TestCase):
    def test_approved_source_keeps_the_existing_koi_pixels(self):
        source, atlas = rgba(generator.SOURCE), rgba(ROOT / "docs/brand/koi-atlas.png")
        for sy, dx, dy in ((0, 128, 192), (80, 256, 576)):
            for y in range(16):
                for x in range(40):
                    offset = ((sy + y) * 320 + x) * 4
                    pixel = atlas[offset:offset + 4]
                    if pixel[3] != 255:
                        continue
                    for row in range(dy + y * 16, dy + (y + 1) * 16):
                        offset = (row * 1024 + dx + x * 16) * 4
                        self.assertEqual(source[offset:offset + 16 * 4], pixel * 16)

    def test_all_real_icns_representations_use_nearest_neighbor(self):
        source = rgba(generator.SOURCE)
        with tempfile.TemporaryDirectory() as temp:
            icon = Path(temp) / "generated.icns"
            generator.generate(icon)
            for label, original in (("generated", icon), ("committed", generator.OUTPUT)):
                iconset = Path(temp) / (label + ".iconset")
                subprocess.run(["iconutil", "-c", "iconset", str(original), "-o", str(iconset)], check=True)
                self.assertEqual({p.name for p in iconset.iterdir()}, {name for name, _ in generator.REPRESENTATIONS})
                for name, size in generator.REPRESENTATIONS:
                    with self.subTest(icon=label, representation=name):
                        actual = rgba(iconset / name)
                        expected = bytearray()
                        for y in range(size):
                            for x in range(size):
                                offset = ((y * 1024 // size + 512 // size) * 1024 + x * 1024 // size + 512 // size) * 4
                                expected.extend(source[offset:offset + 4])
                        self.assertEqual(visible_rgba(actual), visible_rgba(expected))

    def test_bundle_stages_icon_before_signing_and_keeps_release_contract(self):
        with tempfile.TemporaryDirectory() as temp:
            app = Path(temp) / "IkesuDecisionDialog.app"
            packager.stage_app_bundle(ROOT / "apps/mac-dialog-decisions", app, "0.1.7")
            packager.validate_app_icon(app)
            info = plistlib.loads((app / "Contents/Info.plist").read_bytes())
            self.assertEqual(info["CFBundleVersion"], "0.1.7")
            self.assertEqual(info["CFBundleShortVersionString"], "0.1.7")
            self.assertEqual(info["CFBundleIdentifier"], "com.kazuph.ikesu.decisions")
            self.assertTrue(info["LSUIElement"])
            icon = app / "Contents/Resources" / info["CFBundleIconFile"]
            self.assertEqual(icon.read_bytes(), generator.OUTPUT.read_bytes())
            icon.write_bytes(b"invalid icon")
            with self.assertRaises(subprocess.CalledProcessError):
                packager.validate_app_icon(app)


if __name__ == "__main__":
    unittest.main()
