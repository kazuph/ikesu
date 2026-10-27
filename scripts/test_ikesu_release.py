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

class SigningBoundaryTest(unittest.TestCase):
    def test_signing_identity_matches_panel_requirement(self):
        def load(name):
            spec = importlib.util.spec_from_file_location(name, ROOT / ('scripts/' + name + '.py'))
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            return module
        cli, panel = load('sign_ikesu'), load('package_mac_dialog')
        self.assertEqual(cli.IDENTITY, panel.IDENTITY)
        self.assertEqual(cli.REQUIREMENT, panel.REQUIREMENT)

    def test_missing_signing_secrets_refuses_before_signing(self):
        import os
        import subprocess
        env = {k:v for k,v in os.environ.items() if k not in ('MACOS_DEVELOPER_ID_P12','MACOS_DEVELOPER_ID_P12_PASSWORD')}
        result = subprocess.run([os.sys.executable, ROOT/'scripts/sign_ikesu.py','/does/not/exist'],env=env,capture_output=True,text=True)
        self.assertNotEqual(result.returncode,0)
        self.assertIn('certificate and password are required',result.stderr)


class ReleaseArtifactTest(unittest.TestCase):
    def test_manifest_requires_all_four_inputs_and_is_not_overwritten(self):
        import hashlib
        import subprocess
        import sys
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp)
            args=[sys.executable,ROOT/'scripts/ikesu_release.py','manifest','ikesu-v0.4.11','--directory',root]
            first=subprocess.run(args,capture_output=True,text=True)
            self.assertNotEqual(first.returncode,0)
            for platform in release.PLATFORMS:
                (root/('ikesu-'+platform)).write_bytes(('fixture-'+platform).encode())
            completed=subprocess.run(args,capture_output=True,text=True)
            self.assertEqual(completed.returncode,0,completed.stderr)
            sums=release.checksums(root/'SHA256SUMS')
            self.assertEqual(sums,{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in root.glob('ikesu-*')})
            before=(root/'SHA256SUMS').read_bytes()
            again=subprocess.run(args,capture_output=True,text=True)
            self.assertNotEqual(again.returncode,0)
            self.assertEqual(before,(root/'SHA256SUMS').read_bytes())

    def test_formula_symlink_cannot_change_another_file(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp)
            original=root/'other.rb';original.write_text('untouched')
            formula=root/'ikesu.rb';formula.symlink_to(original)
            sums={'ikesu-'+p:'a'*64 for p in release.PLATFORMS}
            with self.assertRaises(ValueError):
                release.render('ikesu-v1.2.3',sums,ROOT/'packaging/homebrew/ikesu.rb.in',formula)
            self.assertEqual(original.read_text(),'untouched')


if __name__ == '__main__':
    unittest.main()
