#!/usr/bin/env python3
"""Parent-authorized copied-session Claude display, never conversation input."""
import argparse,hashlib,json,os,re,shlex,shutil,socket,subprocess,tempfile,time,threading
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--base',required=True);p.add_argument('--binary',required=True);p.add_argument('--observer',required=True);p.add_argument('--source-session',required=True);p.add_argument('--product',choices=['ikesu','herdr'],required=True);p.add_argument('--out',required=True);p.add_argument('--cols',type=int,default=160);p.add_argument('--rows',type=int,default=45);p.add_argument('--kind',choices=['vertical','horizontal','left','right'],default='vertical');p.add_argument('--rate',type=int,default=60);p.add_argument('--count',type=int,default=200);a=p.parse_args()
base=Path(a.base).resolve();binary=Path(a.binary).resolve();observer=Path(a.observer).resolve();out=Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False);home=Path.home();source=Path(a.source_session).resolve();session_id=source.stem
if not source.is_relative_to(home/'.claude/projects') or not re.fullmatch(r'[0-9a-f-]{36}',session_id):raise RuntimeError('source must be an existing conversation jsonl')
source_hash=hashlib.sha256(source.read_bytes()).hexdigest();source_stat=source.stat()
state=Path(tempfile.mkdtemp(prefix='drag2-claude-',dir='/tmp'));(state/'home').mkdir();(state/'state').mkdir();(state/'config').mkdir()
config=state/'config.toml';config.write_text('onboarding=false\n[ui]\nsidebar_width=26\nright_sidebar_width=26\n[ui.sound]\nenabled=false\n')
env={k:v for k,v in os.environ.items() if not k.startswith(('IKESU','HERDR','GTX','CLAUDE','CODEX'))};env.update(HOME=str(state/'home'),XDG_CONFIG_HOME=str(state/'config'),XDG_STATE_HOME=str(state/'state'),XDG_DATA_HOME=str(state/'data'),SHELL='/bin/zsh',TERM='xterm-256color');env.update({a.product.upper()+'_STATE_DIR':str(state/'state'),('IKESU_CONFIG' if a.product=='ikesu' else 'HERDR_CONFIG_PATH'):str(config)})
monitor_stop=threading.Event();monitor_thread=None
app_offsets={};app_pending={};app_latest={};app_started={}
server=None;display=None;endpoint=None;copies=[];workdirs=[];normal_exit=False
log=(out/'server.log').open('w')
def request(method,params):
 with socket.socket(socket.AF_UNIX) as sock:
  sock.settimeout(10);sock.connect(str(endpoint));sock.sendall(json.dumps(dict(id='claude-display',method=method,params=params)).encode()+b'\n');data=b''
  while b'\n' not in data:
   chunk=sock.recv(65536)
   if not chunk:raise RuntimeError('private API disconnected')
   data+=chunk
  result=json.loads(data.split(b'\n')[0])
  if 'error' in result:raise RuntimeError(result)
  return result['result']
def records(path):
 if not path.exists():return []
 raw=path.read_bytes();lines=raw.splitlines();result=[]
 if not raw.endswith(b'\n'):lines=lines[:-1]
 for line in lines:result.append(json.loads(line))
 return result
def update_app(index):
 path=out/f'claude-{index+1}.jsonl'
 if not path.exists():return
 with path.open('rb') as stream:
  stream.seek(app_offsets.get(index,0));raw=app_pending.get(index,b'')+stream.read();app_offsets[index]=stream.tell()
 lines=raw.split(b'\n');app_pending[index]=lines.pop()
 for line in lines:
  record=json.loads(line)
  if record['type']=='claude.started':app_started[index]=record
  elif record['type']=='claude.frame':app_latest[index]=record
def monitor():
 with (out/'process-metrics.jsonl').open('w') as metrics:
  while not monitor_stop.is_set():
   roles={name:process.pid for name,process in [('server',server),('observer',display)] if process is not None and process.poll() is None}
   if (out/'product-process.json').exists():roles['display']=json.loads((out/'product-process.json').read_text())['pid']
   for index in range(2):
    start=app_started.get(index)
    if start:roles[f'claude-{index+1}']=start['pid'];roles[f'relay-{index+1}']=start['relay_pid']
   if roles:
    row=subprocess.run(['ps','-o','pid=,ppid=,rss=,time=,%cpu=','-p',','.join(str(pid) for pid in roles.values())],capture_output=True,text=True)
    metrics.write(json.dumps(dict(clock_ns=time.clock_gettime_ns(time.CLOCK_MONOTONIC),roles=roles,ps=row.stdout,load=os.getloadavg()))+'\n');metrics.flush()
   monitor_stop.wait(.25)
