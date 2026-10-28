"""Observe binaries through isolated real macOS PTYs.

Usage: python3 tests/parity/multi_client_size.py BINARY OUTPUT [extra]
Run on Mac mini; no running user server or clipboard is accessed.
"""
import os, sys, json, subprocess, pty, fcntl, termios, struct, threading, time, pathlib, tempfile, shutil, signal
binary = pathlib.Path(sys.argv[1]).resolve()
out = pathlib.Path(sys.argv[2]).resolve()
out.mkdir(parents=True, exist_ok=True)
root = pathlib.Path(tempfile.mkdtemp(prefix='mcs-', dir='/tmp'))
(out / 'isolation-path.txt').write_text(str(root))
for p in ['home', 'config', 'state', 'data', 'zdot']:
    (root / p).mkdir(exist_ok=True)
(root / 'zdot/.zshrc').write_text("PS1='READY> '\nunset HISTFILE\n")
cfg = 'onboarding=false\n[ui.sound]\nenabled=false\n[ui.toast]\ndelivery="off"\n[session]\nresume_agents_on_restore=false\n'
(root / 'config/herdr').mkdir(exist_ok=True)
(root / 'config/herdr/config.toml').write_text(cfg)
(root / 'config' / binary.name).mkdir(exist_ok=True)
(root / 'config' / binary.name / 'config.toml').write_text(cfg)
(root / 'ikesu.toml').write_text(cfg)
env = {k: v for k, v in os.environ.items() if not k.startswith(('IKESU', 'HERDR', 'TMUX', 'CODEX', 'CLAUDE'))}
env.update(HOME=str(root / 'home'), XDG_CONFIG_HOME=str(root / 'config'), XDG_STATE_HOME=str(root / 'state'), XDG_DATA_HOME=str(root / 'data'), ZDOTDIR=str(root / 'zdot'), SHELL='/bin/zsh', TERM='xterm-256color', IKESU_CONFIG=str(root / 'ikesu.toml'), IKESU_STATE_DIR=str(root / 'state/i'))
launch = ['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny mach-lookup (global-name "com.apple.pasteboard.1"))', str(binary), '--session', 'mcsize']

def cli(*args):
    p = subprocess.run(launch + list(args), env=env, cwd=root, text=True, capture_output=True, timeout=15)
    (out / 'cli.jsonl').open('a').write(json.dumps(dict(args=args, code=p.returncode, stdout=p.stdout, stderr=p.stderr)) + '\n')
    if p.returncode:
        raise RuntimeError((args, p.stdout, p.stderr))
    try:
        return json.loads(p.stdout)
    except json.JSONDecodeError:
        return p.stdout
clients = []

def attach(w, h):
    m, s = pty.openpty()
    fcntl.ioctl(s, termios.TIOCSWINSZ, struct.pack('HHHH', h, w, 0, 0))

    def terminal():
        os.setsid()
        fcntl.ioctl(s, termios.TIOCSCTTY, 0)
    p = subprocess.Popen(launch, stdin=s, stdout=s, stderr=s, env=env, cwd=root, preexec_fn=terminal)
    os.close(s)
    raw = (out / f'client{len(clients)}.raw').open('wb')

    def drain():
        try:
            while True:
                raw.write(os.read(m, 65536))
                raw.flush()
        except OSError:
            pass
    threading.Thread(target=drain, daemon=True).start()
    clients.append((p, m))
    time.sleep(1.5)
    os.write(m, b'\r')
    time.sleep(0.5)
    return len(clients) - 1
rows = []
panes = []

def sample(label):
    sizes = {}
    for pid in panes:
        f = root / (pid + '.sizes')
        initial = len(f.read_text().splitlines()) if f.exists() else 0
        deadline = time.monotonic() + 15
        while True:
            lines = f.read_text().splitlines() if f.exists() else []
            if len(lines) >= initial + 3 and len(set(lines[-3:])) == 1:
                break
            if time.monotonic() >= deadline:
                raise RuntimeError(('unstable PTY measurement', label, pid, lines[-3:]))
            time.sleep(0.02)
        sizes[pid] = lines[-1]
    row = dict(phase=label, sizes=sizes)
    rows.append(row)
    (out / 'observations.json').write_text(json.dumps(rows, indent=2))
    print(json.dumps(row), flush=True)

def result(x):
    return x.get('result', x)

def plist():
    return result(cli('pane', 'list'))['panes']

