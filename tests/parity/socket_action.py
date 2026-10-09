#!/usr/bin/env python3
"""Real socket operations for disposable servers used by the PTY scenarios."""
import json
import os
import re
import shlex
from pathlib import Path
import socket
import subprocess
import sys
import time

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: socket is not in the runner-owned temporary state')
operation = sys.argv[1]


def request(method, params):
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(endpoint)
        connection.sendall(json.dumps({'id': 'parity-fixture', 'method': method,
                                      'params': params}).encode()+b'\n')
        response = bytearray()
        while b'\n' not in response:
            chunk = connection.recv(65536)
            if not chunk:
                raise SystemExit('NOT_RUN: socket closed without reply')
            response.extend(chunk)
        result = json.loads(response.split(b'\n')[0])
        proof_path = Path.home()/'parity-api-actions.json'
        proof = json.loads(proof_path.read_text()) if proof_path.exists() else []
        proof.append({'method':method,'params':params,'response':result})
        proof_path.write_text(json.dumps(proof,ensure_ascii=False,indent=2)+'\n')
        print(json.dumps(result, ensure_ascii=False))
        if 'error' in result:
            raise SystemExit(1)
        return result


if operation == 'open':
    pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
    if not pane:
        raise SystemExit('NOT_RUN: exact pane identity absent')
    method = 'pane.command.execute'
    params = {'pane_id': pane, 'action': 'popup',
              'command': "env PS1='POPUP> ' /bin/zsh -f", 'width': 60, 'height': 20}
elif operation == 'report-state':
    pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
    session = os.environ.get('PARITY_CONTROLLER_SESSION_ID')
    if not pane or not session:
        raise SystemExit('NOT_RUN: pane identity or actual controller session absent')
    method = 'pane.report_agent'
    params = {'pane_id':pane, 'source':'codex', 'agent':'codex',
              'state':sys.argv[2], 'title':'PARITY', 'session_id':session}
elif operation == 'close':
    method, params = 'popup.close', {}
elif operation == 'create-other':
    pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
    if not pane:
        raise SystemExit('NOT_RUN: exact owner pane absent')
    owner = request('pane.get', {'pane_id':pane})['result']['pane']['workspace_id']
    created = request('workspace.create', {'cwd':os.getcwd(), 'label':'other', 'focus':False})
    other = created['result']['workspace']['workspace_id']
    (Path.home()/'parity-popup-owners.json').write_text(json.dumps({'owner':owner,'other':other})+'\n')
    raise SystemExit(0)
elif operation in ('focus-other', 'focus-owner', 'close-owner'):
    owners = json.loads((Path.home()/'parity-popup-owners.json').read_text())
    method = 'workspace.close' if operation == 'close-owner' else 'workspace.focus'
    params = {'workspace_id':owners['other' if operation == 'focus-other' else 'owner']}
elif operation == 'start-job':
    pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
    if not pane:
        raise SystemExit('NOT_RUN: exact caller pane absent')
    resolved = request('pane.get', {'pane_id': pane})
    pane = resolved['result']['pane']['pane_id']
    method = 'run.start'
    argv = ['/bin/sleep', '60']
    if len(sys.argv) > 2 and sys.argv[2] == 'short':
        argv = ['/bin/sh', '-c', 'printf "PARITY_JOB_OUTPUT\\n"; sleep 5; exit 7']
    elif len(sys.argv) > 2 and sys.argv[2] == 'success':
        argv = ['/bin/sh', '-c', 'printf "PARITY_JOB_OUTPUT\\n"; sleep 5; exit 0']
    elif len(sys.argv) > 2 and sys.argv[2] == 'ignore-term':
        ready = shlex.quote(str(Path.home()/'parity-job-ready'))
        argv = ['/bin/sh', '-c', "trap '' TERM; printf PARITY_READY > "+ready+"; sleep 60"]
    params = {'caller_pane': pane, 'label': 'PARITY_JOB', 'cwd': os.getcwd(),
              'argv': argv, 'completion': 'none'}
elif operation == 'cancel-job':
    started = json.loads((Path.home()/'parity-job.json').read_text())
    binary = os.environ.get('PARITY_BINARY_PATH')
    if not binary:
        raise SystemExit('NOT_RUN: isolated binary identity absent')
    result = subprocess.run([binary, 'run', 'cancel', started['result']['job']],
                            capture_output=True, text=True)
    captured = {'exit':result.returncode,'stdout':result.stdout,'stderr':result.stderr}
    (Path.home()/'parity-job-cancel.json').write_text(json.dumps(captured,ensure_ascii=False)+'\n')
    print(json.dumps(captured,ensure_ascii=False))
    raise SystemExit(result.returncode)
elif operation == 'read-job':
    started = json.loads((Path.home()/'parity-job.json').read_text())
    binary = os.environ.get('PARITY_BINARY_PATH')
    if not binary:
        raise SystemExit('NOT_RUN: isolated binary identity absent')
    result = subprocess.run([binary, 'log', started['result']['job']],
                            capture_output=True, text=True)
    captured = json.dumps({'exit': result.returncode, 'stdout': result.stdout,
                           'stderr': result.stderr}, ensure_ascii=False)
    (Path.home()/'parity-job-status.json').write_text(captured+'\n')
    print(captured)
    raise SystemExit(result.returncode)
elif operation == 'check-job-processes':
    started = json.loads((Path.home()/'parity-job.json').read_text())
    job_id = started['result']['job']
    root = Path.home().parent.resolve()
    if not str(root).startswith('/private/tmp/parity-'):
        raise SystemExit('REFUSED: job log is not in the runner-owned temporary state')
    logs = [path for path in root.rglob('*.log') if job_id in path.name]
    if len(logs) != 1:
        raise SystemExit('NOT_RUN: exact job log missing or ambiguous')
    match = re.search(r'^runner_pid: (\d+)$', logs[0].read_text(), re.M)
    if match is None or int(match[1]) <= 1:
        raise SystemExit('NOT_RUN: recorded runner identity absent')
    runner = int(match[1])
    try:
        os.kill(-runner, 0)
        alive = True
    except ProcessLookupError:
        alive = False
    proof = {'job':job_id, 'runner_pid':runner, 'group_alive':alive,
             'checked_unix_ms':time.time_ns()//1000000}
    (Path.home()/'parity-job-processes.json').write_text(json.dumps(proof)+'\n')
    print(json.dumps(proof))
    raise SystemExit(1 if alive else 0)
else:
    raise SystemExit('unsupported fixture operation')
result = request(method, params)
if operation == 'start-job':
    (Path.home()/'parity-job.json').write_text(json.dumps(result)+'\n')
