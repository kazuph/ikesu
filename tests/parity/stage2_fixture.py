#!/usr/bin/env python3
"""Create real workspaces only on observe.py's disposable socket."""
import json,os,socket,sys
from pathlib import Path
endpoint=os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('IKESU_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
 raise SystemExit('REFUSED: not an owned parity socket')
proof=[]
def request(method,params):
 with socket.socket(socket.AF_UNIX) as s:
  s.settimeout(5);s.connect(endpoint);s.sendall(json.dumps(dict(id='stage2',method=method,params=params)).encode()+b'\n');data=b''
  while b'\n' not in data:data+=s.recv(65536)
 r=json.loads(data.split(b'\n')[0]);proof.append(dict(method=method,params=params,response=r))
 if 'error' in r:raise RuntimeError(r)
 return r['result']
for name in ([f'indexed-{i}' for i in range(2,10)] if len(sys.argv)>1 and sys.argv[1]=='indexed' else ['beta','gamma']):
 r=request('workspace.create',dict(label=name,cwd=os.getcwd(),focus=False))
 if name=='gamma':request('workspace.set_section',dict(workspace_id=r['workspace']['workspace_id'],section='favorite'))
request('workspace.list',{})
(Path.home()/'stage2-fixture.json').write_text(json.dumps(proof,indent=2))
print('STAGE2_FIXTURE_READY')
