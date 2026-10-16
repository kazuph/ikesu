#!/usr/bin/env python3
"""Run identical ptyrun steps against isolated herdr and gtx binaries."""
import argparse
import os
import json
import socket
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import hashlib

p = argparse.ArgumentParser()
p.add_argument('--herdr', required=True)
p.add_argument('--cols', type=int, default=160)
p.add_argument('--rows', type=int, default=45)
p.add_argument('--gtx', required=True)
p.add_argument('--ptyrun', required=True)
p.add_argument('--cell-clock', action='store_true', help='save the real PTY time of each cell content change')
p.add_argument('--pixel-mouse', action='store_true', help='advertise SGR pixel mouse support to the attached display')
p.add_argument('--cell-width', type=int, default=0, help='PTY terminal cell width in pixels')
p.add_argument('--cell-height', type=int, default=0, help='PTY terminal cell height in pixels')
p.add_argument('--startup-input', action='store_true', help='observe startup input with onboarding=false; wait for readiness in the steps')
p.add_argument('--hold-color-replies', action='store_true', help='answer background queries explicitly from the test steps')
p.add_argument('--in-app-toast', action='store_true', help='set the documented product-specific in-app delivery before isolated startup')
p.add_argument('--steps', required=True)
p.add_argument('--saved-session-fixture', help='JSON mapping product names to relative runner-owned session paths and saved documents')
p.add_argument('--config-file', help='copy a comparison-only config into each isolated product directory')
p.add_argument('--out', required=True)
p.add_argument('--close-workspace-fixture', action='store_true', help='close the last isolated workspace through the real API after the steps trigger')
p.add_argument('--server', action='store_true', help='run a disposable persistent server for socket-driven UI')
p.add_argument('--reconnect-keyboard', choices=['off', 'retain'], help='cut only the isolated display connection while pane modes change')
p.add_argument('--server-default-workspace', action='store_true', help='observe server/client startup without creating a workspace through the API')
p.add_argument('--server-reuse-initial', action='store_true', help='use the real initial workspace when present, otherwise create it; save all preparation replies')
p.add_argument('--clipboard-guard', action='store_true', help='capture clipboard commands and stop if a changed clipboard hash matches copied bytes')
p.add_argument('--clipboard-sandbox', action='store_true', help='deny pasteboard Mach lookup in the UI/server and all their children')
a = p.parse_args()
if a.reconnect_keyboard and not a.server:
    p.error('keyboard reconnect observation requires --server')
if (a.server_default_workspace or a.server_reuse_initial) and not a.server:
    p.error('server fixture options require --server')
if a.server_default_workspace and a.server_reuse_initial:
    p.error('choose one server fixture preparation')
a.ptyrun = str(Path(a.ptyrun).resolve())
if not Path(a.herdr).is_file():
    print('NOT_RUN: herdr binary absent')
    raise SystemExit(0)
out = Path(a.out).resolve()
out.mkdir(parents=True, exist_ok=True)
(out/'reference.json').write_text(json.dumps({
    'herdr_binary': str(Path(a.herdr).resolve()),
    'herdr_version': subprocess.check_output([a.herdr, '--version'], text=True).strip(),
    'herdr_sha256': hashlib.sha256(Path(a.herdr).read_bytes()).hexdigest(),
    'gtx_binary': str(Path(a.gtx).resolve()),
    'gtx_sha256': hashlib.sha256(Path(a.gtx).read_bytes()).hexdigest(),
    'config': Path(a.config_file).read_text() if a.config_file else None,
    'observer_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
    'procedure_sha256': hashlib.sha256(Path(a.steps).read_bytes()).hexdigest(),
}, indent=2)+'\n')


guard = None
if a.clipboard_guard and a.clipboard_sandbox:
    p.error('choose one clipboard isolation method')
if a.clipboard_guard or a.clipboard_sandbox:
    from clipboard_guard import ClipboardGuard, SANDBOX_PROFILE
    guard = ClipboardGuard(out / 'clipboard-guard', capture_commands=a.clipboard_guard)
    if a.clipboard_sandbox:
        guard.sandbox_probe()
