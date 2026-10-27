#!/usr/bin/env python3
"""Compare pane bytes before shutdown, host stack requests, and workspace moves."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
results = []
for mode in ['local', 'server']:
    for case in sorted((root/mode).glob('protocol-*')):
        measured = {}
        for name in ['herdr', 'ikesu']:
            folder = case/name
            cutoff = json.loads((folder/'complete.json').read_text())['CapturedUnixMs']*1000000
            records = map(json.loads, (folder/'received.jsonl').read_text().splitlines())
            measured[name] = ''.join(row['hex'] for row in records if row['unix_ns'] <= cutoff)
        results.append({'mode': mode, 'case': case.name, 'pass': bool(measured['herdr']) and measured['herdr'] == measured['ikesu'], 'measured': measured})
    expected = [b'\x1b[>7u']
    if mode == 'server':
        expected += [b'\x1b[<1u', b'\x1b[>31u', b'\x1b[<1u', b'\x1b[>7u']*2
    expected += [b'\x1b[<1u']
    measured = {}
    for name in ['herdr', 'ikesu']:
        raw = (root/mode/'lifecycle'/name/'terminal.raw').read_bytes()
        measured[name] = re.findall(rb'\x1b\[[><][0-9;]*u', raw)
    results.append({'mode': mode, 'case': 'lifecycle', 'pass': all(value == expected for value in measured.values()),
                    'measured': {name: [s.hex() for s in values] for name, values in measured.items()}})
    for name in ['herdr', 'ikesu']:
        folder = root/mode/'workspace-cmd'/name
        active = {screen: [line.strip() for line in (folder/(screen+'.txt')).read_text().splitlines() if line.startswith('▌')]
                  for screen in ['first', 'second', 'previous', 'next', 'one', 'two']}
        passed = bool(active['first']) and active['first'] != active['second'] and active['previous'] == active['one'] == active['first'] and active['next'] == active['two'] == active['second']
        results.append({'mode': mode, 'case': 'workspace-cmd', 'product': name, 'pass': passed, 'active': active})
        vim = root/mode/'popup-vim'/name
        if vim.exists():
            saved = (vim/'vim-buffer.txt').read_bytes()
            corners = (vim/'vim-normal.txt').read_text().count('┌')
            results.append({'mode': mode, 'case': 'popup-vim', 'product': name, 'pass': saved == b'STAGE1_VIM_TEXT\n' and corners == 2, 'saved_hex': saved.hex(), 'corners': corners})
(root/'keyboard-gate.json').write_text(json.dumps(results, indent=2)+'\n')
for row in results:
    print(row['mode'], row['case'], row.get('product',''), 'PASS' if row['pass'] else 'FAIL')
raise SystemExit(0 if all(row['pass'] for row in results) else 1)
