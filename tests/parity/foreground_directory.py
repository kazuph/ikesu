"""Real foreground process that changes cwd while its parent shell stays put."""
import json
import os
from pathlib import Path
import socket

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: isolated runner socket required')
pane = os.environ.get('HERDR_PANE_ID') or os.environ.get('GTX_PANE_ID')
proof = []
for phase in ['alpha', 'beta']:
    os.chdir(Path.home()/phase)
    for method, params in [('pane.get', {'pane_id': pane}), ('workspace.list', {})]:
        with socket.socket(socket.AF_UNIX) as s:
            s.settimeout(5)
            s.connect(endpoint)
            s.sendall(json.dumps(dict(id='foreground-directory', method=method, params=params)).encode()+b'\n')
            data = b''
            while b'\n' not in data:
                data += s.recv(65536)
            response = json.loads(data.split(b'\n')[0])
            assert 'error' not in response, response
            proof.append(dict(phase=phase, method=method, response=response))
    (Path.home()/'foreground-directory.json').write_text(json.dumps(proof, indent=2))
    print('FOREGROUND_'+phase.upper(), flush=True)
    input()
