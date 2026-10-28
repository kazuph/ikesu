#!/usr/bin/env python3
"""Real isolated CLI + ptyrun comparison. Run after building, on the bench host."""
import argparse, tomllib, json, os, pathlib, re, signal, subprocess, sys, tempfile, time, socket, select, threading

root=pathlib.Path.cwd()
binary=str(root/'koi-ikesu')
ptyrun=str(root/'koi-ptyrun')
parser=argparse.ArgumentParser();parser.add_argument('--interaction',choices=['menu','ripple','fence','longrun']);parser.add_argument('--seconds',type=int,default=10);parser.add_argument('--state',choices=['idle','working','blocked'],default='working');parser.add_argument('--profile',action='store_true');parser.add_argument('--repeats',type=int,default=3);parser.add_argument('--condition',choices=['empty','pond','full']);parser.add_argument('--evidence',default='koi-bench-results');args=parser.parse_args()
out=root/args.evidence;out.mkdir(exist_ok=True)
base_env={k:v for k,v in os.environ.items() if k not in ('IKESU_SOCKET_PATH','IKESU_SOCKET_PATH_EXPLICIT','IKESU','IKESU_PANE_ID','IKESU_WORKSPACE_ID','IKESU_TAB_ID','SSH_CONNECTION','SSH_TTY','TMUX','STY')}
results=[]

class Traffic:
 def __init__(self,path):
  self.bytes=0;self.snapshots=0;self.messages=[];self.lock=threading.Lock()
  backend=path.with_name('measure-backend.sock');path.rename(backend)
  self.listener=socket.socket(socket.AF_UNIX);self.listener.bind(str(path));self.listener.listen(1)
  def relay():
   client,_=self.listener.accept();server=socket.socket(socket.AF_UNIX);server.connect(str(backend));tail=b''
   try:
    while True:
     readers,_,_=select.select([client,server],[],[])
     for source in readers:
      data=source.recv(65536)
      if not data:return
      if source is server:
       with self.lock:
        self.bytes+=len(data);tail+=data
        while b'\n' in tail:
         line,tail=tail.split(b'\n',1)
         message=json.loads(line)
         self.messages.append(message)
         if message.get('type')=='snapshot':self.snapshots+=1
      (server if source is client else client).sendall(data)
   finally:client.close();server.close();self.listener.close()
  self.thread=threading.Thread(target=relay,daemon=True);self.thread.start()
 def snapshot(self):
  with self.lock:return self.bytes,self.snapshots

