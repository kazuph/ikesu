"""Import an unmodified session saved by the isolated real herdr comparison."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time

from clipboard_guard import SANDBOX_PROFILE

p = argparse.ArgumentParser()
p.add_argument('--snapshot', type=Path, required=True)
p.add_argument('--binary', type=Path, required=True)
p.add_argument('--ptyrun', type=Path, required=True)
p.add_argument('--out', type=Path, required=True)
p.add_argument('--config-file', type=Path, required=True)
a = p.parse_args()
a.binary = a.binary.resolve()
a.ptyrun = a.ptyrun.resolve()
a.out = a.out.resolve()
a.out.mkdir(parents=True, exist_ok=True)
saved = json.loads(a.snapshot.read_text())
files = {Path(k).name: v for k, v in saved.items()
         if k.endswith(('/session.json', '/agent-session-ledger.json', '/agent-session-snapshot.json'))}
assert len([k for k in saved if k.endswith('/session.json')]) == 1
source = json.loads(files['session.json'])
assert source['version'] == 5 and len(source['workspaces']) == 2
cwds = {Path(w['identity_cwd']) for w in source['workspaces']}
for w in source['workspaces']:
    for tab in w['tabs']:
        for pane in tab['panes'].values():
            assert not pane.get('agent_name') and not pane.get('managed_agent_kind'), 'shell-only proof required'
            cwds.add(Path(pane['cwd']))
roots = {Path(*cwd.parts[:4]) for cwd in cwds}
assert len(roots) == 1
oldroot = roots.pop()
assert str(oldroot).startswith('/private/tmp/parity-') and not oldroot.exists(), oldroot
for cwd in cwds:
    cwd.mkdir(parents=True, exist_ok=True)
server = None
try:
    with tempfile.TemporaryDirectory(prefix='tdi-', dir='/tmp') as tmp:
        root = Path(tmp)
        for name in ['source', 'home', 'config', 'state', 'zdot']:
            (root/name).mkdir()
        for name, content in files.items():
            (root/'source'/name).write_text(content)
        env = {k: v for k, v in os.environ.items() if not k.startswith(('GTX', 'HERDR', 'GOTERRACE', 'CODEX', 'CLAUDE'))}
        cfg = root/'config.toml'
        cfg.write_text(a.config_file.read_text())
        (root/'zdot/.zshrc').write_text("PS1='IMPORT> '\nunset HISTFILE\n")
        env.update(HOME=str(root/'home'), ZDOTDIR=str(root/'zdot'), SHELL='/bin/zsh',
                   GTX_STATE_DIR=str(root/'state'), GTX_CONFIG=str(cfg), XDG_CONFIG_HOME=str(root/'config'))
        argv = ['/usr/bin/sandbox-exec', '-p', SANDBOX_PROFILE,
                str(a.binary), '--session', 'import-directory']
        imported = subprocess.run(argv+['session', 'import-herdr', '--source', str(root/'source'), '--yes'], env=env, capture_output=True, text=True, timeout=30)
        (a.out/'import.json').write_text(json.dumps(dict(exit=imported.returncode, stdout=imported.stdout, stderr=imported.stderr), indent=2))
        assert imported.returncode == 0, imported.stderr
        log = (a.out/'server.log').open('w')
        server = subprocess.Popen(argv+['server'], env=env, stdout=log, stderr=log)
        deadline = time.monotonic()+10
        while not list((root/'state').rglob('gtx.sock')):
            assert server.poll() is None and time.monotonic() < deadline, 'owned import server unavailable'
            time.sleep(.02)
        endpoint = list((root/'state').rglob('gtx.sock'))
        assert len(endpoint) == 1
        def request(method, params={}):
            with socket.socket(socket.AF_UNIX) as s:
                s.settimeout(5)
                s.connect(str(endpoint[0]))
                s.sendall(json.dumps(dict(id='import-directory', method=method, params=params)).encode()+b'\n')
                data = b''
                while b'\n' not in data:
                    data += s.recv(65536)
                result = json.loads(data.split(b'\n')[0])
                assert 'error' not in result, result
                return result
        try:
            before = request('workspace.list')
            spaces = before['result']['workspaces']
            assert [w['label'] for w in spaces] == ['alpha', 'chosen'], spaces
            request('workspace.focus', dict(workspace_id=spaces[0]['workspace_id']))
            steps = a.out/'steps'
            fixture = Path(__file__).resolve().parent/'directory_evidence.py'
            steps.write_text('sleep 2s\nsend "cd ../beta\\r"\nsleep 2s\nsend "\\x14c"\nsleep 200ms\nsend "new\\r"\nsleep 1s\nsend "python3 '+str(fixture)+' imported-tab\\r"\nsleep 300ms\nscreen '+str(a.out/'imported.txt')+'\ncells '+str(a.out/'imported.json')+'\nkill\n')
            run = subprocess.run([str(a.ptyrun), '-cols', '160', '-rows', '45', '-pixel-mouse=false', '-steps', str(steps), '--']+argv, env=env, capture_output=True, text=True, timeout=30)
            (a.out/'run.log').write_text(run.stdout+run.stderr)
            assert run.returncode == 0, run.stderr
            after = request('workspace.list')
            assert [w['label'] for w in after['result']['workspaces']] == ['beta', 'chosen'], after
            evidence = json.loads((root/'home/cwd-evidence.json').read_text())
            assert evidence['imported-tab']['cwd'] == str(oldroot/'home/beta'), evidence
            for name, content in files.items():
                assert (root/'source'/name).read_text() == content
            (a.out/'proof.json').write_text(json.dumps(dict(status='PASS', before=before, after=after, directories=evidence,
                source_sha256=hashlib.sha256(files['session.json'].encode()).hexdigest()), indent=2))
        finally:
            request('server.stop')
            server.wait(timeout=10)
            server = None
            log.close()
finally:
    if server is not None:
        server.terminate()
        server.wait(timeout=10)
    shutil.rmtree(oldroot)
print('PASS: real saved herdr session imported; automatic name, chosen name and tab directory retained')
