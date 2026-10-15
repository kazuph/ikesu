import json, os, shlex, socket, subprocess, sys, tempfile, time
from pathlib import Path
binary, ptyrun, procedure, output = map(lambda s:Path(s).resolve(),sys.argv[1:])
output.mkdir(parents=True,exist_ok=True)
base={k:v for k,v in os.environ.items() if not k.startswith(('GTX','HERDR','CODEX','CLAUDE'))}
with tempfile.TemporaryDirectory(prefix='tdm-',dir='/tmp') as tmp, tempfile.TemporaryDirectory(prefix='tab-drag-keys-',dir=Path.home()/'bench-terrace.noindex/work') as keytmp:
 root=Path(tmp)
 envs={}
 for role in ['local','remote']:
  r=root/role
  for name in ['home','config','state','data','zdot']: (r/name).mkdir(parents=True)
  (r/'zdot/.zshrc').write_text("PS1='PARITY> '\nunset HISTFILE\nSAVEHIST=0\n")
  env=dict(base,HOME=str(r/'home'),XDG_CONFIG_HOME=str(r/'config'),XDG_STATE_HOME=str(r/'state'),XDG_DATA_HOME=str(r/'data'),GTX_STATE_DIR=str(r/'state/g'),ZDOTDIR=str(r/'zdot'),SHELL='/bin/zsh')
  envs[role]=env
 # A local SSH daemon with newly generated test keys; no owner's SSH files used.
 for name in ['host','identity']:
  subprocess.run(['/usr/bin/ssh-keygen','-q','-t','ed25519','-N','','-f',str(root/name)],check=True)
 s=socket.socket();s.bind(('127.0.0.1',0));port=s.getsockname()[1];s.close()
 sshdir=root/'local/home/.ssh';sshdir.mkdir(mode=0o700)
 pub=(root/'host.pub').read_text().split()
 (root/'known_hosts').write_text(f'[127.0.0.1]:{port} {pub[0]} {pub[1]}\n')
 (sshdir/'config').write_text(f'Host drag-fixture\n HostName 127.0.0.1\n Port {port}\n User {os.environ["USER"]}\n IdentityFile {root}/identity\n IdentitiesOnly yes\n UserKnownHostsFile {root}/known_hosts\n StrictHostKeyChecking yes\n')
 authorized=Path(keytmp)/'authorized_keys'
 authorized.write_text((root/'identity.pub').read_text());authorized.chmod(0o600)
 conf=root/'sshd_config'
 conf.write_text(f'ListenAddress 127.0.0.1\nPort {port}\nHostKey {root}/host\nPidFile {root}/sshd.pid\nAuthorizedKeysFile {authorized}\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\n')
 log=(output/'sshd.log').open('w')
 sshd=subprocess.Popen(['/usr/sbin/sshd','-D','-e','-f',str(conf)],stdout=log,stderr=log)
 servers=[]
 def request(role,method,params={}):
  with socket.socket(socket.AF_UNIX) as sock:
   sock.settimeout(5);sock.connect(str(root/role/'state/g/gtx-tab-drag/parity/gtx.sock'));sock.sendall(json.dumps({'id':'drag-proof','method':method,'params':params}).encode()+b'\n')
   b=b''
   while b'\n' not in b:b+=sock.recv(65536)
  result=json.loads(b.split(b'\n')[0]);assert 'error' not in result,result;return result
 try:
  # Readiness belongs to this fixture, never orchestration polling.
  deadline=time.monotonic()+5
  while True:
   try:
    with socket.create_connection(('127.0.0.1',port),timeout=.1):break
   except OSError:
    if sshd.poll() is not None or time.monotonic()>deadline:raise RuntimeError('isolated sshd unavailable')
    time.sleep(.02)
  for role in ['local','remote']:
   env=envs[role]
   stream=(output/(role+'-server.log')).open('w')
   proc=subprocess.Popen([str(binary),'--session','parity','server'],env=env,cwd=root/role/'home',stdout=stream,stderr=stream);servers.append((role,proc,stream))
   deadline=time.monotonic()+10
   while not (root/role/'state/g/gtx-tab-drag/parity/gtx.sock').exists():
    if proc.poll() is not None or time.monotonic()>deadline:raise RuntimeError('isolated gtx server unavailable')
    time.sleep(.02)
   ws=request(role,'workspace.list')['result']['workspaces']
   if not ws:request(role,'workspace.create',{'cwd':str(root/role/'home'),'label':role,'focus':True})
   else:request(role,'workspace.rename',{'workspace_id':ws[0]['workspace_id'],'label':role})
  wrapper=root/'remote-endpoint'
  wrapper.write_text('#!/bin/sh\n'+''.join('export '+k+'='+shlex.quote(v)+'\n' for k,v in envs['remote'].items() if k in ['HOME','XDG_CONFIG_HOME','XDG_STATE_HOME','XDG_DATA_HOME','GTX_STATE_DIR','ZDOTDIR','SHELL'])+'exec '+shlex.quote(str(binary))+' "$@"\n');wrapper.chmod(0o700)
  catalog=root/'local/state/g/gtx-tab-drag/parity/machines.json'
  catalog.write_text(json.dumps([dict(id='m_drag_fixture',label='drag-fixture',target='drag-fixture',session='parity',enabled=True)]))
  env=envs['local'];env['GTX_MACHINE_REMOTE_BINARY']=str(wrapper)
  steps=output/'steps'
  steps.write_text('sleep 3s\nclicktext "remote"\nsleep 1s\nclicktext "remote"\nsleep 1s\nscreen '+str(output/'selected.txt')+'\ncells '+str(output/'selected.json')+'\n'+procedure.read_text().replace('@OUT@',str(output)))
  runlog=(output/'run.log').open('w')
  subprocess.run([str(ptyrun),'-cols','160','-rows','45','-pixel-mouse=false','-steps',str(steps),'-raw-output',str(output/'terminal.raw'),'--',str(binary),'--session','parity'],env=env,cwd=root/'local/home',stdout=runlog,stderr=runlog,check=True)
  (output/'remote-tabs.json').write_text(json.dumps(request('remote','tab.list'),indent=2))
  before=request('remote','tab.list')['result']['tabs']
  assert len(before)==3, 'saved machine tabs not selected'
  assert [t['label'] for t in before]==['B','1A','C'], before
  request('remote','server.stop')
  old=servers.pop();old[1].wait(timeout=10);old[2].close()
  stream=(output/'remote-restarted.log').open('w')
  proc=subprocess.Popen([str(binary),'--session','parity','server'],env=envs['remote'],cwd=root/'remote/home',stdout=stream,stderr=stream)
  servers.append(('remote',proc,stream))
  deadline=time.monotonic()+10
  while True:
   try:
    after=request('remote','tab.list')['result']['tabs'];break
   except (OSError,AssertionError):
    if proc.poll() is not None or time.monotonic()>deadline:raise RuntimeError('restarted server unavailable')
    time.sleep(.02)
  identity=lambda tabs:[(t['tab_id'],t['label'],t['focused']) for t in tabs]
  assert identity(before)==identity(after),(before,after)
  (output/'restart.json').write_text(json.dumps(dict(before=before,after=after,status='PASS'),indent=2))
 finally:
  for role,proc,stream in servers:
   try:request(role,'server.stop')
   except Exception:proc.terminate()
   proc.wait(timeout=10);stream.close()
  sshd.terminate();sshd.wait(timeout=5);log.close()
