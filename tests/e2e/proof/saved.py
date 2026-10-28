import contextlib, json, os, pathlib, pwd, shlex, socket, subprocess, tempfile, time, unittest
from mixed import ROOT, OUT, NEW, OLD, PTY, wait_for

class SavedSSH(unittest.TestCase):
    def test_new_client_old_saved_server(self): self.run_pair('saved-new-client-old-server',NEW,OLD,False)
    def test_old_client_new_saved_server(self): self.run_pair('saved-old-client-new-server',OLD,NEW,True)
    def run_pair(self,name,client_bin,remote_bin,dialog):
        out=OUT/name; out.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='ikesu-ssh-proof-',dir=str(ROOT)) as tmp:
            root=pathlib.Path(tmp); home=root/'home'; home.mkdir(); (home/'.ssh').mkdir()
            env=dict(os.environ,HOME=str(home),ZDOTDIR=str(home),SHELL='/usr/bin/zsh',IKESU_STATE_DIR=str(root/'state'),IKESU_CONFIG=str(root/'config.toml'),TERM='xterm-256color')
            for key in list(env):
                if key.startswith(('HERDR_','IKESU')): env.pop(key)
            (home/'.zshrc').write_text("PS1='SSH_PROOF> '\n")
            (root/'config.toml').write_text('onboarding=false\n[session]\nresume_agents_on_restore=false\n')
            for name in ['hostkey','clientkey']:
                subprocess.run(['ssh-keygen','-q','-t','ed25519','-N','','-f',str(root/name)],check=True)
            (root/'authorized_keys').write_text((root/'clientkey.pub').read_text())
            with socket.socket() as s: s.bind(('127.0.0.1',0)); port=s.getsockname()[1]
            host=(root/'hostkey.pub').read_text().split()
            (root/'known_hosts').write_text(f'[127.0.0.1]:{port} {host[0]} {host[1]}\n')
            user=pwd.getpwuid(os.getuid()).pw_name
            cfg=root/'sshd_config'
            cfg.write_text(f'Port {port}\nListenAddress 127.0.0.1\nHostKey {root}/hostkey\nPidFile {root}/sshd.pid\nAuthorizedKeysFile {root}/authorized_keys\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nAllowUsers {user}\nLogLevel VERBOSE\n')
            sshcfg=home/'.ssh/config'
            sshcfg.write_text(f'Host proof\n HostName 127.0.0.1\n Port {port}\n User {user}\n IdentityFile {root}/clientkey\n IdentitiesOnly yes\n UserKnownHostsFile {root}/known_hosts\n StrictHostKeyChecking yes\n')
            bindir=root/'bin'; bindir.mkdir()
            (bindir/'ssh').write_text(f'#!/bin/sh\nexec /usr/bin/ssh -F {shlex.quote(str(sshcfg))} "$@"\n'); (bindir/'ssh').chmod(0o700)
            env['PATH']=str(bindir)+':'+env['PATH']
            wrapper=root/'remote-ikesu'
            # Genuine SSH launches the actual old server transport/CLI, with test-only state paths.
            wrapper.write_text('#!/bin/sh\n'+''.join(f'export {k}={shlex.quote(env[k])}\n' for k in ['HOME','ZDOTDIR','SHELL','IKESU_STATE_DIR','IKESU_CONFIG'])+f'exec {shlex.quote(str(remote_bin))} "$@"\n')
            wrapper.chmod(0o700); env['IKESU_MACHINE_REMOTE_BINARY']=str(wrapper)
            def cli(session,*args,allow_error=False):
                r=subprocess.run([str(NEW),'--session',session,*args],env=env,capture_output=True,text=True,timeout=20)
                if not allow_error: self.assertEqual(r.returncode,0,r.stdout+r.stderr)
                return r
            def rpc(session,method,params):
                with socket.socket(socket.AF_UNIX) as c:
                    c.settimeout(15); c.connect(str(root/'state'/session/'ikesu.sock'))
                    c.sendall((json.dumps(dict(id='saved-proof',method=method,params=params))+'\n').encode())
                    response=json.loads(c.makefile('r').readline()); self.assertNotIn('error',response)
                    return response['result']
            processes=[]; logs=[]; viewer=None; cleanup_errors=[]
            try:
                log=open(out/'saved-sshd.log','w'); logs.append(log)
                sshd=subprocess.Popen(['/usr/sbin/sshd','-D','-e','-f',str(cfg)],stdout=log,stderr=subprocess.STDOUT); processes.append(sshd)
                def ssh_ready():
                    if sshd.poll() is not None: raise AssertionError('sshd exited: '+(out/'saved-sshd.log').read_text())
                    with socket.socket() as s: return s.connect_ex(('127.0.0.1',port))==0
                wait_for(ssh_ready,'isolated sshd')
                auth=subprocess.run([str(bindir/'ssh'),'-o','BatchMode=yes','proof','true'],env=env,capture_output=True,text=True,timeout=15)
                self.assertEqual(auth.returncode,0,auth.stderr)
                for session,binary in [('local',NEW),('remote',remote_bin)]:
                    log=open(out/(f'saved-{session}-server.log'),'w'); logs.append(log)
                    p=subprocess.Popen([str(binary),'--session',session,'server'],env=env,stdout=log,stderr=subprocess.STDOUT); processes.append(p)
                    wait_for(lambda:(root/'state'/session/'ikesu-client.sock').exists(),session+' server')
                cli('remote','workspace','rename','s1','RemoteWorkspace')
                add=cli('local','machine','add','proof','--label','RemoteProof','--remote-session','remote')
                (out/'saved-machine-add.json').write_text(add.stdout)
                for path in [out/'saved-pending.screen.txt',out/'saved-after.screen.txt']: path.unlink(missing_ok=True)
                watchlog=open(out/'saved-watch.jsonl','w'); logs.append(watchlog)
                watch=subprocess.Popen([str(NEW),'--session','local','decision','watch','--all-machines'],env=env,stdout=watchlog,stderr=subprocess.DEVNULL); processes.append(watch)
                def watchlines():
                    return [json.loads(line) for line in (out/'saved-watch.jsonl').read_text().splitlines()]
                wait_for(lambda:any(r['event']=='machine_up' and r['machine']['label']=='RemoteProof' for r in watchlines()),'remote watch up')
                ready=root/'decision-ready'; answered=root/'answer-ready'; screen=out/'saved-pending.screen.txt'; after=out/'saved-after.screen.txt'
                steps=root/'steps'
                steps.write_text('\n'.join(['waittext 15s RemoteWorkspace','clicktext "RemoteWorkspace"','waittext 15s ▌· 1 RemoteWorkspace',f'waitfile {ready} 15s','sleep 300ms',f'screen {screen}','send "printf \'REMOTE_%s\\\\n\' INPUT\\r"','waittext 15s REMOTE_INPUT',f'waitfile {answered} 15s','send "printf \'REMOTE_%s\\\\n\' ANSWERED\\r"','waittext 15s REMOTE_ANSWERED',f'screen {after}'])+'\n')
                viewer=subprocess.Popen([str(PTY),'-cols','120','-rows','40','-steps',str(steps),'-raw-output',str(out/'saved.ansi'),'--',str(client_bin),'--session','local'],env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True); processes.append(viewer)
                decision=rpc('remote','decision.create',dict(kind='ask',title='SAVED_PENDING_TITLE',options=[dict(id='yes',label='SAVED_APPROVE',role='approve')]))['decision']; ready.write_text('pending')
                wait_for(screen.exists,'saved machine screen')
                wait_for(lambda:any(r['event']=='created' and r.get('decision',{}).get('decision_id')==decision['decision_id'] for r in watchlines()),'remote watch created')
                self.assertEqual('SAVED_PENDING_TITLE' in screen.read_text(),dialog)
                if not dialog: self.assertNotIn('! 1',screen.read_text())
                wait_for(lambda:'REMOTE_INPUT' in (out/'saved.ansi').read_text(errors='replace'),'saved pane input')
                remote_read=cli('remote','pane','read','p1','--lines','10')
                self.assertIn('REMOTE_INPUT',remote_read.stdout)
                (out/'saved-remote-pane.json').write_text(remote_read.stdout)
                answer=cli('local','decision','answer',decision['decision_id'],'yes','--machine','RemoteProof')
                duplicate=cli('local','decision','answer',decision['decision_id'],'yes','--machine','RemoteProof',allow_error=True)
                self.assertNotEqual(duplicate.returncode,0); self.assertIn('decision_already_resolved',duplicate.stdout+duplicate.stderr)
                resolved=rpc('remote','decision.get',dict(decision_id=decision['decision_id']))['decision']; self.assertEqual(resolved['status'],'answered')
                wait_for(lambda:any(r['event']=='resolved' and r.get('decision',{}).get('decision_id')==decision['decision_id'] for r in watchlines()),'remote watch resolved')
                answered.write_text('answered'); output,_=viewer.communicate(timeout=20); self.assertEqual(viewer.returncode,0,output); self.assertNotIn('TIMEOUT',output)
                self.assertIn('REMOTE_ANSWERED',after.read_text())
                (out/'saved-result.json').write_text(json.dumps(dict(pending=decision,resolved=resolved,answer=answer.stdout,duplicate=duplicate.stdout+duplicate.stderr,pty_output=output),ensure_ascii=False,indent=2))
            finally:
                for p in processes[3:]:
                    if p.poll() is None: p.terminate()
                    try: p.wait(timeout=15)
                    except subprocess.TimeoutExpired: p.kill(); p.wait()
                    if p.stdout and not p.stdout.closed:
                        (out/'saved-pty-output.log').write_text(p.stdout.read())
                        p.stdout.close()
                for session in ['local','remote']:
                    if (root/'state'/session/'ikesu.sock').exists():
                        try: cli(session,'server','stop',allow_error=True)
                        except subprocess.TimeoutExpired: cleanup_errors.append(session+' stop timed out')
                for p in processes[1:3]:
                    try: p.wait(timeout=15)
                    except subprocess.TimeoutExpired: p.kill(); p.wait(); cleanup_errors.append('test server did not stop')
                if processes:
                    processes[0].terminate(); processes[0].wait(timeout=15)
                for log in logs: log.close()
                self.assertFalse(cleanup_errors,cleanup_errors)
            self.assertFalse(any((root/'state'/s/'ikesu.sock').exists() for s in ['local','remote']))
if __name__=='__main__': unittest.main(verbosity=2)
