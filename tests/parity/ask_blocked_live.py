#!/usr/bin/env python3
"""Observe real Claude AskUserQuestion on disposable ikesu/herdr sessions.

No source of the reference product is inspected. Sound stays disabled; the
unmodified client protocol is recorded by a transparent Unix socket relay.
"""
import argparse
import json
import os
from pathlib import Path
import select
import shlex
import socket
import subprocess
import tempfile
import threading
import time


def until(predicate, timeout, description):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        value = predicate()
        if value:
            return value
        select.select([], [], [], .1)
    raise RuntimeError('timeout: ' + description)


def observe(name, binary, args):
    out = Path(args.out).resolve() / name
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='ask-blocked-', dir='/private/tmp/ikesu-accept') as tmp:
        root = Path(tmp)
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(('IKESU', 'HERDR', 'CLAUDE', 'CODEX'))}
        for sub in ['home', 'config', 'state', 'data', 'cwd', 'zdot']:
            (root / sub).mkdir()
        (root / 'zdot/.zshrc').write_text("PS1='ASK-PROBE> '\nunset HISTFILE\n")
        config = root / 'config.toml'
        config.write_text('[ui.toast]\ndelivery="terminal"\ndelay_seconds=0\n[ui.sound]\nenabled=true\n')
        env.update(HOME=str(root/'home'), XDG_CONFIG_HOME=str(root/'config'), XDG_STATE_HOME=str(root/'state'),
                   XDG_DATA_HOME=str(root/'data'), ZDOTDIR=str(root/'zdot'),
                   IKESU_STATE_DIR=str(root/'state/g'), HERDR_STATE_DIR=str(root/'state/h'),
                   IKESU_CONFIG=str(config), HERDR_CONFIG_PATH=str(config),
                   IKESU_DISABLE_SOUND='1', HERDR_DISABLE_SOUND='1', SHELL='/bin/zsh', TERM='xterm-256color', TERM_PROGRAM='ghostty')
        log = (out/'server.log').open('w')
        server = subprocess.Popen([binary, '--session', 'ask-probe', 'server'],
                                  cwd=root/'cwd', env=env, stdout=log, stderr=log)
        driver = None
        requests = []
        try:
            def endpoints():
                if server.poll() is not None:
                    raise RuntimeError('server exited: '+(out/'server.log').read_text())
                found = [p for p in root.rglob('*.sock') if p.name == name+'.sock']
                return found[0] if len(found) == 1 else None
            api = until(endpoints, 20, 'API socket')

            def request(method, params):
                with socket.socket(socket.AF_UNIX) as sock:
                    sock.settimeout(10)
                    sock.connect(str(api))
                    sock.sendall(json.dumps({'id':'ask-probe', 'method':method, 'params':params}).encode()+b'\n')
                    data = bytearray()
                    while b'\n' not in data:
                        part = sock.recv(65536)
                        if not part:
                            raise RuntimeError('API closed')
                        data.extend(part)
                    response = json.loads(data.split(b'\n')[0])
                    requests.append({'method':method, 'params':params, 'response':response})
                    (out/'api.json').write_text(json.dumps(requests, ensure_ascii=False, indent=2))
                    if 'error' in response:
                        raise RuntimeError(response)
                    return response['result']

            if name == "ikesu":
                # Interpose only on this disposable session's existing attach socket.
                client_path = until(lambda: next(iter(root.rglob(name+'-client.sock')), None), 10, 'client socket')
                upstream = client_path.with_name('observed-client.sock')
                client_path.rename(upstream)
                relay = socket.socket(socket.AF_UNIX)
                relay.bind(str(client_path))
                relay.listen()
                wire = (out/'client-protocol.jsonl').open('wb')

                def forward():
                    downstream, _ = relay.accept()
                    with downstream, socket.socket(socket.AF_UNIX) as peer:
                        peer.connect(str(upstream))
                        while True:
                            ready, _, _ = select.select([downstream, peer], [], [], 1)
                            for source in ready:
                                data = source.recv(1048576)
                                if not data:
                                    return
                                if source == peer:
                                    wire.write(data)
                                    wire.flush()
                                (peer if source == downstream else downstream).sendall(data)
                threading.Thread(target=forward, daemon=True).start()
            ui_log = (out/'ui.log').open('w')
            driver = subprocess.Popen([args.ptyrun, '-cols','160','-rows','45', '-pixel-mouse=false',
                                       '-steps','/dev/stdin', '-raw-output',str(out/'terminal.raw'),
                                       '--',binary,'--session','ask-probe'],
                                      stdin=subprocess.PIPE, stdout=ui_log, stderr=ui_log, text=True,
                                      cwd=root/'cwd', env=env)

            def step(line):
                driver.stdin.write(line+'\n')
                driver.stdin.flush()

            def capture(label):
                path = out/(label+'.txt')
                cells = out/(label+'.cells.json')
                path.unlink(missing_ok=True)
                cells.unlink(missing_ok=True)
                step('cells '+str(cells))
                until(lambda: cells.exists() and cells.stat().st_size > 0, 15, label+' capture')
                frame = json.loads(cells.read_text())
                path.write_text('\n'.join(''.join(cell.get('Content', '') or (' ' if cell.get('Width', 1) else '') for cell in row).rstrip() for row in frame['Cells'])+'\n')
                return path.read_text()

            def click_sidebar(label):
                capture('sidebar-target')
                grid=json.loads((out/'sidebar-target.cells.json').read_text())['Cells']
                border=next(x for row in grid for x,c in enumerate(row) if c['Content']=='┌')
                targets=[]
                workspace_bottom=next(y for y,row in enumerate(grid) if '[menu]' in ''.join(c['Content'] or ' ' for c in row[:border]))
                for y,row in enumerate(grid[:workspace_bottom]):
                    text=''.join(c['Content'] or ' ' for c in row[:border])
                    if label in text:
                        targets.append((text.index(label)+1,y+1))
                if len(targets)!=1:
                    raise RuntimeError('sidebar label must be unique: '+repr(targets))
                x,y=targets[0]
                step(f'mouse down {x} {y}')
                step(f'mouse up {x} {y}')

            created = request('workspace.create', {'cwd':str(root/'cwd'), 'label':'ask-agent', 'focus':True})
            ws = created['workspace']['workspace_id']
            pane = created['root_pane']['pane_id']
            for old in request('workspace.list', {})['workspaces']:
                if old['workspace_id'] != ws:
                    request('workspace.close', {'workspace_id':old['workspace_id']})
            until(lambda: 'ask-agent' in capture('startup'), 20, 'workspace painted')
            # Dismiss only product onboarding, never a permission/trust prompt.
            if 'continue' in capture('onboarding').lower():
                step('send '+json.dumps('\r'))
            command = shlex.join(['env', 'HOME='+str(Path.home()), args.claude,'--permission-mode','auto','--setting-sources',''])
            request('pane.send_input', {'pane_id':pane, 'text':command,'keys':['Enter']})

            def claude_ready():
                screen = capture('claude-startup')
                if 'is this a project you created or one you trust' in screen.lower() or 'trust this folder' in screen.lower() or 'trust the files' in screen.lower():
                    raise RuntimeError('BLOCKED: Claude folder trust requires human decision')
                if 'not logged in' in screen.lower() or 'please run /login' in screen.lower():
                    raise RuntimeError('BLOCKED: Claude authentication')
                return 'auto mode' in screen.lower()
            until(claude_ready, 90, 'real Claude auto mode')
            request('pane.report_agent', {'pane_id':pane,'source':'claude','agent':'claude','state':'working'})
            before = request('pane.get', {'pane_id':pane})
            (out/'before.json').write_text(json.dumps(before,indent=2))
            def working_frame(label):
                capture(label)
                frame=json.loads((out/(label+'.cells.json')).read_text())
                # Catppuccin's working yellow, from internal/render/palette.go.
                return any(c['Style'].get('Fg')=={'R':249,'G':226,'B':175,'A':255} for c in frame['Cells'][0])
            until(lambda:working_frame('before'),20,'working status painted')
            foreground = request('workspace.create', {'cwd':str(root/'cwd'), 'label':'foreground', 'focus':True})
            if name == 'herdr':
                click_sidebar('foreground')
            target='%'+foreground['root_pane']['pane_id'].lstrip('p')+' '
            until(lambda: target in capture('background-ready').splitlines()[1],20,'foreground clicked')
            prompt = ('Use the AskUserQuestion tool now. Ask exactly one question: "Which probe option?" '
                      'with two options labeled "Probe first" and "Probe second", each with a description. '
                      'Do not use any other tools. After I answer or cancel, just say "Probe complete".')
            request('pane.send_input', {'pane_id':pane,'text':prompt,'keys':['Enter']})

            def blocked():
                info = request('pane.get', {'pane_id':pane})
                return info if info['pane']['agent_status'] == 'blocked' else None
            state = until(blocked, 150, 'real AskUserQuestion blocked')
            (out/'blocked.json').write_text(json.dumps(state,indent=2))
            capture('background-blocked')
            until(lambda: (out/'terminal.raw').read_bytes().count(b'\x1b]9;') == 1, 20, 'background terminal notification')
            # Inspect the actual question and tab state once the background notification fired.
            request('workspace.focus', {'workspace_id':ws})
            if name == 'herdr':
                click_sidebar('ask-agent')
            until(lambda: 'Probe first' in capture('question'), 20, 'actual question visible')
            request('pane.send_keys', {'pane_id':pane,'keys':['Escape']})
            restored = until(lambda: (info if (info:=request('pane.get', {'pane_id':pane}))['pane']['agent_status']=='working' else None), 20,'report restored')
            (out/'restored.json').write_text(json.dumps(restored,indent=2))
            until(lambda:working_frame('restored'),20,'working recovery painted')
            (out/'summary.json').write_text(json.dumps({'status':'PASS', 'pane':pane,
                    'before':before['pane']['agent_status'], 'question':state['pane']['agent_status'],
                    'restored':restored['pane']['agent_status'], 'sound_disabled':True,
                    'binary_version':subprocess.check_output([binary,'--version'],text=True).strip()},indent=2))
        except Exception as exc:
            (out/'summary.json').write_text(json.dumps({'status':'BLOCKED' if 'BLOCKED:' in str(exc) else 'FAIL','reason':str(exc)},indent=2))
            raise
        finally:
            if driver:
                driver.terminate()
                try: driver.wait(timeout=10)
                except subprocess.TimeoutExpired: driver.kill(); driver.wait()
            server.terminate()
            try: server.wait(timeout=10)
            except subprocess.TimeoutExpired: server.kill(); server.wait()
            log.close()


if __name__ == '__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--ikesu',dest='ikesu',required=True)
    parser.add_argument('--herdr',required=True)
    parser.add_argument('--claude',required=True)
    parser.add_argument('--ptyrun',required=True)
    parser.add_argument('--out',required=True)
    parser.add_argument('--only', choices=['herdr','ikesu'])
    args=parser.parse_args()
    failures=[]
    for product,binary in [('herdr',args.herdr),('ikesu',args.ikesu)]:
        if args.only and args.only != product:
            continue
        try: observe(product,str(Path(binary).resolve()),args)
        except Exception as exc: failures.append(product+': '+str(exc))
    if failures:
        raise SystemExit('\n'.join(failures))
