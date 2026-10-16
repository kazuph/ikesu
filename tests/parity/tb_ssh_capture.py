#!/usr/bin/python3
"""Transparent byte capture for the observer's own mini SSH connection."""
import os
import errno
import json
from contextlib import closing
from pathlib import Path
import signal
import select
import subprocess
import sys
import threading
import time

prefix = os.environ['TB_SSH_OBSERVER_PREFIX']
root = Path(os.environ['TB_SSH_CAPTURE_ROOT'])
if not prefix.startswith('export HOME=/Users/kazuph/bench-terrace.noindex/work/tb-optimize/r-'):
    raise SystemExit('REFUSED: not an observer-owned remote environment')
root.mkdir(parents=True, exist_ok=True)
(root/(str(os.getpid())+'-observer.json')).write_text(json.dumps({
    'hold_marker': os.environ.get('TB_SSH_HOLD_MARKER'),
    'release_marker': os.environ.get('TB_SSH_RELEASE_MARKER'),
    'abort_marker': os.environ.get('TB_SSH_ABORT_MARKER'),
})+'\n')
args = sys.argv[1:]
index = args.index('mini')
args[index+1:] = [prefix+' '.join(args[index+1:])]
child = subprocess.Popen(['/usr/bin/ssh', *args], stdin=subprocess.PIPE, stdout=subprocess.PIPE, start_new_session=True)


def stop(signum, frame):
    try:
        os.killpg(child.pid, signum)
    except ProcessLookupError:
        pass


for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
    signal.signal(sig, stop)


def forward(source, target, name):
    stem = root/(str(os.getpid())+'-'+name)
    with stem.with_suffix('.bin').open('wb', buffering=0) as log, stem.with_suffix('.jsonl').open('w', buffering=1) as clock:
        offset = 0
        try:
            while True:
                data = os.read(source, 65536)
                if not data:
                    break
                received_at = time.time_ns()
                log.write(data)
                offset += len(data)
                clock.write(json.dumps({'end': offset, 'time_unix_ns': received_at})+'\n')
                held = False
                if name == 'from-mini' and os.environ.get('TB_SSH_HOLD_MARKER'):
                    marker = Path(os.environ['TB_SSH_HOLD_MARKER'])
                    release = Path(os.environ['TB_SSH_RELEASE_MARKER'])
                    if marker.exists() and not release.exists():
                        held = True
                        with closing(select.kqueue()) as events:
                            fd = os.open(release.parent, os.O_RDONLY)
                            try:
                                events.control([select.kevent(fd, filter=select.KQ_FILTER_VNODE,
                                                              flags=select.KQ_EV_ADD, fflags=select.KQ_NOTE_WRITE)], 0, 0)
                                (root/'transport-held.json').write_text(json.dumps({'pid': os.getpid(), 'bytes': len(data), 'time_unix_ns': time.time_ns()})+'\n')
                                deadline = time.monotonic()+10
                                while not release.exists():
                                    if time.monotonic() >= deadline:
                                        raise RuntimeError('observer did not release the real SSH output')
                                    events.control(None, 1, max(0, deadline-time.monotonic()))
                            finally:
                                os.close(fd)
                        abort = os.environ.get('TB_SSH_ABORT_MARKER')
                        if abort and Path(abort).exists():
                            stop(signal.SIGTERM, None)
                            (root/'transport-disconnected.json').write_text(json.dumps({'pid': os.getpid(), 'time_unix_ns': time.time_ns()})+'\n')
                            return
                remaining = memoryview(data)
                while remaining:
                    remaining = remaining[os.write(target, remaining):]
                if held:
                    (root/'transport-resumed.json').write_text(json.dumps({'pid': os.getpid(), 'bytes': len(data), 'time_unix_ns': time.time_ns()})+'\n')
        except OSError as error:
            (root/(str(os.getpid())+'-'+name+'-error.json')).write_text(json.dumps({'errno': error.errno, 'error': str(error)})+'\n')
            if error.errno != errno.EPIPE:
                raise


def input_stream():
    forward(0, child.stdin.fileno(), 'to-mini')
    child.stdin.close()


threading.Thread(target=input_stream, daemon=True).start()
try:
    forward(child.stdout.fileno(), 1, 'from-mini')
finally:
    try:
        child.wait(timeout=5)
    except subprocess.TimeoutExpired:
        stop(signal.SIGKILL, None)
        child.wait(timeout=5)
sys.exit(child.returncode)
