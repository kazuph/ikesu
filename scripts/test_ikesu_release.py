import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('ikesu_release', ROOT / 'scripts/ikesu_release.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseTest(unittest.TestCase):
    def test_tag_is_stable_and_namespaced(self):
        self.assertEqual(release.version('ikesu-v1.2.3'), '1.2.3')
        for tag in ('v1.2.3', 'ikesu-v01.2.3', 'ikesu-v1.2.3-rc1', 'ikesu-mac-dialog-v1.2.3'):
            with self.assertRaises(ValueError):
                release.version(tag)

    def test_formula_checksums_and_version_cannot_regress(self):
        with tempfile.TemporaryDirectory() as temp:
            temp = Path(temp)
            manifest = temp / 'SHA256SUMS'
            manifest.write_text('a' * 64 + '  ikesu-darwin-arm64\n')
            with self.assertRaises(ValueError):
                release.checksums(manifest)
            sums = {'ikesu-' + p: str(n) * 64 for n, p in enumerate(release.PLATFORMS, 1)}
            manifest.write_text(''.join(v + '  ' + k + '\n' for k, v in sums.items()))
            self.assertEqual(release.checksums(manifest), sums)
            formula = temp / 'Formula/ikesu.rb'
            template = ROOT / 'packaging/homebrew/ikesu.rb.in'
            release.render('ikesu-v1.2.3', sums, template, formula)
            first = formula.read_bytes()
            release.render('ikesu-v1.2.3', sums, template, formula)
            self.assertEqual(first, formula.read_bytes())
            with self.assertRaises(ValueError):
                release.render('ikesu-v1.2.2', sums, template, formula)
            sums['ikesu-linux-amd64'] = 'f' * 64
            with self.assertRaises(ValueError):
                release.render('ikesu-v1.2.3', sums, template, formula)
            self.assertEqual(first, formula.read_bytes())


if __name__ == '__main__':
    unittest.main()
