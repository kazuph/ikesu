#!/usr/bin/env python3
"""Probe only an observer-owned --no-session application's real API, if exposed."""
import json
import os
from pathlib import Path
import socket
import time

home=Path.home();root=home.parent
if not str(root.resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not observer-owned HOME')
paths=[p for p in root.rglob('*.sock') if p.name in ('herdr.sock','ikesu.sock')]
proof={'environment':{k:os.environ[k] for k in ['TERM','TERM_PROGRAM','KITTY_WINDOW_ID','TMUX'] if k in os.environ},'sockets':[str(p) for p in paths],'replies':[]}
if paths:
    for i in range(3):
        if i==2:time.sleep(1)
        with socket.socket(socket.AF_UNIX) as c:
            c.settimeout(5);c.connect(str(paths[0]));c.sendall(json.dumps({'id':'standalone','method':'notification.show','params':{'title':'日本語','body':'本文','sound':'none'}}).encode()+b'\n')
            proof['replies'].append(json.loads(c.makefile('rb').readline()))
else:
    proof['unsupported']='standalone application exposes no API socket'
(home/'standalone-notification.json').write_text(json.dumps(proof,ensure_ascii=False,indent=2)+'\n')
print('STANDALONE_DONE',flush=True)
