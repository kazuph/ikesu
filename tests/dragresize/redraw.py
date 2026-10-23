#!/usr/bin/env python3
"""Real resize-driven full-screen terminal workload, no product stubs."""
import os,signal,sys,time
log=sys.argv[1]
if len(sys.argv)>2 and sys.argv[2]=='alt':os.write(1,b'\x1b[?1049h')
def draw(*args):
 size=os.get_terminal_size()
 with open(log,'a') as f: f.write(f'{time.time_ns()} {size.columns} {size.lines}\n')
 os.write(1, ('\x1b[2J\x1b[H'+''.join(f'\x1b[{y+1};1H'+str(y%10)*size.columns for y in range(size.lines))).encode())
signal.signal(signal.SIGWINCH,draw); draw()
while True: signal.pause()
