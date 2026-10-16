#!/usr/bin/env python3
"""Observe the actual tb on mini through an isolated saved-machine viewer."""
import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import select
import shlex
import socket
import subprocess
import tempfile
import time
import uuid
import tb_pointer_probe

p = argparse.ArgumentParser()
p.add_argument('--product', choices=('herdr', 'gtx'), required=True)
p.add_argument('--binary', type=Path, required=True)
p.add_argument('--remote-binary', required=True)
p.add_argument('--remote-tb', required=True)
p.add_argument('--ptyrun', type=Path, required=True)
p.add_argument('--out', type=Path, required=True)
p.add_argument('--measure', action='store_true', help='measure scrolling, switch display, and real PTY key delivery')
p.add_argument('--capture-ssh', action='store_true', help='capture actual plaintext display-transport bytes; diagnostic only')
p.add_argument('--image-probe', action='store_true', help='controlled Kitty producer for transfer contracts; does not measure tb performance')
p.add_argument('--status-delay', help='explicit ptyrun terminal-status response delay, or drop')
p.add_argument('--pixel-mouse', action='store_true', help='let the outer terminal negotiate SGR pixel mouse instead of explicitly declining it')
p.add_argument('--tap-tb-input', action='store_true', help='record input delivered to the actual remote tb PTY')
p.add_argument('--visibility', action='store_true', help='record real saved-machine graphics visibility across split, focus and zoom')
p.add_argument('--pointer-probe', action='store_true', help='verify real link hover and horizontal scroll through page DOM event reports')
p.add_argument('--adjacent-input', action='store_true', help='click a real adjacent saved-machine pane and record its immediate key input')
p.add_argument('--resize-wait', action='store_true', help='hold real SSH output across resize and inspect the pending display')
p.add_argument('--capture-file-images', action='store_true', help='requires ptyrun file-image recording; read real files before viewer cleanup')
p.add_argument('--switch-hold', choices=('expiry', 'disconnect'), help='hold real destination output while typing, then exceed the three-second input limit or disconnect')
a = p.parse_args()
if a.image_probe and a.measure:
    p.error('the controlled image producer cannot be used for tb performance measurement')
if a.tap_tb_input and (a.image_probe or not (a.measure or a.pointer_probe or a.adjacent_input or a.switch_hold)):
    p.error('the input tap requires an actual tb measurement or pointer probe')
if a.visibility and (a.measure or a.image_probe):
    p.error('visibility operations must be separate from timing and controlled-image measurements')
if a.pointer_probe and (a.measure or a.image_probe or a.visibility):
    p.error('pointer probe must be separate from other observation modes')
if a.adjacent_input and (a.measure or a.image_probe or a.visibility or a.pointer_probe):
    p.error('adjacent-pane input must be separate from other observation modes')
if a.resize_wait and (a.measure or a.image_probe or a.visibility or a.pointer_probe or a.adjacent_input or not a.capture_ssh):
    p.error('resize wait requires SSH capture and must be separate from other observation modes')
if a.switch_hold and (a.measure or a.image_probe or a.visibility or a.pointer_probe or a.adjacent_input or a.resize_wait
                      or not a.capture_ssh or not a.tap_tb_input):
    p.error('switch hold requires SSH capture and actual tb input tap, separately from other modes')
out = a.out.resolve()
out.mkdir(parents=True, exist_ok=True)
ssh_config = out/'ssh-config'
control_path = '/tmp/tb-ssh-'+uuid.uuid4().hex[:12]
ssh_config.write_text('Host *\n  ControlMaster auto\n  ControlPersist yes\n  ControlPath '+control_path+
                      '\n  ConnectionAttempts 3\nInclude "'+str(Path.home()/'.ssh/config')+'"\n')
