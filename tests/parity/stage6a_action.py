#!/usr/bin/env python3
"""Operate only observe.py's disposable server and temporary Git repositories."""
import json
import os
from pathlib import Path
import socket
import subprocess
import sys

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned socket')
root = Path.home()
proof = root/'stage6a-actions.json'

def request(method, params):
    with socket.socket(socket.AF_UNIX) as conn:
        conn.settimeout(10)
        conn.connect(endpoint)
        conn.sendall(json.dumps({'id':'stage6a','method':method,'params':params}).encode()+b'\n')
        data = b''
        while b'\n' not in data:
            chunk = conn.recv(65536)
            if not chunk:
                raise RuntimeError('closed without response')
            data += chunk
    reply = json.loads(data.split(b'\n')[0])
    entries = json.loads(proof.read_text()) if proof.exists() else []
    entries.append({'method':method,'params':params,'reply':reply})
    proof.write_text(json.dumps(entries,ensure_ascii=False,indent=2)+'\n')
    return reply

def pane():
    return os.environ.get('HERDR_PANE_ID') or os.environ['GTX_PANE_ID']

op = sys.argv[1]
if op == 'duplicate':
    first = request('pane.command.execute', {'pane_id':pane(),'action':'popup','command':f'python3 {Path(__file__).with_name("key_probe.py")} {root}/popup-received.jsonl 1b5b3f3168'})
    second = request('pane.command.execute', {'pane_id':pane(),'action':'popup','command':'echo REPLACED; /bin/zsh -f'})
    (root/'duplicate.json').write_text(json.dumps([first, second]))
elif op == 'cross':
    request('pane.command.execute', {'pane_id':pane(),'action':'popup','command':f'python3 {Path(__file__).with_name("key_probe.py")} {root}/popup-received.jsonl 1b5b3f3168'})
    owner = request('pane.get',{'pane_id':pane()})['result']['pane']['workspace_id']
    other = request('workspace.create',{'cwd':str(root),'label':'other','focus':True})
    request('popup.close',{})
    request('workspace.focus',{'workspace_id':owner})
elif op == 'size':
    request('pane.command.execute', {'pane_id':pane(),'action':'popup','command':f'stty size > {root}/size-{sys.argv[2]}.txt; printf ABCDEFGHIJ; /bin/zsh -f','width':int(sys.argv[2]),'height':int(sys.argv[3])})
elif op == 'repo':
    repo = root/'fixture-repo'
    repo.mkdir()
    def git(*args):
        subprocess.run(['git','-C',str(repo),*args],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
    git('init','-b','main')
    git('-c','user.name=Parity','-c','user.email=parity@example.invalid','commit','--allow-empty','-m','fixture')
    for n in range(12):
        git('worktree','add','-b',f'branch-{n:02}',str(root/f'checkout-{n:02}'))
    (root/'checkout-01'/'dirty.txt').write_text('untracked\n')
    request('workspace.create',{'cwd':str(repo),'label':'fixture-repo','focus':True})
    request('workspace.list',{})
elif op == 'close':
    request('popup.close',{})
else:
    raise SystemExit('unknown action')
print('ACTION_DONE',flush=True)
