#!/usr/bin/env python3
"""Exercise real Codex questions through an isolated ikesu server's public API."""
import argparse
import json
import os
import re
from pathlib import Path
import shlex
import socket
import subprocess
import tempfile
import time

from clipboard_guard import SANDBOX_PROFILE

p = argparse.ArgumentParser()
p.add_argument('--ikesu',dest='ikesu', required=True)
p.add_argument('--codex', required=True)
p.add_argument('--out', required=True)
a = p.parse_args()
out = Path(a.out).resolve()
out.mkdir(parents=True, exist_ok=True)
trace = []
with tempfile.TemporaryDirectory(prefix='footer-codex-', dir='/tmp') as directory:
    root = Path(directory)
    for name in ['state', 'config', 'data', 'zdot']:
        (root/name).mkdir()
    config = root/'config.toml'
    config.write_text('onboarding=false\n[session]\nresume_agents_on_restore=false\n[decisions]\nscreen_prompts=true\n[ui.sound]\nenabled=false\n')
    (root/'zdot/.zshrc').write_text("PS1='CODEX_TEST> '\nunset HISTFILE\nSAVEHIST=0\n")
    env = dict(os.environ, IKESU_STATE_DIR=str(root/'state'), IKESU_CONFIG=str(config),
               XDG_CONFIG_HOME=str(root/'config'), XDG_DATA_HOME=str(root/'data'),
               XDG_STATE_HOME=str(root/'state'), ZDOTDIR=str(root/'zdot'),
               SHELL='/bin/zsh', IKESU_DISABLE_SOUND='1', TERM='xterm-256color')
    for name in list(env):
        if name.startswith(('HERDR_', 'IKESU_SOCKET_', 'IKESU_PANE_', 'IKESU_WORKSPACE_', 'IKESU_TAB_')):
            env.pop(name)
    log = (out/'server.log').open('w')
    server = subprocess.Popen(['/usr/bin/sandbox-exec', '-p', SANDBOX_PROFILE,
                               str(Path(a.ikesu).resolve()), '--session', 'codex-ui', 'server'],
                              env=env, stdout=log, stderr=subprocess.STDOUT)
    endpoint = root/'state/codex-ui/ikesu.sock'
    def wait_for(label, condition, timeout=60):
        deadline = time.monotonic()+timeout
        while True:
            value = condition()
            if value:
                return value
            if server.poll() is not None or time.monotonic() >= deadline:
                raise RuntimeError('Timeout or server exit while waiting for '+label)
            time.sleep(.05)
    def request(method, params, allow_error=False):
        with socket.socket(socket.AF_UNIX) as sock:
            sock.settimeout(15)
            sock.connect(str(endpoint))
            sock.sendall(json.dumps({'id':'codex-ui-test', 'method':method, 'params':params}).encode()+b'\n')
            data = bytearray()
            while b'\n' not in data:
                chunk = sock.recv(65536)
                if not chunk:
                    raise RuntimeError('API closed without a response')
                data.extend(chunk)
        response = json.loads(data.split(b'\n')[0])
        trace.append({'method':method, 'params':params, 'response':response})
        (out/'api.json').write_text(json.dumps(trace, ensure_ascii=False, indent=2)+'\n')
        if 'error' in response and not allow_error:
            raise RuntimeError(response)
        return response
    pane = None
    def screen():
        return request('pane.read', {'pane_id':pane, 'source':'visible', 'strip_ansi':True})['result']['read']['text']
    def save_screen(name):
        text = screen()
        (out/(name+'.txt')).write_text(text)
        return text
    def send_text(text):
        request('pane.send_text', {'pane_id':pane, 'text':text})
    def enter():
        request('pane.send_keys', {'pane_id':pane, 'keys':['Enter']})
    def pending(label):
        decisions = request('decision.list', {'status':'pending'})['result']['decisions']
        return next((d for d in decisions if d.get('question_label','').startswith(label) and d.get('origin',{}).get('pane_id')==pane), None)
    def answer(decision, **payload):
        def deliver():
            response = request('decision.answer', {'decision_id':decision['decision_id'], **payload}, True)
            if response.get('error',{}).get('code')=='pane_user_active':
                return None
            if 'error' in response:
                raise RuntimeError(response)
            return response
        return wait_for('answer delivery', deliver)
    try:
        wait_for('isolated socket', endpoint.exists, 10)
        workspaces = request('workspace.list', {})['result']['workspaces']
        if not workspaces:
            created = request('workspace.create', {'cwd':os.getcwd(), 'focus':True})['result']
            pane = created['root_pane']['pane_id']
        else:
            pane = request('pane.list', {'workspace_id':workspaces[0]['workspace_id']})['result']['panes'][0]['pane_id']
        wait_for('shell', lambda:'CODEX_TEST>' in screen(), 10)
        command = 'exec '+shlex.quote(str(Path(a.codex).resolve()))+' -m gpt-6.1-sol -c model_reasoning_effort=medium'
        send_text(command)
        enter()
        wait_for('Codex startup', lambda:'GPT-6.1-Sol medium' in screen(), 30)
        save_screen('startup')
        send_text('/plan')
        enter()
        wait_for('Plan mode', lambda:'Plan mode' in screen(), 10)
        prompt = 'Use request_user_input now with two questions in one call: Report format? with Plain text and JSON options; Scope? with Small and Full options. This is a UI acceptance test. Use no other tools. After answers respond with EXACT_ANSWERS followed by the exact received answers as JSON and stop.'
        send_text(prompt)
        enter()
        time.sleep(.5)
        enter()
        first = wait_for('first question', lambda:pending('Question 1/2'), 120)
        save_screen('question-one')
        assert first['kind']=='agent_prompt' and first['allow_text'], first
        assert len(first['options'])==2, first
        assert 'Use request_user_input' not in first['title'], first
        identity = first['decision_id']
        request('pane.send_keys', {'pane_id':pane, 'keys':['Down']})
        second_read = wait_for('same question after cursor', lambda:pending('Question 1/2'))
        assert second_read['decision_id']==identity, second_read
        save_screen('question-cursor')
        chosen = first['options'][0]
        answer(first, option_id=chosen['id'])
        save_screen('after-number-answer')
        second = wait_for('second question', lambda:pending('Question 2/2'), 30)
        save_screen('question-two')
        custom = 'Exact custom scope 42'
        answer(second, text=custom)
        save_screen('after-free-answer')
        def echo_ready():
            value = request('pane.read', {'pane_id':pane, 'source':'recent_unwrapped', 'strip_ansi':True})['result']['read']['text']
            match = re.search(r'(?m)^\s*•\s+EXACT_ANSWERS\b', value)
            if match and custom in value[match.start():] and chosen['label'] in value[match.start():]:
                return value
            return None
        text = wait_for('Codex assistant answer echo', echo_ready, 120)
        (out/'echo.txt').write_text(text)
        (out/'proof.json').write_text(json.dumps({'pass':True, 'first':first, 'second':second, 'chosen':chosen, 'custom':custom, 'codex':str(Path(a.codex).resolve()), 'ikesu':str(Path(a.ikesu).resolve()), 'clipboard_sandbox':True},ensure_ascii=False,indent=2)+'\n')
    finally:
        if pane is not None:
            try:
                save_screen('final')
            except (OSError, RuntimeError):
                pass
        if endpoint.exists():
            request('server.stop', {}, True)
        try:
            server.wait(timeout=10)
        except subprocess.TimeoutExpired:
            server.terminate()
            server.wait(timeout=10)
        log.close()
