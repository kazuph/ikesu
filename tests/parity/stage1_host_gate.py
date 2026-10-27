#!/usr/bin/env python3
"""Require the observed host keyboard request and restoration sequences."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
results = []
for host, extra in [('plain', b''), ('ghostty', b''), ('wezterm', b'\x1b[>4;1m'),
                    ('wezterm-pane', b'\x1b[>4;1m'), ('tmux', b'\x1b[>4;2m')]:
    expected = [b'\x1b[>7u'] + ([extra, b'\x1b[>4;0m'] if extra else []) + [b'\x1b[<1u']
    for mode in ['local', 'server']:
        measured = {}
        for name in ['herdr', 'ikesu']:
            raw = (root/host/mode/name/'terminal.raw').read_bytes()
            measured[name] = re.findall(rb'\x1b\[[><][0-9;]*[um]', raw)
        passed = all(value == expected for value in measured.values())
        results.append({'host': host, 'mode': mode, 'pass': passed,
                        'expected': [value.hex() for value in expected],
                        'measured': {name: [value.hex() for value in values] for name, values in measured.items()}})
(root/'host-gate.json').write_text(json.dumps(results, indent=2)+'\n')
for row in results:
    print(row['host'], row['mode'], 'PASS' if row['pass'] else 'FAIL')
raise SystemExit(0 if all(row['pass'] for row in results) else 1)