try:
 if a.product=='ikesu':env['IKESU_BENCH_STATS']=str(out/'server-runtime.jsonl')
 server=subprocess.Popen([str(binary),'--session','claude-display','server'],env=env,cwd=out,stdout=log,stderr=log)
 deadline=time.monotonic()+15
 while endpoint is None:
  matches=list(state.rglob(a.product+'.sock'))
  if matches:endpoint=matches[0];break
  if server.poll() is not None or time.monotonic()>deadline:raise RuntimeError('private server not ready')
  time.sleep(.025)
 w=request('workspace.create',dict(cwd=str(out),label='copied Claude display',focus=True));pid=w['root_pane']['pane_id']
 for ws in request('workspace.list',{})['workspaces']:
  if ws['workspace_id']!=w['workspace']['workspace_id']:request('workspace.close',dict(workspace_id=ws['workspace_id']))
 request('pane.split',dict(target_pane_id=pid,direction='down' if a.kind=='horizontal' else 'right',cwd=str(out),focus=False))
 panes=request('pane.list',dict(workspace_id=w['workspace']['workspace_id']))['panes']
 kind='left' if a.product=='herdr' and a.kind=='right' else a.kind
 trial=dict(kind=kind,path='near',count=a.count,rate=a.rate,warmup=10,extent=1 if a.rate==4 else 3,release_different=False);(out/'trial.json').write_text(json.dumps(trial))
 steps=['sleep 2s']
 if a.product=='ikesu':
  key='b' if a.kind=='right' else 'B';steps+=['send '+json.dumps('\x02'+key),'sleep 200ms','send '+json.dumps('\x02'+key),'sleep 300ms']
 steps+=['notifyfile '+str(out/'geometry-ready'),'waitfile '+str(out/'claude-ready')+' 180s','screen '+str(out/'before.txt'),'dragtrial '+str(out/'trial.json'),'screen '+str(out/'after.txt'),'notifyfile '+str(out/'trial-finished'),'waitfile '+str(out/'closed')+' 30s']
 (out/'steps').write_text('\n'.join(steps)+'\n')
 if a.product=='ikesu':env['IKESU_BENCH_STATS']=str(out/'client-runtime.jsonl')
 with (out/'observer.log').open('w') as observer_log:
  display=subprocess.Popen([str(observer),'-timeline',str(out/'timeline.jsonl'),'-body-grids','-process-file',str(out/'product-process.json'),'-cols',str(a.cols),'-rows',str(a.rows),'-cell-width','8','-cell-height','16','-xpixel',str(a.cols*8),'-ypixel',str(a.rows*16),'-pixel-mouse=false','-raw-output',str(out/'outer.bin'),'-steps',str(out/'steps'),'--',str(binary),'--session','claude-display'],env=env,cwd=out,stdout=observer_log,stderr=observer_log)
  monitor_thread=threading.Thread(target=monitor);monitor_thread.start()
  deadline=time.monotonic()+20
  while not (out/'geometry-ready').exists():
   if display.poll() is not None or time.monotonic()>deadline:raise RuntimeError('display geometry missing')
   time.sleep(.025)
  for i,pane in enumerate(panes):
   cwd=Path(tempfile.mkdtemp(prefix='claude-view-',dir=str(base)));workdirs.append(cwd)
   project=home/'.claude/projects'/re.sub(r'[^a-zA-Z0-9]','-',str(cwd))
   if project.exists():raise RuntimeError('refuse to overwrite existing conversation folder')
   project.mkdir();copies.append(project);shutil.copyfile(source,project/source.name)
   relay_log=out/f'claude-{i+1}.jsonl';close=out/f'claude-{i+1}.close'
   args=['/usr/bin/env','-u','XDG_CONFIG_HOME','-u','XDG_STATE_HOME','-u','XDG_DATA_HOME','-u','ZDOTDIR',f'HOME={home}',str(observer),'-claude-relay-log',str(relay_log),'-claude-relay-exit',str(close),'--',str(home/'.local/bin/claude'),'--resume',session_id,'--permission-mode','auto']
   command='cd '+shlex.quote(str(cwd))+' && '+shlex.join(args)
   request('pane.send_text',dict(pane_id=pane['pane_id'],text=command));request('pane.send_keys',dict(pane_id=pane['pane_id'],keys=['Enter']))
  deadline=time.monotonic()+120
  while True:
   latest=[]
   for i in range(len(panes)):
    update_app(i);latest.append(app_latest.get(i))
   tails=['\n'.join(r['lines'][-10:]).lower() if r else '' for r in latest]
   if any(('yes, i trust' in text or 'trust this folder' in text) and 'auto mode on' not in text for text in tails):raise RuntimeError('Claude folder trust needs parent; no key was sent')
   if all('auto mode on' in text and 'esc to interrupt' not in text for text in tails):break
   if display.poll() is not None or time.monotonic()>deadline:raise RuntimeError('ordinary resumed Claude waiting screen not verified')
   time.sleep(.1)
  for index,cwd in enumerate(workdirs):
   started=app_started[index]
   info=subprocess.check_output(['lsof','-a','-p',str(started['pid']),'-d','cwd','-Fn'],text=True)
   actual=[Path(line[1:]).resolve() for line in info.splitlines() if line.startswith('n')]
   if actual!=[cwd.resolve()]:raise RuntimeError('Claude did not retain temporary working directory')
  (out/'metadata.json').write_text(json.dumps(dict(product=a.product,version=subprocess.check_output([str(binary),'--version'],text=True).strip(),claude_version=subprocess.check_output([str(home/'.local/bin/claude'),'--version'],text=True).strip(),conversation_input_sent=False,source_session=str(source),source_sha256=source_hash,temporary_workdirs=[str(v) for v in workdirs],copied_project_dirs=[str(v) for v in copies],trial=trial,scenario=dict(kind=a.kind,mode='claude-wait',panes=2,count=a.count,rate=a.rate,cols=a.cols,rows=a.rows),proxy_right=a.product=='herdr' and a.kind=='right',cols=a.cols,rows=a.rows,cell_pixels=[8,16]),indent=2))
  (out/'claude-ready').write_text('ready\n')
  deadline=time.monotonic()+a.count/a.rate+120
  while not (out/'trial-finished').exists():
   if display.poll() is not None or time.monotonic()>deadline:raise RuntimeError('Claude drag observation did not finish')
   time.sleep(.1)
  for i in range(len(panes)):(out/f'claude-{i+1}.close').write_text('ordinary exit\n')
  deadline=time.monotonic()+20
  while not all((out/f'claude-{i+1}.close.exit').exists() for i in range(len(panes))):
   if time.monotonic()>deadline:raise RuntimeError('Claude ordinary exit not verified; parent intervention required')
   time.sleep(.1)
  normal_exit=True;(out/'closed').write_text('ready\n');display.wait(timeout=10)
