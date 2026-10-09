#!/usr/bin/env python3
"""Independent navigator operations on two real shell panes."""
import json
from pathlib import Path
import sys
out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
cases = {
    'up': [('send', '\x1b[A')], 'down': [('send', '\x1b[B')],
    'left-pane': [('send', '\x1b[D')], 'group-left': [('send', '\x1b[A\x1b[A\x1b[D')],
    'group-enter': [('send', '\x1b[A\x1b[A\r')],
    'pane-enter': [('send', '\x1b[A\r')],
    'outside': [('click', '155 42')], 'group-click': [('click', '17 8')],
    'pane-click': [('click', '20 9')],
    'query-escape': [('send', '/pane 2'), ('send', '\x1b')],
    'empty-query-escape': [('send', '/'), ('send', '\x1b')],
    'query-clear': [('send', '/pane 2'), ('send', '\x15')],
    'query-enter': [('send', '/pane 1'), ('send', '\r')],
    'wheel': [('mouse', 'wheel-up 80 20')],
}
for state in 'bwida':
    cases['state-' + state] = [('send', state)]
for name, operations in cases.items():
    body = ['send "\\x02v"', 'sleep 250ms', 'send "\\x02g"', 'sleep 250ms',
            'screen @OUT@/before.txt', 'cells @OUT@/before.json']
    for op, value in operations:
        body.extend([op + ' ' + (json.dumps(value) if op == 'send' else value), 'sleep 250ms'])
    body.extend(['screen @OUT@/after.txt', 'cells @OUT@/after.json', 'kill'])
    (out / (name + '.steps')).write_text('\n'.join(body) + '\n')
