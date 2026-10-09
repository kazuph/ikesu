#!/usr/bin/env python3
"""Generate each menu kind independently with safe shared choices."""
from pathlib import Path
import sys
out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
menus = {
    'pane': ('', 'rclick 70 12', 'Rename pane'),
    'workspace': ('', 'rclick 10 5', 'Rename'),
    'tab': ('send "\\x02c"\nsleep 250ms\nsend "\\r"\nsleep 250ms\n', 'rclick 28 1', 'Rename'),
    'sidebar-blank': ('', 'rclick 10 12', None),
    'main': ('', 'clicktext "[menu]"', 'keybinds'),
}
for kind, (setup, opening, choice) in menus.items():
    for operation in ['escape', 'outside-left', 'outside-right', 'keyboard', 'wheel', 'resize', 'select']:
        if operation == 'select' and choice is None:
            continue
        lines = ['send "printf \'\\\\033[3J\\\\033[H\\\\033[2J\'\\r"', 'sleep 250ms',
                 setup + opening, 'sleep 250ms', 'screen @OUT@/open.txt', 'cells @OUT@/open.json']
        if operation == 'escape':
            lines += ['send "\\x1b"']
        elif operation == 'outside-left':
            lines += ['click 155 42']
        elif operation == 'outside-right':
            lines += ['rclick 155 42']
        elif operation == 'keyboard':
            lines += ['send "\\x1b[A"', 'sleep 250ms', 'screen @OUT@/up.txt', 'cells @OUT@/up.json',
                      'send "\\x1b[B"', 'sleep 250ms', 'screen @OUT@/down.txt', 'cells @OUT@/down.json', 'send "z"']
        elif operation == 'wheel':
            lines += ['mouse wheel-down 80 20', 'sleep 250ms', 'screen @OUT@/wheel-down.txt', 'cells @OUT@/wheel-down.json', 'mouse wheel-up 80 20']
        elif operation == 'resize':
            lines += ['resize 100 25']
        else:
            lines += ['clicktext "' + choice + '"']
        lines += ['sleep 250ms', 'screen @OUT@/after.txt', 'cells @OUT@/after.json', 'kill']
        (out / (kind + '-' + operation + '.steps')).write_text('\n'.join(lines) + '\n')
