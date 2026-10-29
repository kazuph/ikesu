#!/usr/bin/env python3
"""Record unmodified real isolated TUI output for offline xterm.js replay."""
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import sys
import tempfile
import termios
import time

binary, output = map(lambda value:Path(value).resolve(),sys.argv[1:])
output.mkdir(parents=True,exist_ok=True)
records=[]
for theme in ["catppuccin","catppuccin-latte"]:
    root=Path(tempfile.mkdtemp(prefix="ikesu-scrollbar-pty-"))
    config=root/"config.toml"
    config.write_text(f'''onboarding=false
[theme]
name="{theme}"
[terminal]
default_shell="/bin/sh"
[experimental]
allow_nested=true
[ui]
window_title="plain"
pane_scrollbars="auto"
[ui.sound]
enabled=false
''')
    pid,master=pty.fork()
    if pid==0:
        env={key:value for key,value in os.environ.items() if not key.startswith(("IKESU_","HERDR_","GTX_"))}
        env.update(IKESU_CONFIG=str(config),IKESU_STATE_DIR=str(root/"state"),IKESU_DB_PATH=str(root/"jobs.db"),TERM="xterm-256color",SHELL="/bin/sh")
        os.chdir(root)
        os.execve(str(binary),[str(binary),"--no-session"],env)
    fcntl.ioctl(master,termios.TIOCSWINSZ,struct.pack("HHHH",40,140,0,0))
    transcript=bytearray()
    def pump(seconds):
        deadline=time.monotonic()+seconds
        while time.monotonic()<deadline:
            ready,_,_=select.select([master],[],[],max(0,deadline-time.monotonic()))
            if not ready:break
            data=os.read(master,65536)
            if not data:raise RuntimeError("isolated TUI exited before capture")
            transcript.extend(data)
    def send(data):os.write(master,data)
    def capture(state):
        path=output/(theme+"-"+state+".ansi")
        path.write_bytes(transcript)
        records.append(dict(theme=theme,state=state,path=str(path),cols=140,rows=40,recorded_at=time.monotonic()))
    try:
        pump(.5)
        send("i=0; while [ $i -lt 120 ]; do printf '│LINE-%03d  0123456789 abcdefghijklmnopqrstuvwxyz\\n' $i; i=$((i+1)); done; printf 'PTY-READY\\n'\n".encode())
        pump(.5)
        if b"LINE-119" not in transcript:raise RuntimeError("real output did not finish")
        capture("idle")
        send(b"\x1b[<64;40;10M")
        pump(.1)
        capture("scrolling")
        # The requested expiry is 1.5s; observe after the next periodic tick.
        pump(1.7)
        capture("expired")
        send(b"\x1b[<35;26;10M")
        pump(.1)
        capture("hover")
        send(b"\x1b[<35;40;10M")
        pump(.1)
        capture("no-hover")
        send(b"\x1b[<0;26;10M\x1b[<32;34;10M\x1b[<0;34;10m")
        pump(.1)
        capture("dragged")
        send(b"\x02[k")
        pump(.1)
        capture("copy")
        send(b"\x1b[<0;35;40M\x1b[<0;35;40m")
        pump(.1)
        capture("fullscreen")
    finally:
        os.kill(pid,signal.SIGTERM)
        os.waitpid(pid,0)
        os.close(master)
(output/"pty-captures.json").write_text(json.dumps(records,indent=2))
print(f"RECORDED={len(records)}")