finally:
 monitor_stop.set()
 if monitor_thread is not None:monitor_thread.join()
 # Try ordinary Ctrl-C exit even when startup stops at an onboarding screen.
 for i in range(len(copies)):(out/f'claude-{i+1}.close').write_text('ordinary exit\n')
 if not normal_exit and copies:
  deadline=time.monotonic()+20
  while time.monotonic()<deadline and not all((out/f'claude-{i+1}.close.exit').exists() for i in range(len(copies))):time.sleep(.1)
  normal_exit=all((out/f'claude-{i+1}.close.exit').exists() for i in range(len(copies)))
 if normal_exit or not copies:
  if display is not None and display.poll() is None:
   matches=subprocess.run(['pgrep','-u',str(os.getuid()),'-f',str(observer)],capture_output=True,text=True)
   if str(display.pid) in matches.stdout.split():os.kill(display.pid,15);display.wait(timeout=5)
  if server is not None and server.poll() is None:request('server.stop',{});server.wait(timeout=10)
  for project,cwd in zip(copies,workdirs):
   if project.parent!=home/'.claude/projects' or project.name!=re.sub(r'[^a-zA-Z0-9]','-',str(cwd)):raise RuntimeError('cleanup ownership mismatch')
   shutil.rmtree(project)
  for cwd in workdirs:
   if cwd.parent!=base or not cwd.name.startswith('claude-view-'):raise RuntimeError('temporary cwd ownership mismatch')
   shutil.rmtree(cwd)
 source_unchanged=source.stat().st_mtime_ns==source_stat.st_mtime_ns and hashlib.sha256(source.read_bytes()).hexdigest()==source_hash
 (out/'cleanup.json').write_text(json.dumps(dict(claude_normal_exit=normal_exit,original_session_unchanged=source_unchanged,copied_projects_removed=all(not v.exists() for v in copies),temporary_workdirs_removed=all(not v.exists() for v in workdirs),server_pid=None if server is None else server.pid,server_exit=None if server is None else server.poll()),indent=2))
 if not source_unchanged:raise RuntimeError('original conversation changed; do not restore or overwrite it')
