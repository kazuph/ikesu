#!/usr/bin/env python3
"""Observe real terminal delivery, directly and through an unmodified main client.
Run on mini; all app processes and sessions are owned by this invocation.
"""
import argparse, json, os, pathlib, select, shlex, subprocess, tempfile

p=argparse.ArgumentParser()
p.add_argument('--inner',required=True);p.add_argument('--outer',required=True)
p.add_argument('--seconds',type=int,default=10);p.add_argument('--mode',choices=['direct','nested'])
p.add_argument('--ptyrun',required=True);p.add_argument('--evidence',required=True)
a=p.parse_args();out=pathlib.Path(a.evidence).resolve();out.mkdir(parents=True,exist_ok=True)
base={k:v for k,v in os.environ.items() if k not in ('IKESU_SOCKET_PATH','IKESU_SOCKET_PATH_EXPLICIT','IKESU','IKESU_PANE_ID','IKESU_WORKSPACE_ID','IKESU_TAB_ID','SSH_CONNECTION','SSH_TTY','TMUX','STY','IKESU_CPUPROFILE','IKESU_CPUPROFILE_SECS')}
a.inner=str(pathlib.Path(a.inner).resolve());a.outer=str(pathlib.Path(a.outer).resolve());a.ptyrun=str(pathlib.Path(a.ptyrun).resolve())

