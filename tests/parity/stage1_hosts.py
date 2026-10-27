#!/usr/bin/env python3
"""Observe real tmux ancestry and the documented WezTerm host identifiers."""
import argparse
import os
from pathlib import Path
import shlex
import subprocess
import tempfile

p = argparse.ArgumentParser()
p.add_argument('--herdr', required=True)
p.add_argument('--ikesu',dest='ikesu', required=True)
p.add_argument('--ptyrun', required=True)
p.add_argument('--config-file', required=True)
p.add_argument('--out', required=True)
a = p.parse_args()
here = Path(__file__).resolve().parent
for host in ['plain', 'ghostty', 'wezterm', 'wezterm-pane', 'tmux']:
    for mode in ['local', 'server']:
        env = dict(os.environ)
        for key in ['TERM_PROGRAM', 'TMUX', 'WEZTERM_PANE', 'WEZTERM_EXECUTABLE', 'WEZTERM_UNIX_SOCKET']:
            env.pop(key, None)
        if host in ['ghostty', 'wezterm']:
            env['TERM_PROGRAM'] = {'ghostty': 'ghostty', 'wezterm': 'WezTerm'}[host]
        if host == 'wezterm-pane':
            env['WEZTERM_PANE'] = '0'
        command = ['python3', str(here/'observe.py'), '--herdr', a.herdr,
                   '--ikesu', a.ikesu, '--ptyrun', a.ptyrun, '--config-file', a.config_file,
                   '--steps', str(here/'stage1-host.steps'),
                   '--out', str(Path(a.out)/host/mode), '--clipboard-sandbox']
        if mode == 'server':
            command.append('--server')
        print(host, mode, flush=True)
        if host != 'tmux':
            subprocess.run(command, env=env, check=True)
            continue
        # A private tmux server supplies a real TMUX environment and ancestry.
        with tempfile.TemporaryDirectory(prefix='stage1-tmux-', dir='/tmp') as temporary:
            socket = str(Path(temporary)/'socket')
            status = Path(temporary)/'status'
            tmux = ['/opt/homebrew/bin/tmux', '-S', socket, '-f', '/dev/null']
            script = shlex.join(command) + '; code=$?; printf "%s" "$code" > ' + shlex.quote(str(status))
            script += '; ' + shlex.join(tmux + ['wait-for', '-S', 'finished'])
            try:
                subprocess.run(tmux + ['new-session', '-d', '-s', 'stage1', script], env=env, check=True)
                subprocess.run(tmux + ['wait-for', 'finished'], env=env, check=True)
                if status.read_text() != '0':
                    raise RuntimeError('tmux observation failed: ' + status.read_text())
            finally:
                subprocess.run(tmux + ['kill-server'], env=env, check=False, capture_output=True)
