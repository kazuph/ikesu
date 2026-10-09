#!/usr/bin/env python3
"""Run identical ptyrun steps against isolated herdr and gtx binaries."""
import argparse
import os
import json
import socket
from pathlib import Path
import subprocess
import tempfile
import time

p = argparse.ArgumentParser()
p.add_argument('--herdr', required=True)
p.add_argument('--gtx', required=True)
p.add_argument('--ptyrun', required=True)
p.add_argument('--steps', required=True)
p.add_argument('--out', required=True)
p.add_argument('--server', action='store_true', help='run a disposable persistent server for socket-driven UI')
a = p.parse_args()
a.ptyrun = str(Path(a.ptyrun).resolve())
if not Path(a.herdr).is_file():
    print('NOT_RUN: herdr binary absent')
    raise SystemExit(0)
out = Path(a.out).resolve()
out.mkdir(parents=True, exist_ok=True)
for name, binary in [('herdr', a.herdr), ('gtx', a.gtx)]:
    target = out / name
    target.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='parity-', dir='/tmp') as root:
        root = Path(root)
        for directory in ['home', 'config', 'state', 'data', 'zdot', 'cwd']:
            (root / directory).mkdir()
        (root / 'zdot/.zshrc').write_text("PS1='PARITY> '\n")
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(('HERDR', 'GTX', 'GOTERRACE', 'CLAUDE', 'CODEX'))}
        env.update(HOME=str(root / 'home'), ZDOTDIR=str(root / 'zdot'),
                   XDG_CONFIG_HOME=str(root / 'config'), XDG_STATE_HOME=str(root / 'state'),
                   XDG_DATA_HOME=str(root / 'data'), HERDR_STATE_DIR=str(root / 'state/h'),
                   GTX_STATE_DIR=str(root / 'state/g'), SHELL='/bin/zsh', TERM='xterm-256color')
        steps = target / 'steps'
        # Only the first-run onboarding differs. The interaction body is identical.
        preamble = 'sleep 3s\nsend "\\r"\nsleep 500ms\nsend "\\r"\nsleep 1s\n' if name == 'herdr' else 'sleep 3s\n'
        steps.write_text(preamble + Path(a.steps).read_text().replace('@OUT@', str(target)).replace('@FIXTURES@', str(Path(__file__).resolve().parent)))
        with (target / 'run.log').open('w') as log, (target / 'server.log').open('w') as server_log:
            server = None
            binary = str(Path(binary).resolve())
            endpoint = None
            try:
                if a.server:
                    server = subprocess.Popen([binary, '--session', 'parity', 'server'],
                                              cwd=root / 'cwd', env=env, stdout=server_log, stderr=subprocess.STDOUT)
                    (target / 'server-pid.txt').write_text(str(server.pid)+'\n')
                    # Readiness belongs to the isolated test, not job orchestration.
                    deadline = time.monotonic() + 10
                    while endpoint is None:
                        endpoints = [path for path in root.rglob('*.sock')
                                     if path.name in ('herdr.sock', 'gtx.sock')]
                        if len(endpoints) == 1:
                            endpoint = endpoints[0]
                        elif server.poll() is not None or time.monotonic() >= deadline:
                            raise RuntimeError('isolated API socket did not become ready')
                        else:
                            time.sleep(.05)
                    def request(method, params):
                        with socket.socket(socket.AF_UNIX) as sock:
                            sock.settimeout(5)
                            sock.connect(str(endpoint))
                            sock.sendall(json.dumps({'id':'parity-fixture','method':method,'params':params}).encode()+b'\n')
                            response = bytearray()
                            while b'\n' not in response:
                                chunk = sock.recv(65536)
                                if not chunk:
                                    raise RuntimeError('isolated API closed without a reply')
                                response.extend(chunk)
                            result = json.loads(response.split(b'\n')[0])
                            if 'error' in result:
                                raise RuntimeError(result)
                            return result
                    created = request('workspace.create', {'cwd':str(root / 'cwd'), 'label':'parity', 'focus':True})
                    (target / 'fixture.json').write_text(json.dumps(created, indent=2)+'\n')
                subprocess.run([a.ptyrun, '-cols', '160', '-rows', '45', '-pixel-mouse=false',
                            '-steps', str(steps), '--', binary] + (['--session', 'parity'] if a.server else ['--no-session']),
                               cwd=root / 'cwd', env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
            finally:
                if server is not None:
                    replies = []
                    for owned in root.rglob('*.sock'):
                        if owned.name not in ('herdr.sock', 'gtx.sock'):
                            continue
                        try:
                            with socket.socket(socket.AF_UNIX) as sock:
                                sock.settimeout(5)
                                sock.connect(str(owned))
                                sock.sendall(json.dumps({'id':'parity-cleanup','method':'server.stop','params':{}}).encode()+b'\n')
                                replies.append(sock.recv(65536).decode())
                        except OSError as error:
                            replies.append(str(error))
                    try:
                        server.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        server.kill()
                        server.wait()
                        replies.append('FAIL: owned server required kill')
                    (target / 'cleanup.json').write_text(json.dumps({'pid':server.pid, 'exit':server.returncode, 'replies':replies}, indent=2)+'\n')
                    if 'FAIL: owned server required kill' in replies:
                        raise RuntimeError('owned server did not stop gracefully')
