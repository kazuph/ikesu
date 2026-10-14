import argparse, collections, shutil
import os, pathlib, subprocess, tempfile, json, socket, time, pty, fcntl, termios, struct, select, hashlib
parser=argparse.ArgumentParser()
parser.add_argument('--binary',required=True);parser.add_argument('--source',required=True);parser.add_argument('--output',required=True)
options=parser.parse_args()
ROOT=pathlib.Path(options.output).resolve();ROOT.mkdir(parents=True,exist_ok=True)
BIN=str(pathlib.Path(options.binary).resolve());INPUTSOURCE=pathlib.Path(options.source).resolve()
BASE=pathlib.Path(tempfile.mkdtemp(prefix='gi-',dir='/tmp'))
( ROOT/'temporary-root.txt').write_text(str(BASE))
(BASE/'home').mkdir();(BASE/'bin').mkdir()
SOURCE=BASE/'herdr';SOURCE.mkdir()
for name in ['session.json','agent-session-ledger.json','agent-session-snapshot.json']:
 if (INPUTSOURCE/name).exists():shutil.copyfile(INPUTSOURCE/name,SOURCE/name)
# Every agent executable is replaced, not only Claude. The fake processes
# record argv and return; no user's real agent is launched.
for agent in ['claude','codex','devin']:
 f=BASE/'bin'/agent
 f.write_text('#!/bin/sh\nexport PATH=/usr/bin:/bin\nprintf "%s\\n" "'+agent+'|$1|$2" >> "'+str(BASE/'argv.log')+'"\nprintf "FAKE_'+agent+'_READY\\n"\n')
 f.chmod(0o700)
config=BASE/'config.toml'
config.write_text('onboarding=false\n[agent_start.commands]\n'+''.join(agent+'=['+json.dumps(str(BASE/'bin'/agent))+']\n' for agent in ['claude','codex','devin']))
env=os.environ.copy()
for k in list(env):
 if k.startswith('GTX_') or k.startswith('GOTERRACE'):env.pop(k,None)
env.update(HOME=str(BASE/'home'),GTX_STATE_DIR=str(BASE/'state'),GTX_CONFIG=str(config),SHELL='/bin/sh',PATH='/opt/homebrew/bin:/usr/bin:/bin:'+os.environ['PATH'],TERM='xterm-256color')
args=[BIN,'--session','import-real']
def cli(parts,name,input=None,expect=0):
 p=subprocess.run(args+parts,env=env,capture_output=True,text=True,input=input,timeout=30)
 (ROOT/(name+'.stdout')).write_text(p.stdout);(ROOT/(name+'.stderr')).write_text(p.stderr)
 assert p.returncode==expect,(name,p.returncode,p.stderr)
 return p.stdout
hashes={f.name:hashlib.sha256(f.read_bytes()).hexdigest() for f in SOURCE.iterdir() if f.is_file()}
cli(['session','import-herdr','--source',str(SOURCE),'--dry-run'],'real-dry-run')
assert not (BASE/'state').exists()
cli(['session','import-herdr','--source',str(SOURCE)],'real-cancel',input='no\n')
assert not (BASE/'state').exists()
cli(['session','import-herdr','--source',str(SOURCE),'--yes'],'real-import')
saved=BASE/'state/import-real/session.json'
(ROOT/'imported-before-start.json').write_bytes(saved.read_bytes())

sock=BASE/'state/import-real/gtx.sock'
def api(method,params={}):
 c=socket.socket(socket.AF_UNIX);c.settimeout(20);c.connect(str(sock));c.sendall((json.dumps({'id':'evidence','method':method,'params':params})+'\n').encode());data=b''
 while b'\n' not in data:data+=c.recv(65536)
 c.close();r=json.loads(data);assert not r.get('error'),r;return r['result']
