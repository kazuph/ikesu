#!/usr/bin/env python3
"""Isolated real-product drag observation on the measurement host."""
import os,sys,json,socket,subprocess,time,tempfile
from pathlib import Path
base=Path(sys.argv[1]).resolve(); product=sys.argv[2]; mode=sys.argv[3]; case=sys.argv[4]
if product=='herdr' and case=='right':
 raise SystemExit('UNSUPPORTED: herdr 0.2.66 has no right sidebar; no substitute is measured')
profiling=os.environ.get('DRAG_PROFILE')=='1'
root=base/(f'{product}-{mode}-{case}'+('-profile' if profiling else '')); root.mkdir(parents=True,exist_ok=True)
binary=base/'ikesu' if product=='ikesu' else Path.home()/'.local/share/herdr-builds/0.2.66/herdr'
env={k:v for k,v in os.environ.items() if not k.startswith(('IKESU','HERDR','CODEX','CLAUDE'))}
for n in ('home','config','state','data'): (root/n).mkdir(exist_ok=True)
cfg=root/'config.toml'; cfg.write_text('onboarding=false\n[ui]\nsidebar_width=26\nright_sidebar_width=26\n[ui.sound]\nenabled=false\n')
env.update(HOME=str(root/'home'),XDG_CONFIG_HOME=str(root/'config'),XDG_STATE_HOME=str(root/'state'),XDG_DATA_HOME=str(root/'data'),SHELL='/bin/zsh',TERM='xterm-256color')
state=Path(tempfile.mkdtemp(prefix='drag-',dir='/tmp')); (root/'state-path.txt').write_text(str(state))
env.update({f'{product.upper()}_STATE_DIR':str(state),('IKESU_CONFIG' if product=='ikesu' else 'HERDR_CONFIG_PATH'):str(cfg)})
for n in ('home','config','state','data'): (state/n).mkdir()
env.update(HOME=str(state/'home'),XDG_CONFIG_HOME=str(state/'config'),XDG_STATE_HOME=str(state/'state'),XDG_DATA_HOME=str(state/'data'))
if profiling: env.update(IKESU_CPUPROFILE=str(root/'server.cpu'),IKESU_CPUPROFILE_SECS='12')
serverlog=(root/'server.log').open('w'); server=subprocess.Popen([str(binary),'--session','drag','server'],env=env,cwd=root,stdout=serverlog,stderr=serverlog)
def request(method,params):
 with socket.socket(socket.AF_UNIX) as s:
  s.settimeout(10); s.connect(str(endpoint)); s.sendall(json.dumps(dict(id='drag',method=method,params=params)).encode()+b'\n'); b=b''
  while b'\n' not in b: b+=s.recv(65536)
  r=json.loads(b.split(b'\n')[0]);
  if 'error' in r: raise RuntimeError(r)
  return r['result']
try:
 deadline=time.monotonic()+15
 while True:
  socks=list(state.rglob(f'{product}.sock'))
  if socks: endpoint=socks[0]; break
  if server.poll() is not None or time.monotonic()>deadline: raise RuntimeError('socket missing')
  time.sleep(.05)
 w=request('workspace.create',dict(cwd=str(root),label='drag',focus=True)); (root/'create.json').write_text(json.dumps(w,indent=2))
 for ws in request('workspace.list',{})['workspaces']:
  if ws['workspace_id']!=w['workspace']['workspace_id']: request('workspace.close',dict(workspace_id=ws['workspace_id']))
 pane=w['root_pane']['pane_id']
 split=request('pane.split',dict(pane_id=pane,direction='down' if case=='horizontal' else 'right',focus=False,cwd=str(root)))
 panes=request('pane.list',dict(workspace_id=w['workspace']['workspace_id']))['panes']; (root/'panes.json').write_text(json.dumps(panes,indent=2))
 for p in panes:
  log=root/(p['pane_id']+'.resize')
  if mode.startswith('heavy'):
   command=f'python3 {base}/source/tests/dragresize/redraw.py {log}'+(' alt' if mode=='heavy-alt' else '')
  else:
   command=f"function TRAPWINCH() {{ print -r -- \"$EPOCHREALTIME $COLUMNS $LINES\" >> {log}; }}; zmodload zsh/datetime; PS1='LIGHT> '; clear"
  request('pane.send_text',dict(pane_id=p['pane_id'],text=command)); request('pane.send_keys',dict(pane_id=p['pane_id'],keys=['Enter']))
 env['PARITY_OBSERVE_PID']=str(server.pid)
 steps=root/'steps'; steps.write_text(('sleep 1s\nsend "\\x02B"\nsleep 500ms\nsend "\\x02B"\nsleep 1500ms\n' if product=='ikesu' and case!='right' else 'sleep 3s\n')+'screen '+str(root/'before.txt')+'\nsleep 1s\n')
 (root/'version.txt').write_text(subprocess.check_output([str(binary),'--version'],text=True))
 if case!='pilot':
  x,y,dx,dy,wx,wy={'vertical':(0,10,1,0,0,2),'left':(26,10,1,0,27,2),'right':(135,10,-1,0,134,2),'horizontal':(50,0,0,1,27,0)}[case]
  steps.write_text(steps.read_text()+f'dragmeasure 40 {x} {y} {dx} {dy} {wx} {wy}\nscreen {root}/after.txt\n')
 if profiling: env['IKESU_CPUPROFILE']=str(root/'client.cpu')
 with (root/'run.log').open('w') as log:
  subprocess.run([str(base/'ptyrun'),'-cell-clock','-pixel-mouse=false','-cols','160','-rows','45','-raw-output',str(root/'outer.bin'),'-steps',str(steps),'--',str(binary),'--session','drag'],env=env,cwd=root,stdout=log,stderr=log,check=True)
finally:
 # Only the isolated product is addressed. No global process termination.
 if server.poll() is None:
  request('server.stop',{})
 server.wait(timeout=10)
 (root/'cleanup.json').write_text(json.dumps({'server_pid':server.pid,'exit':server.returncode,'state':str(state)}))
 (root/'load.txt').write_text(str(os.getloadavg()))
