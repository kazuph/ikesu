#!/usr/bin/env python3
"""Assert reconnect generations restore the actual reference keyboard request."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
checks = []
for mode, want in [('off', 7), ('retain', 31)]:
    reference = None
    for product in ['herdr', 'ikesu']:
        target = root/('reconnect-'+mode)/product
        raw = (target/'terminal.raw').read_bytes()
        pushes = [int(value) for value in re.findall(rb'\x1b\[>(\d+)u', raw)]
        fixture = json.loads((target/'reconnect.json').read_text())
        pids = [(target/name).read_text() for name in ['pane-ready', 'pane-changed']]
        received = (target/'received-stable.bin').read_bytes()
        if reference is None:
            reference = (pushes, received)
        input_ok = received=='日END'.encode() if mode == 'off' else b'\x1b[' in received
        checks.append(dict(mode=mode, product=product, pushes=pushes, expected=want,
                           same_pane=pids[0]==pids[1], received_hex=received.hex(),
                           pass_gate=pushes[0]==7 and 31 in pushes and pushes[-1]==want
                           and fixture['error'] is None and fixture['history'][-1]['phase']=='display-resumed'
                           and 'RECONNECT_CHANGED' in (target/'reconnected.txt').read_text()
                           and pids[0]==pids[1] and input_ok
                           and (pushes, received)==reference
                           and 'TIMEOUT' not in (target/'run.log').read_text()))
(root/'reconnect-gate.json').write_text(json.dumps(checks, ensure_ascii=False, indent=2)+'\n')
for check in checks:
    print(check['product'], check['mode'], check['pushes'], 'PASS' if check['pass_gate'] else 'FAIL')
raise SystemExit(0 if all(check['pass_gate'] for check in checks) else 1)
