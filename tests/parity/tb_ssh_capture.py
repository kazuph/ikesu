#!/usr/bin/python3
"""Transparent byte capture for the observer's own mini SSH connection."""
import os
from pathlib import Path
import signal
import subprocess
import sys
import threading

prefix = os.environ['TB_SSH_OBSERVER_PREFIX']
root = Path(os.environ['TB_SSH_CAPTURE_ROOT'])
if not prefix.startswith('export HOME=/Users/kazuph/bench-terrace.noindex/work/tb-optimize/r-'):
    raise SystemExit('REFUSED: not an observer-owned remote environment')
root.mkdir(parents=True, exist_ok=True)
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
    with (root/(str(os.getpid())+'-'+name+'.bin')).open('wb', buffering=0) as log:
        try:
            while True:
                data = os.read(source, 65536)
                if not data:
                    break
                log.write(data)
                remaining = memoryview(data)
                while remaining:
                    remaining = remaining[os.write(target, remaining):]
        except (BrokenPipeError, OSError):
            pass


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
