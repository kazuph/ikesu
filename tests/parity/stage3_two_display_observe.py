"""Observe two actual display PTYs attached to one disposable server."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import tomllib

from clipboard_guard import SANDBOX_PROFILE

p = argparse.ArgumentParser()
for name in ['binary', 'ptyrun', 'config', 'out']:
    p.add_argument('--'+name, type=Path, required=True)
p.add_argument('--product', choices=['herdr', 'gtx'], required=True)
p.add_argument('--session-id', required=True)
p.add_argument('--only', nargs='*')
a = p.parse_args()
binary = a.binary.resolve()
a.out.mkdir(parents=True, exist_ok=True)
for kind, position, cols in [('api', True, 160), ('api', False, 160), ('api', False, 60),
                             ('agent', True, 160), ('agent', False, 160), ('agent', False, 60),
                             ('api', 'empty', 160), ('agent', 'empty', 160),
                             ('api', 'empty', 60), ('agent', 'empty', 60),
                             ('api', True, 60), ('agent', True, 60)]:
    name = f'{kind}-{"empty" if position == "empty" else "position" if position else "default"}-{cols}'
    if a.only and name not in a.only:
        continue
    out = (a.out/name).resolve()
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='parity-two-', dir='/private/tmp') as temp:
        root = Path(temp)
        for d in ['home', 'config', 'state', 'data', 'cwd', 'zdot']:
            (root/d).mkdir()
        config = a.config.read_text().replace('[ui.toast]\ndelivery = "system"\n', '')
        config += f'\n[ui.toast]\ndelivery="{a.product}"\ndelay_seconds=0\n'
        if position == 'empty':
            config += '\n[ui.toast.herdr]\nposition=""\n'
        elif kind == 'agent' and position:
            config += '\n[ui.toast.herdr]\nposition="top-right"\n'
        sound = tomllib.loads(config).get('ui', {}).get('sound')
        if sound is None:
            config += '\n[ui.sound]\nenabled=false\n'
        else:
            assert sound['enabled'] is False
        (root/'config.toml').write_text(config)
        (out/'config.toml').write_text(config)
        (root/'zdot/.zshrc').write_text("PS1='PARITY> '\nRPROMPT=''\n")
        env = {k: v for k, v in os.environ.items() if not k.startswith(('HERDR', 'GTX', 'GOTERRACE', 'CODEX', 'CLAUDE'))}
        env.update(HOME=str(root/'home'), ZDOTDIR=str(root/'zdot'), XDG_CONFIG_HOME=str(root/'config'),
                   XDG_STATE_HOME=str(root/'state'), XDG_DATA_HOME=str(root/'data'),
                   HERDR_STATE_DIR=str(root/'state/h'), GTX_STATE_DIR=str(root/'state/g'),
                   HERDR_CONFIG_PATH=str(root/'config.toml'), GTX_CONFIG=str(root/'config.toml'),
                   GTX_DISABLE_SOUND='1', SHELL='/bin/zsh', TERM='xterm-256color')
        launch = ['/usr/bin/sandbox-exec', '-p', SANDBOX_PROFILE, str(binary)]
        replies = []
        endpoint = None

        def wait(check, label, timeout=30):
            end = time.monotonic()+timeout
            while not check():
                if time.monotonic() > end:
                    raise RuntimeError('timeout: '+label)
                time.sleep(.05)

        def request(method, params):
            with socket.socket(socket.AF_UNIX) as conn:
                conn.settimeout(10)
                conn.connect(str(endpoint))
                conn.sendall(json.dumps({'id':'two-display', 'method':method, 'params':params}).encode()+b'\n')
                data = b''
                while b'\n' not in data:
                    chunk = conn.recv(65536)
                    if not chunk:
                        raise RuntimeError('API closed without response')
                    data += chunk
            reply = json.loads(data.split(b'\n')[0])
            replies.append({'method':method, 'params':params, 'reply':reply, 'time':time.monotonic()})
            (out/'api.json').write_text(json.dumps(replies, ensure_ascii=False, indent=2)+'\n')
            if 'error' in reply:
                raise RuntimeError(reply)
            return reply['result']

        def capture(phase):
            (root/phase).write_text(phase+'\n')
            for n in [1, 2]:
                wait(lambda: (out/f'view{n}-{phase}.json').is_file(), phase)

        logs, displays = [], []
        server_log = (out/'server.log').open('w')
        server = subprocess.Popen(launch+['--session', 'parity', 'server'], env=env, cwd=root/'cwd', stdout=server_log, stderr=subprocess.STDOUT)
        try:
            wait(lambda: any(root.rglob(a.product+'.sock')), 'API socket')
            endpoint = next(root.rglob(a.product+'.sock'))
            if not request('workspace.list', {})['workspaces']:
                request('workspace.create', {'cwd':str(root/'cwd'), 'label':'foreground', 'focus':True})
            for n in [1, 2]:
                steps = 'sleep 3s\n'
                for phase in ['ready', 'visible', 'clicked', 'retried', 'expired']:
                    steps += f'waitfile {root/phase} 60s\n'
                    if phase == 'clicked' and n == 1:
                        y = 44 if cols == 60 else 2
                        x = 5 if cols == 60 else cols-3
                        steps += f'mouse down {x} {y}\nmouse up {x} {y}\n'
                    steps += f'sleep 200ms\nscreen {out}/view{n}-{phase}.txt\ncells {out}/view{n}-{phase}.json\n'
                steps += f'waitfile {root/"done"} 60s\nkill\n'
                path = out/f'view{n}.steps'
                path.write_text(steps)
                log = (out/f'view{n}.log').open('w')
                logs.append(log)
                displays.append(subprocess.Popen([str(a.ptyrun), '-cols', str(cols), '-rows', '45', '-steps', str(path), '-raw-output', str(out/f'view{n}.raw'), '--']+launch+['--session', 'parity'], cwd=root/'home', env=env, stdout=log, stderr=subprocess.STDOUT))
            capture('ready')
            if kind == 'api':
                params = {'title':'TWO_DISPLAY', 'body':'BODY', 'sound':'none'}
                if position is True:
                    params['position'] = 'top-right'
                request('notification.show', params)
            else:
                target = request('workspace.create', {'cwd':str(root/'cwd'), 'label':'background', 'focus':False})['root_pane']['pane_id']
                for state in ['working', 'idle']:
                    request('pane.report_agent', {'pane_id':target, 'source':'codex', 'agent':'codex', 'state':state, 'session_id':a.session_id})
            capture('visible')
            capture('clicked')
            request('notification.show', {'title':'RETRY', 'sound':'none', 'position':'top-right'})
            capture('retried')
            time.sleep(11)  # Exceeds the observed 3s explicit / 8s agent lifetime.
            capture('expired')
            request('notification.show', {'title':'AFTER_EXPIRY', 'sound':'none', 'position':'top-right'})
            (root/'done').write_text('done\n')
            codes = [d.wait(timeout=10) for d in displays]
            assert codes == [0, 0], codes
            request('server.stop', {})
            code = server.wait(timeout=10)
            (out/'exit.json').write_text(json.dumps({'displays':codes, 'server':code})+'\n')
            assert code == 0, code
        finally:
            for proc in displays+[server]:
                if proc.poll() is None:
                    proc.kill()
                proc.wait(timeout=10)
            for log in logs+[server_log]:
                log.close()
    (out/'reference.json').write_text(json.dumps({'binary':str(binary), 'sha256':hashlib.sha256(binary.read_bytes()).hexdigest(), 'product':a.product, 'kind':kind, 'position':position, 'cols':cols, 'clipboard':'sandbox-denied, never read', 'sound':'disabled'}, indent=2)+'\n')
    print(name+' captured', flush=True)