interaction = Path(a.steps).read_text()
for name, binary in [('herdr', a.herdr), ('gtx', a.gtx)]:
    target = out / name
    target.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='parity-', dir='/tmp') as root:
        root = Path(root)
        for directory in ['home', 'config', 'state', 'data', 'zdot', 'cwd']:
            (root / directory).mkdir()
        (root / 'zdot/.zshrc').write_text("PS1='PARITY> '\nunset HISTFILE\nSAVEHIST=0\n")
        if a.config_file or a.in_app_toast:
            config = root / 'config' / name / 'config.toml'
            config.parent.mkdir(parents=True)
            config.write_text(Path(a.config_file).read_text() if a.config_file else '')
            if a.in_app_toast:
                with config.open('a') as stream:
                    stream.write(f'\n[ui.toast]\ndelivery = \"{name}\"\n')
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(('HERDR', 'GTX', 'GOTERRACE', 'CLAUDE', 'CODEX'))}
        env.update(HOME=str(root / 'home'), ZDOTDIR=str(root / 'zdot'),
                   XDG_CONFIG_HOME=str(root / 'config'), XDG_STATE_HOME=str(root / 'state'),
                   XDG_DATA_HOME=str(root / 'data'), HERDR_STATE_DIR=str(root / 'state/h'),
                   GTX_STATE_DIR=str(root / 'state/g'), SHELL='/bin/zsh', TERM='xterm-256color')
        if a.saved_session_fixture:
            documents = json.loads(Path(a.saved_session_fixture).read_text())[name]
            for relative, document in documents.items():
                destination = root / relative.replace('@BINARY@', Path(binary).stem)
                if not destination.resolve().is_relative_to(root.resolve()):
                    raise RuntimeError('saved-session fixture escapes isolated root')
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_text(json.dumps(document)+'\n')
            (target/'saved-session-fixture.json').write_text(json.dumps(documents, indent=2)+'\n')
        env['PARITY_OBSERVE_BINARY'] = str(Path(binary).resolve())
        if (a.config_file or a.in_app_toast) and name == 'gtx':
            env['GTX_CONFIG'] = str(config)
        if (a.config_file or a.in_app_toast) and name == 'herdr':
            env['HERDR_CONFIG_PATH'] = str(config)
        if guard is not None:
            guard.environment(env)
        steps = target / 'steps'
        # Only the first-run onboarding differs. The interaction body is identical.
        preamble = 'sleep 3s\nsend "\\r"\nsleep 1s\n' if name == 'herdr' and not a.server else 'sleep 3s\n'
        if a.startup_input:
            preamble = ''
        steps.write_text(preamble + interaction.replace('@OUT@', str(target)).replace('@FIXTURES@', str(Path(__file__).resolve().parent)).replace('@HOME@', str(root/'home')))
        with (target / 'run.log').open('w') as log, (target / 'server.log').open('w') as server_log:
            server = None
            binary = str(Path(binary).resolve())
            launcher = ['/usr/bin/sandbox-exec', '-p', SANDBOX_PROFILE, binary] if a.clipboard_sandbox else [binary]
            if a.clipboard_sandbox:
                (target / 'sandbox-launch.json').write_text(json.dumps({'launcher': launcher, 'server': a.server}, indent=2)+'\n')
            env['PARITY_BINARY_PATH'] = binary
            endpoint = None
            reconnect = None
            close_workspace = None
            try:
                if a.server:
                    server = subprocess.Popen(launcher + ['--session', 'parity', 'server'],
                                              cwd=root / 'cwd', env=env, stdout=server_log, stderr=subprocess.STDOUT)
                    (target / 'server-pid.txt').write_text(str(server.pid)+'\n')
                    env['PARITY_OBSERVE_PID'] = str(server.pid)
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
                    if not a.server_default_workspace:
                        initial_reply = request('workspace.list', {})
                        existing = initial_reply['result']['workspaces']
                        if a.server_reuse_initial and existing:
                            if len(existing) != 1:
                                raise RuntimeError('isolated startup must have exactly one initial workspace')
                            workspace_id = existing[0]['workspace_id']
                            renamed = request('workspace.rename', {'workspace_id': workspace_id, 'label':'parity'})
                            panes = request('pane.list', {'workspace_id': workspace_id})
                            if len(panes['result']['panes']) != 1:
                                raise RuntimeError('initial fixture must have exactly one real pane')
                            created = {'result': {'type':'fixture_snapshot', 'workspace': renamed['result']['workspace'],
                                                  'root_pane':panes['result']['panes'][0]}}
                            preparation = {'initial':initial_reply, 'rename':renamed, 'panes':panes}
                        else:
                            created = request('workspace.create', {'cwd':str(root / 'cwd'), 'label':'parity', 'focus':True})
                            preparation = {'initial':initial_reply, 'create':created}
                        (target / 'fixture-preparation.json').write_text(json.dumps(preparation, indent=2)+'\n')
                        (target / 'fixture.json').write_text(json.dumps(created, indent=2)+'\n')
                        fixture_id = created['result']['workspace']['workspace_id']
                        initial = request('workspace.list', {})
                        cleanup = []
                        for workspace in initial['result']['workspaces']:
                            if workspace['workspace_id'] != fixture_id:
                                cleanup.append(request('workspace.close', {'workspace_id':workspace['workspace_id']}))
                        final = request('workspace.list', {})
                        if [w['workspace_id'] for w in final['result']['workspaces']] != [fixture_id]:
                            raise RuntimeError('isolated UI fixture must contain exactly the created workspace')
                        (target / 'fixture-workspaces.json').write_text(json.dumps({'initial':initial, 'closed':cleanup, 'final':final}, indent=2)+'\n')
                if a.close_workspace_fixture:
                    if endpoint is None: raise RuntimeError('close fixture requires isolated server')
                    from stage5_workspace_controller import CloseWorkspace
                    close_workspace = CloseWorkspace(endpoint, target)
                    close_workspace.start()
                if a.reconnect_keyboard:
                    from stage1_reconnect import Reconnect
                    reconnect = Reconnect(launcher, env, target, a.reconnect_keyboard)
                    reconnect.start()
                    wrapper = root/'display.py'
                    wrapper.write_text('import os,sys\nfrom pathlib import Path\np=Path(sys.argv[1]); q=p.with_suffix(".next"); q.write_text(str(os.getpid())); q.replace(p)\nos.execv(sys.argv[2],sys.argv[2:])\n')
                    launcher = launcher[:-1]+[sys.executable, str(wrapper), str(target/'display-pid'), binary]
                subprocess.run([a.ptyrun, '-cols', str(a.cols), '-rows', str(a.rows), '-pixel-mouse='+str(a.pixel_mouse).lower(),
                            '-cell-width', str(a.cell_width), '-cell-height', str(a.cell_height),
                            '-steps', str(steps), '-raw-output', str(target/'terminal.raw')] + (['-cell-clock'] if a.cell_clock else []) + (['-hold-color-replies'] if a.hold_color_replies else []) + ['--'] + launcher + (['--session', 'parity'] if a.server else ['--no-session']),
                               cwd=root / 'home', env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
            finally:
                if close_workspace:
                    close_workspace.close()
                if reconnect:
                    reconnect.close()
                try:
                    runtime_json = {str(path.relative_to(root)): path.read_text(errors='replace')
                                    for path in root.rglob('*.json') if path.is_file()}
                    (target/'runtime-json.json').write_text(json.dumps(runtime_json, ensure_ascii=False, indent=2)+'\n')
                    job_status = root/'home/parity-job-status.json'
                    if job_status.is_file():
                        (target/'job-status.json').write_text(job_status.read_text())
                    job_logs = {str(path.relative_to(root)): path.read_text(errors='replace')
                                for path in root.rglob('job-logs/*.log') if path.is_file()}
                    (target/'job-logs.json').write_text(json.dumps(job_logs, ensure_ascii=False, indent=2)+'\n')
                    runtime_logs = {str(path.relative_to(root)): path.read_text(errors='replace')
                                    for path in root.rglob('*server.log') if path.is_file()}
                    (target/'runtime-logs.json').write_text(json.dumps(runtime_logs, ensure_ascii=False, indent=2)+'\n')
                    configs = {str(path.relative_to(root)): path.read_text() for path in root.rglob('config.toml') if path.is_file()}
                    (target/'config-files.json').write_text(json.dumps(configs, ensure_ascii=False, indent=2)+'\n')
                finally:
                    try:
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
                    finally:
                        saved_json = {str(path.relative_to(root)): path.read_text(errors='replace')
                                      for path in root.rglob('*.json') if path.is_file()}
                        (target/'saved-json.json').write_text(json.dumps(saved_json, ensure_ascii=False, indent=2)+'\n')
                        if guard is not None:
                            guard.check(name, target/'terminal.raw')
