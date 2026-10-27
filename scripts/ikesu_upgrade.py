#!/usr/bin/env python3
"""Use brew as transport, retaining versioned executables for live sessions."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

STABLE = r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)'


def run(args, env):
    return subprocess.run([str(a) for a in args], env=env, text=True, capture_output=True)


def checked(args, env):
    result = run(args, env)
    if result.returncode:
        raise RuntimeError(f'{args[0]} failed: {result.stderr or result.stdout}')
    return result.stdout.strip()


def binary_version(binary, env):
    text = checked([binary, '--version'], env)
    match = re.fullmatch('ikesu (' + STABLE + ')', text)
    if not match:
        raise RuntimeError('executable did not return a stable ikesu version')
    return text.removeprefix('ikesu ')


def retain(source, version, home):
    destination = home / '.local/lib/ikesu/releases' / version / 'ikesu'
    destination.parent.mkdir(parents=True, exist_ok=True)
    data = source.read_bytes()
    if destination.exists():
        if destination.is_symlink() or hashlib.sha256(destination.read_bytes()).digest() != hashlib.sha256(data).digest():
            raise RuntimeError('refusing to overwrite immutable release ' + str(destination))
        return destination
    # Publish only a complete executable. Exclusive link refuses concurrent replacement.
    fd, temporary = tempfile.mkstemp(prefix='.install-', dir=destination.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
            os.fchmod(stream.fileno(), 0o555)
        os.link(temporary, destination)
    finally:
        os.unlink(temporary)
    return destination


def ping(binary, session, env):
    result = run([binary, '--session', session, 'server', 'ping'], env)
    if result.returncode:
        try:
            payload = json.loads(result.stdout or result.stderr)
        except ValueError:
            raise RuntimeError('cannot establish server state for session ' + session)
        if payload.get('error', {}).get('code') == 'server_not_running':
            return None
        raise RuntimeError('server ping failed for session ' + session + ': ' + result.stdout + result.stderr)
    return json.loads(result.stdout)['result']['version']


def upgrade(args, env, home, entry):
    brew = args.brew or shutil.which('brew')
    if not brew:
        raise RuntimeError('brew not on PATH; specify --brew /absolute/path/to/brew')
    # Keep old kegs through the entire operation, including first installation.
    env = dict(env, HOMEBREW_NO_INSTALL_CLEANUP='1')
    installed = run([brew, 'list', '--versions', 'ikesu'], env)
    checked([brew, 'upgrade', 'ikesu'] if installed.returncode == 0 and installed.stdout.strip()
            else [brew, 'install', 'kazuph/tap/ikesu'], env)
    prefix = Path(checked([brew, '--prefix', 'ikesu'], env))
    metadata = json.loads(checked([brew, 'info', '--json=v2', 'ikesu'], env))
    expected = metadata['formulae'][0]['versions']['stable']
    source = (prefix / 'bin/ikesu').resolve(strict=True)
    print(f'brew: {expected} ({source})', flush=True)
    actual = binary_version(source, env)
    if actual != expected:
        raise RuntimeError(f'brew version {expected} differs from executable {actual}')
    target = retain(source, actual, home)
    if binary_version(target, env) != actual:
        raise RuntimeError('retained executable version mismatch')
    print(f'retained: {actual} ({target})', flush=True)
    for session in args.session or ['default']:
        current = ping(target, session, env)
        if current is None:
            print(f'server [{session}]: not running; no server started', flush=True)
            continue
        result = run([target, '--session', session, 'server', 'live-handoff',
                      '--import-exe', target, '--expected-version', actual], env)
        if result.returncode:
            raise RuntimeError(f'live-handoff failed for {session}; remaining sessions and entry unchanged: '
                               + result.stdout + result.stderr)
        if ping(target, session, env) != actual:
            raise RuntimeError('handoff version verification failed for ' + session)
    entry.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.ikesu-link-', dir=entry.parent) as root:
        replacement = Path(root) / 'ikesu'
        replacement.symlink_to(target)
        os.replace(replacement, entry)
    print(f'entry: {binary_version(entry, env)} ({entry.resolve()})', flush=True)
    return target


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--session', action='append', help='repeat in update order; default: default')
    parser.add_argument('--brew', help='brew executable (otherwise resolved from PATH)')
    args = parser.parse_args()
    for session in args.session or ['default']:
        if not session or session in ('.', '..') or '/' in session or '\\' in session:
            parser.error('invalid session name')
    home = Path.home()
    entry = home / '.local/bin/ikesu'
    lock = home / '.local/lib/ikesu/upgrade.lock'
    lock.parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ)
    status, probe = 0, None
    with lock.open('a') as stream:
        fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        try:
            if entry.exists():
                probe = entry.resolve()
                # Preserve a regular old entry as a rollback binary, too.
                retain(probe, binary_version(probe, env), home)
            probe = upgrade(args, env, home, entry)
        except Exception as error:
            print(str(error), file=sys.stderr)
            status = 1
        finally:
            if probe is None:
                candidate = shutil.which('ikesu')
                probe = Path(candidate) if candidate else None
            print('session versions:', flush=True)
            for session in args.session or ['default']:
                try:
                    if probe is None:
                        raise RuntimeError('no executable available to query the server')
                    value = ping(probe, session, env)
                    print(f'  {session}: {value or "not running"}', flush=True)
                except Exception as error:
                    print(f'  {session}: unavailable ({error})', flush=True)
            if entry.exists():
                print(f'entry version: {binary_version(entry, env)} ({entry.resolve()})', flush=True)
    return status


if __name__ == '__main__':
    sys.exit(main())
