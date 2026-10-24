#!/usr/bin/env python3
"""Version-two real PTY measurement. Every run owns a private HOME/socket."""
import argparse,json,os,shlex,socket,subprocess,tempfile,time,hashlib,threading,sys
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--base',required=True);p.add_argument('--binary',required=True);p.add_argument('--product',choices=['ikesu','herdr'],required=True);p.add_argument('--scenario',required=True);p.add_argument('--out',required=True);a=p.parse_args()
base=Path(a.base).resolve();harness=Path(os.environ.get('DRAG_HARNESS',str(base/'source'))).resolve();observer_binary=Path(os.environ.get('DRAG_OBSERVER',str(base/'ptyrun-v2'))).resolve();binary=Path(a.binary).resolve();out=Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False);c=json.loads(Path(a.scenario).read_text());state=Path(tempfile.mkdtemp(prefix='drag2-',dir='/tmp'))
for n in ['home','config','state','data','zdot']:(state/n).mkdir()
(state/'zdot/.zshrc').write_text("PS1='LIGHT> '\nunset HISTFILE\nSAVEHIST=0\n")
env={k:v for k,v in os.environ.items() if not k.startswith(('IKESU','HERDR','GTX','CODEX','CLAUDE'))}
env.update(HOME=str(state/'home'),XDG_CONFIG_HOME=str(state/'config'),XDG_STATE_HOME=str(state/'state'),XDG_DATA_HOME=str(state/'data'),ZDOTDIR=str(state/'zdot'),SHELL='/bin/zsh',TERM='xterm-256color')
config=state/'config.toml';config.write_text('onboarding=false\n[ui]\nsidebar_width=26\nright_sidebar_width=26\n[ui.sound]\nenabled=false\n[experimental]\nkitty_graphics=true\n')
env.update({f'{a.product.upper()}_STATE_DIR':str(state/'state'),('IKESU_CONFIG' if a.product=='ikesu' else 'HERDR_CONFIG_PATH'):str(config)})
pending_hidden=[]
server=None;endpoint=None;serverlog=(out/'server.log').open('w');monitor_stop=threading.Event();display=None
clock=lambda:time.clock_gettime_ns(time.CLOCK_MONOTONIC)
def request(method,params):
 with socket.socket(socket.AF_UNIX) as sock:
  sock.settimeout(10);sock.connect(str(endpoint));sock.sendall(json.dumps(dict(id='drag2',method=method,params=params)).encode()+b'\n');data=b''
  while b'\n' not in data:
   b=sock.recv(65536)
   if not b:raise RuntimeError('isolated API disconnected')
   data+=b
  result=json.loads(data.split(b'\n')[0])
  if 'error' in result:raise RuntimeError(result)
  return result['result']
def monitor():
 with (out/'process-metrics.jsonl').open('w') as log:
  while not monitor_stop.is_set():
   roles={name:v.pid for name,v in [('server',server),('observer',display)] if v is not None and v.poll() is None}
   if (out/'product-process.json').exists():roles['display']=json.loads((out/'product-process.json').read_text())['pid']
   pids=[str(pid) for pid in roles.values()]
   if pids:
    result=subprocess.run(['ps','-o','pid=,ppid=,rss=,time=,%cpu=','-p',','.join(pids)],capture_output=True,text=True)
    log.write(json.dumps(dict(clock_ns=clock(),roles=roles,ps=result.stdout,load=os.getloadavg()))+'\n');log.flush()
   monitor_stop.wait(.25)