server=None;master=None;viewer=None
try:
 serverlog=open(ROOT/'real-server.log','wb')
 server=subprocess.Popen(args+['server'],env=env,stdin=subprocess.DEVNULL,stdout=serverlog,stderr=serverlog)
 deadline=time.monotonic()+30
 while not sock.exists():
  assert server.poll() is None,'server exited';assert time.monotonic()<deadline,'server startup timeout';time.sleep(.02)
 master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',50,180,0,0))
 viewer=subprocess.Popen(args,env=env,stdin=slave,stdout=slave,stderr=slave,close_fds=True);os.close(slave)
 def capture(path,duration=1):
  end=time.monotonic()+duration;data=b''
  while time.monotonic()<end:
   ready,_,_=select.select([master],[],[],max(0,end-time.monotonic()))
   if ready:
    try:data+=os.read(master,65536)
    except OSError:break
  path.write_bytes(data);return data
 capture(ROOT/'attached-screen.ansi',3)
 workspaces=api('workspace.list')['workspaces']
 (ROOT/'workspace-list.json').write_text(json.dumps(workspaces,ensure_ascii=False,indent=2))
 screens={}
 liveLayouts=[]
 initial=json.loads((ROOT/'imported-before-start.json').read_text())
 for w in initial['workspaces']:
  for t in w['tabs']:
   ref={'pane_id':'p'+str(t['focused'])}
   liveLayouts.append({'export':api('layout.export',ref)['layout'],'geometry':api('pane.layout',ref)['layout']})
 (ROOT/'live-layouts.json').write_text(json.dumps(liveLayouts,ensure_ascii=False,indent=2))
 for i,w in enumerate(workspaces):
  api('workspace.focus',{'workspace_id':w['workspace_id']})
  capture(ROOT/('workspace-%02d.ansi'%i),.15)
 panes=api('pane.list')['panes']
 for p in panes:
  screens[p['pane_id']]=api('pane.read',{'pane_id':p['pane_id'],'strip_ansi':True})
 (ROOT/'pane-screens.json').write_text(json.dumps(screens,ensure_ascii=False,indent=2))
 # A second import exercises the live-session append route and preserves
 # every old PTY while allocating new, noncolliding ids.
 oldIDs={p['pane_id'] for p in panes}
 cli(['session','import-herdr','--source',str(SOURCE),'--yes'],'real-live-append')
 newIDs={p['pane_id'] for p in api('pane.list')['panes']}
 assert oldIDs<=newIDs and len(newIDs)==len(oldIDs)*2
 # Await every previously requested fake launch before testing suppression.
 beforeFile=json.loads((ROOT/'imported-before-start.json').read_text())
 expected=collections.Counter(p.get('agent') for w in beforeFile['workspaces'] for t in w['tabs'] for p in t['panes'] if p.get('agent'))
 expected=collections.Counter({a:n*2 for a,n in expected.items()})
 deadline=time.monotonic()+20
 while True:
  counts=collections.Counter(x.split('|')[0] for x in (BASE/'argv.log').read_text().splitlines() if x.split('|')[0] in expected)
  if counts==expected:break
  assert time.monotonic()<deadline,('fake launches incomplete',counts,expected)
  time.sleep(.02)
 # Probe a real held lock in the source copy: every target must be a shell.
 lock=open(SOURCE/'herdr.lock','w')
 fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
 cli(['session','import-herdr','--source',str(SOURCE),'--dry-run'],'running-lock-dry-run')
 argvBefore=(BASE/'argv.log').read_bytes()
 cli(['session','import-herdr','--source',str(SOURCE),'--yes'],'running-lock-import')
 capture(ROOT/'running-lock-screen.ansi',1)
 assert (BASE/'argv.log').read_bytes()==argvBefore
 lock.close();(SOURCE/'herdr.lock').unlink()
 api('server.stop');server.wait(timeout=20)
 (ROOT/'imported-after-live.json').write_bytes(saved.read_bytes())
 (ROOT/'fake-agent-argv.log').write_bytes((BASE/'argv.log').read_bytes())
finally:
 if viewer is not None:
  viewer.terminate()
  try:viewer.wait(timeout=5)
  except subprocess.TimeoutExpired:viewer.kill();viewer.wait()
 if master is not None:os.close(master)
 if server is not None and server.poll() is None:server.terminate();server.wait(timeout=20)

