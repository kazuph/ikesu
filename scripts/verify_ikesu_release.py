#!/usr/bin/env python3
"""Execute the release on its native runner and boot a disposable session."""
import argparse
import json
import os
from pathlib import Path
import platform
import subprocess
import tempfile
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('binary', type=Path)
    parser.add_argument('version')
    parser.add_argument('goos', choices=('darwin', 'linux'))
    parser.add_argument('goarch', choices=('arm64', 'amd64'))
    args = parser.parse_args()
    binary = args.binary.resolve()
    expected_machine = {'arm64': ('arm64', 'aarch64'), 'amd64': ('x86_64', 'AMD64')}[args.goarch]
    assert platform.system().lower() == args.goos and platform.machine() in expected_machine, 'native runner required'
    metadata = subprocess.check_output(['go', 'version', '-m', str(binary)], text=True)
    for setting in ('GOOS=' + args.goos, 'GOARCH=' + args.goarch, 'CGO_ENABLED=0'):
        assert setting in metadata, 'wrong executable build metadata: ' + setting
    assert subprocess.check_output([binary, '--version'], text=True).strip() == 'ikesu ' + args.version
    if args.goos == 'linux':
        dynamic = subprocess.check_output(['readelf', '-d', str(binary)], text=True)
        assert '(NEEDED)' not in dynamic, 'release must not require an external libc'
    else:
        # Verify the exact downloaded/staged bytes, including Developer ID identity.
        from sign_ikesu import REQUIREMENT
        subprocess.run(['codesign', '--verify', '--strict', '-R', REQUIREMENT, str(binary)], check=True)
    with tempfile.TemporaryDirectory(prefix='ikesu-release-', dir='/tmp') as root:
        env = {k: v for k, v in os.environ.items() if not k.startswith(('IKESU_', 'HERDR_'))}
        env.update(HOME=root, IKESU_STATE_DIR=root + '/state', IKESU_CONFIG=root + '/config.toml', SHELL='/bin/sh')
        Path(env['IKESU_CONFIG']).write_text('onboarding=false\n[experimental]\nallow_nested=true\n[session]\nresume_agents_on_restore=false\n')
        with open(root + '/server.log', 'w') as log:
            server = subprocess.Popen([binary, '--session', 'release-check', 'server'], env=env, stdout=log, stderr=log)
            try:
                # Same startup deadline as existing CLI restart validation.
                deadline = time.monotonic() + 15
                while True:
                    ping = subprocess.run([binary, '--session', 'release-check', 'server', 'ping'], env=env, capture_output=True, text=True)
                    if ping.returncode == 0:
                        assert json.loads(ping.stdout)['result']['version'] == args.version
                        break
                    if server.poll() is not None or time.monotonic() >= deadline:
                        raise RuntimeError('isolated server did not start: ' + Path(root + '/server.log').read_text())
                    time.sleep(.05)
                subprocess.run([binary, '--session', 'release-check', 'server', 'stop'], env=env, check=True, capture_output=True)
                assert server.wait(timeout=15) == 0
            finally:
                if server.poll() is None:
                    server.terminate()
                    server.wait(timeout=15)


if __name__ == '__main__':
    main()
