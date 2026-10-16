"""Real DOM events posted by a real page to an observer-owned local server."""
from contextlib import closing
import json
import os
from pathlib import Path
import re
import select
import shlex
import signal
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PAGE = '''<!doctype html><meta charset="utf-8"><title>TB_POINTER_LOADING</title>
<style>body{margin:0;width:200vw;height:200vh}a{position:fixed;left:0;top:0;width:50vw;height:50vh;background:#ddd}a:hover{background:#acf}</style>
<a href="#target">Real link hover and horizontal scrolling</a>
<script>
const link=document.querySelector('a');
const state={hover:false,x:null,y:null,wheelX:0,scrollX:0,moves:0};
let pending=Promise.resolve();
function publish(){state.hover=link.matches(':hover');state.scrollX=window.scrollX;const body=JSON.stringify(state);document.title='TB_POINTER:'+body;pending=pending.then(()=>fetch('/state',{method:'POST',body}))}
document.addEventListener('pointermove',e=>{state.x=e.clientX;state.y=e.clientY;state.moves++;publish()});
document.addEventListener('wheel',e=>{state.wheelX=e.deltaX;publish()},{passive:true});
window.addEventListener('scroll',publish);publish();
</script>'''


def wait_ready(ssh, remote):
    deadline = time.monotonic()+30
    while True:
        try:
            response = ssh(shlex.join(['cat', remote+'/home/pointer-state.json']),
                           capture_output=True, text=True, timeout=10)
            state = json.loads(response.stdout)
            if state['moves'] == 0 and state['x'] is None:
                return
            raise RuntimeError('pointer page already received input before readiness')
        except subprocess.CalledProcessError:
            if time.monotonic() >= deadline:
                raise RuntimeError('pointer page did not install its DOM listeners')


def stages(pixel):
    steps = [('hover', 'mouse hover 80 22'), ('leave', 'mouse hover 140 40'),
             ('right', 'mouse wheel-right 80 22'), ('left', 'mouse wheel-left 80 22')]
    if pixel:
        # Both points lie in the same 8x16 outer cell; their 4px difference
        # must survive the saved-machine UI translation and browser input.
        steps += [('fine-a', 'pixel hover 322 165'), ('fine-b', 'pixel hover 326 169')]
    return steps


def procedure(root, pixel):
    return ''.join(('waitfile '+str(root/'pointer-pixel-mode-ready')+' 10s\n' if stage == 'fine-a' else '')+
                   command+'\ncells '+str(root/('pointer-'+stage+'.json'))+
                   '\nwaitfile '+str(root/('pointer-'+stage+'-ready'))+' 10s\n'
                   for stage, command in stages(pixel))


def collect(ssh, remote, root, out, viewer, pixel):
    records = []
    with closing(select.kqueue()) as events:
        fd = os.open(root, os.O_RDONLY)
        try:
            events.control([select.kevent(fd, filter=select.KQ_FILTER_VNODE, flags=select.KQ_EV_ADD,
                                          fflags=select.KQ_NOTE_WRITE)], 0, 0)
            for stage, _ in stages(pixel):
                deadline = time.monotonic()+10
                while not (root/('pointer-'+stage+'.json')).exists():
                    if viewer.poll() is not None or time.monotonic() >= deadline:
                        raise RuntimeError('pointer stage was not reached: '+stage)
                    events.control(None, 1, max(0, deadline-time.monotonic()))
                while True:
                    response = ssh(shlex.join(['cat', remote+'/home/pointer-state.json']),
                                   capture_output=True, text=True, timeout=10)
                    state = json.loads(response.stdout)
                    (out/('pointer-'+stage+'-state.json')).write_text(json.dumps(state, indent=2)+'\n')
                    if stage == 'fine-a':
                        matched = state['moves'] > records[-1]['state']['moves']
                    elif stage == 'fine-b':
                        previous = records[-1]['state']
                        matched = state['x']-previous['x'] == 4 and state['y']-previous['y'] == 4
                    else:
                        matched = {'hover': state['hover'] and state['moves'] > 0,
                                   'leave': not state['hover'] and state['moves'] > 0,
                                   'right': state['wheelX'] > 0 and state['scrollX'] > 0,
                                   'left': state['wheelX'] < 0 and state['scrollX'] == 0}[stage]
                    if matched:
                        records.append({'stage': stage, 'state': state})
                        break
                    if time.monotonic() >= deadline:
                        raise RuntimeError('real browser DOM did not satisfy pointer stage: '+stage)
                (out/'pointer-dom.json').write_text(json.dumps(records, indent=2)+'\n')
                if stage == 'left' and pixel:
                    modes = re.findall(rb'\x1b\[\?1016([hl])', (out/'terminal.raw').read_bytes())
                    active = bool(modes and modes[-1] == b'h')
                    (out/'pointer-pixel-mode.json').write_text(json.dumps({'actual_outer_pixel_mode': active})+'\n')
                    if not active:
                        raise RuntimeError('unsupported fine-point observation: actual outer terminal is in cell mode despite pixel capability; no synthetic pixel input was sent')
                    (root/'pointer-pixel-mode-ready').write_text('actual outer pixel mode confirmed')
                (root/('pointer-'+stage+'-ready')).write_text('real DOM state observed')
        finally:
            os.close(fd)


def serve(tb, root):
    root = Path(root)
    if not str(root).startswith('/Users/kazuph/bench-terrace.noindex/work/tb-optimize/r-'):
        raise RuntimeError('not an observer-owned environment')
    state_file = root/'home/pointer-state.json'

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path != '/':
                self.send_error(404)
                return
            body = PAGE.encode()
            self.send_response(200)
            self.send_header('Content-Type', 'text/html; charset=utf-8')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            if self.path != '/state':
                self.send_error(404)
                return
            state = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
            temporary = state_file.with_suffix('.tmp')
            temporary.write_text(json.dumps(state)+'\n')
            temporary.replace(state_file)
            self.send_response(204)
            self.end_headers()

        def log_message(self, *args):
            pass

    with ThreadingHTTPServer(('127.0.0.1', 0), Handler) as server:
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        browser = subprocess.Popen([tb+'/bin/terminal-browser', 'http://127.0.0.1:'+str(server.server_port)+'/',
                                    '--no-merge', '--no-toolbar', '--no-shortcuts', '--no-context-menu',
                                    '--no-overlays', '--no-frame'])

        def stop(signum, frame):
            browser.send_signal(signum)

        for sig in (signal.SIGHUP, signal.SIGINT, signal.SIGTERM):
            signal.signal(sig, stop)
        try:
            browser.wait()
        finally:
            server.shutdown()
            if browser.poll() is None:
                browser.terminate()
                browser.wait(timeout=10)


if __name__ == '__main__':
    serve(*sys.argv[1:])
