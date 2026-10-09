#!/usr/bin/env python3
"""Create identical isolated copy procedures; every run needs clipboard sandboxing."""
from pathlib import Path
import sys

root = Path(sys.argv[1])
root.mkdir(parents=True, exist_ok=True)
setup = '''send "printf '\\\\033[3J\\\\033[H\\\\033[2J'\\r"
sleep 250ms
send "for i in {1..70}; do printf 'ROW-%02d 日本語 alpha βeta\\\\n' $i; done\\r"
sleep 400ms
'''
cases = {
    'multiple-y': ('\\x02[', 'g0vjjll', 'y'),
    'multiple-enter': ('\\x02[', 'g0vjjll', '\\r'),
    'wide-y': ('\\x02[', 'g0lllllllvl', 'y'),
    'backward-y': ('\\x02[', 'gjjllllvkkhh', 'y'),
    'fullscreen-y': ('fullscreen-button', 'g0vjjll', 'y'),
    'no-anchor-enter': ('\\x02[', '', '\\r'),
}
for name, (open_key, selection, confirm) in cases.items():
    if open_key == 'fullscreen-button':
        body = setup + 'clicktext "COPY"\nsleep 200ms\nclicktext "PHONE COPY"\nsleep 200ms\n'
    else:
        body = setup + f'send "{open_key}"\nsleep 200ms\n'
    if selection:
        body += f'send "{selection}"\nsleep 200ms\n'
    body += 'screen @OUT@/selected.txt\ncells @OUT@/selected.json\n'
    body += f'send "{confirm}"\nsleep 200ms\n'
    body += 'screen @OUT@/confirmed.txt\ncells @OUT@/confirmed.json\nsleep 2s\n'
    body += 'screen @OUT@/expired.txt\ncells @OUT@/expired.json\nkill\n'
    (root / (name + '.steps')).write_text(body)
