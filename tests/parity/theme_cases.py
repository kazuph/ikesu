#!/usr/bin/env python3
"""Apply each existing theme through Settings in isolated fresh instances."""
from pathlib import Path
import sys
out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
names = ['catppuccin', 'catppuccin-latte', 'terminal', 'tokyo-night', 'tokyo-night-day',
         'dracula', 'nord', 'gruvbox', 'gruvbox-light', 'one-dark', 'one-light', 'solarized', 'solarized-light']
for index, name in enumerate(names):
    lines=['send "\\x02s"','sleep 250ms']
    for _ in range(len(names)-1):
        lines += ['send "\\x1b[A"', 'sleep 50ms']
    for _ in range(index):
        lines += ['send "\\x1b[B"','sleep 50ms']
    lines += ['screen @OUT@/settings-selection.txt', 'cells @OUT@/settings-selection.json',
              'send "\\r"', 'sleep 250ms', 'screen @OUT@/theme-applied.txt',
              'send "\\x02g"', 'sleep 250ms', 'screen @OUT@/navigator.txt', 'cells @OUT@/navigator.json']
    for key in ('b', 'w', 'i', 'd'):
        lines += [f'send "{key}"', 'sleep 150ms', f'screen @OUT@/filter-{key}.txt',
                  f'cells @OUT@/filter-{key}.json', 'send "a"', 'sleep 150ms']
    lines += ['send "\\x1b"', 'sleep 250ms', 'send "\\x02b"', 'sleep 250ms',
              'screen @OUT@/collapsed.txt', 'cells @OUT@/collapsed.json', 'kill']
    (out/(name+'.steps')).write_text('\n'.join(lines)+'\n')
