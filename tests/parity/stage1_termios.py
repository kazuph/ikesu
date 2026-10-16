#!/usr/bin/env python3
"""Observe real terminal attributes around isolated normal quit and detach."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pty
import re
import select
import struct
import subprocess
import sys
import tempfile
import termios
import time

p = argparse.ArgumentParser()
p.add_argument('--herdr', required=True)
p.add_argument('--gtx', required=True)
p.add_argument('--out', required=True)
a = p.parse_args()
out = Path(a.out).resolve()
out.mkdir(parents=True, exist_ok=True)
failures = []
for product, binary in [('herdr', a.herdr), ('gtx', a.gtx)]:
    binary = str(Path(binary).resolve())
    for mode in ['local-quit', 'server-detach']:
        target = out / product / mode
        target.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='st1-', dir='/tmp') as temporary:
            root = Path(temporary)
            for name in ['home', 'config', 'state', 'data', 'zdot']:
                (root / name).mkdir()
            config = root / 'config.toml'
            config.write_text('onboarding = false\n[keys]\nprefix = "ctrl+t"\n[theme]\nauto_switch = false\n')
            (root / 'zdot/.zshrc').write_text("PS1='TERM_READY> '\nunset HISTFILE\n")
            env = {k: v for k, v in os.environ.items() if not k.startswith(('HERDR', 'GTX', 'GOTERRACE', 'CODEX', 'CLAUDE'))}
            env.update(HOME=str(root/'home'), XDG_CONFIG_HOME=str(root/'config'),
                       XDG_STATE_HOME=str(root/'state'), XDG_DATA_HOME=str(root/'data'),
                       ZDOTDIR=str(root/'zdot'), HERDR_STATE_DIR=str(root/'state/h'),
                       GTX_STATE_DIR=str(root/'state/g'), SHELL='/bin/zsh', TERM='xterm-256color',
                       HERDR_CONFIG_PATH=str(config), GTX_CONFIG=str(config))
            master, slave = pty.openpty()
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 45, 160, 0, 0))
            initial = termios.tcgetattr(slave)
            # Exact starting flags from the parent's real Ghostty observation.
            initial[0] = 0x6902
            initial[3] = 0x200005cb
            termios.tcsetattr(slave, termios.TCSANOW, initial)
            before = termios.tcgetattr(slave)
            def terminal():
                os.setsid()
                fcntl.ioctl(0, termios.TIOCSCTTY, 0)
            launcher = ['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny mach-lookup (global-name "com.apple.pasteboard.1"))', binary]
            command = launcher + (['--no-session'] if mode.startswith('local') else ['--session', 'termios'])
            # Keep the controlling session alive until attributes are sampled:
            # Darwin revokes the slave once its session leader exits.
            sample = root/'after.json'
            worker = ('import json,subprocess,sys,termios; '
                      'code=subprocess.call(sys.argv[2:]); '
                      'v=termios.tcgetattr(0); '
                      'v[6]=[x.hex() if isinstance(x,bytes) else x for x in v[6]]; '
                      'open(sys.argv[1],"w").write(json.dumps([code,v]))')
            process = subprocess.Popen([sys.executable, '-c', worker, str(sample)]+command, stdin=slave, stdout=slave, stderr=slave, env=env, cwd=root/'home', preexec_fn=terminal)
            raw = bytearray()
            def drain(seconds):
                deadline = time.monotonic() + seconds
                while time.monotonic() < deadline:
                    if select.select([master], [], [], min(.05, max(0, deadline-time.monotonic())))[0]:
                        data = os.read(master, 65536)
                        raw.extend(data)
                        if b'\x1b]11;?' in data:
                            os.write(master, b'\x1b]11;rgb:0000/0000/0000\x1b\\')
            try:
                drain(4)
                os.write(master, b'\x14q')
                drain(2)
                code, after = json.loads(sample.read_text()) if sample.exists() else (None, termios.tcgetattr(slave))
                def normalized(value):
                    value = list(value)
                    value[3] &= ~getattr(termios, 'PENDIN', 0)
                    return value[:6] + [[v.hex() if isinstance(v, bytes) else v for v in value[6]]]
                result = dict(product=product, mode=mode, binary=binary,
                              sha256=hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
                              exit=code, before=normalized(before), after=normalized(after),
                              restored=normalized(before)==normalized(after),
                              kitty=[x.decode() for x in re.findall(rb'\x1b\[(?:>|<)\d*u', raw)])
                (target/'result.json').write_text(json.dumps(result, indent=2)+'\n')
                (target/'terminal.raw').write_bytes(raw)
                print(product, mode, 'exit', code, 'restored', result['restored'], flush=True)
                if code != 0 or not result['restored']:
                    failures.append(product+'/'+mode)
            finally:
                if process.poll() is None:
                    process.terminate()
                    process.wait(timeout=5)
                subprocess.run(launcher+['--session', 'termios', 'server', 'stop'], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
                os.close(master)
                os.close(slave)
if failures:
    raise SystemExit('FAIL: '+', '.join(failures))
