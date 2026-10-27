import json, os, pathlib, socket, subprocess, tempfile, time, unittest
ROOT=pathlib.Path(__file__).resolve().parents[3]
OUT=pathlib.Path(os.environ['NODIALOG_EVIDENCE_DIR'])
NEW=OUT/'ikesu'
OLD=pathlib.Path(os.environ['NODIALOG_BASELINE_BIN'])
PTY=OUT/'ptyrun'

def wait_for(predicate, label):
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        if predicate(): return
        time.sleep(.05)
    raise AssertionError('timeout: '+label)

class Mixed(unittest.TestCase):
    def run_pair(self, name, server_bin, client_bin, expects_dialog):
        with tempfile.TemporaryDirectory(prefix='ikesu-mix-',dir='/tmp') as d:
            home=pathlib.Path(d); state=home/'state'; cfg=home/'config.toml'
            cfg.write_text('onboarding=false\n[session]\nresume_agents_on_restore=false\n')
            (home/'.zshrc').write_text("PS1='MIX_PROMPT> '\n")
            env=dict(os.environ,HOME=d,ZDOTDIR=d,IKESU_CONFIG=str(cfg),IKESU_STATE_DIR=str(state),SHELL='/usr/bin/zsh',TERM='xterm-256color')
            for key in list(env):
                if key.startswith(('HERDR_','IKESU')): env.pop(key)
            session='mixed'
            def cli(*args):
                p=subprocess.run([str(NEW),'--session',session,*args],env=env,text=True,capture_output=True,timeout=15)
                self.assertEqual(p.returncode,0,p.stdout+p.stderr)
                return p.stdout
            def rpc(method,params):
                with socket.socket(socket.AF_UNIX) as c:
                    c.settimeout(15); c.connect(str(state/session/'ikesu.sock'))
                    c.sendall((json.dumps(dict(id='mixed',method=method,params=params))+'\n').encode())
                    r=json.loads(c.makefile('r').readline())
                    self.assertNotIn('error',r)
                    return r['result']
            before=OUT/(name+'-before.screen.txt'); pending=OUT/(name+'-pending.screen.txt'); after=OUT/(name+'-after.screen.txt')
            for path in [before,pending,after]: path.unlink(missing_ok=True)
            ready=home/'pending-ready'; resolved=home/'answered-ready'
            steps=home/'steps'
            steps.write_text('\n'.join([
                'waittext 15s MIX_PROMPT>',f'screen {before}',f'waitfile {ready} 15s','sleep 300ms',
                f'screen {pending}', 'send "printf \'MIX_%s\\\\n\' INPUT\\r"', 'waittext 15s MIX_INPUT',
                f'waitfile {resolved} 15s','send "printf \'MIX_%s\\\\n\' ANSWERED\\r"','waittext 15s MIX_ANSWERED',f'screen {after}',
            ])+'\n')
            server_log=open(OUT/(name+'-server.log'),'w')
            server=subprocess.Popen([str(server_bin),'--session',session,'server'],env=env,stdout=server_log,stderr=subprocess.STDOUT)
            viewer=None
            try:
                wait_for(lambda:(state/session/'ikesu-client.sock').exists(),'server client socket')
                viewer=subprocess.Popen([str(PTY),'-cols','120','-rows','40','-steps',str(steps),'-raw-output',str(OUT/(name+'.ansi')),'--',str(client_bin),'--session',session],env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
                wait_for(before.exists,'initial screen')
                decision=rpc('decision.create',dict(kind='ask',title='MIX_PENDING_TITLE',options=[dict(id='yes',label='MIX_APPROVE',role='approve')]))['decision']
                ready.write_text('pending')
                wait_for(pending.exists,'pending screen')
                current=rpc('decision.get',dict(decision_id=decision['decision_id']))['decision']
                self.assertEqual(current['status'],'pending')
                screen=pending.read_text()
                self.assertEqual('MIX_PENDING_TITLE' in screen,expects_dialog,screen)
                if not expects_dialog: self.assertNotIn('! 1',screen)
                wait_for(lambda:'MIX_INPUT' in (OUT/(name+'.ansi')).read_text(errors='replace'),'input while pending')
                cli('decision','answer',decision['decision_id'],'yes')
                resolved.write_text('answered')
                output,_=viewer.communicate(timeout=20)
                self.assertEqual(viewer.returncode,0,output)
                self.assertNotIn('TIMEOUT',output)
                self.assertIn('MIX_ANSWERED',after.read_text())
                self.assertNotIn('MIX_PENDING_TITLE',after.read_text())
                result=rpc('decision.get',dict(decision_id=decision['decision_id']))['decision']
                self.assertEqual(result['status'],'answered')
                (OUT/(name+'-result.json')).write_text(json.dumps(dict(pending=current,resolved=result,pty_output=output),ensure_ascii=False,indent=2))
            finally:
                if viewer and viewer.poll() is None: viewer.kill(); viewer.wait()
                if viewer and viewer.stdout: viewer.stdout.close()
                if server.poll() is None:
                    try: cli('server','stop')
                    except Exception: server.terminate()
                try: server.wait(timeout=15)
                except subprocess.TimeoutExpired: server.kill(); server.wait(); self.fail('server cleanup timed out')
                server_log.close()
            self.assertFalse((state/session/'ikesu.sock').exists(),'server socket left behind')
    def test_new_client_old_server(self): self.run_pair('new-client-old-server',OLD,NEW,False)
    def test_old_client_new_server(self): self.run_pair('old-client-new-server',NEW,OLD,True)
if __name__=='__main__': unittest.main(verbosity=2)