try:
 no_session=c.get('no_session',False)
 if not no_session:
  if a.product=='ikesu':env['IKESU_BENCH_STATS']=str(out/'server-runtime.jsonl')
  server=subprocess.Popen([str(binary),'--session','drag2','server'],env=env,cwd=out,stdout=serverlog,stderr=serverlog)
  deadline=time.monotonic()+15
  while endpoint is None:
   socks=list(state.rglob(a.product+'.sock'))
   if len(socks)==1:endpoint=socks[0];break
   if server.poll() is not None or time.monotonic()>deadline:raise RuntimeError('isolated server not ready')
   time.sleep(.025)
  w=request('workspace.create',dict(cwd=str(out),label='drag2',focus=True))
  for ws in request('workspace.list',{})['workspaces']:
   if ws['workspace_id']!=w['workspace']['workspace_id']:request('workspace.close',dict(workspace_id=ws['workspace_id']))
  root=w['root_pane']['pane_id'];direction='down' if c['kind']=='horizontal' else 'right'
  first=request('pane.split',dict(target_pane_id=root,direction=direction,focus=False,cwd=str(out)))
  while len(request('pane.list',dict(workspace_id=w['workspace']['workspace_id']))['panes'])<c.get('panes',2):
   panes=request('pane.list',dict(workspace_id=w['workspace']['workspace_id']))['panes'];target=panes[-1]['pane_id'];request('pane.split',dict(target_pane_id=target,direction='down' if len(panes)%2==0 else 'right',focus=False,cwd=str(out)))
  if c.get('invisible'):
   hidden=[]
   for method,params in [('tab.create',dict(workspace_id=w['workspace']['workspace_id'],cwd=str(out),label='hidden-tab',focus=False)),('workspace.create',dict(cwd=str(out),label='hidden-space',focus=False))]:
    created=request(method,params);hidden.append(created);pid=created['root_pane']['pane_id']
    cmd=shlex.join(['python3',str(harness/'tests/dragresize/workload.py'),'--id',str(len(hidden)+8),'--out',str(out/f'hidden-{len(hidden)}.jsonl'),'--mode','heavy','--history',str(c.get('history_rows',0))])
    pending_hidden.append((pid,cmd))
   (out/'hidden-panes.json').write_text(json.dumps(hidden,indent=2))
  (out/'panes-before.json').write_text(json.dumps(request('pane.list',dict(workspace_id=w['workspace']['workspace_id'])),indent=2))
 kind='left' if a.product=='herdr' and c['kind']=='right' else c['kind']
 trial=dict(kind=kind,path=c.get('path','near'),count=c.get('count',200),rate=c.get('rate',4),warmup=c.get('warmup',10),extent=c.get('extent',1),release_different=c.get('release_different',False));(out/'trial.json').write_text(json.dumps(trial))
 commands=[]
 for i in range(c.get('panes',2)):
  commands.append(shlex.join(['python3',str(harness/'tests/dragresize/workload.py'),'--id',str(i+1),'--out',str(out/f'app-{i+1}.jsonl'),'--mode',c['mode'],'--history',str(c.get('history_rows',0))]))
 (out/'commands.json').write_text(json.dumps(commands))
 lines=['sleep 2s']
 if a.product=='ikesu':
  # The right-side target is compared with the reference left side using the
  # same pane width, retaining exactly one expanded sidebar on each product.
  key='b' if c['kind']=='right' else 'B'
  lines+=['send '+json.dumps('\x02'+key),'sleep 200ms','send '+json.dumps('\x02'+key),'sleep 300ms']
 if no_session:lines+=['send '+json.dumps('\x02'+('-' if c['kind']=='horizontal' else 'v')),'sleep 1s']
 if no_session:lines+=['panecommands '+str(out/'commands.json')]
 else:lines+=['notifyfile '+str(out/'geometry-ready'),'waitfile '+str(out/'commands-ready')+' 30s']
 lines+=['sleep 4s',f'waitmarkers {c.get("panes",2)} 15s','notifyfile '+str(out/'prepared'),'waitfile '+str(out/'prepared-ack')+' 30s','screen '+str(out/'before.txt'),'dragtrial '+str(out/'trial.json'),'screen '+str(out/'after.txt')]
 (out/'steps').write_text('\n'.join(lines)+'\n')
 (out/'metadata.json').write_text(json.dumps(dict(scenario=c,trial=trial,product=a.product,binary=str(binary),binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),version=subprocess.check_output([str(binary),'--version'],text=True).strip(),state=str(state),cols=c['cols'],rows=c['rows'],cell_pixels=[8,16],clock='CLOCK_MONOTONIC',gc_env={k:env.get(k) for k in ['GOGC','GOMEMLIMIT']},proxy_right=a.product=='herdr' and c['kind']=='right',config=config.read_text(),python=sys.version,clock_resolution_seconds=time.clock_getres(time.CLOCK_MONOTONIC),observer_sha256=hashlib.sha256(observer_binary.read_bytes()).hexdigest(),workload_sha256=hashlib.sha256((harness/'tests/dragresize/workload.py').read_bytes()).hexdigest(),image_pixels=[4,4] if c['mode'].startswith('image') else None),indent=2))
 if a.product=='ikesu':env['IKESU_BENCH_STATS']=str(out/'client-runtime.jsonl')
 launch=[str(binary),'--no-session'] if no_session else [str(binary),'--session','drag2']
 # Redirect only the product's diagnostics, so runtime sampling never prints
 # over the measured screen. The exec preserves the display PID.
 launcher=['/bin/sh','-c','exec "$@" 2>"'+str(out/'client.stderr')+'"','drag2',*launch]
 with (out/'observer.log').open('w') as log:
  display=subprocess.Popen([str(observer_binary),'-process-file',str(out/'product-process.json'),'-timeline',str(out/'timeline.jsonl'),'-pixel-mouse=false','-cell-width','8','-cell-height','16','-xpixel',str(c['cols']*8),'-ypixel',str(c['rows']*16),'-cols',str(c['cols']),'-rows',str(c['rows']),'-file-images',str(out/'image-payloads.jsonl'),'-raw-output',str(out/'outer.bin'),'-steps',str(out/'steps'),'--',*launcher],env=env,cwd=out,stdout=log,stderr=log)
  monitor_thread=threading.Thread(target=monitor);monitor_thread.start()
  if endpoint:
   deadline=time.monotonic()+30
   while not (out/'geometry-ready').exists():
    if display.poll() is not None or time.monotonic()>deadline:raise RuntimeError('client geometry did not become ready')
    time.sleep(.025)
   target_tab=w['root_pane']['tab_id']
   visible=[pane for pane in request('pane.list',dict(workspace_id=w['workspace']['workspace_id']))['panes'] if pane['tab_id']==target_tab]
   if len(visible)!=len(commands):raise RuntimeError('wrong initial visible pane count')
   for pid,cmd in [(pane['pane_id'],cmd) for pane,cmd in zip(visible,commands)]+pending_hidden:
    request('pane.send_text',dict(pane_id=pid,text=cmd));request('pane.send_keys',dict(pane_id=pid,keys=['Enter']))
   (out/'commands-ready').write_text('ready\n')
  deadline=time.monotonic()+30
  while not (out/'prepared').exists():
   if display.poll() is not None or time.monotonic()>deadline:raise RuntimeError('workload preparation did not finish')
   time.sleep(.025)
  history=[]
  if endpoint:
   panes=request('pane.list',dict(workspace_id=w['workspace']['workspace_id']))['panes']
   for pane in panes:
    read=request('pane.read',dict(pane_id=pane['pane_id'],source='recent',lines=10000,format='text',strip_ansi=True))['read']
    history.append(dict(pane=pane,read_rows=len(read['text'].splitlines()),read_utf8_bytes=len(read['text'].encode()),truncated=read['truncated']))
  (out/'history-before.json').write_text(json.dumps(history,indent=2))
  (out/'prepared-ack').write_text('ready\n')
  code=display.wait();monitor_stop.set();monitor_thread.join()
  if code:raise RuntimeError(f'observer exit {code}')
 if endpoint:(out/'panes-after.json').write_text(json.dumps(request('pane.list',dict(workspace_id=w['workspace']['workspace_id'])),indent=2))
finally:
 monitor_stop.set()
 if display is not None and display.poll() is None:
  matches=subprocess.run(['pgrep','-u',str(os.getuid()),'-f',str(observer_binary)],capture_output=True,text=True)
  if str(display.pid) in matches.stdout.split():os.kill(display.pid,15);display.wait(timeout=5)
  else:raise RuntimeError('cannot prove ownership of live observer')
 if server is not None and server.poll() is None:request('server.stop',{});server.wait(timeout=10)
 (out/'cleanup.json').write_text(json.dumps(dict(server_exit=None if server is None else server.poll(),observer_exit=None if display is None else display.poll(),state=str(state))))