source=json.loads((SOURCE/'session.json').read_text());first=json.loads((ROOT/'imported-before-start.json').read_text());last=json.loads((ROOT/'imported-after-live.json').read_text())
# Compare every required field, modulo the deliberately new numeric ids.
def treeShape(n,isHerdr):
 if isHerdr:
  if 'Pane' in n:return ('pane',)
  s=n['Split'];return (s['direction'],s['ratio'],treeShape(s['first'],True),treeShape(s['second'],True))
 if n['leaf']>=0:return ('pane',)
 return ({'v':'Vertical','h':'Horizontal'}[n['direction']],n['ratio'],treeShape(n['first'],False),treeShape(n['second'],False))
def exportedShape(n):
 if n['type']=='pane':return ('pane',)
 return ({'right':'Vertical','down':'Horizontal'}[n['direction']],n['ratio'],exportedShape(n['first']),exportedShape(n['second']))
checks=[]
layoutIndex=0
for old,new in zip(source['workspaces'],first['workspaces']):
 assert old['section']==new['section'] and old['identity_cwd']==new['cwd'] and old['active_tab']==new['active_tab']
 assert (old.get('custom_name') or pathlib.Path(old['identity_cwd']).name)==new['name']
 for ot,nt in zip(old['tabs'],new['tabs']):
  assert (ot.get('custom_name') or '')==nt.get('name','')
  assert treeShape(ot['layout'],True)==treeShape(nt['layout'],False)
  live=liveLayouts[layoutIndex];layoutIndex+=1
  assert treeShape(ot['layout'],True)==exportedShape(live['export']['root'])
  assert live['export']['zoomed']==ot.get('zoomed',False)
  assert live['export']['focused_pane_id']=='p'+str(nt['focused'])
  mapping=dict(zip(ot['pane_order'],nt['order']))
  assert mapping[ot['focused']]==nt['focused']
  assert nt.get('zoomed',0)==(mapping[ot['focused']] if ot.get('zoomed') else 0)
  oldByID=ot['panes'];newByID={p['id']:p for p in nt['panes']}
  for a,b in mapping.items():assert oldByID[str(a)]['cwd']==newByID[b]['cwd']
checks+=[f"{len(source['workspaces'])}スペース/{sum(len(w['tabs']) for w in source['workspaces'])}タブ/{len(oldIDs)}paneの形・名前・section・cwd・フォーカス・比率・zoomが保存結果と実行中APIの両方で一致",'dry-runとキャンセルはstate作成なし',f'稼働中セッションへの追加は既存{len(oldIDs)}paneを保持し{len(newIDs)}paneへ拡張','ロック中の追加は会話再開なし']
assert first['active']==source['active'] and first['sidebar_width']==source['sidebar_width']
assert len(last['workspaces'])==len(first['workspaces'])*3
for f in SOURCE.iterdir():
 if f.name in hashes:assert hashlib.sha256(f.read_bytes()).hexdigest()==hashes[f.name]
for name,digest in hashes.items():assert hashlib.sha256((INPUTSOURCE/name).read_bytes()).hexdigest()==digest
lines=(ROOT/'fake-agent-argv.log').read_text().splitlines()
assert '--last' not in '\n'.join(lines)
actual=collections.Counter(tuple(row.split('|')) for row in lines)
expectedArgs=collections.Counter((p['agent'],'resume' if p['agent']=='codex' else '--resume',p['session_id']) for w in first['workspaces'] for t in w['tabs'] for p in t['panes'] if p.get('agent'))
assert actual==collections.Counter({k:v*2 for k,v in expectedArgs.items()}),(actual,expectedArgs)
checks.append(f'全{sum(actual.values())}回の偽agent起動引数は記録済みIDと一致、--lastなし')
(ROOT/'live-verification.json').write_text(json.dumps({'checks':checks,'source_hashes':hashes,'source_workspaces':len(source['workspaces']),'source_panes':len(oldIDs),'final_panes':len(newIDs)+len(oldIDs),'source_files_unchanged':True},ensure_ascii=False,indent=2))
print(json.dumps(checks,ensure_ascii=False))
