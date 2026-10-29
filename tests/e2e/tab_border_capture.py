#!/usr/bin/env python3
"""Record a dedicated real server/client in a disposable HOME; never attach live sessions."""
import argparse,fcntl,hashlib,json,os,pty,select,struct,subprocess,tempfile,termios,time,tomllib
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--binary',type=Path,required=True);p.add_argument('--output',type=Path,required=True);p.add_argument('--config',type=Path);p.add_argument('--theme');p.add_argument('--gaps',choices=['true','false']);p.add_argument('--view',choices=['normal','narrow','zoom'],default='normal');p.add_argument('--impl-only',action='store_true');a=p.parse_args();a.binary=a.binary.resolve();a.output.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory(prefix='ikesu-border-proof-',dir='/tmp') as temporary:
 root=Path(temporary);home=root/'home';home.mkdir();work=root/'work';work.mkdir();cfg=home/'.config/ikesu/config.toml';source=a.config.read_bytes() if a.config else None
 if source is not None:
  cfg.parent.mkdir(parents=True);cfg.write_bytes(source)
 if a.theme:
  import re
  text=cfg.read_text() if cfg.exists() else ''
  if a.config:text=re.sub(r'(\[theme\][\s\S]*?\bname\s*=\s*)"[^"]+"',lambda m:m[1]+json.dumps(a.theme),text,count=1)
  else:text=f'[theme]\nname={json.dumps(a.theme)}\n'
  cfg.parent.mkdir(parents=True,exist_ok=True);cfg.write_text(text)
 if a.gaps:
  import re
  text=cfg.read_text() if cfg.exists() else ''
  if re.search(r'^\[ui\]\s*$',text,re.M):
   text=re.sub(r'(^\[ui\]\s*$)',lambda m:m[0]+'\npane_gaps='+a.gaps,text,count=1,flags=re.M)
  else:text+='\n[ui]\npane_gaps='+a.gaps+'\n'
  cfg.parent.mkdir(parents=True,exist_ok=True);cfg.write_text(text)

 env={k:v for k,v in os.environ.items() if not k.startswith(('IKESU','HERDR','CODEX','CLAUDE','TMUX','XDG_'))}
 env.update(HOME=str(home),IKESU_STATE_DIR=str(root/'state'),XDG_CONFIG_HOME=str(home/'.config'),XDG_STATE_HOME=str(root/'xdg-state'),XDG_DATA_HOME=str(root/'data'),SHELL='/bin/sh',TERM='xterm-256color',PS1='proof> ',ENV='/dev/null')
 master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',40,160,0,0))
 def terminal():os.setsid();fcntl.ioctl(slave,termios.TIOCSCTTY,0)
 proc=subprocess.Popen([str(a.binary),'--session','proof'],stdin=slave,stdout=slave,stderr=slave,cwd=work,env=env,preexec_fn=terminal);os.close(slave);data=bytearray();records=[]
 def pump(timeout=.05):
  if select.select([master],[],[],timeout)[0]:
   chunk=os.read(master,65536)
   if not chunk:raise RuntimeError('isolated client exited')
   data.extend(chunk);return True
  return False
 def cli(*args):
  r=subprocess.run([str(a.binary),'--session','proof',*args],env=env,cwd=work,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
  if r.returncode:raise RuntimeError((args,r.returncode,r.stdout,r.stderr))
  return json.loads(r.stdout)['result'] if r.stdout.strip().startswith(b'{') else r.stdout.decode()
 def quiet():
  deadline=time.monotonic()+5
  while pump():
   if time.monotonic()>deadline:raise RuntimeError('screen did not settle')
 try:
  deadline=time.monotonic()+10
  while not data:
   if time.monotonic()>deadline:raise RuntimeError('no initial output')
   pump()
  quiet()
  if b'terminal workspace manager' in data:os.write(master,b'\r');quiet()
  tabs=cli('tab','list')['tabs']
  if not tabs:cli('workspace','create','--cwd',str(work),'--focus');quiet();tabs=cli('tab','list')['tabs']
  boss=tabs[0]['tab_id'];cli('tab','rename',boss,'boss')
  created=cli('tab','create','--workspace',tabs[0]['workspace_id'],'--label','impl','--focus');impl=created['tab']['tab_id'];first=created['root_pane']['pane_id']
  cli('pane','split',first,'--direction','right','--no-focus')
  panes=cli('pane','list')['panes'];right=next(x['pane_id'] for x in panes if x['tab_id']==impl and x['pane_id']!=first)
  cli('pane','split',first,'--direction','down','--no-focus');cli('pane','split',right,'--direction','down','--focus');quiet()
  if a.view=='narrow':
   # The framed content loses scrollbar support below width five (outer width seven).
   while True:
    layout=cli('pane','layout','--pane',first)['layout']
    width=next(pane['rect']['width'] for pane in layout['panes'] if pane['pane_id']==first)
    if width<=6:break
    cli('pane','split',first,'--direction','right','--no-focus');quiet()
  for active,tid in ([('impl',impl)] if a.impl_only else [('impl',impl),('boss',boss)]):
   cli('tab','focus',tid);quiet()
   panes=[x for x in cli('pane','list')['panes'] if x['tab_id']==tid]
   if a.view=='zoom':cli('pane','zoom',panes[0]['pane_id'],'--on');quiet()
   layout=cli('pane','layout','--pane',panes[0]['pane_id'])['layout']
   # Record real shell output, with the marker separated in the input.
   for pane in panes:cli('pane','run',pane['pane_id'],"printf 'BORDER_%s\\n' 'READY'")
   deadline=time.monotonic()+10
   for pane in panes:
    while 'BORDER_READY' not in cli('pane','read',pane['pane_id'],'--lines','200').replace('\n','').replace('\r',''):
     if time.monotonic()>deadline:raise RuntimeError('real shell marker never arrived')
     pump()
   quiet();path=a.output/f'{active}.ansi';path.write_bytes(data)
   records.append(dict(active=active,path=str(path.resolve()),layout=layout,cols=160,rows=40,view=a.view,pane_gaps=a.gaps!='false',theme=a.theme or ('tokyo-night' if a.config else 'catppuccin')))
  (a.output/'captures.json').write_text(json.dumps(dict(config_sha256=hashlib.sha256(source).hexdigest() if source is not None else None,theme_custom=tomllib.loads(cfg.read_text()).get('theme',{}).get('custom',{}) if cfg.exists() else {},temporary_home=True,records=records),indent=2))
 finally:
  (a.output/'raw.ansi').write_bytes(data)
  subprocess.run([str(a.binary),'--session','proof','server','stop'],env=env,cwd=work,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10,check=True)
  proc.wait(timeout=10);os.close(master)
print('CAPTURE_OK',a.output)
