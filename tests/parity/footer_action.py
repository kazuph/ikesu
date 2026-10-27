#!/usr/bin/env python3
"""Operate only the existing observer's disposable state for footer checks."""
import json
import os
from pathlib import Path
import socket
import subprocess
import sys

endpoint=Path(os.environ.get('IKESU_SOCKET_PATH') or os.environ.get('HERDR_SOCKET_PATH','')).resolve()
assert str(endpoint).startswith('/private/tmp/parity-'), 'not an observer-owned socket'
root=next(parent for parent in endpoint.parents if parent.name.startswith('parity-'))
operation=sys.argv[1]
backup=root/'home/footer-catalog-backup.json'

def request(method,params):
    with socket.socket(socket.AF_UNIX) as sock:
        sock.settimeout(15);sock.connect(str(endpoint))
        sock.sendall(json.dumps({'id':'footer-test','method':method,'params':params}).encode()+b'\n')
        data=bytearray()
        while b'\n' not in data:
            chunk=sock.recv(65536)
            if not chunk:raise RuntimeError('API closed')
            data.extend(chunk)
    result=json.loads(data.split(b'\n')[0])
    if 'error' in result:raise RuntimeError(result)
    return result

if operation=='connection-notice':
    absent = subprocess.run(['id','-u','footer-ui-absent'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    assert absent.returncode != 0, 'fixture account unexpectedly exists'
    candidates=list(root.rglob('machines.json'))
    assert len(candidates)<=1,candidates
    path=candidates[0] if candidates else endpoint.parent/'machines.json'
    backup.write_text(json.dumps({'path':str(path),'text':path.read_text() if path.exists() else None}))
    path.write_text(json.dumps([{'id':'footer-notice','label':'Footer offline','target':'footer-ui-absent@127.0.0.1','session':'parity','enabled':True}]))
elif operation=='notice':
    candidates=list(root.rglob('machines.json'))
    assert len(candidates)==1, candidates
    path=candidates[0]
    backup.write_text(json.dumps({'path':str(path),'text':path.read_text()}))
    path.write_text('[')
elif operation=='restore':
    saved=json.loads(backup.read_text());path=Path(saved['path'])
    assert path.is_relative_to(root)
    if saved['text'] is None:
        path.unlink()
    else:
        path.write_text(saved['text'])
elif operation=='handoff':
    if os.environ.get('IKESU_SOCKET_PATH'):
        before=request('ping',{})
        result=request('server.live_handoff',{'import_exe':sys.argv[2],'expected_version':'0.1.0-footer-test'})
        after=request('ping',{})
        (root/'home/footer-handoff.json').write_text(json.dumps({'before':before,'handoff':result,'after':after},indent=2)+'\n')
        assert after['result']['version']=='0.1.0-footer-test',after
else:raise ValueError(operation)