ssh_args = ['/usr/bin/ssh', '-F', str(ssh_config)]
observer_source = Path(__file__).read_bytes()
(out/'observer-source.py').write_bytes(observer_source)
binary = str(a.binary.resolve())
version = subprocess.check_output([binary, '--version'], text=True).strip()
if a.product == 'herdr' and version != 'herdr 0.2.57':
    raise SystemExit(f'reference version changed: {version}')
remote = '/Users/kazuph/bench-terrace.noindex/work/tb-optimize/r-'+uuid.uuid4().hex[:6]
config = 'onboarding=false\n[experimental]\nkitty_graphics=true\nallow_nested=true\n[session]\nresume_agents_on_restore=false\n[remote]\nmanage_ssh_config=false\n'
expected_keys = 'abcdefghijklmnop'
(out/'inputs.json').write_text(json.dumps({'product': a.product, 'binary': binary, 'version': version,
                                                       'observer_sha256': hashlib.sha256(observer_source).hexdigest(),
                                                       'ptyrun_sha256': hashlib.sha256(a.ptyrun.read_bytes()).hexdigest(),
                                                       'status_delay': a.status_delay,
                                                       'pixel_mouse': a.pixel_mouse,
                                                       'tap_tb_input': a.tap_tb_input,
                                                       'visibility': a.visibility,
                                                       'pointer_probe': a.pointer_probe,
                                                       'adjacent_input': a.adjacent_input,
                                                       'resize_wait': a.resize_wait,
                                                       'capture_file_images': a.capture_file_images,
                                                       'switch_hold': a.switch_hold,
                                                       'sha256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
                                                       'remote_root': remote, 'remote_binary': a.remote_binary,
                                                       'remote_tb': a.remote_tb, 'measure': a.measure, 'capture_ssh': a.capture_ssh, 'image_probe': a.image_probe, 'expected_keys': expected_keys if a.measure or a.adjacent_input or a.switch_hold else None}, indent=2)+'\n')

RPC = '''import json,pathlib,socket,sys
path,method,params=sys.argv[1:4]
assert path.startswith('/Users/kazuph/bench-terrace.noindex/work/tb-optimize/r-')
matches=list(pathlib.Path(path).glob('**/'+sys.argv[4]))
assert len(matches)==1,matches
with socket.socket(socket.AF_UNIX) as c:
 c.settimeout(10);c.connect(str(matches[0]))
 c.sendall((json.dumps(dict(id='saved-observe',method=method,params=json.loads(params)))+'\\n').encode())
 print(c.makefile('r').readline(),end='')
'''


def ssh(command, **kwargs):
    return subprocess.run(ssh_args+['-o', 'BatchMode=yes', 'mini', command], check=True, **kwargs)


def local_rpc(path, method, params):
    with socket.socket(socket.AF_UNIX) as c:
        c.settimeout(10)
        c.connect(str(path))
        c.sendall((json.dumps(dict(id='saved-observe', method=method, params=params))+'\n').encode())
        return json.loads(c.makefile('r').readline())


def remote_rpc(method, params):
    command = shlex.join(['python3', '-c', RPC, remote, method, json.dumps(params), a.product+'.sock'])
    response = ssh(command, capture_output=True, text=True)
    return json.loads(response.stdout)


def result(reply):
    if 'error' in reply:
        raise RuntimeError(reply)
    return reply['result']


def wait_for_file(path, viewer):
    with closing(select.kqueue()) as events:
        fd = os.open(path.parent, os.O_RDONLY)
        try:
            events.control([select.kevent(fd, filter=select.KQ_FILTER_VNODE, flags=select.KQ_EV_ADD,
                                          fflags=select.KQ_NOTE_WRITE)], 0, 0)
            deadline = time.monotonic()+10
            while not path.exists():
                if viewer.poll() is not None or time.monotonic() >= deadline:
                    raise RuntimeError('saved-machine stage was not captured: '+path.name)
                events.control(None, 1, max(0, deadline-time.monotonic()))
        finally:
            os.close(fd)