def execute(condition,enabled,repeat):
 session=f'koi-bench-{os.getpid()}{repeat}{int(enabled)}{["empty","pond","full"].index(condition)}'
 state=pathlib.Path(tempfile.mkdtemp(prefix='kp232-',dir='/tmp'))
 cfg=state/'config.toml'
 cfg.write_text('onboarding=false\n[session]\nresume_agents_on_restore=false\n[experimental]\nallow_nested=true\n[ui]\nkoi='+str(enabled).lower()+'\n'+('' if condition!='full' else '[ui.sidebar.agents]\nrows=['+','.join(['["agent"]']*60)+']\n'))
 (state/'.zshrc').write_text("PS1='koi-ready> '\n")
 env={**base_env,'IKESU_CONFIG':str(cfg),'IKESU_STATE_DIR':str(state),'ZDOTDIR':str(state),'SHELL':'/bin/zsh','TERM_PROGRAM':'ghostty'}
 ready=state/'ready'
 steps=state/'steps.txt'
 response_command=json.dumps(r"printf '\113\117\111\137\117\113\n'"+'\r')
 fence_seen=state/'fence-seen'
 interaction=[]
 if args.interaction=='menu':
  interaction=['clicktext "[menu]"','waittext 5s koi: on',f'screen {out/session}-menu-on.txt','clicktext "koi: on"','clicktext "[menu]"','waittext 5s koi: off',f'screen {out/session}-menu-off.txt','mark koi-off-saved','clicktext "koi: off"','clicktext "[menu]"','waittext 5s koi: on',f'screen {out/session}-menu-on-again.txt','mark koi-on-saved','send "\\u001b"']
 if args.interaction in ('ripple','fence'):
  if args.interaction=='fence':interaction=[f'waitfile {fence_seen} 30s']
  interaction+=['sendtimed "\\u001b[<0;1165;377M"','send "\\u001b[<0;1165;377m"','sleep 1500ms']
 warmup='' if args.interaction=='fence' else 'sleep 2s\n'
 measurement=f'stats before\nsleep {args.seconds}s\nstats after'
 if args.interaction=='longrun':
  measurement='stats before\n'
  for second in range(0,args.seconds,5):
   # Genuine input and terminal resizes, rather than direct state mutation.
   measurement+=f'resize {160+(second//5)%3} {45+(second//15)%3}\nsend "\\u001b[<0;{1140+(second%20)*3};{370+(second%10)*4}M"\nsend "\\u001b[<0;1165;377m"\nsleep {min(5,args.seconds-second)}s\n'
   if (second+5)%60==0:measurement+=f'stats minute-{(second+5)//60}\n'
  measurement+='stats after'
 steps.write_text(f'waittext 30s koi-ready>\nmark booted\nwaitfile {ready} 30s\n'+warmup+'\n'.join(interaction)+f'\n{measurement}\nscreen {state}/screen.txt\nsend {response_command}\nwaittext 10s KOI_OK\nsend "\\u0002q"\nsleep 100ms\n')

 raw=out/f'{session}.ansi';logpath=out/f'{session}.log'
 server=None;traffic=None;server_samples={};traffic_samples={}
 if condition!='empty':
  sessiondir=state/session;sessiondir.mkdir()
  fd=os.open(sessiondir,os.O_RDONLY);queue=select.kqueue();queue.control([select.kevent(fd,filter=select.KQ_FILTER_VNODE,flags=select.KQ_EV_ADD,fflags=select.KQ_NOTE_WRITE)],0)
  server=subprocess.Popen([binary,'--session',session,'server'],env=env,stdout=open(out/f'{session}-server.log','w'),stderr=subprocess.STDOUT)
  client_socket=sessiondir/'ikesu-client.sock'
  while not client_socket.exists():
   if server.poll() is not None:raise RuntimeError('isolated server exited during startup')
   if not queue.control(None,1,60):raise RuntimeError('isolated server socket timeout')
  queue.close();os.close(fd)
  traffic=Traffic(client_socket)

 command=[ptyrun]+(['-status-delay','drop'] if args.interaction=='fence' else [])+['-cols','160','-rows','45','-xpixel','1280','-ypixel','720','-timeline',str(out/f'{session}.timeline.jsonl'),'-raw-output',str(raw),'-steps',str(steps),'--',binary]
 if condition=='empty':command+=['--no-session']
 else:command+=['--session',session]
 clientenv={**env,'IKESU_CPUPROFILE':str(out/f'{session}.cpu'),'IKESU_CPUPROFILE_SECS':'10'} if args.profile else env
 process=subprocess.Popen(command,env=clientenv,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,bufsize=1)
 lines=[];setup=False;cleanup=[];menu_saved=[]
 def cli(*args):
  # Every command includes the exact isolated session and the scrubbed environment.
  p=subprocess.run([binary,'--session',session,*args],env=env,text=True,capture_output=True,timeout=30)
  if p.returncode:raise RuntimeError(p.stdout+p.stderr)
  return json.loads(p.stdout) if args[:2]==("pane","list") else p.stdout
 try:
  for line in process.stdout:
   lines.append(line)
   if line.startswith('stats before') or line.startswith('stats after'):
    label='before' if line.startswith('stats before') else 'after'
    if server:
     server_samples[label]=subprocess.check_output(['ps','-p',str(server.pid),'-o','time='],text=True).strip()
     traffic_samples[label]=traffic.snapshot()
   if line.startswith('status-query') and setup:fence_seen.write_text('terminal fence pending')
   if 'label="koi-off-saved"' in line or 'label="koi-on-saved"' in line:
    enabled_value=tomllib.loads(cfg.read_text())['ui']['koi']
    expected='koi-on-saved' in line
    assert enabled_value==expected,'menu state and saved file differ'
    menu_saved.append(enabled_value)
   if line.startswith('event mark label="booted"'):
    if condition!='empty':
     listing=cli('pane','list')
     panes=listing['result']['panes']
     pane=panes[0]['pane_id']
     cli('pane','report-agent',pane,'--source','codex','--agent','codex','--state',args.state,'--title','koi benchmark')
    ready.write_text('ready');setup=True
  code=process.wait(timeout=args.seconds+60)
  if code or not setup:raise RuntimeError(f'ptyrun exited {code}, setup={setup}: '+''.join(lines))
 finally:
  uid=subprocess.check_output(['id','-u'],text=True).strip()
  found=subprocess.run(['pgrep','-u',uid,'-f',binary],text=True,capture_output=True)
  for value in found.stdout.split():
   pid=int(value)
   cmd=subprocess.run(['ps','-ww','-p',value,'-o','command='],text=True,capture_output=True).stdout.strip()
   if binary in cmd and f'--session {session}' in cmd and ' server' in cmd:
    subprocess.run(['kill',value],check=True);cleanup.append({'pid':pid,'command':cmd})
  if process.poll() is None:
   # This PID is our exact Popen child, independently validated above by command ownership.
   cmd=subprocess.check_output(['ps','-p',str(process.pid),'-o','command='],text=True).strip()
   assert ptyrun in cmd and binary in cmd
   subprocess.run(['kill',str(process.pid)],check=True);process.wait(timeout=10)
  (out/f'{session}-cleanup.json').write_text(json.dumps({'session':session,'server_signaled':cleanup},indent=2))
  if server:server.wait(timeout=15)
  if traffic:(out/f'{session}-traffic.json').write_text(json.dumps(traffic.messages))
 logpath.write_text(''.join(lines))
 (out/f'{session}-cleanup.json').write_text(json.dumps({'session':session,'server_signaled':cleanup},indent=2))
 captures=re.findall(r'stats (before|after) pid=(\d+) rss_kb,cpu=(\d+)\s+([0-9:.]+) outer_bytes_so_far=(\d+)',''.join(lines))
 if len(captures)!=2:raise RuntimeError('missing CPU/output stats: '+''.join(lines))
 def cpu(value):
  minutes,seconds=value.split(':');return int(minutes)*60+float(seconds)
 before,after=captures
 results.append({'session':session,'mode':'--no-session' if condition=='empty' else '--session','condition':condition,'enabled':enabled,'repeat':repeat,'seconds':args.seconds,'state':args.state,'cpu_seconds':round(cpu(after[3])-cpu(before[3]),3),'server_cpu_seconds':round(cpu(server_samples['after'])-cpu(server_samples['before']),3) if server else None,'server_bytes':traffic_samples['after'][0]-traffic_samples['before'][0] if traffic else None,'snapshot_messages':traffic_samples['after'][1]-traffic_samples['before'][1] if traffic else None,'snapshot_range':[traffic_samples['before'][1],traffic_samples['after'][1]] if traffic else None,'terminal_bytes':int(after[4])-int(before[4]),'input_response_seconds':float(re.findall(r'onscreen ([0-9.]+)', ''.join(lines))[-1]),'server_cleanup':cleanup,'raw_output':str(raw),'log':str(logpath),'state_dir':str(state),'menu_saved':menu_saved,'interaction':args.interaction})

try:
 for repeat in range(args.repeats):
  for condition in ([args.condition] if args.condition else ('empty','pond','full')):
   for enabled in ((True,) if args.interaction else (False,True)):execute(condition,enabled,repeat)
 (out/'results.json').write_text(json.dumps(results,indent=2))
 print(json.dumps(results,indent=2))
finally:
 # Cover startup failures before a ptyrun handle existed, too. Only this
 # invocation's numeric session namespace is eligible for termination.
 uid=subprocess.check_output(['id','-u'],text=True).strip()
 found=subprocess.run(['pgrep','-u',uid,'-f',binary],text=True,capture_output=True)
 remaining=[]
 for value in found.stdout.split():
  command=subprocess.run(['ps','-ww','-p',value,'-o','command='],text=True,capture_output=True).stdout.strip()
  if binary in command and re.search(r'--session koi-bench-'+str(os.getpid())+r'\d+ server',command):
   subprocess.run(['kill',value],check=True);remaining.append({'pid':int(value),'command':command})
 (out/'final-cleanup.json').write_text(json.dumps({'extra_owned_servers_stopped':remaining},indent=2))
