#!/usr/bin/env python3
"""Real mini GUI pasteboard comparison, with all-format backup and restoration."""
import json
import os
from pathlib import Path
import subprocess
import sys

from stage6b_mini_guard import require_mini_gui

require_mini_gui()
root = Path(sys.argv[1]).resolve()
root.mkdir(parents=True, exist_ok=True)
source = Path(__file__).resolve().parent
work = Path.home()/'bench-terrace.noindex/work/detail'
config = root/'config.toml'
config.write_text((work/'user-config.toml').read_text().replace('[keys]\n','[keys]\ngoto="prefix+o"\n')+'\n[ui.sound]\nenabled=false\n')
def send(text):
    return 'send '+json.dumps(text, ensure_ascii=False)+'\nsleep 300ms\n'
def save(name):
    return f'screen @OUT@/{name}.txt\ncells @OUT@/{name}.json\n'
failed = False
for case, payload in [('plain','貼付 alpha'),('controls','al\tpha\x01\nbeta')]:
    out = root/case
    out.mkdir(exist_ok=True)
    data = out/'payload.txt'
    data.write_text(payload)
    steps = out/'native.steps'
    body = ''
    for keyname, key in [('command','\x1b[118;9u'),('control','\x16')]:
        if case == 'plain':
            body += send('\x14c')+send(key)+save('name-'+keyname)+send('\x1b')
            body += send('\x14o/base-')+send(key)+save('navigator-'+keyname)+send('\x1b')+send('\x1b')
        body += send("printf 'alpha beta\\n'\r")+send('\x14[')+send('/')+send(key)+save('copy-'+keyname)+send('\x1b')+send('q')
    steps.write_text(body+'kill\n')
    command = ['/usr/bin/swift',str(source/'stage6b_clipboard_session.swift'),str(out),str(data),sys.executable,
        str(source/'observe.py'),'--herdr',str(work/'herdr-0.2.57'),'--ikesu',str(work/'gtx-stage6b'),
        '--ptyrun',str(work/'ptyrun'),'--config-file',str(config),'--steps',str(steps),'--out',str(out/'observation'),'--silent','--server']
    result = subprocess.run(command)
    if result.returncode:
        failed = True
        break
proof = {}
for case in ('plain','controls'):
    folder = root/case
    try:
        clipboard = json.loads((folder/'proof.json').read_text())
        assert clipboard['backup_verified'] and clipboard['restored_exactly'] and clipboard['ssh_gui_roundtrip']
        values = {}
        errors = []
        for product in ('herdr','gtx'):
            for key in ('command','control'):
                target = folder/'observation'/product
                copy = (target/f'copy-{key}.txt').read_text().splitlines()[-1].strip()
                want = '貼付 alpha' if case == 'plain' else 'alphabeta'
                values[product+'/copy-'+key] = copy
                if '/'+want+'█' not in copy:
                    errors.append([product,key,'copy',copy,want])
                if case == 'plain':
                    lines = (target/f'name-{key}.txt').read_text().splitlines()
                    title = next(i for i,x in enumerate(lines) if '│new tab' in x)
                    value = lines[title+2].split('│')[2].strip()
                    nav = (target/f'navigator-{key}.txt').read_text().splitlines()[5]
                    values[product+'/name-'+key] = value
                    values[product+'/navigator-'+key] = nav.strip()
                    if value != want:
                        errors.append([product,key,'name',value,want])
                    if 'base-'+want not in nav:
                        errors.append([product,key,'navigator',nav.strip(),'base-'+want])
        proof[case] = {'status':'FAIL' if errors else 'PASS','clipboard':clipboard,'values':values,'errors':errors}
        failed = failed or bool(errors)
    except Exception as error:
        proof[case] = {'status':'FAIL','error':str(error)}
        failed = True
(root/'gate.json').write_text(json.dumps(proof,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(proof,ensure_ascii=False,indent=2))
raise SystemExit(failed)
