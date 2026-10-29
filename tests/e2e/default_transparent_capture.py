#!/usr/bin/env python3
"""Capture real isolated ikesu PTY output for the xterm.js comparison test."""
import argparse
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import struct
import subprocess
import tempfile
import termios
import time

parser = argparse.ArgumentParser()
parser.add_argument('--binary', type=Path, required=True)
parser.add_argument('--output', type=Path, required=True)
parser.add_argument('--theme', choices=['default', 'catppuccin-latte'], required=True)
parser.add_argument('--background', choices=['default', 'opaque', 'custom'], default='default')
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='ikesu-transparency-', dir='/tmp') as temporary:
    root = Path(temporary)
    for name in ['home', 'config', 'state', 'data', 'work']:
        (root / name).mkdir()
    config = root / 'config/ikesu/config.toml'
    sections = []
    if args.theme != 'default':
        sections.append('[theme]\nname="catppuccin-latte"')
    if args.background == 'opaque':
        sections.append('[ui]\nopaque_background=true')
    if args.background == 'custom':
        sections.append('[theme.custom]\npanel_bg="#2a4639"')
    if sections:
        config.parent.mkdir(parents=True)
        config.write_text('\n'.join(sections) + '\n')
    configured_before_start = config.exists()
    assert configured_before_start == (args.theme != 'default' or args.background != 'default')
    env = {k: v for k, v in os.environ.items() if not k.startswith(('IKESU', 'HERDR', 'CODEX', 'CLAUDE', 'TMUX'))}
    env.update(HOME=str(root / 'home'), XDG_CONFIG_HOME=str(root / 'config'),
               XDG_STATE_HOME=str(root / 'state'), XDG_DATA_HOME=str(root / 'data'),
               SHELL='/bin/sh', TERM='xterm-256color', PS1='capture> ', ENV='/dev/null')
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 40, 120, 0, 0))
    def controlling_terminal():
        os.setsid()
        fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
    process = subprocess.Popen([str(args.binary.resolve()), '--session', 'capture'], stdin=slave,
                               stdout=slave, stderr=slave, env=env, cwd=root / 'work', preexec_fn=controlling_terminal)
    os.close(slave)
    data = bytearray()
    def receive_until(marker):
        deadline = time.monotonic() + 10
        while marker not in data:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise RuntimeError(f'PTY never displayed {marker!r}: {bytes(data[-2000:])!r}')
            if select.select([master], [], [], remaining)[0]:
                try:
                    chunk = os.read(master, 65536)
                except OSError as error:
                    if error.errno == errno.EIO:
                        raise RuntimeError('PTY exited before the requested screen') from error
                    raise
                if not chunk:
                    raise RuntimeError('PTY closed before the requested screen')
                data.extend(chunk)
    try:
        receive_until(b'terminal workspace manager')
        os.write(master, b'\r')
        os.write(master, b'\x02c\r\x02v')
        os.write(master, b"printf 'TRANSPARENCY_%s\\n' 'READY'\r")
        receive_until(b'TRANSPARENCY_READY')
        # End when the terminal has no pending output, not after a fixed sleep.
        while select.select([master], [], [], 0.1)[0]:
            data.extend(os.read(master, 65536))
        (args.output / 'screen.ansi').write_bytes(data)
        (args.output / 'capture.json').write_text(json.dumps({
            'binary': str(args.binary.resolve()), 'theme': args.theme,
            'background': args.background, 'cols': 120, 'rows': 40,
            'temporary_home': True, 'config_present_at_start': configured_before_start,
            'initial_config': '\n'.join(sections), 'marker': 'TRANSPARENCY_READY',
            'bytes': len(data), 'pid': process.pid,
        }, indent=2) + '\n')
    finally:
        (args.output / 'raw.ansi').write_bytes(data)
        subprocess.run([str(args.binary.resolve()), '--session', 'capture', 'server', 'stop'], env=env, cwd=root/'work', stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=5, check=True)
        if process.poll() is None:
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                # This is the exact subprocess launched above, never a name match.
                process.terminate()
                process.wait(timeout=5)
        os.close(master)
