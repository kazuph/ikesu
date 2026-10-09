#!/usr/bin/env python3
"""Generate independent prefix probes so one modal cannot contaminate another."""
import json
from pathlib import Path
import sys
out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
keys = {'help': '?', 'settings': 's', 'detach': 'q', 'legacy-detach': 'd',
        'reload': 'R', 'vertical': 'v', 'percent': '%', 'horizontal': '-',
        'double-quote': '"', 'close-pane': 'x', 'rename-pane': 'P', 'rename-workspace': 'W',
        'rename-tab': 'T', 'legacy-rename-tab': ',', 'copy-left': '[', 'copy-right': ']',
        'next': 'n', 'previous': 'p', 'workspace-nav': 'w', 'session-nav': 'g',
        'new-worktree': 'G', 'new-workspace': 'N', 'new-tab': 'c', 'close-workspace': 'D',
        'close-tab': 'X', 'legacy-close-tab': '&', 'zoom': 'z', 'resize': 'r',
        'sidebar': 'b', 'left': 'h', 'down': 'j', 'up': 'k', 'right': 'l',
        'tab-next': '\t', 'tab-previous': '\x1b[Z', 'unknown': 'a', 'cancel': '\x1b',
        'send-prefix': '\x02'}
for digit in '123456789':
    keys['number-'+digit] = digit
for name, key in keys.items():
    # Two tabs/workspaces/panes would obscure creation behavior; each starts from a lone shell.
    body = ['send "\\x02"', 'sleep 200ms', 'screen @OUT@/pending.txt',
            'send '+json.dumps(key), 'sleep 300ms', 'screen @OUT@/result.txt', 'kill']
    # strconv.Unquote supports Go escapes; JSON's \u0002 is accepted by Go as well.
    (out / (name+'.steps')).write_text('\n'.join(body)+'\n')
