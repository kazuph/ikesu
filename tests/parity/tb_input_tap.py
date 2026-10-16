#!/usr/bin/env python3
"""Run the real tb on a PTY and record bytes actually delivered to its input."""
import argparse
import fcntl
import os
from pathlib import Path
import select
import signal
import sys
import termios
import tty

p = argparse.ArgumentParser()
p.add_argument('--out', type=Path, required=True)
p.add_argument('command', nargs=argparse.REMAINDER)
a = p.parse_args()
root = Path.home().resolve()
if (not str(root).startswith('/Users/kazuph/bench-terrace.noindex/work/tb-optimize/r-')
        or root.name != 'home' or a.out.resolve().parent != root):
    raise SystemExit('REFUSED: input tap is outside the owned saved-machine pane')
command = a.command[1:] if a.command[:1] == ['--'] else a.command
if not command:
    p.error('the actual terminal-browser command is required')
size = fcntl.ioctl(0, termios.TIOCGWINSZ, bytes(8))
original = termios.tcgetattr(0)
tty.setraw(0)
child, master = os.forkpty()
if child == 0:
    fcntl.ioctl(0, termios.TIOCSWINSZ, size)
    os.execvp(command[0], command)


def resize(*_):
    fcntl.ioctl(master, termios.TIOCSWINSZ, fcntl.ioctl(0, termios.TIOCGWINSZ, bytes(8)))


def stop(signum, _):
    try:
        os.killpg(child, signum)
    except ProcessLookupError:
        pass


def write_all(fd, data, log=None):
    remaining = memoryview(data)
    while remaining:
        count = os.write(fd, remaining)
        if log is not None:
            log.write(remaining[:count])
        remaining = remaining[count:]


for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
    signal.signal(sig, stop)
signal.signal(signal.SIGWINCH, resize)
try:
    with a.out.open('wb', buffering=0) as log:
        while True:
            ready, _, _ = select.select([0, master], [], [])
            if master in ready:
                try:
                    output = os.read(master, 65536)
                except OSError:
                    break
                if not output:
                    break
                write_all(1, output)
            if 0 in ready:
                incoming = os.read(0, 65536)
                if not incoming:
                    break
                write_all(master, incoming, log)
finally:
    stop(signal.SIGHUP, None)
    os.close(master)
    _, status = os.waitpid(child, 0)
    termios.tcsetattr(0, termios.TCSANOW, original)
sys.exit(os.waitstatus_to_exitcode(status))
