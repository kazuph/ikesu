#!/usr/bin/env python3
"""Parent-authorized clipboard command capture for isolated parity processes only."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

SANDBOX_PROFILE = '(version 1)(allow default)(deny mach-lookup (global-name "com.apple.pasteboard.1"))'


def real_clipboard_hash():
    # The real clipboard bytes exist only in memory and are never written or displayed.
    data = subprocess.run(['/usr/bin/pbpaste'], stdout=subprocess.PIPE, check=True).stdout
    return hashlib.sha256(data).hexdigest()


class ClipboardGuard:
    def __init__(self, root, capture_commands=True):
        self.root = Path(root).resolve()
        self.root.mkdir(parents=True, exist_ok=True)
        self.capture_commands = capture_commands
        self.shims = self.root / 'bin'
        self.shims.mkdir(exist_ok=True)
        source = Path(__file__).resolve()
        for name in ('pbcopy', 'pbpaste', 'osascript'):
            target = self.shims / name
            target.write_text('#!' + sys.executable + '\n' + source.read_text())
            target.chmod(0o700)
        self.before = real_clipboard_hash()
        self.audit = self.root / 'calls.jsonl'
        self.records = []

    def environment(self, env):
        if not self.capture_commands:
            return
        env['PATH'] = str(self.shims) + os.pathsep + env.get('PATH', '')
        env['PARITY_CLIP_CAPTURE'] = str(self.root)
        for name in ('pbcopy', 'pbpaste', 'osascript'):
            if shutil.which(name, path=env['PATH']) != str(self.shims / name):
                raise RuntimeError('STOP: clipboard shim not first in PATH')

    def check(self, name):
        after = real_clipboard_hash()
        calls = [json.loads(line) for line in self.audit.read_text().splitlines()] if self.audit.exists() else []
        pbcopy_calls = sum(row['command'] == 'pbcopy' for row in calls)
        result = {'process': name, 'before_sha256': self.before, 'after_sha256': after,
                  'unchanged': self.before == after, 'captured_pbcopy_calls': pbcopy_calls,
                  'commands': [row['command'] for row in calls]}
        self.records.append(result)
        (self.root / 'proof.json').write_text(json.dumps(self.records, indent=2) + '\n')
        if after != self.before:
            raise RuntimeError('STOP: real clipboard hash changed; do not restore it; report to parent')
        # herdr must prove interception. gtx currently emits OSC52 into ptyrun's captured stream.
        if self.capture_commands and name == 'herdr' and pbcopy_calls == 0:
            raise RuntimeError('STOP: herdr did not invoke captured pbcopy; report to parent')

    def sandbox_probe(self):
        # Probe a child of the sandboxed shell, proving the restriction is inherited.
        command = ['/usr/bin/sandbox-exec', '-p', SANDBOX_PROFILE, '/bin/sh', '-c',
                   'printf PARITY_SANDBOX_PROBE | /usr/bin/pbcopy']
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        after = real_clipboard_hash()
        proof = {'command': command, 'exit_code': result.returncode,
                 'stderr': result.stderr.decode(errors='replace'),
                 'before_sha256': self.before, 'after_sha256': after,
                 'unchanged': self.before == after}
        (self.root / 'sandbox-probe.json').write_text(json.dumps(proof, indent=2) + '\n')
        if after != self.before:
            raise RuntimeError('STOP: real clipboard hash changed; report to parent without restoring')
        if result.returncode != 1:
            raise RuntimeError('STOP: sandbox did not reject its child pbcopy')


def capture_command():
    root = Path(os.environ['PARITY_CLIP_CAPTURE'])
    name = Path(sys.argv[0]).name
    row = {'command': name, 'pid': os.getpid(), 'argv': sys.argv[1:]}
    if name == 'pbpaste':
        copied = root / 'captured-input.bin'
        if copied.exists():
            sys.stdout.buffer.write(copied.read_bytes())
    else:
        data = sys.stdin.buffer.read()
        row['captured_bytes'] = len(data)
        if name == 'pbcopy':
            (root / 'captured-input.bin').write_bytes(data)
        else:
            (root / ('osascript-' + str(os.getpid()) + '.bin')).write_bytes(data)
    with (root / 'calls.jsonl').open('a') as f:
        f.write(json.dumps(row) + '\n')
    if name == 'osascript':
        # It must never reach the user's AppleScript runtime, regardless of its arguments.
        raise SystemExit(1)


if __name__ == '__main__':
    capture_command()