lock_log = (out/'lock.log').open('w')
lock = subprocess.Popen(ssh_args+['-T', 'mini', '~/bench-terrace.noindex/work/perf-lock.py python3 -c '+
                         shlex.quote("import sys;print('TB_LOCK_READY',flush=True);sys.stdin.read()")],
                        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=lock_log, text=True)
try:
    for line in lock.stdout:
        lock_log.write(line)
        lock_log.flush()
        if line.strip() == 'TB_LOCK_READY':
            break
    else:
        raise RuntimeError('mini lock was not acquired')
    with (out/'mini-load.log').open('w') as load_log:
        ssh('uptime', stdout=load_log)
    distribution = ssh(shlex.join(['python3', '-c',
                                  'import json,pathlib,subprocess,sys;print(json.dumps({"binary_version":subprocess.check_output([sys.argv[1],"--version"],text=True).strip(),"tb_version":(pathlib.Path(sys.argv[2])/"VERSION").read_text().strip()}))',
                                  a.remote_binary, a.remote_tb]), capture_output=True, text=True)
    (out/'remote-distribution.json').write_text(distribution.stdout)
    if a.product == 'herdr' and json.loads(distribution.stdout)['binary_version'] != version:
        raise RuntimeError('main and mini reference versions differ')
    with tempfile.TemporaryDirectory(prefix='tb-saved-', dir='/tmp') as temporary:
        root = Path(temporary)
        for name in ('home', 'config', 'state/t', 'data', 'bin'):
            (root/name).mkdir(parents=True, exist_ok=True)
        (root/'config.toml').write_text(config)
        # The observer owns this configuration and master; existing identities
        # and host settings are read through Include without changing them.
        (root/'home/.ssh').mkdir(mode=0o700)
        (root/'home/.ssh/config').write_text(ssh_config.read_text())
        (root/'home/.zshrc').write_text("PS1='TB_LOCAL> '\nunset HISTFILE\n")
        remote_env = {'HOME': remote+'/home', 'ZDOTDIR': remote+'/home',
                      'XDG_CONFIG_HOME': remote+'/c', 'XDG_STATE_HOME': remote+'/s',
                      'XDG_DATA_HOME': remote+'/data', 'HERDR_STATE_DIR': remote+'/s',
                      'GTX_STATE_DIR': remote+'/s', 'HERDR_CONFIG_PATH': remote+'/config.toml',
                      'GTX_CONFIG': remote+'/config.toml', 'SHELL': '/bin/zsh'}
        prefix = ''.join('export '+key+'='+shlex.quote(value)+'; ' for key, value in remote_env.items())
        prefix += 'export PATH='+shlex.quote(remote+'/bin')+':"$PATH"; '
        setup = '''import json,pathlib,sys
r=pathlib.Path(sys.argv[1]);cfg=sys.argv[2];wrapper=sys.argv[3]
r.mkdir(mode=0o700)
for d in ['home','c','s/t','data','bin']: (r/d).mkdir(parents=True,exist_ok=True)
(r/'config.toml').write_text(cfg)
(r/'home/.zshrc').write_text("PS1='TB_REMOTE> '\\nunset HISTFILE\\n")
(r/'page.html').write_text('<!doctype html><meta charset="utf-8"><title>TB_PARITY_PAGE</title><h1>Terminal browser parity page</h1>'+''.join('<p>Scroll row '+str(i)+' — same page for both products.</p>' for i in range(200)))
for name in ['herdr','gtx']:
 p=r/'bin'/name;p.write_text(wrapper);p.chmod(0o700)
'''
        wrapper = '#!/bin/sh\n'+prefix+'exec '+shlex.quote(a.remote_binary)+' "$@"\n'
        ssh(shlex.join(['python3', '-c', setup, remote, config, wrapper]))
        if a.pointer_probe:
            ssh(shlex.join(['python3', '-c', 'import pathlib,sys;pathlib.Path(sys.argv[1]).write_text(sys.argv[2])',
                            remote+'/pointer-probe.py', Path(__file__).with_name('tb_pointer_probe.py').read_text()]))
        if a.image_probe:
            ssh(shlex.join(['python3', '-c', 'import pathlib,sys;pathlib.Path(sys.argv[1]).write_text(sys.argv[2])',
                            remote+'/image-probe.py', Path(__file__).with_name('tb_transfer_program.py').read_text()]))
        if a.tap_tb_input:
            ssh(shlex.join(['python3', '-c', 'import pathlib,sys;pathlib.Path(sys.argv[1]).write_text(sys.argv[2])',
                            remote+'/tb-input-tap.py', Path(__file__).with_name('tb_input_tap.py').read_text()]))
        ssh_wrapper = root/'bin/ssh'
        ssh_wrapper.write_text('#!/usr/bin/python3\nimport os,sys\na=sys.argv[1:]\n'
                               "i=a.index('mini')\n"+
                               'a[i+1:]=['+repr(prefix)+'+" ".join(a[i+1:])]\n'
                               'os.execv("/usr/bin/ssh",'+repr(ssh_args)+'+a)\n')
        ssh_wrapper.chmod(0o700)
        env = {k: v for k, v in os.environ.items() if not k.startswith(('HERDR', 'GTX', 'GOTERRACE', 'CODEX', 'CLAUDE'))}
        env.update(HOME=str(root/'home'), ZDOTDIR=str(root/'home'), XDG_CONFIG_HOME=str(root/'config'),
                   XDG_STATE_HOME=str(root/'state'), XDG_DATA_HOME=str(root/'data'),
                   HERDR_STATE_DIR=str(root/'state'), GTX_STATE_DIR=str(root/'state'),
                   HERDR_CONFIG_PATH=str(root/'config.toml'), GTX_CONFIG=str(root/'config.toml'),
                   SHELL='/bin/zsh', TERM='xterm-256color', PATH=str(root/'bin')+':'+os.environ['PATH'],
                   HERDR_MACHINE_REMOTE_BINARY=remote+'/bin/herdr', GTX_MACHINE_REMOTE_BINARY=remote+'/bin/gtx')
        if a.capture_ssh:
            ssh_wrapper.write_bytes(Path(__file__).with_name('tb_ssh_capture.py').read_bytes())
            env.update(TB_SSH_OBSERVER_PREFIX=prefix, TB_SSH_CAPTURE_ROOT=str(out/'ssh'), TB_SSH_CONFIG=str(ssh_config))
            if a.resize_wait or a.switch_hold:
                env.update(TB_SSH_HOLD_MARKER=str(root/'hold-remote-output.json'),
                           TB_SSH_RELEASE_MARKER=str(root/'release-remote-output'))
                if a.switch_hold == 'disconnect':
                    env['TB_SSH_ABORT_MARKER'] = str(root/'disconnect-remote-output')
        owned, logs, cleanup = [], [], []
        remote_pane = ''
        local_pane = ''
        local_socket = root/'config'/'unbound.sock'
        try:
            for name, command, child_env in [
                    ('local-server', [binary, '--session', 't', 'server'], env),
                    ('remote-server', ssh_args+['-T', 'mini', prefix+shlex.join([a.remote_binary, '--session', 't', 'server'])], None)]:
                log = (out/(name+'.log')).open('w'); logs.append(log)
                owned.append(subprocess.Popen(command, env=child_env, stdout=log, stderr=subprocess.STDOUT))
            with closing(select.kqueue()) as events:
                fd = os.open(root/'config', os.O_RDONLY)
                try:
                    events.control([select.kevent(fd, filter=select.KQ_FILTER_VNODE, flags=select.KQ_EV_ADD,
                                                  fflags=select.KQ_NOTE_WRITE)], 0, 0)
                    deadline = time.monotonic()+15
                    while not local_socket.exists():
                        matches = list(root.glob('**/'+a.product+'.sock'))
                        if len(matches) == 1:
                            local_socket = matches[0]
                            break
                        if owned[0].poll() is not None or time.monotonic() >= deadline:
                            raise RuntimeError('owned local server did not bind its socket')
                        events.control(None, 1, min(.1, max(0, deadline-time.monotonic())))
                finally:
                    os.close(fd)
            deadline = time.monotonic()+15
            while True:
                try:
                    result(remote_rpc('workspace.list', {}))
                    break
                except subprocess.CalledProcessError:
                    if owned[1].poll() is not None or time.monotonic() >= deadline:
                        raise RuntimeError('owned remote server did not bind its socket')
            for request, label in [(lambda method, params: local_rpc(local_socket, method, params), 'TB_LOCAL'),
                                   (remote_rpc, 'TB_REMOTE')]:
                created = result(request('workspace.create', {'cwd': remote if label == 'TB_REMOTE' else str(root/'home'),
                                                              'label': label, 'focus': True}))
                workspace = created['workspace']['workspace_id']
                for ws in result(request('workspace.list', {}))['workspaces']:
                    if ws['workspace_id'] != workspace:
                        result(request('workspace.close', {'workspace_id': ws['workspace_id']}))
                if label == 'TB_REMOTE':
                    remote_pane = created['root_pane']['pane_id']
                else:
                    local_pane = created['root_pane']['pane_id']
            if a.measure or a.switch_hold:
                receiver = root/'key-receiver.py'
                receiver.write_bytes(Path(__file__).with_name('tb_key_program.py').read_bytes())
                receiver_command = shlex.join(['python3', str(receiver), '--out', str(root/'keys.json')])
                result(local_rpc(local_socket, 'pane.send_input', {'pane_id': local_pane, 'text': receiver_command, 'keys': ['Enter']}))
            if a.adjacent_input:
                receiver = remote+'/home/key-receiver.py'
                ssh(shlex.join(['python3', '-c', 'import pathlib,sys;pathlib.Path(sys.argv[1]).write_text(sys.argv[2])',
                                receiver, Path(__file__).with_name('tb_key_program.py').read_text()]))
                adjacent = result(remote_rpc('pane.split', {'target_pane_id': remote_pane, 'direction': 'right', 'focus': False}))['pane']['pane_id']
                (out/'adjacent-pane.json').write_text(json.dumps({'tb': remote_pane, 'receiver': adjacent})+'\n')
                receiver_command = shlex.join(['python3', receiver, '--out', remote+'/home/adjacent-keys.json'])
                result(remote_rpc('pane.send_input', {'pane_id': adjacent, 'text': receiver_command, 'keys': ['Enter']}))
            added = subprocess.run([binary, '--session', 't', 'machine', 'add', 'mini', '--label', 'tb-host', '--remote-session', 't'],
                                   env=env, capture_output=True, text=True, timeout=30)
            (out/'machine-add.log').write_text(added.stdout+added.stderr)
            added.check_returncode()
            (out/'machine-add.json').write_text(added.stdout)
            command = shlex.join([a.remote_tb+'/bin/terminal-browser', 'file://'+remote+'/page.html', '--no-merge',
                                  '--no-toolbar', '--no-shortcuts', '--no-context-menu', '--no-overlays', '--no-frame'])
            if a.image_probe:
                command = shlex.join(['python3', remote+'/image-probe.py'])
            if a.pointer_probe:
                command = shlex.join(['python3', remote+'/pointer-probe.py', a.remote_tb, remote])
            if a.tap_tb_input:
                command = shlex.join(['python3', remote+'/tb-input-tap.py', '--out', remote+'/home/tb-input.bin', '--'])+' '+command
            result(remote_rpc('pane.send_input', {'pane_id': remote_pane, 'text': command, 'keys': ['Enter']}))
            ready, raw = root/'image-ready', out/'terminal.raw'
            raw.touch()
            steps = root/'steps'
            steps.write_text(('waittext 20s LOCAL_SWITCH_READY\n' if a.measure or a.switch_hold else '')+'waittext 20s TB_REMOTE\ncells '+str(out/'pre-click.json')+'\nclicktext "TB_REMOTE"\nwaitfile '+str(ready)+' 30s\n'
                             'cells '+str(out/'tb-visible.json')+'\nstats tb-visible\nkill\n')
            if a.measure:
                procedure = steps.read_text().replace('kill\n', '')
                procedure += 'locate "TB_LOCAL"\nmark scroll-start\n'
                # P4b's scroll rate is scheduled against one clock, not relative sleeps.
                procedure += 'wheelburst 125 125 80 22\n'
                procedure += ('clicklocated "TB_LOCAL"\nsend '+json.dumps(expected_keys+'\r')+'\n'
                              'waittext 10s LOCAL_KEYS_RECORDED\ncells '+str(out/'switch-local.json')+'\n'
                              'stats switch-local\nmark measurement-end\nkill\n')
                steps.write_text(procedure)
            if a.image_probe:
                procedure = steps.read_text().replace('kill\n', '')+'waittext 10s TB_TRANSFER_INITIAL\nstats transfer-initial\n'
                for key, label in [('s', 'SMALL'), ('l', 'LARGE'), ('n', 'NOISE')]:
                    procedure += 'send '+json.dumps(key)+'\nwaittext 10s TB_TRANSFER_'+label+'\nstats transfer-'+label.lower()+'\n'
                steps.write_text(procedure+'kill\n')
            if a.pointer_probe:
                steps.write_text(steps.read_text().replace('kill\n', '')+tb_pointer_probe.procedure(root, a.pixel_mouse)+'kill\n')
            if a.adjacent_input:
                steps.write_text(steps.read_text().replace('kill\n', '')+
                                 'waittext 10s LOCAL_SWITCH_READY\nmouse down 120 22\nmouse up 120 22\nsend '+json.dumps(expected_keys+'\r')+'\n'
                                 'waittext 10s LOCAL_KEYS_RECORDED\ncells '+str(out/'adjacent-input.json')+'\nkill\n')
            if a.resize_wait:
                steps.write_text(steps.read_text().replace('kill\n', '')+
                                 'cells '+str(root/'hold-remote-output.json')+'\nmark resize-start\nresize 140 40\n'
                                 'waitfile '+str(out/'ssh/transport-held.json')+' 10s\n'
                                 'cells '+str(out/'pending-resize.json')+'\nstats pending-resize\n'
                                 'waitfile '+str(out/'ssh/transport-resumed.json')+' 10s\n'
                                 'cells '+str(out/'resumed-resize.json')+'\nstats resumed-resize\nkill\n')
            if a.switch_hold:
                steps.write_text(steps.read_text().replace('kill\n', '')+
                                 'clicktext "TB_LOCAL"\nwaittext 10s LOCAL_SWITCH_READY\nlocate "TB_REMOTE"\n'
                                 'cells '+str(root/'hold-remote-output.json')+'\nmark switch-hold-start\nclicklocated "TB_REMOTE"\n'
                                 'send '+json.dumps(expected_keys+'\r')+'\ncells '+str(out/'pending-input.json')+'\n'
                                 'waitfile '+str(root/'switch-hold-done')+' 10s\ncells '+str(out/'post-hold.json')+'\nstats switch-hold\nkill\n')
            (out/'steps').write_text(steps.read_text())
            log = (out/'viewer.log').open('w'); logs.append(log)
            viewer = subprocess.Popen([str(a.ptyrun.resolve()), '-cols', '160', '-rows', '45', '-cell-width', '8',
                                       '-cell-height', '16', '-xpixel', '1280', '-ypixel', '720', '-steps', str(steps),
                                       '-raw-output', str(raw), '-cell-clock', '-pixel-mouse='+str(a.pixel_mouse).lower(),
                                       *(['-file-images', str(out/'file-images.jsonl')] if a.capture_file_images else []),
                                       *(['-status-delay', a.status_delay] if a.status_delay is not None else []),
                                       '--', binary, '--session', 't'],
                                      env=env, stdout=log, stderr=subprocess.STDOUT)
            owned.append(viewer)
            with closing(select.kqueue()) as events:
                fd = os.open(raw, os.O_RDONLY)
                try:
                    events.control([select.kevent(fd, filter=select.KQ_FILTER_VNODE, flags=select.KQ_EV_ADD,
                                                  fflags=select.KQ_NOTE_WRITE)], 0, 0)
                    deadline = time.monotonic()+30
                    while b'\x1b_Ga=t,' not in raw.read_bytes():
                        if viewer.poll() is not None or time.monotonic() >= deadline:
                            raise RuntimeError('no actual tb image upload reached the saved-machine viewer')
                        events.control(None, 1, max(0, deadline-time.monotonic()))
                    (out/'remote-graphics-info.json').write_text(json.dumps(
                        remote_rpc('pane.graphics.info', {'pane_id': remote_pane}), indent=2)+'\n')
                    if a.visibility:
                        visibility = []

                        def sample(phase, panes):
                            for pane in panes:
                                visibility.append({'phase': phase, 'role': 'original' if pane == remote_pane else 'split',
                                                   'response': remote_rpc('pane.graphics.info', {'pane_id': pane})})
                            (out/'remote-visibility.json').write_text(json.dumps(visibility, indent=2)+'\n')

                        sample('single', [remote_pane])
                        split = result(remote_rpc('pane.split', {'target_pane_id': remote_pane, 'direction': 'right', 'focus': False}))['pane']['pane_id']
                        sample('split-original-focused', [remote_pane, split])
                        result(remote_rpc('pane.focus', {'pane_id': split}))
                        sample('split-new-focused', [remote_pane, split])
                        result(remote_rpc('pane.zoom', {'pane_id': split, 'mode': 'on'}))
                        sample('zoom-new', [remote_pane, split])
                        result(remote_rpc('pane.zoom', {'pane_id': split, 'mode': 'off'}))
                        result(remote_rpc('pane.focus', {'pane_id': remote_pane}))
                        sample('restored', [remote_pane, split])
                    if a.pointer_probe:
                        tb_pointer_probe.wait_ready(ssh, remote)
                    ready.write_text('actual image upload received')
                finally:
                    os.close(fd)
            if a.pointer_probe:
                tb_pointer_probe.collect(ssh, remote, root, out, viewer, a.pixel_mouse)
            if a.resize_wait:
                wait_for_file(out/'pending-resize.json', viewer)
                (root/'release-remote-output').write_text('pending display captured; release real SSH data')
            if a.switch_hold:
                wait_for_file(out/'pending-input.json', viewer)
                wait_for_file(out/'ssh/transport-held.json', viewer)
                captured = json.loads((out/'pending-input.json').read_text())['CapturedUnixMs']
                if a.switch_hold == 'expiry':
                    # Stage8 T5 specifies a three-second input lifetime. Wait
                    # from the post-input capture so release is beyond it.
                    deadline = captured/1000+3
                    with closing(select.kqueue()) as timer:
                        while time.time() < deadline:
                            timer.control(None, 1, max(0, deadline-time.time()))
                else:
                    (root/'disconnect-remote-output').write_text('disconnect this observer-owned SSH transport')
                (root/'release-remote-output').write_text('release destination after the required failure condition')
                wait_for_file(out/('ssh/transport-resumed.json' if a.switch_hold == 'expiry' else 'ssh/transport-disconnected.json'), viewer)
                if a.switch_hold == 'disconnect':
                    # Include reconnection during the existing input lifetime,
                    # rather than ending the observation immediately at EOF.
                    deadline = captured/1000+3
                    with closing(select.kqueue()) as timer:
                        while time.time() < deadline:
                            timer.control(None, 1, max(0, deadline-time.time()))
                (out/'switch-hold-control.json').write_text(json.dumps({'mode': a.switch_hold, 'time_unix_ns': time.time_ns()})+'\n')
                (root/'switch-hold-done').write_text('actual destination transport condition completed')
            viewer.wait()
            if viewer.returncode != 0 or 'TIMEOUT' in (out/'viewer.log').read_text():
                raise RuntimeError('saved-machine viewer procedure failed')
            if a.measure:
                (out/'keys.json').write_bytes((root/'keys.json').read_bytes())
            if a.image_probe:
                probe = ssh(shlex.join(['cat', remote+'/home/image-probe.json']), capture_output=True, text=True)
                (out/'remote-image-probe.json').write_text(probe.stdout)

        finally:
            if a.adjacent_input:
                try:
                    keys = ssh(shlex.join(['cat', remote+'/home/adjacent-keys.json']), capture_output=True)
                    (out/'adjacent-keys.json').write_bytes(keys.stdout)
                except Exception as error:
                    cleanup.append({'adjacent_keys_error': str(error)})
            if (root/'keys.json').exists():
                (out/'keys.json').write_bytes((root/'keys.json').read_bytes())
            if a.tap_tb_input:
                try:
                    tapped = ssh(shlex.join(['cat', remote+'/home/tb-input.bin']), capture_output=True)
                    (out/'remote-tb-input.bin').write_bytes(tapped.stdout)
                except Exception as error:
                    cleanup.append({'tb_input_capture_error': str(error)})
            (out/'local-runtime-logs.json').write_text(json.dumps({str(p.relative_to(root)):p.read_text(errors='replace') for p in root.glob('**/*.log')}, indent=2)+'\n')
            try:
                snapshot = remote_rpc('pane.read', {'pane_id': remote_pane})
                (out/'remote-pane-final.json').write_text(json.dumps(snapshot, indent=2)+'\n')
            except Exception as error:
                cleanup.append({'pane_snapshot_error': str(error)})
            try:
                collected = ssh(shlex.join(['python3', '-c', "import json,pathlib,sys;r=pathlib.Path(sys.argv[1]);print(json.dumps({str(p.relative_to(r)):p.read_text(errors='replace') for p in r.glob('s/terminal-browser-*/logs/*.log')}))", remote]), capture_output=True, text=True)
                (out/'remote-tb-logs.json').write_text(collected.stdout)
            except Exception as error:
                cleanup.append({'tb_log_error': str(error)})
            if not a.image_probe:
                try:
                    shutdown = ssh(prefix+shlex.join([a.remote_tb+'/bin/terminal-browser', 'shutdown']),
                                   capture_output=True, text=True, timeout=20)
                    (out/'tb-shutdown.log').write_text(shutdown.stdout+shutdown.stderr)
                except Exception as error:
                    cleanup.append({'tb_shutdown_error': str(error)})
            for route in ('local', 'remote'):
                try:
                    reply = local_rpc(local_socket, 'server.stop', {}) if route == 'local' else remote_rpc('server.stop', {})
                    cleanup.append({'route': route, 'reply': reply})
                except Exception as error:
                    cleanup.append({'route': route, 'error': str(error)})
            for process in reversed(owned):
                if process is owned[-1] and len(owned) == 3 and process.poll() is None:
                    process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.terminate()
                    process.wait(timeout=10)
                    cleanup.append({'pid': process.pid, 'forced_termination': True})
            for log in logs:
                log.close()
            (out/'cleanup.json').write_text(json.dumps(cleanup, indent=2)+'\n')
finally:
    lock.stdin.close()
    try:
        lock.wait(timeout=15)
    except subprocess.TimeoutExpired:
        lock.terminate()
        lock.wait(timeout=10)
    lock.stdout.close()
    lock_log.close()
    closed = subprocess.run(ssh_args+['-O', 'exit', 'mini'], capture_output=True, text=True)
    (out/'ssh-master-close.log').write_text(closed.stdout+closed.stderr)
print('Saved-machine image procedure captured; real-terminal visual acceptance remains unverified.')
