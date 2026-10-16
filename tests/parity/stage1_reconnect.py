#!/usr/bin/env python3
"""Observe a real live handoff while its display is stopped and pane modes change."""
import json
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import threading
import time
import tty


class Reconnect:
    def __init__(self, launcher, env, target, mode):
        self.launcher, self.env = launcher, env
        self.target, self.mode = Path(target), mode
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self.run)
        self.error = None

    def start(self):
        self.thread.start()

    def wait(self, name):
        deadline = time.monotonic()+20
        while not (self.target/name).exists():
            if self.stop.wait(.01) or time.monotonic() > deadline:
                raise RuntimeError('fixture did not reach '+name)

    def run(self):
        history = []
        display = None
        try:
            self.wait('pane-ready')
            self.wait('display-pid')
            display = int((self.target/'display-pid').read_text())
            os.kill(display, signal.SIGSTOP)
            history.append({'phase':'display-stopped', 'pid':display, 'ns':time.monotonic_ns()})
            result = subprocess.run(self.launcher+['--session', 'parity', 'server', 'live-handoff', '--import-exe', self.launcher[-1]],
                                    env=self.env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
            (self.target/'handoff.log').write_bytes(result.stdout)
            if result.returncode:
                raise RuntimeError('real server handoff failed: '+str(result.returncode))
            history.append({'phase':'disconnected', 'ns':time.monotonic_ns()})
            (self.target/'pane-mode').write_text(self.mode)
            self.wait('pane-changed')
            history.append({'phase':'pane-changed', 'ns':time.monotonic_ns()})
            os.kill(display, signal.SIGCONT)
            display = None
            history.append({'phase':'display-resumed', 'ns':time.monotonic_ns()})
            (self.target/'reconnected').write_text('resumed')
        except Exception as error:
            self.error = str(error)
        finally:
            if display is not None:
                os.kill(display, signal.SIGCONT)
            (self.target/'reconnect.json').write_text(json.dumps({'mode':self.mode, 'history':history, 'error':self.error}, indent=2)+'\n')

    def close(self):
        self.stop.set()
        self.thread.join(timeout=22)


if __name__ == '__main__':
    target = Path(sys.argv[1])
    tty.setraw(0)
    os.write(1, b'\x1b[>31u\r\nRECONNECT_READY\r\n')
    time.sleep(.3)
    (target/'pane-ready').write_text(str(os.getpid()))
    while not (target/'pane-mode').exists():
        time.sleep(.01)
    mode = (target/'pane-mode').read_text()
    if mode == 'off':
        os.write(1, b'\x1b[<u')
    os.write(1, b'\r\nRECONNECT_CHANGED\r\n')
    time.sleep(.3)
    (target/'pane-changed').write_text(str(os.getpid()))
    received = bytearray()
    last_input = None
    with (target/'received.bin').open('ab', buffering=0) as output:
        while True:
            if not select.select([0], [], [], .05)[0]:
                if last_input is not None and time.monotonic()-last_input > .1 and not (target/'received-stable.bin').exists():
                    (target/'received-stable.bin').write_bytes(received)
                continue
            data = os.read(0, 65536)
            if not data:
                break
            output.write(data)
            received.extend(data)
            last_input = time.monotonic()
