#!/usr/bin/env python3
"""Real shell/full-screen workload with monotonic PTY and frame sequence logs."""
import argparse,json,os,signal,subprocess,time,base64
p=argparse.ArgumentParser();p.add_argument('--id',type=int,required=True);p.add_argument('--out',required=True);p.add_argument('--mode',choices=['light','heavy','alt','image','image-update'],required=True);p.add_argument('--history',type=int,default=0);a=p.parse_args()
clock=lambda:time.clock_gettime_ns(time.CLOCK_MONOTONIC)
log=open(a.out,'a',buffering=1);seq=0;child=None
# Real output creates history through the terminal, never by mutating storage.
w,h=os.get_terminal_size();history_bytes=0
for i in range(a.history):
 data=(f'H{i:08d} '+('h'*w)+'\r\n').encode();history_bytes+=os.write(1,data)
log.write(json.dumps(dict(event='history_written',clock_ns=clock(),id=a.id,rows=a.history,output_bytes=history_bytes))+'\n')
if a.mode=='alt':os.write(1,b'\x1b[?1049h')
def draw(receipt=False):
 global seq
 at=clock();w,h=os.get_terminal_size();seq+=1
 log.write(json.dumps(dict(event='pty_size' if receipt else 'initial',clock_ns=at,id=a.id,seq=seq,width=w,height=h))+'\n')
 if a.mode!='light':os.write(1,('\x1b[2J'+''.join(f'\x1b[{y+1};1H'+str(y%10)*w for y in range(h))).encode())
 if not(0<a.id<16 and w<256 and h<256 and seq<65536):raise RuntimeError('unsupported marker dimensions/sequence')
 marker=f'@{a.id:x}{seq:04x}{w:02x}{h:02x}!'
 # Position every glyph explicitly, including across a very narrow pane.
 data='\x1b7'+''.join(f'\x1b[{i//w+1};{i%w+1}H{c}' for i,c in enumerate(marker) if i//w<h)+'\x1b8'
 sent_at=clock();os.write(1,data.encode())
 if a.mode in ('image','image-update'):
  rgb=bytes([seq%256 if a.mode=='image-update' else 64,128,192])*16
  image=f'\x1b_Ga=T,f=24,s=4,v=4,i={a.id},q=2,c=2,r=2,C=1;'+base64.b64encode(rgb).decode()+'\x1b\\'
  os.write(1,(f'\x1b[{max(1,h-1)};{max(1,w-1)}H'+image).encode())
 log.write(json.dumps(dict(event='app_written',clock_ns=sent_at,id=a.id,seq=seq,width=w,height=h,bytes=len(data)))+'\n')
signal.signal(signal.SIGWINCH,lambda *_:draw(True));draw()
if a.mode=='light':child=subprocess.Popen(['/bin/zsh','-f','+m'],env=dict(os.environ,PS1='LIGHT> '))
if child is not None:
 time.sleep(.1);log.write(json.dumps(dict(event='shell_group',clock_ns=clock(),monitor_group=os.getpgrp(),shell_group=os.getpgid(child.pid),foreground_group=os.tcgetpgrp(0)))+'\n')
try:
 while child is None or child.poll() is None:
  if a.mode=='image-update':time.sleep(1/30);draw()
  else:signal.pause()
finally:
 log.close()
