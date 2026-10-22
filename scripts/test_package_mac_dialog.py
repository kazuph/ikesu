"""Verify the actual release staging files and their archived notices."""
import importlib.util
from pathlib import Path
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("package_mac_dialog", ROOT / "scripts/package_mac_dialog.py")
packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packager)


class PackageNoticesTest(unittest.TestCase):
    def test_release_contains_original_mit_notice(self):
        with tempfile.TemporaryDirectory() as temp:
            package = Path(temp) / "mac-dialog-decisions"
            package.mkdir()
            packager.stage_release_files(ROOT, package)
            archive = Path(temp) / "release.tar.gz"
            with tarfile.open(archive, "w:gz") as tar:
                tar.add(package, arcname=package.name)
            with tarfile.open(archive) as tar:
                notice = tar.extractfile("mac-dialog-decisions/third_party/unicode-width/LICENSE-MIT").read()
            self.assertEqual(notice, (ROOT / "third_party/unicode-width/LICENSE-MIT").read_bytes())
            self.assertIn(b"Copyright (c) 2015 The Rust Project Developers", notice)
            for source in ("internal/cellwidth/width.go", "apps/mac-dialog-decisions/unicode-width.swift"):
                self.assertIn("third_party/unicode-width/LICENSE-MIT", (ROOT / source).read_text().splitlines()[1])


if __name__ == "__main__":
    unittest.main()
