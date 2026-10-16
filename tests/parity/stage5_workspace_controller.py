#!/usr/bin/env python3
"""Close the last real workspace from outside its terminating process group."""
import json
from pathlib import Path
import socket
import threading
import time

class CloseWorkspace:
    def __init__(self, endpoint, target):
        self.endpoint, self.target = Path(endpoint), Path(target)
        if not str(self.endpoint.resolve()).startswith('/private/tmp/parity-'):
            raise RuntimeError('REFUSED: endpoint outside observation sandbox')
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self.run)
        self.error = None

    def request(self, method, params):
        with socket.socket(socket.AF_UNIX) as sock:
            sock.settimeout(5); sock.connect(str(self.endpoint))
            sock.sendall(json.dumps({'id':'stage5','method':method,'params':params}).encode()+b'\n')
            data = bytearray()
            while b'\n' not in data:
                chunk = sock.recv(65536)
                if not chunk: raise RuntimeError('API closed before response')
                data.extend(chunk)
        reply = json.loads(data.split(b'\n')[0])
        if 'error' in reply: raise RuntimeError(reply)
        return reply

    def start(self): self.thread.start()

    def run(self):
        proof = {}
        try:
            deadline = time.monotonic()+30
            while not (self.target/'close-ready').exists():
                if self.stop.wait(.02) or time.monotonic()>deadline:
                    raise RuntimeError('close trigger not reached')
            proof['before'] = self.request('workspace.list', {})
            workspaces = proof['before']['result']['workspaces']
            if len(workspaces)!=1: raise RuntimeError('expected one last workspace')
            proof['closed'] = self.request('workspace.close', {'workspace_id':workspaces[0]['workspace_id']})
            proof['after'] = self.request('workspace.list', {})
            current = proof['after']['result']['workspaces']
            proof['panes'] = self.request('pane.list', {'workspace_id':current[0]['workspace_id']}) if current else None
        except Exception as error:
            self.error = str(error); proof['error'] = self.error
        finally:
            (self.target/'close-proof.json').write_text(json.dumps(proof,indent=2)+'\n')
            (self.target/'close-finished').write_text('finished\n')

    def close(self):
        self.stop.set(); self.thread.join(10)
        if self.thread.is_alive() or self.error: raise RuntimeError(self.error or 'controller did not stop')
