#!/usr/bin/env python3
"""Validate stable CLI release inputs and render the single tap Formula."""
import argparse
import hashlib
from pathlib import Path
import re
import subprocess

PLATFORMS = ('darwin-arm64', 'darwin-amd64', 'linux-arm64', 'linux-amd64')
STABLE = r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)'


def version(tag):
    if not re.fullmatch('ikesu-v' + STABLE, tag):
        raise ValueError('expected stable tag ikesu-vMAJOR.MINOR.PATCH')
    return tag.removeprefix('ikesu-v')


def checksums(path):
    result = {}
    for line in Path(path).read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (ikesu-(?:darwin|linux)-(?:arm64|amd64))', line)
        if not match or match[2] in result:
            raise ValueError('invalid or duplicate release checksum')
        result[match[2]] = match[1]
    if set(result) != {'ikesu-' + p for p in PLATFORMS}:
        raise ValueError('all four platform checksums are required')
    return result


def render(tag, sums, template, destination):
    release = version(tag)
    destination = Path(destination)
    if destination.exists():
        previous = re.search(r'^  version "([0-9]+\.[0-9]+\.[0-9]+)"$', destination.read_text(), re.M)
        if not previous:
            raise ValueError('cannot determine current Formula version')
        if tuple(map(int, release.split('.'))) < tuple(map(int, previous[1].split('.'))):
            raise ValueError('refusing Formula version downgrade')
    text = Path(template).read_text().replace('@VERSION@', release)
    for platform in PLATFORMS:
        text = text.replace('@SHA_' + platform.upper().replace('-', '_') + '@', sums['ikesu-' + platform])
    if re.search(r'@[A-Z_]+@', text):
        raise ValueError('unresolved Formula template value')
    if destination.exists() and previous[1] == release and destination.read_text() != text:
        raise ValueError('refusing to replace a published Formula version with different bytes')
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(text)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=('validate', 'manifest', 'formula'))
    parser.add_argument('tag')
    parser.add_argument('--directory', default='release-assets')
    parser.add_argument('--formula', default='Formula/ikesu.rb')
    parser.add_argument('--template', default='packaging/homebrew/ikesu.rb.in')
    args = parser.parse_args()
    release = version(args.tag)
    if args.operation == 'validate':
        source = Path('cmd/ikesu/main.go').read_text()
        if f'const version = "{release}"' not in source:
            raise ValueError('tag and cmd/ikesu/main.go version differ')
        subprocess.run(['git', 'merge-base', '--is-ancestor', 'HEAD', 'origin/main'], check=True)
    elif args.operation == 'manifest':
        directory = Path(args.directory)
        expected = {'ikesu-' + p for p in PLATFORMS}
        if {p.name for p in directory.iterdir()} != expected:
            raise ValueError('release must contain exactly four verified executables')
        rows = []
        for name in sorted(expected):
            path = directory / name
            if not path.is_file() or path.is_symlink() or not path.stat().st_size:
                raise ValueError('invalid release executable')
            rows.append(hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + name)
        (directory / 'SHA256SUMS').write_text('\n'.join(rows) + '\n')
    else:
        render(args.tag, checksums(Path(args.directory) / 'SHA256SUMS'), args.template, args.formula)


if __name__ == '__main__':
    main()