for mode in ([a.mode] if a.mode else ('direct','nested')):
 state=pathlib.Path(tempfile.mkdtemp(prefix='koi-wire-',dir='/tmp'))
 owned=[];servers=[];streams=[];client=None
 def instance(binary,name):
  directory=state/name;directory.mkdir()
  cfg=directory/'config.toml'
  cfg.write_text('onboarding=false\n[session]\nresume_agents_on_restore=false\n[experimental]\nallow_nested=true\nkitty_graphics=true\n')
  (directory/'.zshrc').write_text("PS1='koi-"+name+"-ready> '\n")
  env={**base,'IKESU_CONFIG':str(cfg),'IKESU_STATE_DIR':str(directory),'ZDOTDIR':str(directory),'SHELL':'/bin/zsh','TERM_PROGRAM':'ghostty'}
  session=f'koi-wire-{os.getpid()}-{mode}-{name}'
  sd=directory/session;sd.mkdir();fd=os.open(sd,os.O_RDONLY);q=select.kqueue();q.control([select.kevent(fd,filter=select.KQ_FILTER_VNODE,flags=select.KQ_EV_ADD,fflags=select.KQ_NOTE_WRITE)],0)
  log=open(out/f'{mode}-{name}-server.log','w');streams.append(log)
  proc=subprocess.Popen([binary,'--session',session,'server'],env=env,stdout=log,stderr=subprocess.STDOUT);servers.append(proc);owned.append((binary,session))
  while not (sd/'ikesu-client.sock').exists():
   if proc.poll() is not None:raise RuntimeError('server startup failed')
   if not q.control(None,1,60):raise RuntimeError('server socket timeout')
  q.close();os.close(fd)
  return binary,session,env
 def cli(inst,*args):
  b,s,e=inst;r=subprocess.run([b,'--session',s,*args],env=e,text=True,capture_output=True,timeout=30)
  if r.returncode:raise RuntimeError(r.stdout+r.stderr)
  return json.loads(r.stdout) if args[:2] in (('pane','list'),('pane','layout')) else r.stdout
 try:
  inner=instance(a.inner,'inner');outer=instance(a.outer,'outer') if mode=='nested' else inner
  ready=state/'ready';tail=state/'tail';steps=state/'steps'
  # The tail is generated from the real outer pane geometry once booted.
  steps.write_text('waittext 30s koi-'+('outer' if mode=='nested' else 'inner')+'-ready>\nmark outer-booted\n'+('waittext 30s koi-inner-ready>\n' if mode=='nested' else '')+'mark inner-booted\nwaitfile '+str(ready)+' 30s\n')
  # ptyrun consumes its steps stream continuously; FIFO lets geometry choose the click.
  fifo=state/'commands';os.mkfifo(fifo)
  log=[]
  clientenv={**outer[2], 'IKESU_CPUPROFILE':str(out/f'{mode}-outer.cpu'),'IKESU_CPUPROFILE_SECS':'10'} if mode=='nested' else outer[2]
  client=subprocess.Popen([a.ptyrun,'-cols','220','-rows','70','-xpixel','1760','-ypixel','1120','-timeline',str(out/f'{mode}.jsonl'),'-raw-output',str(out/f'{mode}.ansi'),'-steps',str(fifo),'--',outer[0],'--session',outer[1]],env=clientenv,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,bufsize=1)
  commands=open(fifo,'w',buffering=1);commands.write(steps.read_text())
  rect={'x':0,'y':0,'width':220,'height':70}
  for line in client.stdout:
   log.append(line)
   if 'label="outer-booted"' in line and mode=='nested':
    pane=cli(outer,'pane','list')['result']['panes'][0]['pane_id']
    snap=cli(outer,'pane','layout','--pane',pane)['result']['layout']
    border=next(v['rect'] for v in snap['panes'] if v['pane_id']==pane)
    rect={'x':border['x']+1,'y':border['y']+1,'width':border['width']-2,'height':border['height']-2}
    # Drop all inherited routing variables, then apply this inner session's own paths.
    command=['env']
    for key in ('IKESU_SOCKET_PATH','IKESU_SOCKET_PATH_EXPLICIT','IKESU','IKESU_PANE_ID','IKESU_WORKSPACE_ID','IKESU_TAB_ID'):
     command+=['-u',key]
    for key in ('IKESU_CONFIG','IKESU_STATE_DIR','ZDOTDIR','SHELL','TERM_PROGRAM'):
     command.append(key+'='+inner[2][key])
    command += [inner[0],'--session',inner[1]]
    cli(outer,'pane','run',pane,shlex.join(command))
   if 'label="inner-booted"' in line:
    for i in range(4):cli(inner,'tab','create','--no-focus','--label','wire-fish-'+str(i))
    panes=cli(inner,'pane','list')['result']['panes']
    for pane in panes:cli(inner,'pane','report-agent',pane['pane_id'],'--source','codex','--agent','codex','--state','working','--title','wire measurement')
    # Right sidebar is 30 columns; click its center, well below the agent list.
    x=rect['x']+rect['width']-15;y=rect['y']+rect['height']//2
    metadata={'mode':mode,'outer_pane':rect,'click_cell':[x,y],'click_px':[(x-1)*8+4,(y-1)*16+8],'free_seconds':a.seconds,'agents':len(panes),'inner':inner[0],'outer':outer[0],'sessions':[s for _,s in owned]}
    (out/f'{mode}-metadata.json').write_text(json.dumps(metadata,indent=2))
    commands.write(f'sleep 2s\nstats before\nsleep {a.seconds}s\nstats after\nscreen '+str(out/f'{mode}-before.txt')+f'\nmousetimed down {x} {y}\nmousetimed up {x} {y}\nsleep 2500ms\nscreen '+str(out/f'{mode}-after.txt')+'\nsend "\\u0002q"\nsleep 100ms\n');commands.close();ready.write_text('ready')
  if client.wait(timeout=30):raise RuntimeError(''.join(log))
 finally:
  (out/f'{mode}.log').write_text(''.join(log) if 'log' in locals() and isinstance(log,list) else '')
  uid=subprocess.check_output(['id','-u'],text=True).strip();cleanup=[]
  for binary,session in owned:
   found=subprocess.run(['pgrep','-u',uid,'-f',binary],text=True,capture_output=True)
   for pid in found.stdout.split():
    cmd=subprocess.run(['ps','-ww','-p',pid,'-o','command='],text=True,capture_output=True).stdout.strip()
    if binary in cmd and f'--session {session}' in cmd:
     subprocess.run(['kill',pid],check=True);cleanup.append({'pid':int(pid),'command':cmd})
  if client and client.poll() is None:
   cmd=subprocess.check_output(['ps','-ww','-p',str(client.pid),'-o','command='],text=True).strip()
   assert a.ptyrun in cmd
   subprocess.run(['kill',str(client.pid)],check=True);client.wait(timeout=15)
  for server in servers:server.wait(timeout=15)
  for stream in streams:stream.close()
  (out/f'{mode}-cleanup.json').write_text(json.dumps(cleanup,indent=2))
