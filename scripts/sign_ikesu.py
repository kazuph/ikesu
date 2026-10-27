#!/usr/bin/env python3
"""Sign only the CLI with the same Developer ID requirement as the Mac panel."""
import base64
import os
from pathlib import Path
import secrets
import shlex
import subprocess
import sys
import tempfile

IDENTITY = 'Developer ID Application: KAZUHIRO HOMMA (67M8FDFZ28)'
REQUIREMENT = ('=anchor apple generic and certificate leaf[subject.OU] = "67M8FDFZ28" '
               'and certificate leaf[subject.CN] = "' + IDENTITY + '"')


def main():
    encoded = os.environ.get('MACOS_DEVELOPER_ID_P12', '')
    password = os.environ.get('MACOS_DEVELOPER_ID_P12_PASSWORD', '')
    if not encoded or not password:
        raise RuntimeError('Developer ID certificate and password are required')
    env = {k: v for k, v in os.environ.items() if k not in ('MACOS_DEVELOPER_ID_P12', 'MACOS_DEVELOPER_ID_P12_PASSWORD')}

    def run(*args):
        p = subprocess.run(args, env=env, capture_output=True, text=True)
        if p.returncode:
            raise RuntimeError(Path(args[0]).name + ' failed')
        return p.stdout

    original = shlex.split(run('security', 'list-keychains', '-d', 'user'))
    with tempfile.TemporaryDirectory(prefix='ikesu-sign-') as root:
        root = Path(root)
        certificate, keychain = root / 'developer.p12', root / 'signing.keychain-db'
        certificate.touch(mode=0o600)
        certificate.write_bytes(base64.b64decode(encoded, validate=True))
        key_password = secrets.token_urlsafe()
        created = False
        try:
            run('security', 'create-keychain', '-p', key_password, str(keychain))
            created = True
            run('security', 'unlock-keychain', '-p', key_password, str(keychain))
            run('security', 'list-keychains', '-d', 'user', '-s', str(keychain), *original)
            run('security', 'import', str(certificate), '-k', str(keychain), '-P', password,
                '-T', '/usr/bin/codesign', '-T', '/usr/bin/security')
            run('security', 'set-key-partition-list', '-S', 'apple-tool:,apple:,codesign:', '-s', '-k', key_password, str(keychain))
            run('codesign', '--force', '--options', 'runtime', '--timestamp', '--sign', IDENTITY,
                '--keychain', str(keychain), sys.argv[1])
            run('codesign', '--verify', '--strict', '-R', REQUIREMENT, sys.argv[1])
        finally:
            try:
                run('security', 'list-keychains', '-d', 'user', '-s', *original)
            finally:
                if created:
                    run('security', 'delete-keychain', str(keychain))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