def watch(pid):
    panes.append(pid)
    cli('pane', 'run', pid, f'while :; do stty size >> {root}/{pid}.sizes; sleep 0.1; done')

def send(c, b):
    (out / 'input.jsonl').open('a').write(json.dumps(dict(client=c, data=b.hex())) + '\n')
    os.write(clients[c][1], b)
try:
    if len(sys.argv) > 3 and sys.argv[3] == 'extra':
        a = attach(196, 50)
        watch(plist()[0]['pane_id'])
        sample('A-only')
        b = attach(82, 30)
        sample('B-connect')
        c = attach(140, 40)
        sample('C-connect')
        send(b, b'x')
        sample('B-last-key')
        send(b, b'\x02q')
        clients[b][0].wait(timeout=10)
        sample('B-detached-two-remain')
        send(a, b'\x1b[<35;40;10M')
        sample('A-hover-after-detach')
        send(c, b'\x02q')
        clients[c][0].wait(timeout=10)
        sample('C-detached-one-remains')
        b = attach(82, 30)
        sample('B-reconnect')
        cli('tab', 'create', '--no-focus')
        time.sleep(0.6)
        for p in plist():
            if p['pane_id'] not in panes:
                watch(p['pane_id'])
        sample('hidden-tab-created')
        send(b, b'\x02n')
        sample('B-select-second-tab')
        send(a, b'x')
        sample('A-key-other-tab')
        fcntl.ioctl(clients[a][1], termios.TIOCSWINSZ, struct.pack('HHHH', 55, 210, 0, 0))
        os.kill(clients[a][0].pid, signal.SIGWINCH)
        sample('A-resize-other-tab')
        send(b, b'x')
        sample('B-key-own-tab')
        send(a, b'\x02n')
        sample('A-select-second-tab')
        send(a, b'\x1b[<0;40;10M')
        sample('A-press')
        send(a, b'\x1b[<0;40;10m')
        sample('A-release')
    else:
        a = attach(196, 50)
        watch(plist()[0]['pane_id'])
        sample('A-only')
        b = attach(82, 30)
        sample('B-connect')
        events = [('hover', b'\x1b[<35;40;10M'), ('press', b'\x1b[<0;40;10M'), ('release', b'\x1b[<0;40;10m'), ('wheel', b'\x1b[<64;40;10M'), ('key', b'x'), ('paste', b'\x1b[200~abc\x1b[201~'), ('focus-in', b'\x1b[I'), ('focus-out', b'\x1b[O')]

        def series(prefix):
            for name, event in events:
                send(b, b'x')
                sample(prefix + '-B-reset-' + name)
                send(a, event)
                sample(prefix + '-A-' + name)
        series('two')
        c = attach(140, 40)
        sample('C-connect')
        series('three')
        cli('pane', 'split', panes[0], '--direction', 'right', '--no-focus')
        time.sleep(0.6)
        for p in plist():
            if p['pane_id'] not in panes:
                watch(p['pane_id'])
        sample('split')
        series('split-three')
        cli('tab', 'create', '--no-focus')
        time.sleep(0.6)
        for p in plist():
            if p['pane_id'] not in panes:
                watch(p['pane_id'])
        cli('workspace', 'create', '--no-focus')
        time.sleep(0.6)
        for p in plist():
            if p['pane_id'] not in panes:
                watch(p['pane_id'])
        sample('hidden-created')
        series('hidden')
        fcntl.ioctl(clients[a][1], termios.TIOCSWINSZ, struct.pack('HHHH', 55, 210, 0, 0))
        os.kill(clients[a][0].pid, signal.SIGWINCH)
        sample('A-resize')
        send(b, b'x')
        sample('B-key-after-resize')
        send(b, b'\x02q')
        clients[b][0].wait(timeout=10)
        sample('B-disconnect-two-remain')
        send(a, b'\x1b[<35;40;10M')
        sample('A-hover-after-disconnect')
        send(c, b'\x02q')
        clients[c][0].wait(timeout=10)
        sample('C-disconnect-one-remains')
finally:
    for p, m in clients:
        if p.poll() is None:
            os.write(m, b'\x02q')
            p.wait(timeout=10)
    try:
        cli('server', 'stop')
    except Exception as e:
        raise RuntimeError('isolated server cleanup failed') from e
    for p, m in clients:
        p.wait(timeout=10)
        os.close(m)
    for f in root.rglob('*'):
        if f.is_file() and (not f.is_socket()):
            dest = out / 'isolated' / f.relative_to(root)
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(f, dest)
    shutil.rmtree(root)
