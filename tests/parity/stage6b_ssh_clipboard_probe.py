#!/usr/bin/env python3
"""One pbpaste read in the same launch environment as observe.py's herdr."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

from stage6b_mini_guard import require_mini_gui

require_mini_gui()

payload = Path(sys.argv[1]).read_bytes()
proof_path = Path(sys.argv[2])
with tempfile.TemporaryDirectory(prefix='parity-', dir='/tmp') as directory:
    root = Path(directory)
    for name in ('home','config','state','data','zdot','cwd'):
        (root/name).mkdir()
    env = {k:v for k,v in os.environ.items() if not k.startswith(('HERDR','IKESU','CLAUDE','CODEX'))}
    work = Path.home()/'bench-terrace.noindex/work/detail'
    env.update(HOME=str(root/'home'), ZDOTDIR=str(root/'zdot'),
        XDG_CONFIG_HOME=str(root/'config'), XDG_STATE_HOME=str(root/'state'),
        XDG_DATA_HOME=str(root/'data'), HERDR_STATE_DIR=str(root/'state/h'),
        IKESU_STATE_DIR=str(root/'state/g'), SHELL='/bin/zsh', TERM='xterm-256color',
        IKESU_DISABLE_SOUND='1', HERDR_CONFIG_PATH=str(root/'config/herdr/config.toml'),
        PARITY_OBSERVE_BINARY=str(work/'herdr-0.2.57'), PARITY_BINARY_PATH=str(work/'herdr-0.2.57'))
    config = root/'config/herdr/config.toml'
    config.parent.mkdir(parents=True)
    config.write_text((work/'user-config.toml').read_text().replace('[keys]\n','[keys]\ngoto="prefix+o"\n')+'\n[ui.sound]\nenabled=false\n')
    read = subprocess.run(['/usr/bin/pbpaste'], cwd=root/'home', env=env, capture_output=True)
    proof = {'command':['/usr/bin/pbpaste'], 'reads':1, 'exit':read.returncode,
        'expected_sha256':hashlib.sha256(payload).hexdigest(), 'actual_sha256':hashlib.sha256(read.stdout).hexdigest(),
        'exact_payload':read.stdout==payload, 'bytes':len(read.stdout), 'stderr':read.stderr.decode(errors='replace'),
        'environment_source':'observe.py herdr launch environment, including isolated HOME/XDG/ZDOTDIR/config/cwd'}
    proof_path.write_text(json.dumps(proof,indent=2)+'\n')
    print(json.dumps(proof,indent=2))
