#!/usr/bin/env python3
"""Probe notification.show without attaching a client to an isolated real server."""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time

p=argparse.ArgumentParser();p.add_argument('binary');p.add_argument('product',choices=['herdr','gtx']);p.add_argument('out',type=Path);a=p.parse_args()
a.out.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory(prefix='parity-',dir='/tmp') as tmp:
    root=Path(tmp)
    env={k:v for k,v in os.environ.items() if not k.startswith(('HERDR','GTX','CODEX','CLAUDE'))}
    env.update(HOME=str(root),XDG_CONFIG_HOME=str(root/'config'),XDG_STATE_HOME=str(root/'state'),XDG_DATA_HOME=str(root/'data'),SHELL='/bin/sh',GTX_DISABLE_SOUND='1')
    config=root/'config.toml';config.write_text(f'[ui.toast]\ndelivery="{a.product}"\n[ui.sound]\nenabled=false\n')
    env['HERDR_CONFIG_PATH' if a.product=='herdr' else 'GTX_CONFIG']=str(config)
    with (a.out/'server.log').open('w') as log:
        child=subprocess.Popen([a.binary,'--session','parity','server'],env=env,cwd=root,stdout=log,stderr=log)
        try:
            end=time.monotonic()+15
            while True:
                paths=list(root.rglob(a.product+'.sock'))
                if paths: break
                if child.poll() is not None or time.monotonic()>end: raise RuntimeError('server did not create isolated socket')
                time.sleep(.02)
            def call(method,params):
                with socket.socket(socket.AF_UNIX) as c:
                    c.settimeout(5);c.connect(str(paths[0]));c.sendall(json.dumps({'id':'headless','method':method,'params':params}).encode()+b'\n')
                    return json.loads(c.makefile('rb').readline())
            result=call('notification.show',{'title':'日本語','sound':'none'})
            (a.out/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
            stop=call('server.stop',{});child.wait(timeout=10)
            (a.out/'cleanup.json').write_text(json.dumps({'stop':stop,'exit':child.returncode})+'\n')
        finally:
            if child.poll() is None: child.kill();child.wait();raise RuntimeError('server required kill')
