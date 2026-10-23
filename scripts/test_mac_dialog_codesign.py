"""Integration check using an existing Developer ID app without changing it."""
import os
from pathlib import Path
import subprocess
import unittest

from scripts.test_package_mac_dialog import packager


class DeveloperIDRequirementTest(unittest.TestCase):
    def test_real_developer_id_requirement(self):
        app = Path(os.environ["IKESU_SIGNED_TEST_APP"])
        self.assertTrue(app.is_dir(), app)
        for requirement, accepted in (
            (packager.REQUIREMENT, True),
            (packager.REQUIREMENT.replace("67M8FDFZ28", "0000000000"), False),
        ):
            with self.subTest(accepted=accepted):
                result = subprocess.run(
                    ["/usr/bin/codesign", "--verify", "--deep", "--strict", "-R", requirement, str(app)],
                    text=True, capture_output=True, timeout=30,
                )
                self.assertEqual(result.returncode == 0, accepted, result.stderr)
                self.assertNotIn("invalid requirement specification", result.stderr)


if __name__ == "__main__":
    unittest.main()
